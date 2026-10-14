package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

type writingTargets struct {
	models []evals.WritingModelSpec
	judge  evals.WritingModelSpec
}
type writingEvidence struct{ class, source, note string }
type writingModelResolver struct {
	globalCfg            *providers.GlobalConfig
	costMap              map[string]costs.ModelCost
	cat                  *catalog.Catalog
	judgeMaxOutputTokens int
}

func resolveWritingModels(modelSelectors []string, judgeSelector string, judgeMaxOutputTokens int, pricingFile string) (writingTargets, map[string]providers.Config, error) {
	if len(modelSelectors) == 0 {
		return writingTargets{}, nil, errors.New("at least one exact --model is required")
	}
	globalCfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return writingTargets{}, nil, err
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return writingTargets{}, nil, fmt.Errorf("load model costs: %w", err)
	}
	if strings.TrimSpace(pricingFile) != "" {
		var overlay map[string]costs.ModelCost
		overlay, err = costs.Load(pricingFile)
		if err != nil {
			return writingTargets{}, nil, fmt.Errorf("load run pricing overlay: %w", err)
		}
		if costMap == nil {
			costMap = make(map[string]costs.ModelCost)
		}
		for key, value := range overlay {
			costMap[key] = value
		}
	}
	catPath, err := catalog.Path()
	if err != nil {
		return writingTargets{}, nil, err
	}
	cat, err := catalog.Load(catPath)
	if err != nil {
		return writingTargets{}, nil, fmt.Errorf("load catalog: %w", err)
	}
	resolver := writingModelResolver{globalCfg: globalCfg, costMap: costMap, cat: cat, judgeMaxOutputTokens: judgeMaxOutputTokens}
	models := make([]evals.WritingModelSpec, 0, len(modelSelectors))
	configs := make(map[string]providers.Config)
	seen := make(map[string]struct{})
	for _, selector := range modelSelectors {
		var spec evals.WritingModelSpec
		var cfg providers.Config
		spec, cfg, err = resolver.resolve(selector, false)
		if err != nil {
			return writingTargets{}, nil, err
		}
		if _, ok := seen[spec.ID()]; ok {
			return writingTargets{}, nil, fmt.Errorf("duplicate generation model %q", spec.ID())
		}
		seen[spec.ID()] = struct{}{}
		models = append(models, spec)
		configs[spec.ID()] = cfg
	}
	judge, judgeConfig, err := resolver.resolve(judgeSelector, true)
	if err != nil {
		return writingTargets{}, nil, err
	}
	configs[judge.ID()] = judgeConfig
	return writingTargets{models: models, judge: judge}, configs, nil
}

func writingRuntimeConfig(cfg providers.Config, model evals.WritingModelSpec) providers.Config {
	cfg.InputCostPM = model.InputPer1M
	cfg.OutputCostPM = model.OutputPer1M
	return cfg
}

func writingExternalEvidence(cat *catalog.Catalog, provider, model string) (writingEvidence, error) {
	entry := modelEntryForTarget(cat, provider, model)
	if task, evidence, ok := catalog.BestQualityEvidence(entry); ok {
		if writingExternalQualitySource(evidence.Source) {
			return writingEvidence{class: "exact", source: evidence.Source, note: task}, nil
		}
	}
	projection, ok, err := evals.ReviewedProjectionForArtifact(model)
	if err != nil {
		return writingEvidence{}, fmt.Errorf("load external projection evidence: %w", err)
	}
	if ok {
		return writingEvidence{class: "projection", source: projection.SourceKey, note: projection.Confidence + ": " + projection.Basis}, nil
	}
	return writingEvidence{class: "missing", note: "no reviewed exact or projected evidence"}, nil
}

func writingExternalQualitySource(source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	return strings.Contains(source, "llm_stats") || strings.Contains(source, "artificial_analysis")
}

func exactWritingModelSelector(selector string) bool {
	provider, model, ok := strings.Cut(strings.TrimSpace(selector), "/")
	return ok && strings.TrimSpace(provider) != "" && strings.TrimSpace(model) != ""
}

func (r writingModelResolver) resolve(selector string, judge bool) (evals.WritingModelSpec, providers.Config, error) {
	if !exactWritingModelSelector(selector) {
		return evals.WritingModelSpec{}, providers.Config{}, fmt.Errorf("model selector %q must be exact provider/model", selector)
	}
	targets, err := catalog.ResolveModels(r.cat, r.globalCfg.Providers, r.costMap, catalog.SelectorOptions{
		Selector:           selector,
		IncludeUnknownCost: true,
		Blocklist:          providers.NewBlocklist(r.globalCfg.Blocklist),
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
	if ok, reason := catalog.TextChatCapability(target.Provider, target.Model, modelEntryForTarget(r.cat, target.Provider, target.Model)); !ok {
		return evals.WritingModelSpec{}, providers.Config{}, fmt.Errorf("model %s/%s is not text-chat capable: %s", target.Provider, target.Model, reason)
	}
	maxTokens := target.Config.MaxTokens
	if judge {
		maxTokens = r.judgeMaxOutputTokens
	} else if maxTokens <= 0 {
		maxTokens = 4096
	}
	evidence, err := writingExternalEvidence(r.cat, target.Provider, target.Model)
	if err != nil {
		return evals.WritingModelSpec{}, providers.Config{}, err
	}
	spec := evals.WritingModelSpec{Provider: target.Provider, Model: target.Model, InputPer1M: target.InputPer1M, OutputPer1M: target.OutputPer1M, MaxOutputTokens: maxTokens, Workers: target.Config.GetWorkers(), EvidenceClass: evidence.class, EvidenceSource: evidence.source, EvidenceNote: evidence.note}
	cfg := writingRuntimeConfig(target.Config, spec)
	cfg.MaxTokens = maxTokens
	return spec, cfg, nil
}
