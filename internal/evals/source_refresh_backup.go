package evals

import (
	"fmt"
	"os"
	"path/filepath"
)

func preserveStatsV1EvaluationSourceCache(opts Options, cachePath string) (string, error) {
	if _, err := os.Stat(cachePath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("inspect previous LLM Stats cache: %w", err)
	}
	backup := LLMStatsStatsV1SourceBackupPath(opts.CacheDir, opts.Now())
	if err := os.MkdirAll(filepath.Dir(backup), 0o750); err != nil {
		return "", fmt.Errorf("create LLM Stats backup directory: %w", err)
	}
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return "", fmt.Errorf("read previous LLM Stats cache: %w", err)
	}
	backupRoot, err := os.OpenRoot(filepath.Dir(backup))
	if err != nil {
		return "", fmt.Errorf("preserve previous LLM Stats cache: %w", err)
	}
	defer func() { _ = backupRoot.Close() }()
	if err := backupRoot.WriteFile(filepath.Base(backup), data, 0o600); err != nil {
		return "", fmt.Errorf("preserve previous LLM Stats cache: %w", err)
	}
	return backup, nil
}
