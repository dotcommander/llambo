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
	if cacheName == "writingbench" {
		name, url = "WritingBench", opts.WritingBenchURL
	}
	if cacheName == "eqbench-creative-v3" {
		name, url = "EQ-Bench Creative v3", opts.EQBenchCreativeURL
	}
	if cacheName == "official-lfm25-2.6b" {
		name, url = "LiquidAI LFM2.5-2.6B card", opts.OfficialLFMURL
	}
	if cacheName == "official-qwen3.8-27b" {
		name, url = "Qwen3.8-27B card", opts.OfficialQwenURL
	}
	if cacheName == "official-gpt-oss-20b" {
		name, url = "OpenAI gpt-oss model card", opts.OfficialGPTOSSURL
	}
	if cacheName == "official-lfm25-vl-3b" {
		name, url = "LiquidAI LFM2.5-VL-3B card", opts.OfficialLFMVLURL
	}
	if cacheName == "official-gemma4" {
		name, url = "Google Gemma 4 model card", opts.OfficialGemmaURL
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

// loadOptionalOfficialSource preserves an otherwise usable catalog when a
// reviewed model card is temporarily unavailable. A successful refresh still
// goes through loadSource's atomic snapshot write; an unavailable card is
// exposed in source status and never creates inferred evidence.
func loadOptionalOfficialSource(ctx context.Context, opts Options, cacheName string, fetch func(context.Context) (sourceSnapshot, error)) (sourceSnapshot, SourceStatus) {
	snapshot, status, err := loadSource(ctx, opts, cacheName, fetch)
	if err == nil {
		if err := validateOfficialCardSnapshot(cacheName, snapshot); err != nil {
			return sourceSnapshot{}, invalidOfficialCardStatus(status, err)
		}
		return snapshot, status
	}
	return sourceSnapshot{}, status
}

func readOptionalOfficialSnapshot(opts Options, cacheName string) (sourceSnapshot, SourceStatus) {
	path := filepath.Join(opts.CacheDir, cacheName+".json")
	snapshot, err := readSnapshot(path)
	name, url := "", ""
	switch cacheName {
	case "official-lfm25-2.6b":
		name, url = "LiquidAI LFM2.5-2.6B card", opts.OfficialLFMURL
	case "official-qwen3.8-27b":
		name, url = "Qwen3.8-27B card", opts.OfficialQwenURL
	case "official-gpt-oss-20b":
		name, url = "OpenAI gpt-oss model card", opts.OfficialGPTOSSURL
	case "official-lfm25-vl-3b":
		name, url = "LiquidAI LFM2.5-VL-3B card", opts.OfficialLFMVLURL
	case "official-gemma4":
		name, url = "Google Gemma 4 model card", opts.OfficialGemmaURL
	}
	if err != nil {
		return sourceSnapshot{}, SourceStatus{Name: name, URL: url, Cache: "unavailable", Error: err.Error()}
	}
	if err := validateOfficialCardSnapshot(cacheName, snapshot); err != nil {
		return sourceSnapshot{}, SourceStatus{Name: name, URL: url, Cache: "unavailable", Error: "invalid official card cache: " + err.Error()}
	}
	stale := opts.Now().Sub(snapshot.FetchedAt) > opts.TTL
	markSnapshotStale(&snapshot, stale)
	cache := "cached"
	if stale {
		cache = "stale"
	}
	return snapshot, statusFor(name, url, snapshot, cache, nil)
}

func invalidOfficialCardStatus(status SourceStatus, err error) SourceStatus {
	status.Cache = "unavailable"
	status.Models = 0
	status.Error = "invalid official card cache: " + err.Error()
	return status
}

func statusFor(name, url string, snapshot sourceSnapshot, cache string, err error) SourceStatus {
	s := SourceStatus{
		Name: name, URL: url, FetchedAt: snapshot.FetchedAt, Cache: cache, Models: len(snapshot.Models),
		Version: snapshot.Version, CommitSHA: snapshot.CommitSHA, ContentSHA: snapshot.ContentSHA, Methodology: snapshot.Method,
		Observations: snapshot.Observations, RegistryVersion: snapshot.RegistryVersion, EvidenceGrade: snapshot.EvidenceGrade,
	}
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
	// O_EXCL temp file + rename: a predictable ".tmp" suffix would let
	// concurrent writers clobber each other and symlink attacks target a
	// known path.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}
