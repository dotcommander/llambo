package cmd

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

type commandWritingExecutor struct {
	run        *promptRun
	configs    map[string]providers.Config
	timeout    time.Duration
	mu         sync.Mutex
	identity   map[string]*evals.IdentityTracker
	providerMu sync.Mutex
	providers  map[string]*providers.OpenAIProvider
}

func newCommandWritingExecutor(errOut io.Writer, configs map[string]providers.Config, timeout time.Duration) (*commandWritingExecutor, error) {
	run, err := newPromptRun(errOut)
	if err != nil {
		return nil, err
	}
	return &commandWritingExecutor{run: run, configs: configs, timeout: timeout, identity: make(map[string]*evals.IdentityTracker), providers: make(map[string]*providers.OpenAIProvider)}, nil
}

func (e *commandWritingExecutor) Execute(parent context.Context, call evals.WritingExecutionCall) (evals.WritingExecutionResult, error) {
	cfg, ok := e.configs[call.Model.ID()]
	if !ok {
		return evals.WritingExecutionResult{}, fmt.Errorf("no runtime config for %s", call.Model.ID())
	}
	entry := providers.ProviderEntry{Name: call.Model.Provider, Config: cfg}
	ctx, cancel := context.WithTimeout(parent, e.timeout)
	defer cancel()
	ctx = writingExecutionContext(ctx, cfg, call)
	release, err := e.run.acquireProvider(ctx, entry)
	if err != nil {
		return evals.WritingExecutionResult{}, err
	}
	defer release()
	provider, err := e.providerFor(call.Kind, entry)
	if err != nil {
		return evals.WritingExecutionResult{}, fmt.Errorf("create writing provider: %w", err)
	}
	result, callErr := provider.ChatWithInfoContext(ctx, call.SystemPrompt, call.UserPrompt)
	output := evals.WritingExecutionResult{
		Content:      result.Content,
		Provider:     result.Provider,
		Model:        result.Model,
		FinishReason: result.FinishReason,
	}
	if result.Usage != nil {
		output.Usage.Known = true
		output.Usage.PromptTokens = result.Usage.PromptTokens
		output.Usage.CompletionTokens = result.Usage.CompletionTokens
		output.Usage.TotalTokens = result.Usage.TotalTokens
		output.Usage.CacheReadTokens = result.Usage.CacheReadTokens
		output.Usage.CacheWriteTokens = result.Usage.CacheWriteTokens
		output.Usage.ReasoningTokens = result.Usage.ReasoningTokens
		if result.Usage.Cost != nil {
			output.Usage.CostUSD = result.Usage.Cost.TotalCost
			output.Usage.CostKnown = true
		} else {
			uncached := max(output.Usage.PromptTokens-output.Usage.CacheReadTokens, 0)
			output.Usage.CostUSD = float64(uncached)*call.Model.InputPer1M/1_000_000 + float64(output.Usage.CacheReadTokens)*call.Model.CacheReadPer1M/1_000_000 + float64(output.Usage.CacheWriteTokens)*call.Model.CacheWritePer1M/1_000_000 + float64(output.Usage.CompletionTokens)*call.Model.OutputPer1M/1_000_000
			output.Usage.CostKnown = (output.Usage.CacheReadTokens == 0 || call.Model.CacheReadPer1M > 0) && (output.Usage.CacheWriteTokens == 0 || call.Model.CacheWritePer1M > 0)
		}
	}
	if call.Model.InputPer1M == 0 && call.Model.OutputPer1M == 0 {
		output.Usage.Known = true
		output.Usage.CostKnown = true
	}
	if result.Route != nil {
		output.Route = result.Route.Chosen
	}
	if callErr != nil {
		return output, callErr
	}
	if err := e.validateExecutionResult(call.Model, result.Provider, result.Model, result.Content); err != nil {
		return output, err
	}
	return output, nil
}

func writingExecutionContext(parent context.Context, cfg providers.Config, call evals.WritingExecutionCall) context.Context {
	maxTokens := call.MaxOutputTokens
	var temperature *float64
	if call.Settings.TemperatureSet {
		temperature = &call.Settings.Temperature
	}
	ctx := providers.WithChatRequestOverrides(parent, &maxTokens, temperature, nil)
	overrides := maps.Clone(call.Settings.ExtraBody)
	if incoming, ok := overrides["chat_template_kwargs"].(map[string]any); ok {
		merged := make(map[string]any)
		for _, configured := range []map[string]any{cfg.ExtraBody, cfg.ExtraBodyByModel[cfg.Model]} {
			if kwargs, ok := configured["chat_template_kwargs"].(map[string]any); ok {
				for key, value := range kwargs {
					merged[key] = value
				}
			}
		}
		for key, value := range incoming {
			merged[key] = value
		}
		overrides["chat_template_kwargs"] = merged
	}
	return providers.WithJSONOverrides(ctx, overrides)
}

func (e *commandWritingExecutor) providerFor(kind string, entry providers.ProviderEntry) (*providers.OpenAIProvider, error) {
	key := kind + "\x00" + entry.Name + "\x00" + entry.Config.Model
	e.providerMu.Lock()
	defer e.providerMu.Unlock()
	if provider := e.providers[key]; provider != nil {
		return provider, nil
	}
	provider, err := providers.NewOpenAIWithSharedRoutingMetrics(map[string]providers.Config{entry.Name: entry.Config}, e.run.metrics)
	if err != nil {
		return nil, err
	}
	e.providers[key] = provider
	return provider, nil
}

func (e *commandWritingExecutor) validateExecutionResult(requested evals.WritingModelSpec, provider, model, content string) error {
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%s returned an empty response", requested.ID())
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	tracker := e.identity[requested.ID()]
	if tracker == nil {
		tracker = evals.NewIdentityTracker(requested.ID())
		e.identity[requested.ID()] = tracker
	}
	return tracker.Record(provider + "/" + model)
}

func validateWritingExecutionResult(requested evals.WritingModelSpec, provider, model, content string) error {
	if provider != requested.Provider || model != requested.Model {
		return fmt.Errorf("served identity %s/%s does not match requested %s", provider, model, requested.ID())
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("%s returned an empty response", requested.ID())
	}
	return nil
}

func (e *commandWritingExecutor) Close() {
	if e == nil {
		return
	}
	e.providerMu.Lock()
	for key, provider := range e.providers {
		provider.Shutdown()
		delete(e.providers, key)
	}
	e.providerMu.Unlock()
	if e.run != nil {
		e.run.close()
	}
}
