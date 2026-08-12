package cmd

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
	"github.com/dotcommander/llambo/providers"
)

type commandWritingExecutor struct {
	run     *promptRun
	configs map[string]providers.Config
	timeout time.Duration
}

func newCommandWritingExecutor(errOut io.Writer, configs map[string]providers.Config, timeout time.Duration) (*commandWritingExecutor, error) {
	run, err := newPromptRun(errOut)
	if err != nil {
		return nil, err
	}
	return &commandWritingExecutor{run: run, configs: configs, timeout: timeout}, nil
}

func (e *commandWritingExecutor) Execute(parent context.Context, call evals.WritingExecutionCall) (evals.WritingExecutionResult, error) {
	cfg, ok := e.configs[call.Model.ID()]
	if !ok {
		return evals.WritingExecutionResult{}, fmt.Errorf("no runtime config for %s", call.Model.ID())
	}
	cfg.MaxTokens = call.MaxOutputTokens
	if call.Settings.Temperature > 0 {
		cfg.Temperature = call.Settings.Temperature
	}
	cfg.ExtraBody = maps.Clone(cfg.ExtraBody)
	if cfg.ExtraBody == nil {
		cfg.ExtraBody = make(map[string]any)
	}
	for key, value := range call.Settings.ExtraBody {
		cfg.ExtraBody[key] = value
	}
	entry := providers.ProviderEntry{Name: call.Model.Provider, Config: cfg}
	ctx, cancel := context.WithTimeout(parent, e.timeout)
	defer cancel()
	release, err := e.run.acquireProvider(ctx, entry)
	if err != nil {
		return evals.WritingExecutionResult{}, err
	}
	defer release()
	provider, err := providers.NewOpenAIWithSharedRoutingMetrics(map[string]providers.Config{entry.Name: entry.Config}, e.run.metrics)
	if err != nil {
		return evals.WritingExecutionResult{}, fmt.Errorf("create writing provider: %w", err)
	}
	defer provider.Shutdown()
	result, callErr := provider.ChatWithInfoContext(ctx, call.SystemPrompt, call.UserPrompt)
	output := evals.WritingExecutionResult{
		Content:      result.Content,
		Provider:     result.Provider,
		Model:        result.Model,
		FinishReason: result.FinishReason,
	}
	if result.Usage != nil {
		output.Usage.PromptTokens = result.Usage.PromptTokens
		output.Usage.CompletionTokens = result.Usage.CompletionTokens
		output.Usage.TotalTokens = result.Usage.TotalTokens
		if result.Usage.Cost != nil {
			output.Usage.CostUSD = result.Usage.Cost.TotalCost
		} else {
			output.Usage.CostUSD = float64(output.Usage.PromptTokens)*call.Model.InputPer1M/1_000_000 + float64(output.Usage.CompletionTokens)*call.Model.OutputPer1M/1_000_000
		}
	}
	if result.Route != nil {
		output.Route = result.Route.Chosen
	}
	if callErr != nil {
		return output, callErr
	}
	if err := validateWritingExecutionResult(call.Model, result.Provider, result.Model, result.Content); err != nil {
		return output, err
	}
	return output, nil
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
	if e != nil && e.run != nil {
		e.run.close()
	}
}
