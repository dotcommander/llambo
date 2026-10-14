package evals

import (
	"errors"
)

func validateResearchSource(source ResearchSource) error {
	identity := validLedgerID(source.ID) && validLedgerURL(source.Locator) && validLedgerText(source.Owner) && validLedgerText(source.Title)
	provenance := validLedgerRevision(source.Revision) && sourceLedgerHash.MatchString(source.ContentSHA256) && validLedgerText(source.Methodology) && !source.AccessedAt.IsZero()
	admission := validArtifactVerification(source.ArtifactKind, source.Verification, source.Status) && validLedgerClass(source.SourceClass) && validIndependence(source.Independence) && validSourceStatus(source.Status)
	if !identity || !provenance || !admission {
		return errors.New("is incomplete or invalid")
	}
	return nil
}
