package evals

import (
	"strings"
)

func statsV1ScoreIdentityComplete(row llmStatsStatsV1ScoreRow) bool {
	identity := strings.TrimSpace(row.ModelID) != "" && strings.TrimSpace(row.ModelName) != "" && strings.TrimSpace(row.Organization) != ""
	provenance := strings.TrimSpace(row.BenchmarkName) != "" && strings.TrimSpace(row.Source) != "" && strings.TrimSpace(row.URL) != ""
	return identity && provenance
}
