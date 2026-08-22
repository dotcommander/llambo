package evals

import "math"

type EligibilityDiagnostics struct {
	MinScore         float64 `json:"min_score"`
	ScoreCategory    string  `json:"score_category,omitempty"`
	MaxOutputPrice   float64 `json:"max_output_price"`
	InputModels      int     `json:"input_models"`
	IncludedModels   int     `json:"included_models"`
	ExcludedModels   int     `json:"excluded_models"`
	MissingPrimary   int     `json:"missing_primary"`
	BelowScore       int     `json:"below_score"`
	OverOutputPrice  int     `json:"over_output_price"`
	UnknownPriceKept int     `json:"unknown_output_price_kept"`
}

// ApplyEligibility filters only a named capability. Score filtering is invalid
// for speed/price because those remain operational measures.
func ApplyCategoryEligibility(report *Report, category string, minScore, maxOutputPrice float64) {
	if report == nil || (minScore < 0 && maxOutputPrice < 0) {
		return
	}
	diagnostics := &EligibilityDiagnostics{MinScore: minScore, ScoreCategory: category, MaxOutputPrice: maxOutputPrice, InputModels: len(report.Models)}
	filtered := make([]ReportModel, 0, len(report.Models))
	for _, model := range report.Models {
		if minScore >= 0 {
			score := model.LlamboScores[category]
			if score == nil {
				diagnostics.MissingPrimary++
				diagnostics.ExcludedModels++
				continue
			}
			if score.Score < minScore {
				diagnostics.BelowScore++
				diagnostics.ExcludedModels++
				continue
			}
		}
		if maxOutputPrice >= 0 && model.Projection == nil {
			price, known := maximumOutputPrice(model)
			if !known {
				diagnostics.UnknownPriceKept++
			} else if price > maxOutputPrice {
				diagnostics.OverOutputPrice++
				diagnostics.ExcludedModels++
				continue
			}
		}
		filtered = append(filtered, model)
	}
	diagnostics.IncludedModels = len(filtered)
	report.Models = filtered
	report.Eligibility = diagnostics
}

func maximumOutputPrice(model ReportModel) (float64, bool) {
	prices := make([]float64, 0, 2)
	if model.LLMStats != nil && model.LLMStats.OutputPrice != nil && !math.IsNaN(*model.LLMStats.OutputPrice) && !math.IsInf(*model.LLMStats.OutputPrice, 0) {
		prices = append(prices, *model.LLMStats.OutputPrice)
	}
	if model.AA != nil && model.AA.OutputPrice != nil && !math.IsNaN(*model.AA.OutputPrice) && !math.IsInf(*model.AA.OutputPrice, 0) {
		prices = append(prices, *model.AA.OutputPrice)
	}
	if len(prices) == 0 {
		return 0, false
	}
	maximum := prices[0]
	for _, price := range prices[1:] {
		if price > maximum {
			maximum = price
		}
	}
	return maximum, true
}
