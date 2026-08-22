package cmd

import (
	"fmt"

	"github.com/dotcommander/llambo/internal/evals"
)

func (c *evalsExportNormalizedCommand) Run(parent *evalsCommand, io *commandIO) error {
	for _, name := range []string{
		"refresh",
		"refresh-official-model-cards",
		"live-omlx",
		"format",
		"output",
		"export-prompts",
		"prompt-source",
		"prompt-limit",
		"discover-open-models",
		"discover-limit",
		"limit",
		"rank-by",
		"min-score",
		"max-output-price",
		"validation-receipts",
		"omlx-url",
	} {
		if io.FlagChanged(name) {
			return fmt.Errorf("--%s is not supported with evals export normalized", name)
		}
	}
	cacheDir, err := evaluationCacheDirectory(parent.CacheDir)
	if err != nil {
		return err
	}
	result, err := evals.Fetch(io.Context(), evals.Options{
		CacheDir:     cacheDir,
		Offline:      true,
		AllowPartial: parent.AllowPartial,
	})
	if err != nil {
		return err
	}
	report, err := evals.BuildReportWithProjectionFile(result, "matrix", parent.Projections)
	if err != nil {
		return err
	}
	manifest, err := evals.WriteNormalizedDataset(result, report, c.OutputDir)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(io.ErrOrStderr(), "Normalized dataset written to %s (%d sources, %d models, %d source observations)\n",
		c.OutputDir, manifest.RowCounts["sources.jsonl"], manifest.RowCounts["models.jsonl"], manifest.RowCounts["source_observations.jsonl"])
	return err
}
