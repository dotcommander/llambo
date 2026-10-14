package evals

import (
	"strings"
)

func writingEvidenceRowComplete(result BenchmarkResult) bool {
	identity := result.Score != nil && finite(*result.Score) && strings.TrimSpace(result.SourceID) != "" && strings.TrimSpace(result.SourceClass) != ""
	provenance := strings.TrimSpace(result.EvidenceGrade) != "" && strings.TrimSpace(result.Method) != "" && strings.TrimSpace(result.ContentSHA) != "" && strings.TrimSpace(result.Version) != ""
	return identity && provenance
}
