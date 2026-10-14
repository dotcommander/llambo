package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

func executeWritingCommand(cmd *commandIO, options *evalsWritingRunCommand, prepared writingCommandPlan) error {
	manifest, records, adapter := prepared.manifest, prepared.records, prepared.adapter
	configs, plan := prepared.configs, prepared.plan
	if err := verifyWritingOMLXModels(cmd.Context(), manifest, configs); err != nil {
		return err
	}
	ledger, err := evals.OpenCampaignLedger(options.CampaignLedger, options.MaxCampaignCost)
	if err != nil {
		return err
	}
	store, err := evals.OpenWritingRunStore(options.OutputDir, manifest)
	if err != nil {
		return err
	}
	owner, err := evals.NewCampaignReservationOwner(manifest.RunID, evals.WritingRunIdentityHash(manifest), options.OutputDir)
	if err != nil {
		return errors.Join(err, store.Close())
	}
	if _, err = ledger.Reserve(owner, plan.WorstCaseCostUSD, options.MaxRunCost); err != nil {
		return errors.Join(err, store.Close())
	}
	executor, err := newCommandWritingExecutor(cmd.ErrOrStderr(), configs, options.Timeout)
	if err != nil {
		return errors.Join(err, store.Close(), ledger.RecordOwned(owner, nil, partial1))
	}
	defer executor.Close()
	runErr := evals.RunWritingEvaluation(cmd.Context(), records, evals.WritingEvaluation{Manifest: manifest, Adapter: adapter, Executor: executor, Store: store})
	generations, judgments := store.Records()
	closeErr := store.Close()
	complete := runErr == nil && closeErr == nil
	receipt, artifactErr := evals.WriteWritingRunArtifacts(options.OutputDir, evals.WritingRunArtifactInput{Manifest: manifest, Adapter: adapter, Generations: generations, Judgments: judgments, Complete: complete, Now: time.Now()})
	status := receipt.Status
	if runErr != nil || closeErr != nil || artifactErr != nil {
		status = partial1
	}
	observedPtr := writingObservedCost(manifest, generations, judgments)
	ledgerErr := ledger.RecordOwned(owner, observedPtr, status)
	if err := errors.Join(runErr, closeErr, artifactErr, ledgerErr); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Writing evaluation %s: %s\nJudge execution: %s\nReport: %s\nObserved cost: $%.6f\n", receipt.RunID, receipt.Status, plan.JudgeExecution, filepath.Join(options.OutputDir, "report.md"), receipt.Report.ObservedCostUSD)
	return runErr
}
