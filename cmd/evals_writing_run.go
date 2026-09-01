package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
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
	ReasoningEffort  string        `name:"reasoning-effort" default:"default" enum:"default,off,low,medium,high" help:"Candidate generation reasoning effort; off explicitly disables thinking, default leaves model settings unchanged"`
	ThinkingBudget   int           `name:"thinking-budget-tokens" help:"Candidate thinking-token budget (1..65536); omitted leaves the provider/model budget unchanged"`
	MaxRunCost       float64       `name:"max-run-cost" help:"Required positive worst-case USD budget with --execute"`
	MaxCampaignCost  float64       `name:"max-campaign-cost" help:"Total campaign USD cap; required with --execute"`
	LocalUseCase     string        `name:"local-use-case" help:"Specific reason external benchmark evidence is insufficient; required for local targets with --execute"`
	Execute          bool          `help:"Allow provider-backed generation and judging; omitted means provider-free dry run"`
}

func (c *evalsWritingRunCommand) Run(parent *evalsCommand, io *commandIO) error {
	for _, name := range []string{"refresh", "refresh-official-model-cards", "format", "output", "export-prompts", "prompt-source", "prompt-limit", "discover-open-models", "discover-limit", "allow-partial", "rank-by", "offline", "projections", "min-score", "validation-receipts", "max-output-price", "omlx-url", "no-omlx"} {
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
	for _, selector := range []string{options.Model, options.JudgeModel} {
		if isGPTProModel(selector) {
			return fmt.Errorf("local evaluation of GPT Pro models is prohibited by user policy: %s", selector)
		}
	}
	if limit < 1 || limit > 100 {
		return fmt.Errorf("--limit must be between 1 and 100")
	}
	if options.Concurrency < 1 || options.Concurrency > 8 {
		return fmt.Errorf("--concurrency must be between 1 and 8")
	}
	if options.Timeout <= 0 {
		return fmt.Errorf("--timeout must be positive")
	}
	if options.JudgeTokens < 1 || options.JudgeTokens > 65536 {
		return fmt.Errorf("--judge-max-output-tokens must be between 1 and 65536")
	}
	if options.GenerationTokens < 0 || options.GenerationTokens > 65536 || (options.GenerationTokens == 0 && cmd.FlagChanged("generation-max-output-tokens")) {
		return fmt.Errorf("--generation-max-output-tokens must be between 1 and 65536")
	}
	if options.ThinkingBudget < 0 || options.ThinkingBudget > 65536 || (options.ThinkingBudget == 0 && cmd.FlagChanged("thinking-budget-tokens")) {
		return fmt.Errorf("--thinking-budget-tokens must be between 1 and 65536")
	}
	if options.ThinkingBudget > 0 && options.ReasoningEffort == "off" {
		return fmt.Errorf("--thinking-budget-tokens cannot be combined with --reasoning-effort off")
	}
	cleanOutputDir := filepath.Clean(options.OutputDir)
	if cleanOutputDir == "." || strings.TrimSpace(options.OutputDir) == "" || filepath.Dir(cleanOutputDir) == cleanOutputDir {
		return fmt.Errorf("--output-dir must be an explicit directory")
	}
	adapter, err := evals.WritingAdapter(options.Benchmark)
	if err != nil {
		return err
	}
	judgeTokens, err := writingJudgeTokenCap(adapter, options.JudgeTokens, cmd.FlagChanged("judge-max-output-tokens"))
	if err != nil {
		return err
	}
	iterations, err := writingIterations(adapter.ID(), options.Iterations)
	if err != nil {
		return err
	}
	records, inputHash, err := evals.LoadWritingPromptFile(options.Input)
	if err != nil {
		return err
	}
	if len(records) > limit {
		records = records[:limit]
	}
	if err := evals.ValidateWritingPromptRecords(records, adapter); err != nil {
		return err
	}
	models, judge, configs, err := resolveWritingModels([]string{options.Model}, options.JudgeModel, judgeTokens, options.PricingFile)
	if err != nil {
		return err
	}
	if options.GenerationTokens > 0 {
		for i := range models {
			models[i].MaxOutputTokens = options.GenerationTokens
			cfg := configs[models[i].ID()]
			cfg.MaxTokens = options.GenerationTokens
			configs[models[i].ID()] = cfg
		}
	}
	manifest := evals.NewWritingRunManifest(options.Input, inputHash, records, adapter, models, judge, iterations, options.Concurrency, options.Timeout, options.MaxRunCost, time.Now())
	applyWritingReasoningSettings(&manifest.Identity.Generation, strings.TrimSpace(options.ReasoningEffort), options.ThinkingBudget)
	manifest.Identity.LocalUseCase = strings.TrimSpace(options.LocalUseCase)
	manifest.RunID = writingManifestRunID(manifest)
	plan, err := evals.PlanWritingRun(manifest, records, adapter)
	if err != nil {
		return err
	}
	if !options.Execute {
		return writeWritingDryRun(cmd.OutOrStdout(), manifest, plan)
	}
	if options.MaxRunCost <= 0 {
		return fmt.Errorf("--execute requires a positive --max-run-cost")
	}
	if options.MaxCampaignCost <= 0 {
		return fmt.Errorf("--execute requires a positive --max-campaign-cost")
	}
	if (strings.HasPrefix(options.Model, "omlx/") || strings.HasPrefix(options.JudgeModel, "omlx/")) && strings.TrimSpace(options.LocalUseCase) == "" {
		return fmt.Errorf("local execution requires a specific --local-use-case")
	}
	if plan.WorstCaseCostUSD > options.MaxRunCost+1e-12 {
		return fmt.Errorf("worst-case run estimate $%.6f exceeds --max-run-cost $%.6f", plan.WorstCaseCostUSD, options.MaxRunCost)
	}
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
	if _, err := ledger.Reserve(owner, plan.WorstCaseCostUSD, options.MaxRunCost); err != nil {
		return errors.Join(err, store.Close())
	}
	executor, err := newCommandWritingExecutor(cmd.ErrOrStderr(), configs, options.Timeout)
	if err != nil {
		return errors.Join(err, store.Close(), ledger.RecordOwned(owner, nil, "partial"))
	}
	defer executor.Close()
	runErr := evals.RunWritingEvaluation(cmd.Context(), manifest, records, adapter, executor, store)
	generations, judgments := store.Records()
	closeErr := store.Close()
	complete := runErr == nil && closeErr == nil
	receipt, artifactErr := evals.WriteWritingRunArtifacts(options.OutputDir, manifest, adapter, generations, judgments, complete, time.Now())
	status := receipt.Status
	if runErr != nil || closeErr != nil || artifactErr != nil {
		status = "partial"
	}
	observedPtr := writingObservedCost(manifest, generations, judgments)
	ledgerErr := ledger.RecordOwned(owner, observedPtr, status)
	if err := errors.Join(runErr, closeErr, artifactErr, ledgerErr); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Writing evaluation %s: %s\nJudge execution: %s\nReport: %s\nObserved cost: $%.6f\n", receipt.RunID, receipt.Status, plan.JudgeExecution, filepath.Join(options.OutputDir, "report.md"), receipt.Report.ObservedCostUSD)
	return runErr
}

func writingJudgeTokenCap(adapter evals.WritingBenchmarkAdapter, requested int, explicit bool) (int, error) {
	if adapter.ID() == "prose-screen" && !explicit {
		return evals.DefaultProseEvaluationMaxTokens, nil
	}
	return requested, nil
}

type writingCommandExecutor interface {
	evals.WritingExecutor
	Close()
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

func writingIterations(benchmark string, requested int) (int, error) {
	switch benchmark {
	case "writingbench":
		if requested == 0 || requested == 1 {
			return 1, nil
		}
		return 0, fmt.Errorf("WritingBench requires --iterations 1")
	case "eqbench-creative-v3":
		if requested == 0 {
			return 3, nil
		}
		if requested < 1 || requested > 3 {
			return 0, fmt.Errorf("EQ-Bench Creative v3 --iterations must be between 1 and 3")
		}
		return requested, nil
	case "prose-screen":
		if requested == 0 || requested == 1 {
			return 1, nil
		}
		return 0, fmt.Errorf("prose-screen requires --iterations 1")
	default:
		return 0, fmt.Errorf("unsupported writing benchmark %q", benchmark)
	}
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

func resolveWritingModels(modelSelectors []string, judgeSelector string, judgeMaxOutputTokens int, pricingFile string) ([]evals.WritingModelSpec, evals.WritingModelSpec, map[string]providers.Config, error) {
	if len(modelSelectors) == 0 {
		return nil, evals.WritingModelSpec{}, nil, fmt.Errorf("at least one exact --model is required")
	}
	globalCfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, evals.WritingModelSpec{}, nil, err
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return nil, evals.WritingModelSpec{}, nil, fmt.Errorf("load model costs: %w", err)
	}
	if strings.TrimSpace(pricingFile) != "" {
		overlay, err := costs.Load(pricingFile)
		if err != nil {
			return nil, evals.WritingModelSpec{}, nil, fmt.Errorf("load run pricing overlay: %w", err)
		}
		if costMap == nil {
			costMap = make(map[string]costs.ModelCost)
		}
		for key, value := range overlay {
			costMap[key] = value
		}
	}
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return nil, evals.WritingModelSpec{}, nil, err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return nil, evals.WritingModelSpec{}, nil, fmt.Errorf("load catalog: %w", err)
	}
	resolve := func(selector string, judge bool) (evals.WritingModelSpec, providers.Config, error) {
		if !exactWritingModelSelector(selector) {
			return evals.WritingModelSpec{}, providers.Config{}, fmt.Errorf("model selector %q must be exact provider/model", selector)
		}
		targets, err := catalog.ResolveModels(cat, globalCfg.Providers, costMap, catalog.SelectorOptions{
			Selector:           selector,
			IncludeUnknownCost: true,
			Blocklist:          providers.NewBlocklist(globalCfg.Blocklist),
		})
		if err != nil {
			return evals.WritingModelSpec{}, providers.Config{}, err
		}
		if len(targets) != 1 {
			return evals.WritingModelSpec{}, providers.Config{}, fmt.Errorf("model selector %q resolved to %d targets, want 1", selector, len(targets))
		}
		target := targets[0]
		if target.CostStatus == catalog.CostUnknown {
			return evals.WritingModelSpec{}, providers.Config{}, fmt.Errorf("model %s/%s has unknown input or output pricing", target.Provider, target.Model)
		}
		if ok, reason := catalog.TextChatCapability(target.Provider, target.Model, modelEntryForTarget(cat, target.Provider, target.Model)); !ok {
			return evals.WritingModelSpec{}, providers.Config{}, fmt.Errorf("model %s/%s is not text-chat capable: %s", target.Provider, target.Model, reason)
		}
		maxTokens := target.Config.MaxTokens
		if judge {
			maxTokens = judgeMaxOutputTokens
		} else if maxTokens <= 0 {
			maxTokens = 4096
		}
		evidenceClass, evidenceSource, evidenceNote, err := writingExternalEvidence(cat, target.Provider, target.Model)
		if err != nil {
			return evals.WritingModelSpec{}, providers.Config{}, err
		}
		spec := evals.WritingModelSpec{Provider: target.Provider, Model: target.Model, InputPer1M: target.InputPer1M, OutputPer1M: target.OutputPer1M, MaxOutputTokens: maxTokens, Workers: target.Config.GetWorkers(), EvidenceClass: evidenceClass, EvidenceSource: evidenceSource, EvidenceNote: evidenceNote}
		cfg := writingRuntimeConfig(target.Config, spec)
		cfg.MaxTokens = maxTokens
		return spec, cfg, nil
	}
	models := make([]evals.WritingModelSpec, 0, len(modelSelectors))
	configs := make(map[string]providers.Config)
	seen := make(map[string]struct{})
	for _, selector := range modelSelectors {
		spec, cfg, err := resolve(selector, false)
		if err != nil {
			return nil, evals.WritingModelSpec{}, nil, err
		}
		if _, ok := seen[spec.ID()]; ok {
			return nil, evals.WritingModelSpec{}, nil, fmt.Errorf("duplicate generation model %q", spec.ID())
		}
		seen[spec.ID()] = struct{}{}
		models = append(models, spec)
		configs[spec.ID()] = cfg
	}
	judge, judgeConfig, err := resolve(judgeSelector, true)
	if err != nil {
		return nil, evals.WritingModelSpec{}, nil, err
	}
	configs[judge.ID()] = judgeConfig
	return models, judge, configs, nil
}

func writingRuntimeConfig(cfg providers.Config, model evals.WritingModelSpec) providers.Config {
	cfg.InputCostPM = model.InputPer1M
	cfg.OutputCostPM = model.OutputPer1M
	return cfg
}

func writingExternalEvidence(cat *catalog.Catalog, provider, model string) (string, string, string, error) {
	entry := modelEntryForTarget(cat, provider, model)
	if task, evidence, ok := catalog.BestQualityEvidence(entry); ok {
		if writingExternalQualitySource(evidence.Source) {
			return "exact", evidence.Source, task, nil
		}
	}
	projection, ok, err := evals.ReviewedProjectionForArtifact(model)
	if err != nil {
		return "", "", "", fmt.Errorf("load external projection evidence: %w", err)
	}
	if ok {
		return "projection", projection.SourceKey, projection.Confidence + ": " + projection.Basis, nil
	}
	return "missing", "", "no reviewed exact or projected evidence", nil
}

func writingExternalQualitySource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return strings.Contains(source, "llm_stats") || strings.Contains(source, "artificial_analysis")
}

func exactWritingModelSelector(selector string) bool {
	provider, model, ok := strings.Cut(strings.TrimSpace(selector), "/")
	return ok && strings.TrimSpace(provider) != "" && strings.TrimSpace(model) != ""
}
