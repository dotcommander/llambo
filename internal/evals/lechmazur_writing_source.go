package evals

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func fetchLechMazurWriting(ctx context.Context, opts Options) (sourceSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, opts.LechMazurWritingURL, nil)
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
		return sourceSnapshot{}, fmt.Errorf("GET %s: HTTP %d", opts.LechMazurWritingURL, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWritingSourceBody+1))
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("read Lech Mazur writing leaderboard: %w", err)
	}
	if len(body) > maxWritingSourceBody {
		return sourceSnapshot{}, fmt.Errorf("Lech Mazur writing leaderboard exceeds %d bytes", maxWritingSourceBody)
	}
	rows, err := ParseWritingLeaderboard(string(body))
	if err != nil {
		return sourceSnapshot{}, fmt.Errorf("parse Lech Mazur writing leaderboard: %w", err)
	}
	sum := sha256.Sum256(body)
	contentSHA := fmt.Sprintf("%x", sum)
	version := normalizeHTTPETag(resp.Header.Get("ETag"))
	commit := strings.TrimSpace(resp.Header.Get("X-Repo-Commit"))
	if commit == "" {
		commit = strings.TrimSpace(resp.Header.Get("X-Linked-ETag"))
	}
	if version == "" {
		version = commit
	}
	if version == "" {
		version = contentSHA
	}
	fetchedAt := opts.Now().UTC()
	models := make([]Model, 0, len(rows))
	for _, row := range rows {
		score := row.Score
		result := BenchmarkResult{
			Score:          &score,
			Version:        version,
			URL:            opts.LechMazurWritingURL,
			CommitSHA:      commit,
			ContentSHA:     contentSHA,
			Method:         "Lech Mazur pairwise Thurstone creative-writing comparison",
			FetchedAt:      fetchedAt,
			Details:        map[string]float64{"rank": float64(row.Rank), "win_chance_percent": row.WinChance, "uncertainty_lower": row.Lower, "uncertainty_upper": row.Upper},
			SourceClass:    string(SourceOwnerResult),
			EvidenceGrade:  "owner",
			Unit:           "comparison score",
			Direction:      "higher",
			Cohort:         "lechmazur-writing-live",
			Locator:        opts.LechMazurWritingURL,
			SourceID:       WritingPrimaryID,
			SourceRevision: version,
		}
		models = append(models, Model{
			Key:        WritingPrimaryID + ":" + canonicalKey(row.Model),
			Name:       row.Model,
			Benchmarks: map[string]BenchmarkResult{"fiction-live": result},
		})
	}
	return sourceSnapshot{
		FetchedAt:    fetchedAt,
		Models:       models,
		URL:          opts.LechMazurWritingURL,
		Version:      version,
		CommitSHA:    commit,
		ContentSHA:   contentSHA,
		Method:       "Lech Mazur pairwise Thurstone creative-writing comparison",
		Observations: len(models),
	}, nil
}
