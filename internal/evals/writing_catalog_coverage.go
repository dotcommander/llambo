package evals

import "strings"

type WritingModelCoverage struct {
	ModelID                 string   `json:"model_id"`
	ModelName               string   `json:"model_name"`
	BenchmarkID             string   `json:"benchmark_id"`
	Status                  string   `json:"status"`
	MatchedLeaderboardModel string   `json:"matched_leaderboard_model,omitempty"`
	MatchType               string   `json:"match_type,omitempty"`
	Rank                    int      `json:"rank,omitempty"`
	ComparisonScore         *float64 `json:"comparison_score,omitempty"`
	WinChancePercent        *float64 `json:"win_chance_percent,omitempty"`
	Notes                   string   `json:"notes,omitempty"`
}

func buildWritingModelCoverage(models []WritingOpenModel, rows []WritingLeaderboardRow) []WritingModelCoverage {
	coverage := make([]WritingModelCoverage, 0, len(models))
	for _, model := range models {
		item := WritingModelCoverage{
			ModelID:     model.ID,
			ModelName:   model.Name,
			BenchmarkID: WritingPrimaryID,
			Status:      "needs-run",
			Notes:       "No exact public leaderboard row for this reviewed model ID.",
		}
		aliases := model.LeaderboardAliases
		if len(aliases) == 0 {
			aliases = []string{model.Name}
		}
		for _, row := range rows {
			matchedAlias := writingModelMatchedAlias(row.Model, aliases)
			if matchedAlias == "" {
				continue
			}
			score := row.Score
			winChance := row.WinChance
			item.Status = "measured"
			item.MatchedLeaderboardModel = row.Model
			item.Rank = row.Rank
			item.ComparisonScore = &score
			item.WinChancePercent = &winChance
			if model.LeaderboardMatchType != "variant" && normalizeWritingModelLabel(row.Model) == normalizeWritingModelLabel(matchedAlias) {
				item.MatchType = "exact"
				item.Notes = "Matched by an exact normalized leaderboard label; score remains source-native."
			} else {
				item.MatchType = "variant"
				item.Notes = "Matched by an explicit variant alias. Treat the score as indicative, not an exact serving-mode measurement."
			}
			break
		}
		coverage = append(coverage, item)
	}
	return coverage
}

func writingModelMatchedAlias(row string, aliases []string) string {
	row = normalizeWritingModelLabel(row)
	if row == "" {
		return ""
	}
	for _, alias := range aliases {
		normalizedAlias := normalizeWritingModelLabel(alias)
		if normalizedAlias != "" && strings.Contains(row, normalizedAlias) {
			return alias
		}
	}
	return ""
}

func normalizeWritingModelLabel(value string) string {
	var normalized strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			normalized.WriteRune(r)
		}
	}
	return normalized.String()
}
