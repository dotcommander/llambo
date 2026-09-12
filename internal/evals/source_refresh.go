package evals

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type writingEvidenceSource struct {
	id      string
	name    string
	url     func(Options) string
	fetcher func(context.Context, Options) (sourceSnapshot, error)
}

func writingEvidenceSources() []writingEvidenceSource {
	return []writingEvidenceSource{
		{id: "writingbench", name: "WritingBench", url: func(opts Options) string { return opts.WritingBenchURL }, fetcher: fetchWritingBench},
		{id: "eqbench-creative-v3", name: "EQ-Bench Creative v3", url: func(opts Options) string { return opts.EQBenchCreativeURL }, fetcher: fetchEQBenchCreative},
		{id: WritingPrimaryID, name: "Lech Mazur Creative Story-Writing", url: func(opts Options) string { return opts.LechMazurWritingURL }, fetcher: fetchLechMazurWriting},
		{id: ArenaCreativeSourceID, name: "Arena Creative Writing", url: func(opts Options) string { return opts.ArenaCreativeURL }, fetcher: fetchArenaCreative},
	}
}

func writingEvidenceSourceFor(name string) (writingEvidenceSource, bool) {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, source := range writingEvidenceSources() {
		if source.id == name {
			return source, true
		}
	}
	return writingEvidenceSource{}, false
}

func defaultWritingEvidenceSourceNames() []string {
	sources := writingEvidenceSources()
	names := make([]string, 0, len(sources))
	for _, source := range sources {
		names = append(names, source.id)
	}
	return names
}

// RefreshSourceOptions configures caching behavior for evaluation source refreshes.
type RefreshSourceOptions struct {
	TTL   time.Duration // Minimum duration to retain cached source snapshots. If > 0, skips sources refreshed within TTL.
	Force bool          // If true, bypasses the TTL cache and forces upstream fetch.
}

// RefreshEvaluationSources is the single source-ingestion entry point. It owns
// source selection, validation, predecessor preservation, and cache publication
// so CLI callers do not need source-specific branches. Each cache replacement is
// atomic; a multi-source refresh validates every snapshot before publishing any.
func RefreshEvaluationSources(ctx context.Context, opts Options, names []string) ([]SourceStatus, []string, error) {
	return RefreshEvaluationSourcesWithOptions(ctx, opts, names, RefreshSourceOptions{})
}

// RefreshEvaluationSourcesWithOptions fetches or returns cached evaluation sources according to RefreshSourceOptions.
func RefreshEvaluationSourcesWithOptions(ctx context.Context, opts Options, names []string, refreshOpts RefreshSourceOptions) ([]SourceStatus, []string, error) {
	opts.applyDefaults()
	if len(names) == 0 {
		names = defaultWritingEvidenceSourceNames()
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
		if !refreshOpts.Force && refreshOpts.TTL > 0 {
			path := filepath.Join(opts.CacheDir, "llm-stats.json")
			if snapshot, err := readSnapshot(path); err == nil && strings.HasPrefix(snapshot.Method, "LLM Stats Stats v1 models") && snapshot.RegistryVersion == SourceRegistryVersion {
				if age := opts.Now().Sub(snapshot.FetchedAt); age >= 0 && age < refreshOpts.TTL && validateWritingEvidenceSnapshot("llm-stats-stats-v1", snapshot) == nil {
					return []SourceStatus{statusFor("LLM Stats", opts.LLMStatsStatsV1ModelsURL, snapshot, "cached", nil)}, nil, nil
				}
			}
		}
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

	cachedStatuses := make(map[string]SourceStatus)
	var toFetch []string

	for _, name := range normalized {
		if !refreshOpts.Force && refreshOpts.TTL > 0 {
			path := filepath.Join(opts.CacheDir, name+".json")
			if snapshot, err := readSnapshot(path); err == nil {
				if age := opts.Now().Sub(snapshot.FetchedAt); age >= 0 && age < refreshOpts.TTL && validateWritingEvidenceSnapshot(name, snapshot) == nil {
					sourceDef, _ := writingEvidenceSourceFor(name)
					cachedStatuses[name] = statusFor(sourceDef.name, sourceDef.url(opts), snapshot, "cached", nil)
					continue
				}
			}
		}
		toFetch = append(toFetch, name)
	}

	if len(toFetch) == 0 {
		statuses := make([]SourceStatus, 0, len(normalized))
		for _, name := range normalized {
			statuses = append(statuses, cachedStatuses[name])
		}
		return statuses, nil, nil
	}

	backups, err := preserveEvaluationSourceCaches(opts.CacheDir, toFetch, opts.Now())
	if err != nil {
		return nil, nil, err
	}
	fetchedStatuses, err := refreshWritingEvidenceSources(ctx, opts, toFetch)
	if err != nil {
		return nil, nil, err
	}

	for i, name := range toFetch {
		cachedStatuses[name] = fetchedStatuses[i]
	}

	statuses := make([]SourceStatus, 0, len(normalized))
	for _, name := range normalized {
		statuses = append(statuses, cachedStatuses[name])
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
	opts.Refresh = true
	type stagedSource struct {
		source   writingEvidenceSource
		snapshot sourceSnapshot
	}
	staged := make([]stagedSource, 0, len(names))
	for _, name := range names {
		source, ok := writingEvidenceSourceFor(name)
		if !ok {
			return nil, fmt.Errorf("unsupported evaluation source %q", name)
		}
		snapshot, err := source.fetcher(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", name, err)
		}
		snapshot.FetchedAt = opts.Now().UTC()
		if err := validateWritingEvidenceSnapshot(name, snapshot); err != nil {
			return nil, fmt.Errorf("validate %s: %w", name, err)
		}
		staged = append(staged, stagedSource{source: source, snapshot: snapshot})
	}
	statuses := make([]SourceStatus, 0, len(staged))
	for _, staged := range staged {
		if err := writeSnapshot(filepath.Join(opts.CacheDir, staged.source.id+".json"), staged.snapshot); err != nil {
			return nil, fmt.Errorf("cache %s: %w", staged.source.id, err)
		}
		statuses = append(statuses, statusFor(staged.source.name, staged.source.url(opts), staged.snapshot, "fetched", nil))
	}
	return statuses, nil
}

func validateWritingEvidenceSourceNames(names []string) error {
	for _, name := range names {
		if _, ok := writingEvidenceSourceFor(name); !ok {
			supported := defaultWritingEvidenceSourceNames()
			sort.Strings(supported)
			return fmt.Errorf("unsupported evaluation source %q (supported: %s)", name, strings.Join(supported, ", "))
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

// WritingEvidenceSourceBackupPath provides the recovery artifact named by the
// source-scoped refresh command. It is intentionally deterministic to the call
// so a refresh receipt can name exactly one predecessor.
func WritingEvidenceSourceBackupPath(cacheDir, name string, now time.Time) string {
	return filepath.Join(cacheDir, "backups", now.UTC().Format("20060102T150405.000000000"), name+".json")
}

func LLMStatsStatsV1SourceBackupPath(cacheDir string, now time.Time) string {
	return filepath.Join(cacheDir, "backups", now.UTC().Format("20060102T150405.000000000"), "llm-stats.json")
}
