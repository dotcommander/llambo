package evals

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"time"
)

const maxEQBenchCreativeBody = 8 << 20

func fetchEQBenchCreative(ctx context.Context, opts Options) (sourceSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.EQBenchCreativeURL, nil)
	if err != nil {
		return sourceSnapshot{}, err
	}
	req.Header.Set("User-Agent", "llambo-evals/1")
	resp, err := opts.Client.Do(req)
	if err != nil {
		return sourceSnapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return sourceSnapshot{}, fmt.Errorf("GET %s: HTTP %d", opts.EQBenchCreativeURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxEQBenchCreativeBody+1))
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("read EQ-Bench Creative source: %w", err)
	}
	if len(body) > maxEQBenchCreativeBody {
		return sourceSnapshot{}, fmt.Errorf("EQ-Bench Creative source exceeds %d bytes", maxEQBenchCreativeBody)
	}
	rows, err := ParseEQBenchCreativeJS(body)
	if err != nil {
		return sourceSnapshot{}, err
	}
	sum := sha256.Sum256(body)
	fetchedAt := time.Now().UTC()
	if opts.Now != nil {
		fetchedAt = opts.Now().UTC()
	}
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		result := row.Result
		result.URL, result.Version, result.CommitSHA, result.ContentSHA = opts.EQBenchCreativeURL, EQBenchCreativeCommit, EQBenchCreativeCommit, fmt.Sprintf("%x", sum)
		result.SourceID = "eqbench-creative-v3"
		result.SourceClass = string(SourceOwnerResult)
		result.EvidenceGrade = "owner"
		result.SourceRevision = EQBenchCreativeCommit
		result.Method = "EQ-Bench Creative v3 elo_score from pinned creative_writing.js"
		result.Judge = "Claude Sonnet 4.6"
		result.FetchedAt = fetchedAt
		models = append(models, Model{Key: "eqbench-creative-v3:" + canonicalKey(row.Model), Name: row.Model, Organization: row.Organization, Benchmarks: map[string]BenchmarkResult{"eqbench-creative-v3": result}})
	}
	return sourceSnapshot{FetchedAt: fetchedAt, Models: models, URL: opts.EQBenchCreativeURL, Version: EQBenchCreativeCommit, CommitSHA: EQBenchCreativeCommit, ContentSHA: fmt.Sprintf("%x", sum), Method: "EQ-Bench Creative v3 elo_score from pinned creative_writing.js"}, nil
}
