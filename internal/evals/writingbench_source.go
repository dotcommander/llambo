package evals

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const maxWritingBenchBody = 32 << 20

func fetchWritingBench(ctx context.Context, opts Options) (sourceSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.WritingBenchURL, nil)
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
		return sourceSnapshot{}, fmt.Errorf("GET %s: HTTP %d", opts.WritingBenchURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWritingBenchBody+1))
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("read WritingBench score.xlsx: %w", err)
	}
	if len(body) > maxWritingBenchBody {
		return sourceSnapshot{}, fmt.Errorf("WritingBench score.xlsx exceeds %d bytes", maxWritingBenchBody)
	}
	rows, err := ParseWritingBenchXLSX(body)
	if err != nil {
		return sourceSnapshot{}, err
	}
	sum := sha256.Sum256(body)
	version := strings.Trim(strings.TrimSpace(resp.Header.Get("ETag")), `"`)
	commit := strings.TrimSpace(resp.Header.Get("X-Repo-Commit"))
	if commit == "" {
		commit = strings.TrimSpace(resp.Header.Get("X-Linked-ETag"))
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
