package evals

import (
	"fmt"
	"html/template"
	"strings"
	"time"
)

// RenderHTML formats the score matrix and evidence with escaped model identities.
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
		if source.Cache == stale1 || (!source.FetchedAt.IsZero() && report.GeneratedAt.Sub(source.FetchedAt) > DefaultTTL) {
			fmt.Fprintf(&b, "<p><strong>Stale evidence:</strong> %s (%s) remains scoreable but lowers confidence.</p>", template.HTMLEscapeString(source.Name), template.HTMLEscapeString(source.FetchedAt.UTC().Format(time.RFC3339)))
		}
	}
	b.WriteString("<table data-sortable=\"true\"><thead><tr><th>Model</th><th>Agents</th><th>Coding</th><th>Instruction Following</th><th>Long Context</th><th>Reasoning</th><th>Writing</th><th>Speed</th><th>Price</th></tr></thead><tbody>")
	for _, row := range rows {
		fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(row.Name), formatLlambo(row.LlamboScores[agents1]), formatLlambo(row.LlamboScores[coding1]), formatLlambo(row.LlamboScores[instructionFollowing]), formatLlambo(row.LlamboScores[longContext]), formatLlambo(row.LlamboScores[reasoning1]), formatLlambo(row.LlamboScores[writing1]), formatOperational(row.Scores[speed1]), fmtOutputPrice(row))
	}
	b.WriteString("</tbody></table>")
	if len(projected) > 0 {
		b.WriteString("<h2>Local projections</h2><p>Projected artifacts inherit reviewed upstream category evidence with explicit confidence calibration.</p><table><thead><tr><th>Model</th><th>Agents</th><th>Coding</th><th>Instruction Following</th><th>Long Context</th><th>Reasoning</th><th>Writing</th><th>Speed</th><th>Price</th></tr></thead><tbody>")
		for _, row := range projected {
			fmt.Fprintf(&b, "<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>", template.HTMLEscapeString(row.Name), formatLlambo(row.LlamboScores[agents1]), formatLlambo(row.LlamboScores[coding1]), formatLlambo(row.LlamboScores[instructionFollowing]), formatLlambo(row.LlamboScores[longContext]), formatLlambo(row.LlamboScores[reasoning1]), formatLlambo(row.LlamboScores[writing1]), formatOperational(row.Scores[speed1]), fmtOutputPrice(row))
		}
		b.WriteString("</tbody></table>")
	}
	writeCoverageCampaignHTML(&b, report.CoverageCampaign)
	b.WriteString("<h2>Evidence</h2><ul>")
	writeReportEvidenceHTML(&b, append(append([]ReportModel(nil), rows...), projected...))
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
