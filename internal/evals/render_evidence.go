package evals

import (
	"fmt"
	"strings"
)

func writeReportEvidenceMarkdown(b *strings.Builder, rows []ReportModel) {
	for _, row := range rows {
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			score := row.LlamboScores[category]
			if score == nil {
				continue
			}
			fmt.Fprintf(b, "- %s / %s: score %.1f; coverage %.1f%%; trusted coverage %.1f%%; confidence %s; winner %s; stale %t%s\n", row.Name, category, score.Score, 100*score.Coverage, 100*score.TrustedCoverage, score.Confidence, score.WinnerStatus, score.Stale, projectionSuffix(row))
			for _, contribution := range scoreContributions(score) {
				fmt.Fprintf(b, "  - %s / %s: raw %s, percentile %s, frozen population %d, nominal/effective weight %.3f/%.3f, grade %s%s\n", contribution.Family, contribution.Benchmark, formatRaw(contribution.RawScore), formatRaw(contribution.Percentile), contribution.ReferencePopulation, contribution.NominalWeight, contribution.EffectiveWeight, contribution.EvidenceGrade, formatEvidenceProvenance(contribution))
			}
		}
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			reason := row.UnresolvedReasons[category]
			if row.LlamboScores[category] == nil {
				fmt.Fprintf(b, "- %s / %s: unresolved — %s%s\n", row.Name, category, reason, projectionSuffix(row))
			}
		}
	}
}
