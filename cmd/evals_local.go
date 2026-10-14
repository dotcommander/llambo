package cmd

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

// Repeated string literals consolidated by goconst.
const (
	partial1 = "partial"
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
	if io.FlagChanged(refreshOfficialModelCards) {
		return errors.New("--refresh-official-model-cards is not supported with evals local run")
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
	prepared, err := prepareLocalCommand(io, o)
	if err != nil {
		return err
	}
	m, plan := prepared.manifest, prepared.plan
	if !o.Execute {
		return json.NewEncoder(io.OutOrStdout()).Encode(struct {
			Mode          string                 `json:"mode"`
			Manifest      evals.LocalRunManifest `json:"manifest"`
			Plan          evals.LocalRunPlan     `json:"plan"`
			ProviderCalls int                    `json:"provider_calls"`
		}{"dry-run", m, plan, 0})
	}
	if err = validateLocalExecution(o, plan); err != nil {
		return err
	}
	verified, err := verifyLocalCommandModels(io, o, omlxURL)
	if err != nil {
		return err
	}
	return executeLocalCommand(io, o, prepared, verified)
}
