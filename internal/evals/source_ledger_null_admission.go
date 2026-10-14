package evals

import (
	"fmt"
)

func validateScopedNullCellReceipt(receipt ScopedNullReceipt, cell coverageCampaignCell, sourceIDs map[string]struct{}) error {
	key := coverageCellKey(receipt.ModelKey, receipt.Category)
	if receipt.Status != cell.Status || receipt.Reason != cell.Reason || receipt.LastCheckedRevision != cell.LastCheckedRevision {
		return fmt.Errorf("scoped-null receipt %s does not match frozen campaign status, reason, or revision", key)
	}
	if !validCoverageStatus(receipt.Status) || receipt.Status == present1 || !validLedgerText(receipt.NextCheck) {
		return fmt.Errorf("scoped-null receipt %s is incomplete or invalid", key)
	}
	if !equalStringSlices(receipt.OrderedSourceClasses, orderedPrimaryClasses(receipt.Category)) {
		return fmt.Errorf("scoped-null receipt %s has an incomplete or reordered source ladder", key)
	}
	if err := validateRequiredLedgerReferences(receipt.SourceIDs, sourceIDs, "receipt source"); err != nil {
		return fmt.Errorf("scoped-null receipt %s: %w", key, err)
	}
	return nil
}
