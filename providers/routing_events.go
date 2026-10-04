package providers

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// RouteEvent captures one routing decision and outcome.
type RouteEvent struct {
	Timestamp           time.Time        `json:"timestamp"`
	Mode                string           `json:"mode"`
	Intent              RoutingIntent    `json:"intent"`
	EstimatedTokens     int              `json:"estimated_tokens"`
	PlannedProvider     string           `json:"planned_provider,omitempty"`
	ChosenProvider      string           `json:"chosen_provider,omitempty"`
	Model               string           `json:"model,omitempty"`
	LatencyMs           int64            `json:"latency_ms,omitempty"`
	CostUSD             float64          `json:"cost_usd,omitempty"`
	Success             bool             `json:"success"`
	Error               string           `json:"error,omitempty"`
	Candidates          []CandidateScore `json:"candidates,omitempty"`
	IsCanary            bool             `json:"is_canary,omitempty"`
	PromotionIneligible bool             `json:"promotion_ineligible,omitempty"`
}

// RouteEventLogger appends route events to JSONL.
type RouteEventLogger struct {
	mu     sync.Mutex
	path   string
	file   *os.File
	logger *slog.Logger
}

func NewRouteEventLogger(path string) *RouteEventLogger {
	if path == "" {
		return nil
	}
	return &RouteEventLogger{path: path, logger: slog.Default()}
}

func (l *RouteEventLogger) Log(event RouteEvent) error {
	if l == nil || l.path == "" {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	if l.file == nil {
		if err := os.MkdirAll(filepath.Dir(l.path), 0755); err != nil {
			return err
		}
		f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		l.file = f
	}

	b, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := l.file.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

func (l *RouteEventLogger) Close() {
	if err := l.close(); err != nil {
		logger := l.logger
		if logger == nil {
			logger = slog.Default()
		}
		logger.Warn("close route event log failed", "path", l.path, "error", err)
	}
}

func (l *RouteEventLogger) close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		err := l.file.Close()
		l.file = nil
		return err
	}
	return nil
}

func ReadRouteEvents(path string) ([]RouteEvent, error) {
	if path == "" {
		return nil, fmt.Errorf("route events path is empty")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []RouteEvent
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for s.Scan() {
		lineNo++
		line := strings.TrimSpace(s.Text())
		if line == "" {
			continue
		}
		var ev RouteEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("decode route event line %d: %w", lineNo, err)
		}
		events = append(events, ev)
	}
	if err := s.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return events, nil
}
