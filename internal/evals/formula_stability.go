package evals

import (
	"math"
	"sort"
)

func applyRankStability(rows []ReportModel, models []Model, metrics []normalizedMetric, profile string) StabilityDiagnostics {
	baseline := ranksForScores(rows, func(row ReportModel) *ExternalScore { return row.Scores[profile] })
	maxSpread := make([]int, len(rows))
	meanKendall := 0.0
	minTop20 := 1.0
	worstMetric := ""
	variants := 0
	for _, metric := range metrics {
		if len(metric.percentile) == 0 {
			continue
		}
		variantRows := buildScoredRows(models, metrics, metric.spec.name)
		variant := ranksForScores(variantRows, func(row ReportModel) *ExternalScore { return row.Scores[profile] })
		meanKendall += rankKendall(baseline, variant)
		top20 := rankTopKOverlap(baseline, variant, 20)
		if top20 < minTop20 {
			minTop20 = top20
			worstMetric = metric.spec.name
		}
		variants++
		for i := range rows {
			if baseline[i] == 0 {
				continue
			}
			if variant[i] == 0 {
				maxSpread[i] = len(rows)
				continue
			}
			shift := absInt(baseline[i] - variant[i])
			if shift > maxSpread[i] {
				maxSpread[i] = shift
			}
		}
	}
	if variants > 0 {
		meanKendall /= float64(variants)
	}
	for i := range rows {
		if score := rows[i].Scores[profile]; score != nil {
			score.RankSpread = maxSpread[i]
			switch {
			case maxSpread[i] <= 5:
				score.RankStability = "high"
			case maxSpread[i] <= 15:
				score.RankStability = "medium"
				if score.Confidence == "high" {
					score.Confidence = "medium"
				}
			default:
				score.RankStability = "low"
				score.Confidence = "low"
			}
		}
	}
	status := "stable"
	if meanKendall < .90 || minTop20 < .80 {
		status = "unstable"
	}
	return StabilityDiagnostics{Status: status, Variants: variants, MeanKendall: meanKendall, MinTop20Overlap: minTop20, WorstMetric: worstMetric}
}

func rankKendall(left, right []int) float64 {
	concordant, discordant := 0.0, 0.0
	for i := 0; i < len(left); i++ {
		if left[i] == 0 || right[i] == 0 {
			continue
		}
		for j := i + 1; j < len(left); j++ {
			if left[j] == 0 || right[j] == 0 {
				continue
			}
			baselineOrder := compareInt(left[i], left[j])
			variantOrder := compareInt(right[i], right[j])
			if baselineOrder == 0 || variantOrder == 0 {
				continue
			}
			if baselineOrder == variantOrder {
				concordant++
			} else {
				discordant++
			}
		}
	}
	if concordant+discordant == 0 {
		return 1
	}
	return (concordant - discordant) / (concordant + discordant)
}

func rankTopKOverlap(left, right []int, k int) float64 {
	leftTop, rightTop := map[int]bool{}, map[int]bool{}
	for i := range left {
		if left[i] > 0 && left[i] <= k {
			leftTop[i] = true
		}
		if right[i] > 0 && right[i] <= k {
			rightTop[i] = true
		}
	}
	intersection := 0
	for index := range leftTop {
		if rightTop[index] {
			intersection++
		}
	}
	denominator := max(len(leftTop), len(rightTop))
	if denominator == 0 {
		return 1
	}
	return float64(intersection) / float64(denominator)
}

func compareInt(left, right int) int {
	if left < right {
		return -1
	}
	if left > right {
		return 1
	}
	return 0
}

func ranksForScores(rows []ReportModel, scoreFor func(ReportModel) *ExternalScore) []int {
	indices := make([]int, 0, len(rows))
	for i, row := range rows {
		if scoreFor(row) != nil {
			indices = append(indices, i)
		}
	}
	sort.SliceStable(indices, func(i, j int) bool {
		leftIndex, rightIndex := indices[i], indices[j]
		left, right := scoreFor(rows[leftIndex]), scoreFor(rows[rightIndex])
		if left.Score == right.Score {
			if rows[leftIndex].Name == rows[rightIndex].Name {
				return rows[leftIndex].Key < rows[rightIndex].Key
			}
			return rows[leftIndex].Name < rows[rightIndex].Name
		}
		return left.Score > right.Score
	})
	ranks := make([]int, len(rows))
	previousScore := math.Inf(1)
	previousRank := 0
	for position, index := range indices {
		score := scoreFor(rows[index]).Score
		if position == 0 || score != previousScore {
			previousRank = position + 1
			previousScore = score
		}
		ranks[index] = previousRank
	}
	return ranks
}
