package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/internal/costs"
	"github.com/dotcommander/llambo/providers"
)

func (cliOpts *invocationOptions) executePromptAgainstAllProvidersCore(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	// Load configuration
	globalCfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}

	// Get enabled providers
	enabled := providers.FilterEnabledProviders(globalCfg.Providers)
	if len(enabled) == 0 {
		return nil, fmt.Errorf("no enabled providers found in config")
	}
	cat, err := loadCatalogForPingFilter()
	if err != nil {
		return nil, err
	}
	enabled, skippedNonChat := filterTextChatTargets(errOut, cat, enabled, func(entry providers.ProviderEntry) (string, string) {
		return entry.Name, entry.Config.Model
	})
	writeTextChatSkippedSummary(errOut, skippedNonChat)
	if len(enabled) == 0 {
		return nil, fmt.Errorf("no enabled text chat-capable providers found in config")
	}

	results := runOrderedParallel(enabled, func(index int, entry providers.ProviderEntry) PromptResult {
		modelPrompt := cliOpts.promptTextForModel(promptText, systemPrompt, index, len(enabled), entry.Name, entry.Config.Model)
		return executePromptForProviderWithRun(ctx, entry, modelPrompt, systemPrompt, timeoutSecs, run)
	})
	return results, nil
}

func defaultExecutePromptAgainstAllProviders(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	return executePromptAgainstAllProvidersCore(context.Background(), os.Stderr, promptText, systemPrompt, timeoutSecs, nil)
}

var executePromptAgainstAllProviders = defaultExecutePromptAgainstAllProviders

func (cliOpts *invocationOptions) executePromptAgainstAllProvidersTo(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	if reflect.ValueOf(executePromptAgainstAllProviders).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstAllProviders).Pointer() {
		return executePromptAgainstAllProviders(promptText, systemPrompt, timeoutSecs)
	}
	return cliOpts.executePromptAgainstAllProvidersCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, nil)
}

func (cliOpts *invocationOptions) executePromptAgainstSelectedModelsCore(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	if cliOpts.promptModels == "" && cliOpts.promptProviders == "" && !cliOpts.promptFreeOnly && cliOpts.promptMaxOutputCost <= 0 {
		if run == nil {
			return cliOpts.executePromptAgainstAllProvidersTo(ctx, errOut, promptText, systemPrompt, timeoutSecs)
		}
		return cliOpts.executePromptAgainstAllProvidersCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
	}
	selector := cliOpts.promptModels
	if selector == "" && cliOpts.promptFreeOnly {
		selector = "free"
	}

	globalCfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	// Global cap from config applies when the per-call flag is unset (0).
	effectiveMaxOutputCost := cliOpts.promptMaxOutputCost
	effectiveIncludeUnknownCost := cliOpts.promptIncludeUnknownCost
	if effectiveMaxOutputCost == 0 {
		effectiveMaxOutputCost = globalCfg.MaxOutputCost
		if globalCfg.MaxOutputCost > 0 {
			effectiveIncludeUnknownCost = true
		}
	}
	costMap, err := costs.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load model costs: %w", err)
	}
	catPath, err := catalog.CatalogPath()
	if err != nil {
		return nil, err
	}
	selected, err := resolveTextChatSelection(errOut, catPath, globalCfg.Providers, costMap, catalog.SelectorOptions{
		Selector:           selector,
		ProviderFilter:     cliOpts.promptProviders,
		IncludeQuarantine:  cliOpts.promptIncludeQuarantine,
		FreeOnly:           cliOpts.promptFreeOnly,
		IncludeUnknownCost: effectiveIncludeUnknownCost,
		MaxOutputCost:      effectiveMaxOutputCost,
		Blocklist:          providers.NewBlocklist(globalCfg.Blocklist),
	})
	if err != nil {
		return nil, err
	}

	results := runOrderedParallel(selected, func(index int, target catalog.ModelTarget) PromptResult {
		modelPrompt := cliOpts.promptTextForModel(promptText, systemPrompt, index, len(selected), target.Provider, target.Model)
		result := executePromptForProviderWithRun(ctx, providers.ProviderEntry{Name: target.Provider, Config: target.Config}, modelPrompt, systemPrompt, timeoutSecs, run)
		result.CostStatus = string(target.CostStatus)
		result.InputCostPer1M = target.InputPer1M
		result.OutputCostPer1M = target.OutputPer1M
		result.EstimatedInputCost = estimateTokenCost(estimateTokens(systemPrompt)+estimateTokens(promptText), target.InputPer1M)
		result.EstimatedOutputCost = estimateTokenCost(target.Config.MaxTokens, target.OutputPer1M)
		return result
	})
	return results, nil
}

func defaultExecutePromptAgainstSelectedModels(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	return executePromptAgainstSelectedModelsCore(context.Background(), os.Stderr, promptText, systemPrompt, timeoutSecs, nil)
}

var executePromptAgainstSelectedModels = defaultExecutePromptAgainstSelectedModels

func (cliOpts *invocationOptions) executePromptAgainstSelectedModelsTo(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	if reflect.ValueOf(executePromptAgainstSelectedModels).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstSelectedModels).Pointer() {
		return executePromptAgainstSelectedModels(promptText, systemPrompt, timeoutSecs)
	}
	return cliOpts.executePromptAgainstSelectedModelsCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, nil)
}

func (cliOpts *invocationOptions) executePromptAgainstSelectedModelsWithRun(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	if reflect.ValueOf(executePromptAgainstSelectedModels).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstSelectedModels).Pointer() {
		return executePromptAgainstSelectedModels(promptText, systemPrompt, timeoutSecs)
	}
	return cliOpts.executePromptAgainstSelectedModelsCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
}

// executePromptForProvider sends the prompt to a single provider
func executePromptForProvider(entry providers.ProviderEntry, promptText, systemPrompt string, timeoutSecs int) PromptResult {
	return executePromptForProviderContext(context.Background(), entry, promptText, systemPrompt, timeoutSecs)
}

func executePromptForProviderContext(parent context.Context, entry providers.ProviderEntry, promptText, systemPrompt string, timeoutSecs int) PromptResult {
	return executePromptForProviderWithRun(parent, entry, promptText, systemPrompt, timeoutSecs, nil)
}

func executePromptForProviderWithRun(parent context.Context, entry providers.ProviderEntry, promptText, systemPrompt string, timeoutSecs int, run *promptRun) PromptResult {
	if run != nil {
		return run.execute(parent, entry, promptText, systemPrompt, timeoutSecs)
	}
	start := time.Now()

	// Create provider
	configs := map[string]providers.Config{
		entry.Name: entry.Config,
	}

	provider, err := providers.NewOpenAI(configs)
	if err != nil {
		return PromptResult{
			Provider: entry.Name,
			Model:    entry.Config.Model,
			Error:    fmt.Errorf("failed to create provider: %w", err),
			Latency:  time.Since(start),
		}
	}
	defer provider.Shutdown()

	// Execute chat request with context timeout
	timeout := time.Duration(timeoutSecs) * time.Second
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	result, err := provider.ChatWithInfoContext(ctx, systemPrompt, promptText)
	latency := time.Since(start)

	if err != nil {
		return PromptResult{
			Provider: entry.Name,
			Model:    entry.Config.Model,
			Error:    err,
			Latency:  latency,
		}
	}

	return PromptResult{
		Provider: entry.Name,
		Model:    result.Model,
		Response: result.Content,
		Latency:  latency,
		Error:    nil,
	}
}

// Scalar helpers retain their signatures with independent default options.
func executePromptAgainstSelectedModelsWithRun(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	return defaultInvocationOptions().executePromptAgainstSelectedModelsWithRun(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
}

func executePromptAgainstSelectedModelsTo(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	return defaultInvocationOptions().executePromptAgainstSelectedModelsTo(ctx, errOut, promptText, systemPrompt, timeoutSecs)
}

func executePromptAgainstSelectedModelsCore(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	return defaultInvocationOptions().executePromptAgainstSelectedModelsCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
}

func executePromptAgainstAllProvidersTo(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	return defaultInvocationOptions().executePromptAgainstAllProvidersTo(ctx, errOut, promptText, systemPrompt, timeoutSecs)
}

func executePromptAgainstAllProvidersCore(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	return defaultInvocationOptions().executePromptAgainstAllProvidersCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
}
