package evals

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ArenaCreativeSourceID = "arena-creative-writing"
	maxArenaCreativeBody  = 32 << 20
)

type arenaCreativeRating struct {
	Rating float64 `json:"rating"`
	Upper  float64 `json:"rating_q975"`
	Lower  float64 `json:"rating_q025"`
}

func fetchArenaCreative(ctx context.Context, opts Options) (sourceSnapshot, error) {
	body, headers, err := fetchWritingEvidenceHTTP(ctx, opts.Client, opts.ArenaCreativeURL, "Arena Creative Writing source", maxArenaCreativeBody)
	if err != nil {
		return sourceSnapshot{}, err
	}
	var categories map[string]map[string]arenaCreativeRating
	if err := json.Unmarshal(body, &categories); err != nil {
		return sourceSnapshot{}, fmt.Errorf("parse Arena Creative Writing source: %w", err)
	}
	rows := categories["creative_writing"]
	if len(rows) == 0 {
		return sourceSnapshot{}, fmt.Errorf("Arena source has no creative_writing rows")
	}
	sum := sha256.Sum256(body)
	contentSHA := fmt.Sprintf("%x", sum)
	version := normalizeHTTPETag(headers.Get("ETag"))
	commit := strings.TrimSpace(headers.Get("X-Repo-Commit"))
	if version == "" {
		version = commit
	}
	if version == "" {
		version = contentSHA
	}
	fetchedAt := opts.Now().UTC()
	models := make([]Model, 0, len(rows))
	names := make([]string, 0, len(rows))
	for name := range rows {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		row := rows[name]
		if !finite(row.Rating) || !finite(row.Lower) || !finite(row.Upper) || row.Lower > row.Upper {
			return sourceSnapshot{}, fmt.Errorf("Arena model %q has invalid rating bounds", name)
		}
		score := row.Rating
		result := BenchmarkResult{Score: &score, Version: version, URL: opts.ArenaCreativeURL, CommitSHA: commit, ContentSHA: contentSHA, Method: "Arena human-preference Bradley-Terry creative_writing rating", FetchedAt: fetchedAt, Details: map[string]float64{"rating_q025": row.Lower, "rating_q975": row.Upper}, SourceClass: string(SourceOwnerResult), EvidenceGrade: "owner", Unit: "rating", Direction: "higher", Cohort: "arena-creative-writing-live", Locator: opts.ArenaCreativeURL, SourceID: ArenaCreativeSourceID, SourceRevision: version}
		models = append(models, Model{Key: ArenaCreativeSourceID + ":" + canonicalKey(name), Name: name, Benchmarks: map[string]BenchmarkResult{"lmsys-writing": result}})
	}
	return sourceSnapshot{FetchedAt: fetchedAt, Models: models, URL: opts.ArenaCreativeURL, Version: version, CommitSHA: commit, ContentSHA: contentSHA, Method: "Arena human-preference Bradley-Terry creative_writing rating", Observations: len(models)}, nil
}
