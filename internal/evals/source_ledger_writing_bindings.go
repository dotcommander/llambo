package evals

import (
	"errors"
	"strings"
)

func validateWritingBenchLedgerBindings(ledger SourceResearchLedger, source ResearchSource, revision string) error {
	claimFound, searchFound := false, false
	for _, claim := range ledger.Claims {
		if claim.ID == "writingbench-target-boundary" && claim.Kind == "direct" && containsString(claim.SupportingIDs, source.ID) && strings.Contains(claim.Claim, "no matching target-family row") {
			claimFound = true
		}
	}
	for _, search := range ledger.Searches {
		if search.ID == "writing-primary-ladder" && search.Revision == revision && containsString(search.SourceIDs, source.ID) {
			searchFound = true
		}
	}
	if !claimFound || !searchFound {
		return errors.New("WritingBench no-match claim or search is not bound to the verified identity snapshot")
	}
	return nil
}
