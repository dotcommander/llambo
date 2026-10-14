package evals

import "fmt"

func validateSourceLedgerIdentities(ledger SourceResearchLedger) (map[string]struct{}, error) {
	sourceIDs, err := validateSourceLedgerSourceIDs(ledger.Sources)
	if err != nil {
		return nil, err
	}
	if err := validateSourceLedgerClaimIDs(ledger.Claims, sourceIDs); err != nil {
		return nil, err
	}
	if err := validateSourceLedgerSearchIDs(ledger.Searches, sourceIDs); err != nil {
		return nil, err
	}
	return sourceIDs, nil
}

func validateSourceLedgerSourceIDs(sources []ResearchSource) (map[string]struct{}, error) {
	sourceIDs := make(map[string]struct{}, len(sources))
	for i, source := range sources {
		if err := validateResearchSource(source); err != nil {
			return nil, fmt.Errorf("source %d: %w", i+1, err)
		}
		if _, exists := sourceIDs[source.ID]; exists {
			return nil, fmt.Errorf("source research ledger has duplicate source %q", source.ID)
		}
		sourceIDs[source.ID] = struct{}{}
	}
	return sourceIDs, nil
}

func validateSourceLedgerClaimIDs(claims []ResearchClaim, sourceIDs map[string]struct{}) error {
	claimIDs := make(map[string]struct{}, len(claims))
	for i, claim := range claims {
		if _, exists := claimIDs[claim.ID]; exists {
			return fmt.Errorf("source research ledger has duplicate claim %q", claim.ID)
		}
		claimIDs[claim.ID] = struct{}{}
		if err := validateResearchClaim(claim, sourceIDs); err != nil {
			return fmt.Errorf("claim %d: %w", i+1, err)
		}
	}
	return nil
}

func validateSourceLedgerSearchIDs(searches []ResearchSearch, sourceIDs map[string]struct{}) error {
	searchIDs := make(map[string]struct{}, len(searches))
	for i, search := range searches {
		if _, exists := searchIDs[search.ID]; exists {
			return fmt.Errorf("source research ledger has duplicate search %q", search.ID)
		}
		searchIDs[search.ID] = struct{}{}
		if err := validateResearchSearch(search, sourceIDs); err != nil {
			return fmt.Errorf("search %d: %w", i+1, err)
		}
	}
	return nil
}
