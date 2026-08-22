package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

// evalsLocalRunCommand is wired into evalsCommand by the owning command-tree
// change. It is kept here so local evaluation mechanics remain independently
// testable and dry runs never contact a provider.
type evalsLocalRunCommand struct {
	Suite           evals.LocalSuite `required:"" enum:"tools,asr,tts,embeddings,acceleration" help:"Task-specific local evaluation suite"`
	Input           string           `required:"" help:"Sealed local evaluation manifest JSON"`
	Model           string           `required:"" help:"Exact target as provider/model"`
	OutputDir       string           `name:"output-dir" required:"" help:"New run directory"`
	CampaignLedger  string           `name:"campaign-ledger" required:"" help:"Shared campaign budget ledger JSON"`
	Limit           int              `default:"1" help:"Maximum sealed cases"`
	Concurrency     int              `default:"1" help:"Worker limit; local runs require one"`
	Timeout         time.Duration    `default:"10m" help:"Per-call timeout"`
	MaxRunCost      float64          `name:"max-run-cost" help:"Worst-case USD run cap; required with --execute"`
	MaxCampaignCost float64          `name:"max-campaign-cost" help:"Total campaign USD cap; required with --execute"`
	LocalUseCase    string           `name:"local-use-case" help:"Specific reason external benchmark evidence is insufficient; required with --execute"`
	ReferenceModel  string           `name:"reference-model" help:"Exact baseline model for paired suites"`
	FeatureModel    string           `name:"feature-model" help:"Exact helper model for acceleration"`
	OMLXSettings    string           `name:"omlx-settings" help:"Exact model_settings.json path for an approved acceleration run"`
	ExclusiveOMLX   bool             `name:"exclusive-omlx-settings-approved" help:"Confirm the separately approved exclusive OMLX settings window"`
	InputPer1M      float64          `name:"input-per-1m" help:"Known USD input price per million tokens"`
	OutputPer1M     float64          `name:"output-per-1m" help:"Known USD output price per million tokens"`
	Execute         bool             `help:"Allow provider-backed calls; default is a provider-free dry run"`
}

func (c *evalsLocalRunCommand) Run(parent *evalsCommand, io *commandIO) error {
	if io.FlagChanged("refresh-official-model-cards") {
		return fmt.Errorf("--refresh-official-model-cards is not supported with evals local run")
	}
	// Kong binds the inherited evals --limit flag on the parent command. Copy
	// its explicitly positive value into the leaf runner before planning.
	if parent.Limit > 0 {
		c.Limit = parent.Limit
	}
	return runLocalEvaluationCommandAt(io, c, parent.OMLXURL)
}

func runLocalEvaluationCommand(io *commandIO, o *evalsLocalRunCommand) error {
	return runLocalEvaluationCommandAt(io, o, "http://127.0.0.1:8000")
}

func runLocalEvaluationCommandAt(io *commandIO, o *evalsLocalRunCommand, omlxURL string) error {
	for _, selector := range []string{o.Model, o.ReferenceModel, o.FeatureModel} {
		if isGPTProModel(selector) {
			return fmt.Errorf("local evaluation of GPT Pro models is prohibited by user policy: %s", selector)
		}
	}
	if o.Concurrency != 1 {
		return fmt.Errorf("local runs require --concurrency 1")
	}
	if o.Limit < 1 || o.Timeout <= 0 || strings.TrimSpace(o.Model) == "" || filepath.Clean(o.OutputDir) == "." {
		return fmt.Errorf("positive limit, timeout, exact model, and explicit output directory are required")
	}
	in, inputHash, err := evals.LoadLocalInputManifest(o.Input, o.Suite)
	if err != nil {
		return err
	}
	if !io.FlagChanged("input-per-1m") || !io.FlagChanged("output-per-1m") {
		return fmt.Errorf("known pricing requires --input-per-1m and --output-per-1m (explicit zero is valid for local models)")
	}
	pricing := evals.LocalPricing{InputPer1M: o.InputPer1M, OutputPer1M: o.OutputPer1M, Known: true}
	if o.Execute {
		pricing, err = resolveLocalExecutionPricing(o.Model, pricing)
		if err != nil {
			return err
		}
	}
	m, err := evals.NewLocalRunManifest(o.Input, inputHash, in, o.Model, o.ReferenceModel, o.FeatureModel, o.Limit, o.Timeout, pricing, time.Now())
	if err != nil {
		return err
	}
	if strings.TrimSpace(o.LocalUseCase) != "" {
		evals.SealLocalRunUseCase(&m, o.LocalUseCase)
	}
	plan, err := evals.PlanLocalRun(m, in)
	if err != nil {
		return err
	}
	if !o.Execute {
		return json.NewEncoder(io.OutOrStdout()).Encode(struct {
			Mode          string                 `json:"mode"`
			Manifest      evals.LocalRunManifest `json:"manifest"`
			Plan          evals.LocalRunPlan     `json:"plan"`
			ProviderCalls int                    `json:"provider_calls"`
		}{"dry-run", m, plan, 0})
	}
	if o.MaxRunCost <= 0 {
		return fmt.Errorf("--execute requires positive --max-run-cost")
	}
	if o.MaxCampaignCost <= 0 {
		return fmt.Errorf("--execute requires positive --max-campaign-cost")
	}
	if strings.TrimSpace(o.LocalUseCase) == "" {
		return fmt.Errorf("--execute requires a specific --local-use-case")
	}
	if plan.WorstCaseCostUSD > o.MaxRunCost+1e-12 {
		return fmt.Errorf("worst-case run estimate $%.6f exceeds --max-run-cost $%.6f", plan.WorstCaseCostUSD, o.MaxRunCost)
	}
	if strings.TrimSpace(o.CampaignLedger) == "" {
		return fmt.Errorf("--execute requires --campaign-ledger")
	}
	verified := ""
	verifiedReference := ""
	if strings.HasPrefix(o.Model, "omlx/") {
		if strings.TrimSpace(omlxURL) == "" {
			omlxURL = "http://127.0.0.1:8000"
		}
		modelID := strings.TrimPrefix(o.Model, "omlx/")
		verified, err = evals.VerifyOMLXExactModel(io.Context(), omlxURL, "", modelID, nil)
		if err != nil {
			timer := time.NewTimer(5 * time.Second)
			defer timer.Stop()
			select {
			case <-io.Context().Done():
				return io.Context().Err()
			case <-timer.C:
			}
			verified, err = evals.VerifyOMLXExactModel(io.Context(), omlxURL, "", modelID, nil)
			if err != nil {
				return fmt.Errorf("verify live OMLX model after retry: %w", err)
			}
		}
	}
	if o.Suite == evals.LocalSuiteTTS && strings.HasPrefix(o.ReferenceModel, "omlx/") {
		verifiedReference, err = evals.VerifyOMLXExactModel(io.Context(), omlxURL, "", strings.TrimPrefix(o.ReferenceModel, "omlx/"), nil)
		if err != nil {
			return fmt.Errorf("verify live TTS round-trip ASR model: %w", err)
		}
	}
	executor, closeExecutor, err := newTypedLocalExecutor(io.ErrOrStderr(), o.Suite, o.Model, o.ReferenceModel, o.FeatureModel, omlxURL, o.OMLXSettings, o.ExclusiveOMLX, o.Timeout, verified, verifiedReference)
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
	defer store.Close()
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
		report.Status = "partial"
		report.Errors = append(report.Errors, scoreErr.Error())
	}
	receipt, artifactErr := store.Finalize(report, time.Now())
	status := receipt.Status
	if runErr != nil || scoreErr != nil || artifactErr != nil {
		status = "partial"
	}
	observedPtr, observedErr := evals.ObservedLocalCost(m.Identity.Pricing, calls)
	if observedErr != nil && runErr == nil && artifactErr == nil {
		return fmt.Errorf("derive observed campaign cost: %w", observedErr)
	}
	if err := ledger.RecordOwned(owner, observedPtr, status); err != nil && runErr == nil && artifactErr == nil {
		return fmt.Errorf("record campaign cost: %w", err)
	}
	if err := errors.Join(runErr, scoreErr); err != nil {
		return err
	}
	if artifactErr != nil {
		return artifactErr
	}
	_, err = fmt.Fprintf(io.OutOrStdout(), "Local evaluation %s: %s\nReport: %s\n", receipt.RunID, receipt.Status, filepath.Join(o.OutputDir, "report.md"))
	return err
}
