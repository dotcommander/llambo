package cmd

import (
	"fmt"
	"sort"
	"strings"
)

func sampleReason(reasons []string) string {
	if len(reasons) == 0 {
		return "-"
	}
	return reasons[0]
}

func markdownTableCell(value string) string {
	return strings.ReplaceAll(value, "|", "\\|")
}

func splitCSV(v string) []string {
	return normalizeCSVTokens(v)
}

func pct(numerator, denominator int) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func avgInt64(total int64, count int) int64 {
	if count == 0 {
		return 0
	}
	return total / int64(count)
}

func avgFloat(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func formatProviderCounts(counts map[string]int, total int) string {
	if len(counts) == 0 {
		return "-"
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s=%d(%.1f%%)", n, counts[n], pct(counts[n], total)))
	}
	return strings.Join(parts, ", ")
}
