package evals

import (
	"errors"
	"fmt"
	"strings"
)

func validateWritingBenchCampaignBlock(ledger SourceResearchLedger, manifest coverageCampaignManifest) error {
	snapshot, err := loadWritingBenchIdentitySnapshot()
	if err != nil {
		return fmt.Errorf("load WritingBench identity snapshot: %w", err)
	}
	if err := WritingBenchSnapshotProvesNoCampaignMatches(snapshot, manifest); err != nil {
		return err
	}
	var source *ResearchSource
	for i := range ledger.Sources {
		if ledger.Sources[i].ID == "writingbench-score-xlsx" {
			source = &ledger.Sources[i]
			break
		}
	}
	if !writingBenchLedgerSourceMatchesSnapshot(source, snapshot) {
		return errors.New("WritingBench ledger source is not bound to the verified identity snapshot")
	}
	if err := validateWritingBenchLedgerBindings(ledger, *source, snapshot.Revision); err != nil {
		return err
	}
	return validateWritingBenchCampaignReceipts(ledger.ScopedNulls, manifest, *source, snapshot.Revision)
}

func validateWritingBenchCampaignReceipts(receipts []ScopedNullReceipt, manifest coverageCampaignManifest, source ResearchSource, revision string) error {
	for _, target := range manifest.Targets {
		for _, receipt := range receipts {
			if receipt.ModelKey != target.Key || receipt.Category != writing1 {
				continue
			}
			if receipt.Status != "externally-blocked" || receipt.LastCheckedRevision != revision || !containsString(receipt.SourceIDs, source.ID) {
				return fmt.Errorf("WritingBench receipt for %q is not bound to the verified no-match snapshot", target.Key)
			}
		}
	}
	return nil
}

func writingBenchLedgerSourceMatchesSnapshot(source *ResearchSource, snapshot WritingBenchIdentitySnapshot) bool {
	return source != nil && source.ArtifactKind == "normalized-identity-snapshot" && source.Verification == sha256Verified && source.Status == normalizedFrozen && source.Locator == snapshot.Locator && source.Revision == snapshot.Revision && source.ContentSHA256 == snapshot.ContentSHA256 && strings.Contains(source.Methodology, snapshot.Methodology)
}
