package evals

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func RenderMarkdown(report Report, limit int) string {
	canonical, projected := partitionProjectionRows(report.Models)
	if limit <= 0 || limit > len(canonical) {
		limit = len(canonical)
	}
	rows := canonical[:limit]
	var b strings.Builder
	b.WriteString("# Llambo Score Matrix\n\n")
	fmt.Fprintf(&b, "Generated: %s  \nFormula: `%s`  \nRanked by: `%s`\n\n", report.GeneratedAt.Format(time.RFC3339), report.FormulaVersion, report.RankingProfile)
	writeSourceFreshnessMarkdown(&b, report.Sources, report.GeneratedAt)
	if report.Eligibility != nil {
		filters := make([]string, 0, 2)
		if report.Eligibility.MinScore >= 0 {
			filters = append(filters, fmt.Sprintf("%s >= %.1f", report.Eligibility.ScoreCategory, report.Eligibility.MinScore))
		}
		if report.Eligibility.MaxOutputPrice >= 0 {
			filters = append(filters, fmt.Sprintf("output price <= $%.3g/1M", report.Eligibility.MaxOutputPrice))
		}
		fmt.Fprintf(&b, "Eligibility filters: %s; %d of %d models included.\n\n", strings.Join(filters, "; "), report.Eligibility.IncludedModels, report.Eligibility.InputModels)
	}
	if report.OMLX != nil {
		if report.OMLX.Status == "unavailable" {
			b.WriteString("OMLX live selection: unavailable; projected availability omitted.\n\n")
		} else {
			fmt.Fprintf(&b, "OMLX live selection: %s.\n\n", report.OMLX.Status)
		}
	}
	b.WriteString("Six independent capability scores use reviewed cached remote benchmarks only. Operational speed and price are displayed separately and never affect capability rankings.\n\n")
	b.WriteString("| Model | Agents | Coding | Instruction Following | Long Context | Reasoning | Writing | Speed | Price |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", row.Name, formatLlambo(row.LlamboScores["agents"]), formatLlambo(row.LlamboScores["coding"]), formatLlambo(row.LlamboScores["instruction-following"]), formatLlambo(row.LlamboScores["long-context"]), formatLlambo(row.LlamboScores["reasoning"]), formatLlambo(row.LlamboScores["writing"]), formatOperational(row.Scores["speed"]), fmtOutputPrice(row))
	}
	if len(projected) > 0 {
		b.WriteString("\n## Local projections\n\nProjected artifacts inherit reviewed upstream evidence with identity-confidence calibration, remain outside the canonical matrix, and always have low Llambo confidence.\n\n")
		b.WriteString("| Model | Agents | Coding | Instruction Following | Long Context | Reasoning | Writing | Speed | Price |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
		for _, row := range projected {
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", row.Name, formatLlambo(row.LlamboScores["agents"]), formatLlambo(row.LlamboScores["coding"]), formatLlambo(row.LlamboScores["instruction-following"]), formatLlambo(row.LlamboScores["long-context"]), formatLlambo(row.LlamboScores["reasoning"]), formatLlambo(row.LlamboScores["writing"]), formatOperational(row.Scores["speed"]), fmtOutputPrice(row))
		}
	}
	writeCoverageCampaignMarkdown(&b, report.CoverageCampaign)
	b.WriteString("\n## Evidence\n\n")
	for _, row := range append(append([]ReportModel(nil), rows...), projected...) {
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			score := row.LlamboScores[category]
			if score == nil {
				continue
			}
			fmt.Fprintf(&b, "- %s / %s: score %.1f; coverage %.1f%%; trusted coverage %.1f%%; confidence %s; winner %s; stale %t%s\n", row.Name, category, score.Score, 100*score.Coverage, 100*score.TrustedCoverage, score.Confidence, score.WinnerStatus, score.Stale, projectionSuffix(row))
			for _, contribution := range scoreContributions(score) {
				fmt.Fprintf(&b, "  - %s / %s: raw %s, percentile %s, frozen population %d, nominal/effective weight %.3f/%.3f, grade %s%s\n", contribution.Family, contribution.Benchmark, formatRaw(contribution.RawScore), formatRaw(contribution.Percentile), contribution.ReferencePopulation, contribution.NominalWeight, contribution.EffectiveWeight, contribution.EvidenceGrade, formatEvidenceProvenance(contribution))
			}
		}
		for _, category := range sortedLlamboCategories(row.LlamboScores) {
			reason := row.UnresolvedReasons[category]
			if row.LlamboScores[category] == nil {
				fmt.Fprintf(&b, "- %s / %s: unresolved — %s%s\n", row.Name, category, reason, projectionSuffix(row))
			}
		}
	}
	if report.Validation != nil {
		b.WriteString("\n## Local validation diagnostic\n\n")
		b.WriteString("Read-only sealed exact-ID receipt comparison; it does not affect Llambo Scores, ordering, or filters.\n\n")
		b.WriteString("| Category | Exact pairs | Spearman | Status |\n|---|---:|---:|---|\n")
		for _, category := range report.Validation.Categories {
			fmt.Fprintf(&b, "| %s | %d | %s | %s |\n", category.Category, category.SampleSize, formatCorrelation(category.Spearman), category.Status)
		}
	}
	return b.String()
}

func writeSourceFreshnessMarkdown(b *strings.Builder, sources []SourceStatus, generatedAt time.Time) {
	for _, source := range sources {
		if source.Cache == "stale" || (!source.FetchedAt.IsZero() && generatedAt.Sub(source.FetchedAt) > DefaultTTL) {
			fmt.Fprintf(b, "Warning: source `%s` is stale (%s). It remains scoreable but lowers confidence.\n\n", source.Name, source.FetchedAt.UTC().Format(time.RFC3339))
		}
	}
}

func writeCoverageCampaignMarkdown(b *strings.Builder, campaign *CoverageCampaign) {
	if campaign == nil {
		return
	}
	fmt.Fprintf(b, "\n## External score coverage\n\nCampaign `%s`: **%s** — %d/%d cells present; %d unresolved.\n\n", campaign.TargetVersion, campaign.Status, campaign.PresentCells, campaign.TotalCells, len(campaign.Unresolved))
	if len(campaign.Unresolved) > 0 {
		b.WriteString("| Model | Category | Status | Reason | Last checked |\n|---|---|---|---|---|\n")
		for _, cell := range campaign.Unresolved {
			fmt.Fprintf(b, "| %s | %s | %s | %s | %s |\n", markdownCell(cell.ModelKey), markdownCell(cell.Category), markdownCell(cell.Status), markdownCell(cell.Reason), markdownCell(cell.LastCheckedRevision))
		}
	}
	if len(campaign.InactiveOrNew) > 0 {
		b.WriteString("\nInactive/new models are tracked outside this frozen campaign:\n\n| Model | Reason |\n|---|---|\n")
		for _, note := range campaign.InactiveOrNew {
			fmt.Fprintf(b, "| %s | %s |\n", markdownCell(note.Key), markdownCell(note.Reason))
		}
	}
}

func markdownCell(value string) string {
	value = strings.ReplaceAll(value, "|", "\\|")
	return strings.ReplaceAll(value, "\n", " ")
}

func formatCorrelation(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.3f", *value)
}

func formatEvidenceProvenance(e benchmarkEvidence) string {
	parts := make([]string, 0, 4)
	if e.SourceVersion != "" {
		parts = append(parts, "version "+e.SourceVersion)
	}
	if e.SourceID != "" {
		parts = append(parts, "source "+e.SourceID)
	}
	if e.SourceRevision != "" {
		parts = append(parts, "source revision "+e.SourceRevision)
	}
	if len(e.Mirrors) > 0 {
		parts = append(parts, fmt.Sprintf("%d deduplicated mirror(s)", len(e.Mirrors)))
	}
	if e.CommitSHA != "" {
		parts = append(parts, "commit "+e.CommitSHA)
	}
	if e.ContentSHA != "" {
		parts = append(parts, "sha256 "+e.ContentSHA)
	}
	if e.Methodology != "" {
		parts = append(parts, "method "+e.Methodology)
	}
	if e.JudgeVersion != "" {
		parts = append(parts, "judge "+e.JudgeVersion)
	}
	if len(parts) == 0 {
		return ""
	}
	return "; " + strings.Join(parts, "; ")
}

func formatLlambo(score *LlamboScore) string {
	if score == nil {
		return "unresolved"
	}
	status := ""
	if score.WinnerStatus != "" {
		status = "; " + score.WinnerStatus
	}
	if score.Stale {
		status += "; stale"
	}
	if score.Estimated {
		status += "; estimated"
	}
	mark := ""
	if score.Estimated {
		mark = "ᵉ"
	}
	return fmt.Sprintf("%.1f%s (%s; %.0f%% coverage%s)", score.Score, mark, score.Confidence, 100*score.TrustedCoverage, status)
}

// formatOverall is retained for decoding and inspecting historical v1
// snapshots. Active LLAMBO-6 renderers never call it.
func formatOverall(score *OverallScore) string {
	if score == nil || score.Score == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f (%s)", *score.Score, score.Confidence)
}

func scoreContributions(score *LlamboScore) []benchmarkEvidence {
	if len(score.Contributions) > 0 {
		return score.Contributions
	}
	if score.Primary != nil && score.Primary.Benchmark != "" {
		return []benchmarkEvidence{*score.Primary}
	}
	return nil
}
func formatOperational(score *ExternalScore) string {
	if score == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", score.Score)
}
func formatRaw(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.4g", *value)
}
func formatAgreement(value *float64) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", *value)
}
func projectionSuffix(row ReportModel) string {
	if row.Projection == nil {
		return ""
	}
	return " (projected)"
}

func sortedLlamboCategories(scores map[string]*LlamboScore) []string {
	keys := make([]string, 0, len(scores))
	for key := range scores {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func partitionProjectionRows(rows []ReportModel) ([]ReportModel, []ReportModel) {
	canonical, projected := make([]ReportModel, 0, len(rows)), make([]ReportModel, 0)
	for _, row := range rows {
		if row.Projection == nil {
			canonical = append(canonical, row)
		} else {
			projected = append(projected, row)
		}
	}
	return canonical, projected
}

func fmtOutputPrice(row ReportModel) string {
	if row.Projection != nil {
		return "local/projected"
	}
	value, ok := maximumOutputPrice(row)
	if !ok {
		return "—"
	}
	return fmt.Sprintf("$%.3g", value)
}
