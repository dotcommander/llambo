package evals

// EligibilityDiagnostics describes the report-level model cutoff applied after
// LES scoring. Filtering never changes formula populations or percentiles.
type EligibilityDiagnostics struct {
	MinOverall       float64 `json:"min_overall"`
	MaxOutputPrice   float64 `json:"max_output_price"`
	InputModels      int     `json:"input_models"`
	IncludedModels   int     `json:"included_models"`
	ExcludedModels   int     `json:"excluded_models"`
	MissingOverall   int     `json:"missing_overall"`
	BelowOverall     int     `json:"below_overall"`
	OverOutputPrice  int     `json:"over_output_price"`
	UnknownPriceKept int     `json:"unknown_output_price_kept"`
}

// ApplyEligibility keeps models that satisfy the configured score and known
// output-price limits. Models without an overall score cannot demonstrate score
// eligibility. Unknown hosted prices remain eligible and are counted. Local
// projections are exempt because hosted API prices do not describe local runs.
// A negative limit disables that individual filter.
func ApplyEligibility(report *Report, minOverall, maxOutputPrice float64) {
	if report == nil || (minOverall < 0 && maxOutputPrice < 0) {
		return
	}
	diagnostics := &EligibilityDiagnostics{
		MinOverall: minOverall, MaxOutputPrice: maxOutputPrice, InputModels: len(report.Models),
	}
	filtered := make([]ReportModel, 0, len(report.Models))
	for _, model := range report.Models {
		if minOverall >= 0 {
			overall := model.Scores["overall"]
			if overall == nil {
				diagnostics.MissingOverall++
				diagnostics.ExcludedModels++
				continue
			}
			if overall.Score < minOverall {
				diagnostics.BelowOverall++
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

// ApplyMinimumOverall preserves the score-only operation for callers that do
// not want a price constraint.
func ApplyMinimumOverall(report *Report, threshold float64) {
	ApplyEligibility(report, threshold, -1)
}

func maximumOutputPrice(model ReportModel) (float64, bool) {
	prices := make([]float64, 0, 2)
	if model.LLMStats != nil && model.LLMStats.OutputPrice != nil {
		prices = appendFinitePrice(prices, *model.LLMStats.OutputPrice)
	}
	if model.AA != nil && model.AA.OutputPrice != nil {
		prices = appendFinitePrice(prices, *model.AA.OutputPrice)
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
