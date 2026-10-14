package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

type evalsWritingRunCommand struct {
	Input            string        `required:"" help:"Sealed writing prompt JSONL exported by llambo"`
	Benchmark        string        `required:"" enum:"writingbench,eqbench-creative-v3,prose-screen" help:"Benchmark adapter"`
	Model            string        `required:"" help:"One exact generation target as provider/model"`
	JudgeModel       string        `name:"judge-model" required:"" help:"Exact judge target as provider/model"`
	OutputDir        string        `name:"output-dir" required:"" help:"Explicit run directory for the manifest, ledgers, reports, and receipt"`
	CampaignLedger   string        `name:"campaign-ledger" required:"" help:"Shared campaign budget ledger JSON"`
	PricingFile      string        `name:"pricing-file" help:"Run-scoped explicit pricing CSV overlay; does not mutate the live catalog"`
	Iterations       int           `help:"Iterations: WritingBench and prose-screen require 1; EQ-Bench defaults to 3 (1..3)"`
	Concurrency      int           `default:"2" help:"Global worker limit (1..8; provider worker limits also apply)"`
	Timeout          time.Duration `default:"5m" help:"Per-call timeout"`
	JudgeTokens      int           `name:"judge-max-output-tokens" default:"8192" help:"Maximum judge output tokens (1..65536)"`
	GenerationTokens int           `name:"generation-max-output-tokens" help:"Override candidate generation output tokens (1..65536); omitted uses model config"`
	ReasoningEffort  string        `name:"reasoning-effort" default:"default" enum:"default,off,minimal,low,medium,high" help:"Candidate generation reasoning effort; off explicitly disables thinking, default leaves model settings unchanged"`
	ThinkingBudget   int           `name:"thinking-budget-tokens" help:"Candidate thinking-token budget (1..65536); omitted leaves the provider/model budget unchanged"`
	MaxRunCost       float64       `name:"max-run-cost" help:"Required positive worst-case USD budget with --execute"`
	MaxCampaignCost  float64       `name:"max-campaign-cost" help:"Total campaign USD cap; required with --execute"`
	LocalUseCase     string        `name:"local-use-case" help:"Specific reason external benchmark evidence is insufficient; required for local targets with --execute"`
	Execute          bool          `help:"Allow provider-backed generation and judging; omitted means provider-free dry run"`
}

func (c *evalsWritingRunCommand) Run(parent *evalsCommand, io *commandIO) error {
	for _, name := range []string{refresh1, refreshOfficialModelCards, format1, output1, exportPrompts1, promptSource1, promptLimit1, discoverOpenModels, discoverLimit1, "allow-partial", rankBy, offline1, "projections", minScore, validationReceipts, maxOutputPrice, omlxURL1, "no-omlx"} {
		if io.FlagChanged(name) {
			return fmt.Errorf("--%s is not supported with evals writing run", name)
		}
	}
	limit := parent.Limit
	if !io.FlagChanged("limit") {
		limit = 5
	}
	return runWritingEvaluationCommand(io, c, limit)
}

func runWritingEvaluationCommand(cmd *commandIO, options *evalsWritingRunCommand, limit int) error {
	if err := validateWritingRunOptions(cmd, options, limit); err != nil {
		return err
	}
	prepared, err := prepareWritingCommand(cmd, options, limit)
	if err != nil {
		return err
	}
	if !options.Execute {
		return writeWritingDryRun(cmd.OutOrStdout(), prepared.manifest, prepared.plan)
	}
	if err := validateWritingExecution(options, prepared.plan); err != nil {
		return err
	}
	return executeWritingCommand(cmd, options, prepared)
}

func writingJudgeTokenCap(adapter evals.WritingBenchmarkAdapter, requested int, explicit bool) int {
	if adapter.ID() == "prose-screen" && !explicit {
		return evals.DefaultProseEvaluationMaxTokens
	}
	return requested
}

func writingManifestRunID(manifest evals.WritingRunManifest) string {
	return evals.WritingRunIdentityHash(manifest)[:16]
}

func writingObservedCost(manifest evals.WritingRunManifest, generations []evals.WritingGenerationRecord, judgments []evals.WritingJudgmentRecord) *float64 {
	observed := 0.0
	for _, record := range generations {
		observed += record.Usage.CostUSD
		if (!record.Usage.Known || writingUsageCostUnknown(record.Usage)) && writingModelIsPaid(manifest.Identity.Models, record.Provider, record.Model) {
			return nil
		}
	}
	for _, record := range judgments {
		observed += record.Usage.CostUSD
		if (!record.Usage.Known || writingUsageCostUnknown(record.Usage)) && (manifest.Identity.Judge.InputPer1M > 0 || manifest.Identity.Judge.OutputPer1M > 0) {
			return nil
		}
	}
	return &observed
}

func writingUsageCostUnknown(usage evals.WritingUsage) bool {
	return !usage.CostKnown && (usage.CacheReadTokens > 0 || usage.CacheWriteTokens > 0)
}

func writingModelIsPaid(models []evals.WritingModelSpec, provider, model string) bool {
	for _, spec := range models {
		if spec.Provider == provider && spec.Model == model {
			return spec.InputPer1M > 0 || spec.OutputPer1M > 0
		}
	}
	return true
}

func isGPTProModel(selector string) bool {
	provider, model, ok := strings.Cut(strings.ToLower(strings.TrimSpace(selector)), "/")
	if !ok || provider != "openai" || !strings.HasPrefix(model, "gpt-") {
		return false
	}
	return strings.HasSuffix(model, "-pro") || strings.Contains(model, "-pro-")
}

func writeWritingDryRun(out io.Writer, manifest evals.WritingRunManifest, plan evals.WritingRunPlan) error {
	value := struct {
		Mode          string                   `json:"mode"`
		ProviderCalls int                      `json:"provider_calls"`
		Manifest      evals.WritingRunManifest `json:"manifest"`
		Plan          evals.WritingRunPlan     `json:"plan"`
	}{Mode: "dry-run", ProviderCalls: 0, Manifest: manifest, Plan: plan}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
