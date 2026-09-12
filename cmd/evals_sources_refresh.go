package cmd

import (
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func (c *evalsSourcesRefreshCommand) Run(parent *evalsCommand, io *commandIO) error {
	for _, name := range []string{"refresh", "refresh-official-model-cards", "live-omlx", "offline", "format", "output"} {
		if io.FlagChanged(name) {
			return fmt.Errorf("--%s is not supported with evals sources refresh", name)
		}
	}
	cacheDir, err := evaluationCacheDirectory(parent.CacheDir)
	if err != nil {
		return err
	}
	statuses, backups, err := evals.RefreshEvaluationSourcesWithOptions(io.Context(), evals.Options{
		CacheDir:       cacheDir,
		LLMStatsAPIKey: os.Getenv("LLM_STATS_KEY"),
		Client:         &http.Client{Timeout: 45 * time.Second},
		Now:            time.Now,
	}, c.Names, evals.RefreshSourceOptions{
		TTL:   c.TTL,
		Force: c.Force,
	})
	if err != nil {
		return err
	}
	for _, backup := range backups {
		fmt.Fprintf(io.ErrOrStderr(), "Preserved previous source cache: %s\n", backup)
	}
	for _, status := range statuses {
		if status.Cache == "cached" {
			refreshedAgo := "never"
			if !status.FetchedAt.IsZero() {
				refreshedAgo = humanDuration(time.Since(status.FetchedAt)) + " ago"
			}
			fmt.Fprintf(io.OutOrStdout(), "%-32s ✓ %d models, %d observations (cached, refreshed %s, TTL %s), revision %s, sha %s\n",
				status.Name, status.Models, status.Observations, refreshedAgo, humanDuration(c.TTL), status.Version, status.ContentSHA)
			continue
		}
		fmt.Fprintf(io.OutOrStdout(), "%-32s ✓ %d models, %d observations, revision %s, sha %s\n", status.Name, status.Models, status.Observations, status.Version, status.ContentSHA)
	}
	return nil
}
