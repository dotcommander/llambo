package evals

import "sort"

func compareReportRows(left, right ReportModel, rankBy string) bool {
	if rankBy == matrix1 {
		leftScored, rightScored := hasCapabilityScore(left), hasCapabilityScore(right)
		if leftScored != rightScored {
			return leftScored
		}
		if left.Name == right.Name {
			return left.Key < right.Key
		}
		return left.Name < right.Name
	}
	leftScore, rightScore := reportRowRankValue(left, rankBy), reportRowRankValue(right, rankBy)
	if leftScore == nil {
		return false
	}
	if rightScore == nil {
		return true
	}
	if *leftScore == *rightScore {
		if left.Name == right.Name {
			return left.Key < right.Key
		}
		return left.Name < right.Name
	}
	return *leftScore > *rightScore
}

func reportRowRankValue(row ReportModel, rankBy string) *float64 {
	if validCapabilityCategory(rankBy) {
		if score := row.LlamboScores[rankBy]; score != nil && !score.Estimated {
			return &score.Score
		}
		return nil
	}
	if score := row.Scores[rankBy]; score != nil {
		return &score.Score
	}
	return nil
}

func buildCategoryRankings(rows []ReportModel) map[string]CategoryRanking {
	rankings := make(map[string]CategoryRanking, len(categorySpecs))
	for _, spec := range categorySpecs {
		ranking := CategoryRanking{Category: spec.name, Status: "unresolved"}
		ranking.Entries = categoryRankEntries(rows, spec.name)
		selectCategoryWinners(&ranking, rows)
		rankings[spec.name] = ranking
	}
	return rankings
}

func categoryRankEntries(rows []ReportModel, category string) []CategoryRankedModel {
	ranking := CategoryRanking{}
	for _, row := range rows {
		score := row.LlamboScores[category]
		if score == nil || score.Estimated {
			continue
		}
		identity := 1.0
		if row.Projection != nil {
			identity = projectionMultiplier(row.Projection)
		}
		ranking.Entries = append(ranking.Entries, CategoryRankedModel{Key: row.Key, Score: score.Score, TrustedCoverage: score.TrustedCoverage, IdentityConfidence: identity})
	}
	sort.Slice(ranking.Entries, func(i, j int) bool {
		left, right := ranking.Entries[i], ranking.Entries[j]
		if left.Score != right.Score {
			return left.Score > right.Score
		}
		if left.TrustedCoverage != right.TrustedCoverage {
			return left.TrustedCoverage > right.TrustedCoverage
		}
		if left.IdentityConfidence != right.IdentityConfidence {
			return left.IdentityConfidence > right.IdentityConfidence
		}
		return left.Key < right.Key
	})
	return ranking.Entries
}

func familyCount(rows []ReportModel, category, key string) int {
	for _, row := range rows {
		if row.Key == key && row.LlamboScores[category] != nil {
			if row.LlamboScores[category].Estimated {
				return 0
			}
			return len(row.LlamboScores[category].Families)
		}
	}
	return 0
}

func applyCategoryWinnerStatuses(rows []ReportModel, rankings map[string]CategoryRanking) {
	for category, ranking := range rankings {
		for _, entry := range ranking.Entries {
			if entry.WinnerStatus == "" {
				continue
			}
			for i := range rows {
				if rows[i].Key == entry.Key && rows[i].LlamboScores[category] != nil {
					rows[i].LlamboScores[category].WinnerStatus = entry.WinnerStatus
				}
			}
		}
	}
}

func hasCapabilityScore(model ReportModel) bool {
	for _, score := range model.LlamboScores {
		if score != nil && !score.Estimated {
			return true
		}
	}
	return false
}
