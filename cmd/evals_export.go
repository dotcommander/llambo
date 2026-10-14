package cmd

import (
	"fmt"

	"github.com/dotcommander/llambo/internal/evals"
)

// Repeated string literals consolidated by goconst.
const (
	discoverLimit1            = "discover-limit"
	discoverOpenModels        = "discover-open-models"
	exportPrompts1            = "export-prompts"
	format1                   = "format"
	liveOMLX                  = "live-omlx"
	maxOutputPrice            = "max-output-price"
	minScore                  = "min-score"
	omlxURL1                  = "omlx-url"
	output1                   = "output"
	promptLimit1              = "prompt-limit"
	promptSource1             = "prompt-source"
	rankBy                    = "rank-by"
	refresh1                  = "refresh"
	refreshOfficialModelCards = "refresh-official-model-cards"
	validationReceipts        = "validation-receipts"
)

func (c *evalsExportNormalizedCommand) Run(parent *evalsCommand, io *commandIO) error {
	for _, name := range []string{
		refresh1,
		refreshOfficialModelCards,
		liveOMLX,
		format1,
		output1,
		exportPrompts1,
		promptSource1,
		promptLimit1,
		discoverOpenModels,
		discoverLimit1,
		"limit",
		rankBy,
		minScore,
		maxOutputPrice,
		validationReceipts,
		omlxURL1,
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
