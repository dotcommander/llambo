package evals

import (
	"fmt"
	"html/template"
	"strings"
)

func writeReportEvidenceHTML(b *strings.Builder, rows []ReportModel) {
	for _, row := range rows {
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			score := row.LlamboScores[category]
			if score == nil {
				continue
			}
			fmt.Fprintf(b, "<li>%s / %s: score %.1f; coverage %.1f%%; trusted coverage %.1f%%; confidence %s; winner %s; stale %t%s<ul>", template.HTMLEscapeString(row.Name), template.HTMLEscapeString(category), score.Score, 100*score.Coverage, 100*score.TrustedCoverage, template.HTMLEscapeString(score.Confidence), template.HTMLEscapeString(score.WinnerStatus), score.Stale, projectionSuffix(row))
			for _, contribution := range scoreContributions(score) {
				fmt.Fprintf(b, "<li>%s / %s: raw %s; percentile %s; frozen population %d; nominal/effective weight %.3f/%.3f; grade %s%s</li>", template.HTMLEscapeString(contribution.Family), template.HTMLEscapeString(contribution.Benchmark), formatRaw(contribution.RawScore), formatRaw(contribution.Percentile), contribution.ReferencePopulation, contribution.NominalWeight, contribution.EffectiveWeight, template.HTMLEscapeString(contribution.EvidenceGrade), template.HTMLEscapeString(formatEvidenceProvenance(contribution)))
			}
			b.WriteString("</ul></li>")
		}
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			reason := row.UnresolvedReasons[category]
			if row.LlamboScores[category] == nil {
				fmt.Fprintf(b, "<li>%s / %s: unresolved — %s%s</li>", template.HTMLEscapeString(row.Name), template.HTMLEscapeString(category), template.HTMLEscapeString(reason), projectionSuffix(row))
			}
		}
	}
}
