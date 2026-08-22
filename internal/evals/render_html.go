package evals

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

func RenderHTML(report Report, limit int) string {
	canonical, projected := partitionProjectionRows(report.Models)
	if limit <= 0 || limit > len(canonical) {
		limit = len(canonical)
	}
	rows := canonical[:limit]
	var b strings.Builder
	b.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><title>Llambo Score Matrix</title><style>body{font:15px system-ui;margin:2rem}table{border-collapse:collapse}th,td{border:1px solid #ccc;padding:.4rem;text-align:left}th{background:#eee}</style></head><body>")
	fmt.Fprintf(&b, "<h1>Llambo Category Scores</h1><p>Formula <code>%s</code>; ranked by <code>%s</code>. Six independent category scores use frozen cached reference populations. Speed and price are operational-only.</p>", template.HTMLEscapeString(report.FormulaVersion), template.HTMLEscapeString(report.RankingProfile))
	for _, source := range report.Sources {
		if source.Cache == "stale" || (!source.FetchedAt.IsZero() && report.GeneratedAt.Sub(source.FetchedAt) > DefaultTTL) {
			fmt.Fprintf(&b, "<p><strong>Stale evidence:</strong> %s (%s) remains scoreable but lowers confidence.</p>", template.HTMLEscapeString(source.Name), template.HTMLEscapeString(source.FetchedAt.UTC().Format(time.RFC3339)))
		}
	}
	b.WriteString("<table data-sortable=\"true\"><thead><tr><th>Model</th><th>Agents</th><th>Coding</th><th>Instruction Following</th><th>Long Context</th><th>Reasoning</th><th>Writing</th><th>Speed</th><th>Price</th></tr></thead><tbody>")
	for _, row := range rows {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(row.Name), formatLlambo(row.LlamboScores["agents"]), formatLlambo(row.LlamboScores["coding"]), formatLlambo(row.LlamboScores["instruction-following"]), formatLlambo(row.LlamboScores["long-context"]), formatLlambo(row.LlamboScores["reasoning"]), formatLlambo(row.LlamboScores["writing"]), formatOperational(row.Scores["speed"]), fmtOutputPrice(row))
	}
	b.WriteString("</tbody></table>")
	if len(projected) > 0 {
		b.WriteString("<h2>Local projections</h2><p>Projected artifacts inherit reviewed upstream category evidence with explicit confidence calibration.</p><table><thead><tr><th>Model</th><th>Agents</th><th>Coding</th><th>Instruction Following</th><th>Long Context</th><th>Reasoning</th><th>Writing</th><th>Speed</th><th>Price</th></tr></thead><tbody>")
		for _, row := range projected {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(row.Name), formatLlambo(row.LlamboScores["agents"]), formatLlambo(row.LlamboScores["coding"]), formatLlambo(row.LlamboScores["instruction-following"]), formatLlambo(row.LlamboScores["long-context"]), formatLlambo(row.LlamboScores["reasoning"]), formatLlambo(row.LlamboScores["writing"]), formatOperational(row.Scores["speed"]), fmtOutputPrice(row))
		}
		b.WriteString("</tbody></table>")
	}
	writeCoverageCampaignHTML(&b, report.CoverageCampaign)
	b.WriteString("<h2>Evidence</h2><ul>")
	for _, row := range append(append([]ReportModel(nil), rows...), projected...) {
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			score := row.LlamboScores[category]
			if score == nil {
				continue
			}
			fmt.Fprintf(&b, "<li>%s / %s: score %.1f; coverage %.1f%%; trusted coverage %.1f%%; confidence %s; winner %s; stale %t%s<ul>", template.HTMLEscapeString(row.Name), template.HTMLEscapeString(category), score.Score, 100*score.Coverage, 100*score.TrustedCoverage, template.HTMLEscapeString(score.Confidence), template.HTMLEscapeString(score.WinnerStatus), score.Stale, projectionSuffix(row))
			for _, contribution := range scoreContributions(score) {
				fmt.Fprintf(&b, "<li>%s / %s: raw %s; percentile %s; frozen population %d; nominal/effective weight %.3f/%.3f; grade %s%s</li>", template.HTMLEscapeString(contribution.Family), template.HTMLEscapeString(contribution.Benchmark), formatRaw(contribution.RawScore), formatRaw(contribution.Percentile), contribution.ReferencePopulation, contribution.NominalWeight, contribution.EffectiveWeight, template.HTMLEscapeString(contribution.EvidenceGrade), template.HTMLEscapeString(formatEvidenceProvenance(contribution)))
			}
			b.WriteString("</ul></li>")
		}
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			reason := row.UnresolvedReasons[category]
			if row.LlamboScores[category] == nil {
				fmt.Fprintf(&b, "<li>%s / %s: unresolved — %s%s</li>", template.HTMLEscapeString(row.Name), template.HTMLEscapeString(category), template.HTMLEscapeString(reason), projectionSuffix(row))
			}
		}
	}
	if report.Validation != nil {
		b.WriteString("</ul><h2>Local validation diagnostic</h2><p>Read-only sealed exact-ID receipt comparison; it does not affect Llambo Scores, ordering, or filters.</p><table><thead><tr><th>Category</th><th>Exact pairs</th><th>Spearman</th><th>Status</th></tr></thead><tbody>")
		for _, category := range report.Validation.Categories {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(category.Category), category.SampleSize, formatCorrelation(category.Spearman), template.HTMLEscapeString(category.Status))
		}
		b.WriteString("</tbody></table><ul>")
	}
	b.WriteString("</ul></body></html>")
	return b.String()
}

func writeCoverageCampaignHTML(b *strings.Builder, campaign *CoverageCampaign) {
	if campaign == nil {
		return
	}
	fmt.Fprintf(b, "<h2>External score coverage</h2><p>Campaign <code>%s</code>: <strong>%s</strong> — %d/%d cells present; %d unresolved.</p>", template.HTMLEscapeString(campaign.TargetVersion), template.HTMLEscapeString(campaign.Status), campaign.PresentCells, campaign.TotalCells, len(campaign.Unresolved))
	if len(campaign.Unresolved) > 0 {
		b.WriteString("<table><thead><tr><th>Model</th><th>Category</th><th>Status</th><th>Reason</th><th>Last checked</th></tr></thead><tbody>")
		for _, cell := range campaign.Unresolved {
			fmt.Fprintf(b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(cell.ModelKey), template.HTMLEscapeString(cell.Category), template.HTMLEscapeString(cell.Status), template.HTMLEscapeString(cell.Reason), template.HTMLEscapeString(cell.LastCheckedRevision))
		}
		b.WriteString("</tbody></table>")
	}
	if len(campaign.InactiveOrNew) > 0 {
		b.WriteString("<h3>Inactive/new models</h3><table><thead><tr><th>Model</th><th>Reason</th></tr></thead><tbody>")
		for _, note := range campaign.InactiveOrNew {
			fmt.Fprintf(b, "<tr><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(note.Key), template.HTMLEscapeString(note.Reason))
		}
		b.WriteString("</tbody></table>")
	}
}
