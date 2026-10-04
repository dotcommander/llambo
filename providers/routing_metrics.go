package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/dotcommander/llambo/internal/filetxn"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"
)

const dailyWindowFormat = "2006-01-02"

// ProviderRuntimeMetrics stores rolling runtime metrics per provider.
type ProviderRuntimeMetrics struct {
	Requests         int64     `json:"requests"`
	Successes        int64     `json:"successes"`
	Failures         int64     `json:"failures"`
	Timeouts         int64     `json:"timeouts"`
	TotalLatencyMs   int64     `json:"total_latency_ms"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	TotalCostUSD     float64   `json:"total_cost_usd"`
	QualityScore     float64   `json:"quality_score"`
	QualityCount     int64     `json:"quality_count"`
	DailyWindow      string    `json:"daily_window,omitempty"`
	DailyRequests    int64     `json:"daily_requests"`
	DailyTokens      int64     `json:"daily_tokens"`
	DailyCostUSD     float64   `json:"daily_cost_usd"`
	LastUpdated      time.Time `json:"last_updated"`
}

// RoutingMetricsStore persists provider metrics to disk.
type RoutingMetricsStore struct {
	mu        sync.RWMutex
	saveMu    sync.Mutex
	path      string
	lastSaved time.Time
	interval  time.Duration
	logger    *slog.Logger
	rename    func(string, string) error
	Providers map[string]*ProviderRuntimeMetrics `json:"providers"`
}

func NewRoutingMetricsStore(path string) (*RoutingMetricsStore, error) {
	return newRoutingMetricsStore(path, slog.Default(), os.Rename)
}

func newRoutingMetricsStore(path string, logger *slog.Logger, rename func(string, string) error) (*RoutingMetricsStore, error) {
	store := &RoutingMetricsStore{
		path:      path,
		interval:  time.Second,
		logger:    logger,
		rename:    rename,
		Providers: make(map[string]*ProviderRuntimeMetrics),
	}

	if err := store.load(); err != nil {
		return nil, err
	}

	return store, nil
}

func (s *RoutingMetricsStore) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}

	var decoded struct {
		Providers map[string]*ProviderRuntimeMetrics `json:"providers"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		if quarantineErr := s.quarantineCorruptFile(); quarantineErr != nil {
			s.logger.Warn("quarantine corrupt routing metrics file failed", "path", s.path, "error", quarantineErr)
		}
		return nil
	}
	if decoded.Providers != nil {
		s.Providers = decoded.Providers
	}
	return nil
}

func (s *RoutingMetricsStore) quarantineCorruptFile() error {
	if s.path == "" {
		return nil
	}
	corruptPath := fmt.Sprintf("%s.corrupt-%s", s.path, time.Now().UTC().Format("20060102T150405Z"))
	return s.rename(s.path, corruptPath)
}

func (s *RoutingMetricsStore) Save() error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	if s.path == "" {
		return nil
	}

	s.mu.RLock()
	snapshot := make(map[string]*ProviderRuntimeMetrics, len(s.Providers))
	for k, v := range s.Providers {
		if v == nil {
			continue
		}
		copyVal := *v
		snapshot[k] = &copyVal
	}
	s.mu.RUnlock()

	return saveRoutingMetricsSnapshot(s.path, snapshot)
}

func saveRoutingMetricsSnapshot(path string, providers map[string]*ProviderRuntimeMetrics) error {
	if path == "" {
		return nil
	}

	data, err := json.MarshalIndent(struct {
		Providers map[string]*ProviderRuntimeMetrics `json:"providers"`
	}{Providers: providers}, "", "  ")
	if err != nil {
		return err
	}

	return filetxn.WriteAtomic(path, data, 0o600)
}

func (s *RoutingMetricsStore) Record(provider string, latency time.Duration, usage *LLMUsage, err error) {
	if provider == "" {
		return
	}
	now := time.Now()
	nowUTC := now.UTC()
	today := nowUTC.Format(dailyWindowFormat)

	s.mu.Lock()
	m, ok := s.Providers[provider]
	if !ok {
		m = &ProviderRuntimeMetrics{}
		s.Providers[provider] = m
	}
	m.Requests++
	m.TotalLatencyMs += latency.Milliseconds()
	resetDailyWindowIfNeeded(m, today)
	m.DailyRequests++
	if err != nil {
		m.Failures++
		errLower := strings.ToLower(err.Error())
		if strings.Contains(errLower, "timeout") || strings.Contains(errLower, "deadline") {
			m.Timeouts++
		}
	} else {
		m.Successes++
	}

	if usage != nil {
		m.PromptTokens += int64(usage.PromptTokens)
		m.CompletionTokens += int64(usage.CompletionTokens)
		m.TotalTokens += int64(usage.TotalTokens)
		m.DailyTokens += int64(usage.TotalTokens)
		if usage.Cost != nil {
			m.TotalCostUSD += usage.Cost.TotalCost
			m.DailyCostUSD += usage.Cost.TotalCost
		}
	}
	m.LastUpdated = now
	shouldSave := s.path != "" && (s.lastSaved.IsZero() || now.Sub(s.lastSaved) >= s.interval)
	if shouldSave {
		s.lastSaved = now
	}
	s.mu.Unlock()

	if shouldSave {
		if saveErr := s.Save(); saveErr != nil {
			s.logger.Warn("save routing metrics failed", "path", s.path, "error", saveErr)
		}
	}
}

// RecordQuality updates the rolling quality score for a provider.
// Score should be in [0, 1]. Uses cumulative moving average.
func (s *RoutingMetricsStore) RecordQuality(provider string, score float64) {
	if provider == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.Providers[provider]
	if !ok {
		return // only record quality for providers already tracked by Record()
	}
	m.QualityCount++
	m.QualityScore += (score - m.QualityScore) / float64(m.QualityCount)
}

func resetDailyWindowIfNeeded(m *ProviderRuntimeMetrics, today string) {
	if m.DailyWindow == today {
		return
	}
	m.DailyWindow = today
	m.DailyRequests = 0
	m.DailyTokens = 0
	m.DailyCostUSD = 0
}

// ComputeResponseQuality computes a quality heuristic in [0, 1] from response characteristics.
// Only called on successful responses; success component is baked into the weights.
func ComputeResponseQuality(promptLen, responseLen int, latencyMs int64) float64 {
	contentScore := 0.0
	if promptLen > 0 && responseLen > 0 {
		ratio := float64(responseLen) / float64(promptLen)
		contentScore = 1.0 - 1.0/(1.0+ratio)
	} else if responseLen > 0 {
		contentScore = 0.5
	}

	latencyScore := 1.0 / (1.0 + float64(latencyMs)/1000.0)

	lengthPenalty := 1.0
	if responseLen == 0 {
		lengthPenalty = 0.0
	} else if responseLen < 10 {
		lengthPenalty = 0.3
	}

	// Weights: content 0.40, success 0.30 (constant for successful requests),
	// latency 0.15, length 0.15
	score := 0.40*contentScore + 0.30 + 0.15*latencyScore + 0.15*lengthPenalty
	if score > 1 {
		return 1
	}
	return score
}

func (s *RoutingMetricsStore) Snapshot() map[string]ProviderRuntimeMetrics {
	today := time.Now().UTC().Format(dailyWindowFormat)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]ProviderRuntimeMetrics, len(s.Providers))
	for k, v := range s.Providers {
		if v == nil {
			continue
		}
		m := *v
		if m.DailyWindow != today {
			m.DailyRequests = 0
			m.DailyTokens = 0
			m.DailyCostUSD = 0
		}
		out[k] = m
	}
	return out
}
