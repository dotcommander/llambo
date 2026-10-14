package cmd

import (
	"errors"
	"fmt"

	"github.com/dotcommander/llambo/internal/evals"
)

type localCommandCompletion struct {
	receipt     evals.LocalReceipt
	runErr      error
	scoreErr    error
	artifactErr error
}

// recordLocalCampaignCompletion accounts for finalized calls before returning
// the run, scoring or artifact failure in its original precedence.
func recordLocalCampaignCompletion(ledger *evals.CampaignLedger, owner evals.CampaignReservationOwner, pricing evals.LocalPricing, calls []evals.LocalCall, completed localCommandCompletion) error {
	status := completed.receipt.Status
	if completed.runErr != nil || completed.scoreErr != nil || completed.artifactErr != nil {
		status = partial1
	}
	observed, observedErr := evals.ObservedLocalCost(pricing, calls)
	if observedErr != nil && completed.runErr == nil && completed.artifactErr == nil {
		return fmt.Errorf("derive observed campaign cost: %w", observedErr)
	}
	if err := ledger.RecordOwned(owner, observed, status); err != nil && completed.runErr == nil && completed.artifactErr == nil {
		return fmt.Errorf("record campaign cost: %w", err)
	}
	if err := errors.Join(completed.runErr, completed.scoreErr); err != nil {
		return err
	}
	return completed.artifactErr
}
