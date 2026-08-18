package catalog

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

type QualityImportRecord struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Task     string  `json:"task"`
	Score    float64 `json:"score"`
	Source   string  `json:"source,omitempty"`
	Notes    string  `json:"notes,omitempty"`
}

func RecordQualityEvidence(cat *Catalog, record QualityImportRecord, now time.Time) error {
	if cat == nil {
		return fmt.Errorf("catalog is nil")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	provider := strings.TrimSpace(record.Provider)
	model := strings.TrimSpace(record.Model)
	task := normalizeTag(record.Task)
	if provider == "" {
		return fmt.Errorf("quality record missing provider")
	}
	if model == "" {
		return fmt.Errorf("quality record missing model")
	}
	if task == "" {
		return fmt.Errorf("quality record missing task")
	}
	if math.IsNaN(record.Score) || record.Score < 0 || record.Score > 1 {
		return fmt.Errorf("quality score for %s/%s %s must be in [0,1]", provider, model, task)
	}

	if cat.Providers == nil {
		cat.Providers = make(map[string]*ProviderCatalog)
	}
	pc := cat.Providers[provider]
	if pc == nil {
		pc = &ProviderCatalog{Models: make(map[string]*ModelEntry)}
		cat.Providers[provider] = pc
	}
	if pc.Models == nil {
		pc.Models = make(map[string]*ModelEntry)
	}
	entry := pc.Models[model]
	if entry == nil {
		entry = &ModelEntry{FirstSeen: now, LastSeen: now}
		pc.Models[model] = entry
	}
	if entry.Quality == nil {
		entry.Quality = make(map[string]QualityEvidence)
	}
	entry.Quality[task] = QualityEvidence{
		Score:     record.Score,
		Source:    strings.TrimSpace(record.Source),
		Notes:     strings.TrimSpace(record.Notes),
		UpdatedAt: now,
	}
	AddTag(entry, task)
	return nil
}

func BestQualityEvidence(entry *ModelEntry) (string, QualityEvidence, bool) {
	if entry == nil || len(entry.Quality) == 0 {
		return "", QualityEvidence{}, false
	}
	tasks := make([]string, 0, len(entry.Quality))
	for task := range entry.Quality {
		tasks = append(tasks, task)
	}
	sort.Strings(tasks)
	bestTask := ""
	var best QualityEvidence
	for _, task := range tasks {
		evidence := entry.Quality[task]
		if bestTask == "" || evidence.Score > best.Score {
			bestTask = task
			best = evidence
		}
	}
	return bestTask, best, true
}

func BestBenchmarkEvidence(entry *ModelEntry) (string, BenchmarkEvidence, bool) {
	if entry == nil || len(entry.Benchmarks) == 0 {
		return "", BenchmarkEvidence{}, false
	}
	keys := make([]string, 0, len(entry.Benchmarks))
	for key := range entry.Benchmarks {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	bestKey := ""
	var best BenchmarkEvidence
	for _, key := range keys {
		evidence := entry.Benchmarks[key]
		if bestKey == "" || evidence.UpdatedAt.After(best.UpdatedAt) || (evidence.UpdatedAt.Equal(best.UpdatedAt) && key < bestKey) {
			bestKey = key
			best = evidence
		}
	}
	return bestKey, best, true
}
