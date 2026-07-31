package cmd

import (
	"fmt"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
)

// isNewModel returns true if the model was first seen within 5 minutes of the last refresh.
func isNewModel(m *catalog.ModelEntry, lastRefresh time.Time) bool {
	if lastRefresh.IsZero() {
		return false
	}
	return m.FirstSeen.After(lastRefresh.Add(-5 * time.Minute))
}

// humanAge renders a past time as "3d ago", "2h ago", etc.
func humanAge(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return humanDuration(time.Since(t)) + " ago"
}

func healthLabel(m *catalog.ModelEntry) string {
	if m == nil || m.LastPing.CheckedAt.IsZero() {
		return "unknown"
	}
	if m.LastPing.Success {
		return fmt.Sprintf("ok %dms", m.LastPing.LatencyMS)
	}
	if m.LastPing.ErrorCategory != "" {
		return "fail " + m.LastPing.ErrorCategory
	}
	return "fail"
}

func contextLabel(m *catalog.ModelEntry) string {
	if m == nil {
		return "—"
	}
	if m.Metadata.ContextLength > 0 {
		return compactCount(m.Metadata.ContextLength)
	}
	if m.Metadata.TopProvider.ContextLength > 0 {
		return compactCount(m.Metadata.TopProvider.ContextLength)
	}
	return "—"
}

func paramsLabel(m *catalog.ModelEntry) string {
	if m == nil || len(m.Metadata.SupportedParameters) == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", len(m.Metadata.SupportedParameters))
}

func reasoningLabel(m *catalog.ModelEntry) string {
	if m == nil || len(m.Metadata.Reasoning) == 0 {
		return "—"
	}
	if mandatory, ok := m.Metadata.Reasoning["mandatory"].(bool); ok && mandatory {
		return "mandatory"
	}
	if enabled, ok := m.Metadata.Reasoning["default_enabled"].(bool); ok && enabled {
		return "default"
	}
	return "yes"
}

func qualityLabel(m *catalog.ModelEntry) string {
	task, evidence, ok := catalog.BestQualityEvidence(m)
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%s %.3f", task, evidence.Score)
}

func compactCount(n int) string {
	if n >= 1000 && n%1000 == 0 {
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

func sortedCatalogProviders(cat *catalog.Catalog) []string {
	names := make([]string, 0, len(cat.Providers))
	for name := range cat.Providers {
		names = append(names, name)
	}
	sortStrings(names)
	return names
}

func sortedModelIDs(pc *catalog.ProviderCatalog) []string {
	ids := make([]string, 0, len(pc.Models))
	for id := range pc.Models {
		ids = append(ids, id)
	}
	sortStrings(ids)
	return ids
}

func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
}
