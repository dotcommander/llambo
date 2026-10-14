package cmd

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func executeLocalCommand(io *commandIO, o *evalsLocalRunCommand, prepared localCommandPlan, verified localVerification) error {
	m, in, plan := prepared.manifest, prepared.input, prepared.plan
	executor, closeExecutor, err := newTypedLocalExecutor(io.ErrOrStderr(), localExecutorSpec{suite: o.Suite, model: o.Model, referenceModel: o.ReferenceModel, featureModel: o.FeatureModel, baseURL: verified.baseURL, settingsPath: o.OMLXSettings, exclusiveApproved: o.ExclusiveOMLX, timeout: o.Timeout, verified: verified.model, verifiedReference: verified.reference})
	if err != nil {
		return err
	}
	defer closeExecutor()
	ledger, err := evals.OpenCampaignLedger(o.CampaignLedger, o.MaxCampaignCost)
	if err != nil {
		return err
	}
	store, err := evals.OpenLocalRunStore(o.OutputDir, m)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	owner, err := evals.NewCampaignReservationOwner(m.RunID, evals.LocalRunIdentityHash(m), o.OutputDir)
	if err != nil {
		return err
	}
	if _, err = ledger.Reserve(owner, plan.WorstCaseCostUSD, o.MaxRunCost); err != nil {
		return err
	}
	calls, runErr := evals.RunStoredLocalEvaluation(io.Context(), m, in, executor, store)
	report, scoreErr := evals.ScoreLocalCalls(m, calls)
	if scoreErr != nil {
		report.Status = partial1
		report.Errors = append(report.Errors, scoreErr.Error())
	}
	receipt, artifactErr := store.Finalize(report, time.Now())
	if err = recordLocalCampaignCompletion(ledger, owner, m.Identity.Pricing, calls, localCommandCompletion{receipt: receipt, runErr: runErr, scoreErr: scoreErr, artifactErr: artifactErr}); err != nil {
		return err
	}
	_, err = fmt.Fprintf(io.OutOrStdout(), "Local evaluation %s: %s\nReport: %s\n", receipt.RunID, receipt.Status, filepath.Join(o.OutputDir, "report.md"))
	return err
}
