package evals

import (
	"fmt"
	"strings"
)

func validateCoverageTargetCells(target coverageCampaignTarget) error {
	if len(target.Cells) != len(categorySpecs) {
		return fmt.Errorf("coverage campaign target %q has %d cells, want %d", target.Key, len(target.Cells), len(categorySpecs))
	}
	for _, category := range categorySpecs {
		cell, ok := target.Cells[category.name]
		if !ok || !validCoverageStatus(cell.Status) || strings.TrimSpace(cell.Reason) == "" || strings.TrimSpace(cell.LastCheckedRevision) == "" {
			return fmt.Errorf("coverage campaign target %q category %q is incomplete", target.Key, category.name)
		}
	}
	return nil
}
