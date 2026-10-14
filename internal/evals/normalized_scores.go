package evals

import (
	"sort"
)

func normalizedScoreRecords(report Report) ([]NormalizedScoreRecord, []NormalizedScoreContributionRecord) {
	categoryOrder := make(map[string]int, len(categorySpecs))
	for index, category := range categorySpecs {
		categoryOrder[category.name] = index
	}
	scores := make([]NormalizedScoreRecord, 0, len(report.Models)*len(categorySpecs))
	contributions := make([]NormalizedScoreContributionRecord, 0)
	unresolvedRank := normalizedUnresolvedRanks(report)
	for _, row := range report.Models {
		rowScores, rowContributions := normalizedRowScores(row, report, unresolvedRank)
		scores = append(scores, rowScores...)
		contributions = append(contributions, rowContributions...)
	}

	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].Category != scores[j].Category {
			return categoryOrder[scores[i].Category] < categoryOrder[scores[j].Category]
		}
		if scores[i].EditorialRank != scores[j].EditorialRank {
			return scores[i].EditorialRank < scores[j].EditorialRank
		}
		return scores[i].ModelKey < scores[j].ModelKey
	})
	sort.SliceStable(contributions, func(i, j int) bool {
		if contributions[i].Category != contributions[j].Category {
			return categoryOrder[contributions[i].Category] < categoryOrder[contributions[j].Category]
		}
		if contributions[i].ModelKey != contributions[j].ModelKey {
			return contributions[i].ModelKey < contributions[j].ModelKey
		}
		if contributions[i].BenchmarkFamily != contributions[j].BenchmarkFamily {
			return contributions[i].BenchmarkFamily < contributions[j].BenchmarkFamily
		}
		return contributions[i].Benchmark < contributions[j].Benchmark
	})
	return scores, contributions
}

func normalizedUnresolvedRanks(report Report) map[string]int {
	unresolvedRank := make(map[string]int, len(categorySpecs))
	for category, ranking := range report.CategoryRankings {
		unresolvedKeys := make([]string, 0)
		ranked := make(map[string]struct{}, len(ranking.Entries))
		for _, entry := range ranking.Entries {
			ranked[entry.Key] = struct{}{}
		}
		for _, row := range report.Models {
			if row.LlamboScores[category] == nil {
				unresolvedKeys = append(unresolvedKeys, row.Key)
			} else if _, ok := ranked[row.Key]; !ok {
				unresolvedKeys = append(unresolvedKeys, row.Key)
			}
		}
		sort.Strings(unresolvedKeys)
		for index, key := range unresolvedKeys {
			unresolvedRank[category+"\x00"+key] = len(ranking.Entries) + index + 1
		}
	}
	return unresolvedRank
}

func normalizedRowScores(row ReportModel, report Report, unresolvedRank map[string]int) ([]NormalizedScoreRecord, []NormalizedScoreContributionRecord) {
	scores := make([]NormalizedScoreRecord, 0, len(categorySpecs))
	contributions := make([]NormalizedScoreContributionRecord, 0)
	rowKind := "canonical"
	if row.Projection != nil {
		rowKind = "projection"
	}
	for _, category := range categorySpecs {
		score := row.LlamboScores[category.name]
		rank := unresolvedRank[category.name+"\x00"+row.Key]
		if ranking := report.CategoryRankings[category.name]; ranking.Entries != nil {
			for index, entry := range ranking.Entries {
				if entry.Key == row.Key {
					rank = index + 1
					break
				}
			}
		}
		record := NormalizedScoreRecord{
			Category: category.name, EditorialRank: rank, ModelKey: row.Key, ModelName: row.Name,
			RowKind: rowKind, Score: nil, UnresolvedReason: row.UnresolvedReasons[category.name],
		}
		if score != nil {
			record.Score = &score.Score
			record.Coverage, record.TrustedCoverage, record.Confidence = score.Coverage, score.TrustedCoverage, score.Confidence
			record.Families, record.Stale, record.Estimated, record.EstimateMethod = score.Families, score.Stale, score.Estimated, score.EstimateMethod
			record.WinnerStatus = score.WinnerStatus
		}
		if record.UnresolvedReason == "" && score == nil {
			record.UnresolvedReason = "no eligible reviewed cached evidence"
		}
		scores = append(scores, record)
		if score == nil {
			continue
		}
		for _, contribution := range score.Contributions {
			contributions = append(contributions, NormalizedScoreContributionRecord{
				Category: category.name, ModelKey: row.Key, RowKind: rowKind,
				BenchmarkFamily: contribution.Family, Benchmark: contribution.Benchmark,
				RawScore: contribution.RawScore, Percentile: contribution.Percentile,
				FrozenPopulation: contribution.ReferencePopulation, EvidenceGrade: contribution.EvidenceGrade,
				SourceID: contribution.SourceID, SourceClass: contribution.SourceClass,
				SourceRevision: contribution.SourceRevision, ContentSHA256: contribution.ContentSHA,
				NominalWeight: contribution.NominalWeight, EvidenceMultiplier: contribution.EvidenceMultiplier,
				IdentityMultiplier: contribution.IdentityMultiplier, EffectiveWeight: contribution.EffectiveWeight,
			})
		}
	}
	return scores, contributions
}
