package evals

import (
	"context"
	"path/filepath"
	"strings"
)

func normalizeEvaluationSourceNames(names []string) []string {
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
	return normalized
}

func refreshStatsV1WithCache(ctx context.Context, opts Options, refreshOpts RefreshSourceOptions) ([]SourceStatus, []string, error) {
	if status, ok := reusableStatsV1EvaluationSource(opts, refreshOpts); ok {
		return []SourceStatus{status}, nil, nil
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

func reusableEvaluationSource(opts Options, name string, refreshOpts RefreshSourceOptions) (SourceStatus, bool) {
	if refreshOpts.Force || refreshOpts.TTL <= 0 {
		return SourceStatus{}, false
	}
	snapshot, err := readSnapshot(filepath.Join(opts.CacheDir, name+".json"))
	if err != nil {
		return SourceStatus{}, false
	}
	age := opts.Now().Sub(snapshot.FetchedAt)
	if age < 0 || age >= refreshOpts.TTL || validateWritingEvidenceSnapshot(snapshot) != nil {
		return SourceStatus{}, false
	}
	sourceDef, _ := writingEvidenceSourceFor(name)
	return statusFor(sourceDef.name, sourceDef.url(opts), snapshot, cached1, nil), true
}

func reusableStatsV1EvaluationSource(opts Options, refreshOpts RefreshSourceOptions) (SourceStatus, bool) {
	if refreshOpts.Force || refreshOpts.TTL <= 0 {
		return SourceStatus{}, false
	}
	snapshot, err := readSnapshot(filepath.Join(opts.CacheDir, "llm-stats.json"))
	if err != nil || !strings.HasPrefix(snapshot.Method, "LLM Stats Stats v1 models") || snapshot.RegistryVersion != SourceRegistryVersion {
		return SourceStatus{}, false
	}
	age := opts.Now().Sub(snapshot.FetchedAt)
	if age < 0 || age >= refreshOpts.TTL || validateWritingEvidenceSnapshot(snapshot) != nil {
		return SourceStatus{}, false
	}
	return statusFor("LLM Stats", opts.LLMStatsStatsV1ModelsURL, snapshot, cached1, nil), true
}
