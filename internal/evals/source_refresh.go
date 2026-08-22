package evals

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var defaultWritingEvidenceSources = []string{"writingbench", "eqbench-creative-v3", WritingPrimaryID, ArenaCreativeSourceID}

// RefreshEvaluationSources is the single source-ingestion entry point. It owns
// source selection, validation, predecessor preservation, and cache publication
// so CLI callers do not need source-specific branches. Each cache replacement is
// atomic; a multi-source refresh validates every snapshot before publishing any.
func RefreshEvaluationSources(ctx context.Context, opts Options, names []string) ([]SourceStatus, []string, error) {
	opts.applyDefaults()
	if len(names) == 0 {
		names = append([]string(nil), defaultWritingEvidenceSources...)
	}
	normalized := make([]string, 0, len(names))
	for _, name := range names {
		for _, item := range strings.Split(strings.ToLower(name), ",") {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			normalized = append(normalized, item)
		}
	}
	if len(normalized) == 1 && normalized[0] == "llm-stats-stats-v1" {
		status, backup, err := RefreshLLMStatsStatsV1Source(ctx, opts)
		if err != nil {
			return nil, nil, err
		}
		backups := []string(nil)
		if backup != "" {
			backups = append(backups, backup)
		}
		return []SourceStatus{status}, backups, nil
	}
	for _, name := range normalized {
		if name == "llm-stats-stats-v1" {
			return nil, nil, fmt.Errorf("llm-stats-stats-v1 must be refreshed alone")
		}
	}
	if err := validateWritingEvidenceSourceNames(normalized); err != nil {
		return nil, nil, err
	}
	backups, err := preserveEvaluationSourceCaches(opts.CacheDir, normalized, opts.Now())
	if err != nil {
		return nil, nil, err
	}
	statuses, err := refreshWritingEvidenceSources(ctx, opts, normalized)
	if err != nil {
		return nil, nil, err
	}
	return statuses, backups, nil
}

func preserveEvaluationSourceCaches(cacheDir string, names []string, now time.Time) ([]string, error) {
	backups := make([]string, 0, len(names))
	for _, name := range names {
		source := filepath.Join(cacheDir, name+".json")
		data, err := os.ReadFile(source)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read previous %s cache: %w", name, err)
		}
		backup := WritingEvidenceSourceBackupPath(cacheDir, name, now)
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return nil, fmt.Errorf("create source backup directory: %w", err)
		}
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return nil, fmt.Errorf("preserve previous %s cache: %w", name, err)
		}
		backups = append(backups, backup)
	}
	return backups, nil
}

// refreshWritingEvidenceSources fetches only the reviewed writing evidence
// sources, validates their row-level provenance, and publishes their cache
// snapshots. RefreshEvaluationSources supplies normalized, validated names.
// Fetches are staged in memory first so a later validation failure cannot leave
// a partially refreshed source set.
func refreshWritingEvidenceSources(ctx context.Context, opts Options, names []string) ([]SourceStatus, error) {
	fetchers := map[string]func(context.Context, Options) (sourceSnapshot, error){
		"writingbench":        fetchWritingBench,
		"eqbench-creative-v3": fetchEQBenchCreative,
		WritingPrimaryID:      fetchLechMazurWriting,
		ArenaCreativeSourceID: fetchArenaCreative,
	}
	opts.Refresh = true
	type stagedSource struct {
		name     string
		snapshot sourceSnapshot
	}
	staged := make([]stagedSource, 0, len(names))
	for _, name := range names {
		snapshot, err := fetchers[name](ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", name, err)
		}
		snapshot.FetchedAt = opts.Now().UTC()
		if err := validateWritingEvidenceSnapshot(name, snapshot); err != nil {
			return nil, fmt.Errorf("validate %s: %w", name, err)
		}
		staged = append(staged, stagedSource{name: name, snapshot: snapshot})
	}
	statuses := make([]SourceStatus, 0, len(staged))
	for _, source := range staged {
		if err := writeSnapshot(filepath.Join(opts.CacheDir, source.name+".json"), source.snapshot); err != nil {
			return nil, fmt.Errorf("cache %s: %w", source.name, err)
		}
		statuses = append(statuses, statusFor(writingEvidenceSourceName(source.name), writingEvidenceSourceURL(source.name, opts), source.snapshot, "fetched", nil))
	}
	return statuses, nil
}

func validateWritingEvidenceSourceNames(names []string) error {
	for _, name := range names {
		switch strings.TrimSpace(strings.ToLower(name)) {
		case "writingbench", "eqbench-creative-v3", WritingPrimaryID, ArenaCreativeSourceID:
		default:
			return fmt.Errorf("unsupported evaluation source %q (supported: arena-creative-writing, eqbench-creative-v3, lechmazur-writing, writingbench)", name)
		}
	}
	return nil
}

func validateWritingEvidenceSnapshot(name string, snapshot sourceSnapshot) error {
	if len(snapshot.Models) == 0 || strings.TrimSpace(snapshot.ContentSHA) == "" || strings.TrimSpace(snapshot.Method) == "" {
		return fmt.Errorf("incomplete source snapshot")
	}
	for _, model := range snapshot.Models {
		for benchmark, result := range model.Benchmarks {
			if result.Score == nil || !finite(*result.Score) ||
				strings.TrimSpace(result.SourceID) == "" ||
				strings.TrimSpace(result.SourceClass) == "" ||
				strings.TrimSpace(result.EvidenceGrade) == "" ||
				strings.TrimSpace(result.Method) == "" ||
				strings.TrimSpace(result.ContentSHA) == "" ||
				strings.TrimSpace(result.Version) == "" {
				return fmt.Errorf("model %q benchmark %q has incomplete row provenance", model.Key, benchmark)
			}
		}
	}
	return nil
}

// RefreshLLMStatsStatsV1Source performs the source-scoped authenticated Stats
// v1 refresh. Raw pages are immutable sealed artifacts; the replaceable
// llm-stats.json cache is backed up and changed only after the complete
// model+observation snapshot validates.
func RefreshLLMStatsStatsV1Source(ctx context.Context, opts Options) (SourceStatus, string, error) {
	opts.applyDefaults()
	snapshot, err := fetchLLMStatsStatsV1Snapshot(ctx, opts)
	if err != nil {
		return SourceStatus{}, "", fmt.Errorf("fetch llm-stats-stats-v1: %w", err)
	}
	snapshot.FetchedAt = opts.Now().UTC()
	if err := validateWritingEvidenceSnapshot("llm-stats-stats-v1", snapshot); err != nil {
		return SourceStatus{}, "", fmt.Errorf("validate llm-stats-stats-v1: %w", err)
	}
	cachePath := filepath.Join(opts.CacheDir, "llm-stats.json")
	backup := ""
	if _, err := os.Stat(cachePath); err == nil {
		backup = LLMStatsStatsV1SourceBackupPath(opts.CacheDir, opts.Now())
		if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
			return SourceStatus{}, "", fmt.Errorf("create LLM Stats backup directory: %w", err)
		}
		data, err := os.ReadFile(cachePath)
		if err != nil {
			return SourceStatus{}, "", fmt.Errorf("read previous LLM Stats cache: %w", err)
		}
		if err := os.WriteFile(backup, data, 0o600); err != nil {
			return SourceStatus{}, "", fmt.Errorf("preserve previous LLM Stats cache: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return SourceStatus{}, "", fmt.Errorf("inspect previous LLM Stats cache: %w", err)
	}
	if err := writeSnapshot(cachePath, snapshot); err != nil {
		return SourceStatus{}, "", fmt.Errorf("cache llm-stats-stats-v1: %w", err)
	}
	return statusFor("LLM Stats", opts.LLMStatsStatsV1ModelsURL, snapshot, "fetched", nil), backup, nil
}

func writingEvidenceSourceName(name string) string {
	if name == "eqbench-creative-v3" {
		return "EQ-Bench Creative v3"
	}
	if name == WritingPrimaryID {
		return "Lech Mazur Creative Story-Writing"
	}
	if name == ArenaCreativeSourceID {
		return "Arena Creative Writing"
	}
	return "WritingBench"
}

func writingEvidenceSourceURL(name string, opts Options) string {
	if name == "eqbench-creative-v3" {
		return opts.EQBenchCreativeURL
	}
	if name == WritingPrimaryID {
		return opts.LechMazurWritingURL
	}
	if name == ArenaCreativeSourceID {
		return opts.ArenaCreativeURL
	}
	return opts.WritingBenchURL
}

// WritingEvidenceSourceBackupPath provides the recovery artifact named by the
// source-scoped refresh command. It is intentionally deterministic to the call
// so a refresh receipt can name exactly one predecessor.
func WritingEvidenceSourceBackupPath(cacheDir, name string, now time.Time) string {
	return filepath.Join(cacheDir, "backups", now.UTC().Format("20060102T150405.000000000"), name+".json")
}

func LLMStatsStatsV1SourceBackupPath(cacheDir string, now time.Time) string {
	return filepath.Join(cacheDir, "backups", now.UTC().Format("20060102T150405.000000000"), "llm-stats.json")
}
