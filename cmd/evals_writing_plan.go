package cmd

import (
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

type writingCommandPlan struct {
	adapter  evals.WritingBenchmarkAdapter
	records  []evals.WritingPromptRecord
	manifest evals.WritingRunManifest
	plan     evals.WritingRunPlan
	configs  map[string]providers.Config
}

func prepareWritingCommand(cmd *commandIO, options *evalsWritingRunCommand, limit int) (writingCommandPlan, error) {
	adapter, err := evals.WritingAdapter(options.Benchmark)
	if err != nil {
		return writingCommandPlan{}, err
	}
	judgeTokens := writingJudgeTokenCap(adapter, options.JudgeTokens, cmd.FlagChanged("judge-max-output-tokens"))
	iterations, err := adapter.Iterations(options.Iterations)
	if err != nil {
		return writingCommandPlan{}, err
	}
	records, inputHash, err := evals.LoadWritingPromptFile(options.Input)
	if err != nil {
		return writingCommandPlan{}, err
	}
	if len(records) > limit {
		records = records[:limit]
	}
	if err = evals.ValidateWritingPromptRecords(records, adapter); err != nil {
		return writingCommandPlan{}, err
	}
	targets, configs, err := resolveWritingModels([]string{options.Model}, options.JudgeModel, judgeTokens, options.PricingFile)
	if err != nil {
		return writingCommandPlan{}, err
	}
	models, judge := targets.models, targets.judge
	if options.GenerationTokens > 0 {
		for i := range models {
			models[i].MaxOutputTokens = options.GenerationTokens
			cfg := configs[models[i].ID()]
			cfg.MaxTokens = options.GenerationTokens
			configs[models[i].ID()] = cfg
		}
	}
	manifest := evals.NewWritingRunManifest(evals.WritingRunManifestOptions{InputPath: options.Input, InputHash: inputHash, Records: records, Adapter: adapter, Models: models, Judge: judge, Iterations: iterations, Concurrency: options.Concurrency, Timeout: options.Timeout, MaxRunCost: options.MaxRunCost, Now: time.Now()})
	applyWritingReasoningSettings(&manifest.Identity.Generation, strings.TrimSpace(options.ReasoningEffort), options.ThinkingBudget)
	manifest.Identity.LocalUseCase = strings.TrimSpace(options.LocalUseCase)
	manifest.RunID = writingManifestRunID(manifest)
	plan, err := evals.PlanWritingRun(manifest, records, adapter)
	if err != nil {
		return writingCommandPlan{}, err
	}
	return writingCommandPlan{adapter: adapter, records: records, manifest: manifest, plan: plan, configs: configs}, nil
}
