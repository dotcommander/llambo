package evals

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

func RenderMarkdown(report Report, limit int) string {
	canonicalRows, projectedRows := partitionProjectionRows(report.Models)
	if limit <= 0 || limit > len(canonicalRows) {
		limit = len(canonicalRows)
	}
	var b strings.Builder
	b.WriteString("# Llambo External Evaluation Report\n\n")
	fmt.Fprintf(&b, "Generated: %s  \nFormula: `%s`  \nRanked by: `%s`  \n\n", report.GeneratedAt.Format(time.RFC3339), report.FormulaVersion, report.RankingProfile)
	b.WriteString("## Source status\n\n")
	for _, source := range report.Sources {
		fmt.Fprintf(&b, "- [%s](%s): %s, %d models", source.Name, source.URL, source.Cache, source.Models)
		if !source.FetchedAt.IsZero() {
			fmt.Fprintf(&b, ", fetched %s", source.FetchedAt.Format(time.RFC3339))
		}
		if source.Error != "" {
			fmt.Fprintf(&b, " (%s)", source.Error)
		}
		b.WriteString("\n")
	}

	b.WriteString("\n## Formula\n\n")
	fmt.Fprintf(&b, "- Population: %d canonical model rows; %d unique dual-source identities (exact or normalized).\n", report.Formula.Population, report.Formula.DualSourceModels)
	fmt.Fprintf(&b, "- Transformation: %s.\n", report.Formula.Method)
	fmt.Fprintf(&b, "- Frozen reference: %s; %d LLM Stats and %d Artificial Analysis models.\n", report.Formula.Reference.CreatedAt.Format(time.RFC3339), report.Formula.Reference.SourceModelCounts["llm_stats"], report.Formula.Reference.SourceModelCounts["artificial_analysis"])
	fmt.Fprintf(&b, "- Reference drift: %s; maximum KS %.3f", report.Formula.Drift.Status, report.Formula.Drift.MaxKS)
	if report.Formula.Drift.WorstMetric != "" {
		fmt.Fprintf(&b, " (%s)", report.Formula.Drift.WorstMetric)
	}
	b.WriteString(".\n")
	if report.Eligibility != nil {
		filters := make([]string, 0, 2)
		if report.Eligibility.MinOverall >= 0 {
			filters = append(filters, fmt.Sprintf("overall >= %.1f", report.Eligibility.MinOverall))
		}
		if report.Eligibility.MaxOutputPrice >= 0 {
			filters = append(filters, fmt.Sprintf("known output price <= %s/1M", fmtPrice(report.Eligibility.MaxOutputPrice)))
		}
		fmt.Fprintf(&b, "- Eligibility filters: %s; %d of %d models included, %d excluded (%d missing overall, %d below overall, %d over price, %d unknown prices retained).\n",
			strings.Join(filters, "; "), report.Eligibility.IncludedModels, report.Eligibility.InputModels,
			report.Eligibility.ExcludedModels, report.Eligibility.MissingOverall, report.Eligibility.BelowOverall,
			report.Eligibility.OverOutputPrice, report.Eligibility.UnknownPriceKept)
	}
	fmt.Fprintf(&b, "- Tracked projections: %d of %d applied from %s registry version %d.\n", report.Projections.Applied, report.Projections.Configured, report.Projections.RegistrySource, report.Projections.Version)
	for _, missing := range report.Projections.Missing {
		fmt.Fprintf(&b, "  - Missing upstream `%s` for tracked artifact `%s`.\n", missing.SourceKey, missing.ArtifactKey)
	}
	if report.OMLX != nil {
		if report.OMLX.Status == "unavailable" {
			fmt.Fprintf(&b, "- OMLX live selection: unavailable; projected availability omitted (%s).\n", report.OMLX.Error)
		} else {
			fmt.Fprintf(&b, "- OMLX live selection: %d discovered at %s; %d matched, %d unreviewed live, %d inactive reviewed, %d excluded.\n",
				report.OMLX.Discovered, report.OMLX.Endpoint, len(report.OMLX.MatchedModels), len(report.OMLX.UnmatchedModels), len(report.OMLX.InactiveReviewedModels), len(report.OMLX.ExcludedModels))
		}
	}
	for _, reason := range report.Formula.Drift.Reasons {
		fmt.Fprintf(&b, "  - %s.\n", reason)
	}
	fmt.Fprintf(&b, "- Jackknife stability: %s across %d metric removals; mean Kendall %.3f, minimum top-20 overlap %.1f%%", report.Formula.Stability.Status, report.Formula.Stability.Variants, report.Formula.Stability.MeanKendall, report.Formula.Stability.MinTop20Overlap*100)
	if report.Formula.Stability.WorstMetric != "" {
		fmt.Fprintf(&b, " (%s)", report.Formula.Stability.WorstMetric)
	}
	b.WriteString(".\n")
	if report.AAVersion > 0 {
		fmt.Fprintf(&b, "- Artificial Analysis index version: %.1f.\n", report.AAVersion)
	}
	b.WriteString("- Profiles: ")
	profileNames := make([]string, 0, len(report.Formula.Profiles))
	for _, profile := range report.Formula.Profiles {
		profileNames = append(profileNames, profile.Name)
	}
	b.WriteString(strings.Join(profileNames, ", "))
	b.WriteString(".\n")

	fmt.Fprintf(&b, "\n## Rankings (by %s)\n\n", report.RankingProfile)
	b.WriteString("| Rank | Model | Org | Access | Overall | General | Coding | Reasoning | Agents | Writing | Long ctx | Speed | Value | Output $/1M | Evidence | AA intelligence | LLM reasoning |\n")
	b.WriteString("| ---: | --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: |\n")
	previousScore := 0.0
	previousRank := 0
	for i, row := range canonicalRows[:limit] {
		selected := row.Scores[report.RankingProfile]
		rank := "—"
		if selected != nil {
			if previousRank == 0 || selected.Score != previousScore {
				previousRank = i + 1
				previousScore = selected.Score
			}
			rank = fmt.Sprintf("%d", previousRank)
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			rank, escapePipe(row.Name), escapePipe(row.Organization), fmtAccess(row.Open),
			fmtExternalScore(row.Scores["overall"]), fmtExternalScore(row.Scores["general"]), fmtExternalScore(row.Scores["coding"]),
			fmtExternalScore(row.Scores["reasoning"]), fmtExternalScore(row.Scores["agents"]), fmtExternalScore(row.Scores["writing"]),
			fmtExternalScore(row.Scores["long-context"]), fmtExternalScore(row.Scores["speed"]), fmtExternalScore(row.Scores["value"]),
			fmtOutputPrice(row), fmtEvidence(selected), fmtAA(row.AA), fmtIndex(row.LLMStats, "reasoning"))
	}

	if len(projectedRows) > 0 {
		b.WriteString("\n## Tracked local/OSS projections\n\n")
		b.WriteString("These rows inherit external evidence from the named upstream model. Their confidence is capped by the projection registry; access and cost are local/projected, not copied upstream API claims.\n\n")
		fmt.Fprintf(&b, "| Local artifact | Projected from | Selected (%s) | Overall | Coding | Agents | Writing | Cost | Evidence |\n", escapePipe(report.RankingProfile))
		b.WriteString("| --- | --- | ---: | ---: | ---: | ---: | ---: | --- | --- |\n")
		for _, row := range projectedRows {
			selected := row.Scores[report.RankingProfile]
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n",
				escapePipe(row.Name), escapePipe(row.Projection.SourceKey),
				fmtExternalScore(selected),
				fmtExternalScore(row.Scores["overall"]), fmtExternalScore(row.Scores["coding"]),
				fmtExternalScore(row.Scores["agents"]), fmtExternalScore(row.Scores["writing"]),
				fmtOutputPrice(row), fmtEvidence(selected))
		}
	}

	b.WriteString("\n## Interpretation\n\n")
	b.WriteString("Scores are deterministic percentiles of the external source metrics, transformed by the declared policy weights. They are not predictions of retired Llambo tests and do not use local evaluation targets. Missing evidence contributes a neutral 50 and widens the reported evidence interval; it never becomes zero. Coverage, cross-source disagreement, and rank stability remain separate from capability scores. Percentiles are relative rankings, not probabilities of task success. Prefer a task-specific profile over `overall` when the workload is narrow.\n\n")
	b.WriteString("Sources: [LLM Stats](https://llm-stats.com/) and [Artificial Analysis](https://artificialanalysis.ai/).\n")
	return b.String()
}

func fmtOutputPrice(row ReportModel) string {
	if row.Projection != nil {
		return "local/projected"
	}
	var prices []float64
	if row.LLMStats != nil && row.LLMStats.OutputPrice != nil {
		prices = appendFinitePrice(prices, *row.LLMStats.OutputPrice)
	}
	if row.AA != nil && row.AA.OutputPrice != nil {
		prices = appendFinitePrice(prices, *row.AA.OutputPrice)
	}
	if len(prices) == 0 {
		return "—"
	}
	sort.Float64s(prices)
	if len(prices) == 1 || prices[0] == prices[len(prices)-1] {
		return fmtPrice(prices[0])
	}
	return fmt.Sprintf("%s–%s", fmtPrice(prices[0]), fmtPrice(prices[len(prices)-1]))
}

func partitionProjectionRows(rows []ReportModel) (canonical, projected []ReportModel) {
	canonical = make([]ReportModel, 0, len(rows))
	projected = make([]ReportModel, 0)
	for _, row := range rows {
		if row.Projection != nil {
			projected = append(projected, row)
			continue
		}
		canonical = append(canonical, row)
	}
	return canonical, projected
}

func appendFinitePrice(prices []float64, price float64) []float64 {
	if math.IsNaN(price) || math.IsInf(price, 0) || price < 0 {
		return prices
	}
	return append(prices, price)
}

func fmtPrice(price float64) string {
	switch {
	case price == 0:
		return "$0"
	case price < .01:
		return fmt.Sprintf("$%.4f", price)
	case price < 1:
		return fmt.Sprintf("$%.3f", price)
	default:
		return fmt.Sprintf("$%.2f", price)
	}
}

func fmtExternalScore(score *ExternalScore) string {
	if score == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", score.Score)
}

func fmtEvidence(score *ExternalScore) string {
	if score == nil {
		return "—"
	}
	parts := []string{fmt.Sprintf("%s, %.0f%%", score.Confidence, score.Coverage*100), fmt.Sprintf("%.1f–%.1f", score.Low, score.High)}
	if score.Disagreement != nil {
		parts = append(parts, fmt.Sprintf("Δ%.1f", *score.Disagreement))
	}
	if score.RankStability != "" {
		parts = append(parts, fmt.Sprintf("%s ±%d", score.RankStability, score.RankSpread))
	}
	return strings.Join(parts, "; ")
}

func fmtAccess(v *bool) string {
	if v == nil {
		return "unknown"
	}
	if *v {
		return "open"
	}
	return "closed"
}

func fmtAA(v *ArtificialMetrics) string {
	if v == nil || v.Intelligence == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f", *v.Intelligence)
}

func fmtIndex(v *LLMStatsMetrics, name string) string {
	if v == nil {
		return "—"
	}
	index, ok := v.Indexes[name]
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.1f", index.Conservative)
}

func escapePipe(s string) string { return strings.ReplaceAll(s, "|", "\\|") }

func sortedWeightKeys(weights map[string]float64) []string {
	keys := make([]string, 0, len(weights))
	for key := range weights {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
