package evals

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"time"
)

const maxWritingBenchBody = 32 << 20

func fetchWritingBench(ctx context.Context, opts Options) (sourceSnapshot, error) {
	body, headers, err := fetchWritingEvidenceHTTP(ctx, opts.Client, opts.WritingBenchURL, "WritingBench score.xlsx", maxWritingBenchBody)
	if err != nil {
		return sourceSnapshot{}, err
	}
	rows, err := ParseWritingBenchXLSX(body)
	if err != nil {
		return sourceSnapshot{}, err
	}
	sum := sha256.Sum256(body)
	version := normalizeHTTPETag(headers.Get("ETag"))
	commit := strings.TrimSpace(headers.Get("X-Repo-Commit"))
	if commit == "" {
		commit = strings.TrimSpace(headers.Get("X-Linked-ETag"))
	}
	fetchedAt := time.Now().UTC()
	if opts.Now != nil {
		fetchedAt = opts.Now().UTC()
	}
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		result := row.Result
		result.URL, result.Version, result.CommitSHA, result.ContentSHA = opts.WritingBenchURL, version, commit, fmt.Sprintf("%x", sum)
		result.SourceID = "writingbench"
		result.SourceClass = string(SourceOwnerResult)
		result.EvidenceGrade = "owner"
		result.SourceRevision = commit
		if result.SourceRevision == "" {
			result.SourceRevision = version
		}
		result.Method = "WritingBench official score.xlsx Overall"
		result.Judge = "Claude-Sonnet-4-5"
		result.FetchedAt = fetchedAt
		models = append(models, Model{Key: "writingbench:" + canonicalKey(row.Model), Name: row.Model, Organization: row.Organization, Benchmarks: map[string]BenchmarkResult{"writingbench": result}})
	}
	return sourceSnapshot{FetchedAt: fetchedAt, Models: models, URL: opts.WritingBenchURL, Version: version, CommitSHA: commit, ContentSHA: fmt.Sprintf("%x", sum), Method: "WritingBench official score.xlsx Overall"}, nil
}
