package evals

func clearCategoryWinnerStatuses(rows []ReportModel) {
	for i := range rows {
		for _, score := range rows[i].LlamboScores {
			if score != nil {
				score.WinnerStatus = ""
			}
		}
	}
}

func applyOMLXScoreInput(row *ReportModel, input OMLXScoreInput) {
	if input.Projection != nil {
		row.Projection = cloneProjection(input.Projection)
	}
	if input.Capabilities != nil {
		row.LlamboScores = cloneCategoryScores(input.Capabilities)
	}
	if row.LlamboScores == nil {
		row.LlamboScores = map[string]*LlamboScore{}
	}
	if row.UnresolvedReasons == nil {
		row.UnresolvedReasons = map[string]string{}
	}
	for _, category := range categorySpecs {
		if _, ok := row.LlamboScores[category.name]; !ok {
			row.UnresolvedReasons[category.name] = "no eligible reviewed cached evidence"
		}
	}
}
