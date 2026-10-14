package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

type localCommandPlan struct {
	input    evals.LocalInputManifest
	manifest evals.LocalRunManifest
	plan     evals.LocalRunPlan
}

func prepareLocalCommand(io *commandIO, o *evalsLocalRunCommand) (localCommandPlan, error) {
	if err := validateLocalRunOptions(o); err != nil {
		return localCommandPlan{}, err
	}
	in, inputHash, err := evals.LoadLocalInputManifest(o.Input, o.Suite)
	if err != nil {
		return localCommandPlan{}, err
	}
	if !io.FlagChanged("input-per-1m") || !io.FlagChanged("output-per-1m") {
		return localCommandPlan{}, errors.New("known pricing requires --input-per-1m and --output-per-1m (explicit zero is valid for local models)")
	}
	pricing := evals.LocalPricing{InputPer1M: o.InputPer1M, OutputPer1M: o.OutputPer1M, Known: true}
	if o.Execute {
		pricing, err = resolveLocalExecutionPricing(o.Model, pricing)
		if err != nil {
			return localCommandPlan{}, err
		}
	}
	m, err := evals.NewLocalRunManifest(evals.LocalManifestInput{InputPath: o.Input, InputSHA256: inputHash, Input: in, Model: o.Model, Reference: o.ReferenceModel, Feature: o.FeatureModel, Limit: o.Limit, Timeout: o.Timeout, Pricing: pricing, CreatedAt: time.Now()})
	if err != nil {
		return localCommandPlan{}, err
	}
	if strings.TrimSpace(o.LocalUseCase) != "" {
		evals.SealLocalRunUseCase(&m, o.LocalUseCase)
	}
	plan, err := evals.PlanLocalRun(m, in)
	if err != nil {
		return localCommandPlan{}, err
	}
	return localCommandPlan{input: in, manifest: m, plan: plan}, nil
}

func validateLocalExecution(o *evalsLocalRunCommand, plan evals.LocalRunPlan) error {
	if o.MaxRunCost <= 0 {
		return errors.New("--execute requires positive --max-run-cost")
	}
	if o.MaxCampaignCost <= 0 {
		return errors.New("--execute requires positive --max-campaign-cost")
	}
	if strings.TrimSpace(o.LocalUseCase) == "" {
		return errors.New("--execute requires a specific --local-use-case")
	}
	if plan.WorstCaseCostUSD > o.MaxRunCost+1e-12 {
		return fmt.Errorf("worst-case run estimate $%.6f exceeds --max-run-cost $%.6f", plan.WorstCaseCostUSD, o.MaxRunCost)
	}
	if strings.TrimSpace(o.CampaignLedger) == "" {
		return errors.New("--execute requires --campaign-ledger")
	}
	return nil
}

func validateLocalRunOptions(o *evalsLocalRunCommand) error {
	for _, selector := range []string{o.Model, o.ReferenceModel, o.FeatureModel} {
		if isGPTProModel(selector) {
			return fmt.Errorf("local evaluation of GPT Pro models is prohibited by user policy: %s", selector)
		}
	}
	if o.Concurrency != 1 {
		return errors.New("local runs require --concurrency 1")
	}
	if o.Limit < 1 || o.Timeout <= 0 || strings.TrimSpace(o.Model) == "" || filepath.Clean(o.OutputDir) == "." {
		return errors.New("positive limit, timeout, exact model, and explicit output directory are required")
	}
	return nil
}
