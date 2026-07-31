package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func loadArtificialAnalysis(ctx context.Context, opts Options) (sourceSnapshot, SourceStatus, error) {
	cache, cacheErr := readSnapshot(filepath.Join(opts.CacheDir, "artificial-analysis.json"))
	if !opts.Refresh && cacheErr == nil && opts.Now().Sub(cache.FetchedAt) < opts.TTL {
		return cache, statusFor("Artificial Analysis", ArtificialAnalysisURL, cache, "fresh", nil), nil
	}
	if strings.TrimSpace(opts.AAAPIKey) == "" {
		err := errors.New("AA_API_KEY is not set")
		if cacheErr == nil {
			return cache, statusFor("Artificial Analysis", ArtificialAnalysisURL, cache, "stale", err), nil
		}
		return sourceSnapshot{}, SourceStatus{Name: "Artificial Analysis", URL: ArtificialAnalysisURL, Cache: "unavailable", Error: err.Error()}, err
	}
	return loadSource(ctx, opts, "artificial-analysis", func(ctx context.Context) (sourceSnapshot, error) {
		return fetchArtificialAnalysis(ctx, opts)
	})
}

func loadSource(ctx context.Context, opts Options, cacheName string, fetch func(context.Context) (sourceSnapshot, error)) (sourceSnapshot, SourceStatus, error) {
	path := filepath.Join(opts.CacheDir, cacheName+".json")
	cached, cacheErr := readSnapshot(path)
	name, url := "LLM Stats", LLMStatsLeaderboardURL
	if cacheName == "artificial-analysis" {
		name, url = "Artificial Analysis", ArtificialAnalysisURL
	}
	if !opts.Refresh && cacheErr == nil && opts.Now().Sub(cached.FetchedAt) < opts.TTL {
		return cached, statusFor(name, url, cached, "fresh", nil), nil
	}
	next, err := fetch(ctx)
	if err == nil && len(next.Models) == 0 {
		err = errors.New("source returned no models")
	}
	if err == nil {
		next.FetchedAt = opts.Now().UTC()
		if writeErr := writeSnapshot(path, next); writeErr != nil {
			return sourceSnapshot{}, SourceStatus{}, fmt.Errorf("cache %s: %w", name, writeErr)
		}
		return next, statusFor(name, url, next, "fetched", nil), nil
	}
	if cacheErr == nil {
		return cached, statusFor(name, url, cached, "stale", err), nil
	}
	return sourceSnapshot{}, SourceStatus{Name: name, URL: url, Cache: "unavailable", Error: err.Error()}, fmt.Errorf("fetch %s: %w", name, err)
}

func statusFor(name, url string, snapshot sourceSnapshot, cache string, err error) SourceStatus {
	s := SourceStatus{Name: name, URL: url, FetchedAt: snapshot.FetchedAt, Cache: cache, Models: len(snapshot.Models)}
	if err != nil {
		s.Error = err.Error()
	}
	return s
}

func readSnapshot(path string) (sourceSnapshot, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return sourceSnapshot{}, err
	}
	var snapshot sourceSnapshot
	if err := json.Unmarshal(b, &snapshot); err != nil {
		return sourceSnapshot{}, err
	}
	if snapshot.FetchedAt.IsZero() {
		return sourceSnapshot{}, errors.New("cache has no fetched_at")
	}
	return snapshot, nil
}

func writeSnapshot(path string, snapshot sourceSnapshot) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
