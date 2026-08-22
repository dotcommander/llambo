package evals

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed testdata/llambo-v2-ifeval.json
var officialIFEvalSnapshotJSON []byte

func loadOfficialIFEvalSnapshot() (sourceSnapshot, error) {
	var snapshot sourceSnapshot
	if err := json.Unmarshal(officialIFEvalSnapshotJSON, &snapshot); err != nil {
		return sourceSnapshot{}, fmt.Errorf("decode official IFEval snapshot: %w", err)
	}
	if len(snapshot.Models) == 0 || snapshot.CommitSHA == "" || snapshot.ContentSHA == "" {
		return sourceSnapshot{}, fmt.Errorf("official IFEval snapshot is incomplete")
	}
	for i := range snapshot.Models {
		result, ok := snapshot.Models[i].Benchmarks["ifeval-official"]
		if !ok || result.Score == nil {
			return sourceSnapshot{}, fmt.Errorf("official IFEval snapshot row %d has no score", i+1)
		}
		result.Identity = IdentityMatchExact
		if result.Version == "" {
			result.Version = snapshot.Version
		}
		if result.URL == "" {
			result.URL = snapshot.URL
		}
		if result.CommitSHA == "" {
			result.CommitSHA = snapshot.CommitSHA
		}
		if result.ContentSHA == "" {
			result.ContentSHA = snapshot.ContentSHA
		}
		if result.Method == "" {
			result.Method = snapshot.Method
		}
		if result.FetchedAt.IsZero() {
			result.FetchedAt = snapshot.FetchedAt
		}
		result.SourceID = "ifeval-official"
		result.SourceClass = string(SourceFirstPartyResult)
		result.EvidenceGrade = "first_party"
		result.SourceRevision = snapshot.CommitSHA
		snapshot.Models[i].Benchmarks["ifeval-official"] = result
	}
	return snapshot, nil
}
