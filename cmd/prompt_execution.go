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

func executePromptAgainstAllProvidersCore(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
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
	filtered := enabled[:0]
	skippedNonChat := 0
	for _, entry := range enabled {
		if ok, reason := catalog.TextChatCapability(entry.Name, entry.Config.Model, modelEntryForTarget(cat, entry.Name, entry.Config.Model)); !ok {
			skippedNonChat++
			fmt.Fprintf(errOut, "Chat filter: skipped %s/%s (%s)\n", entry.Name, entry.Config.Model, reason)
			continue
		}
		filtered = append(filtered, entry)
	}
	enabled = filtered
	if skippedNonChat > 0 {
		fmt.Fprintf(errOut, "Chat filter: skipped %d non-text chat target(s)\n", skippedNonChat)
	}
	if len(enabled) == 0 {
		return nil, fmt.Errorf("no enabled text chat-capable providers found in config")
	}

	results := runOrderedParallel(enabled, func(index int, entry providers.ProviderEntry) PromptResult {
		modelPrompt := promptTextForModel(promptText, systemPrompt, index, len(enabled), entry.Name, entry.Config.Model)
		return executePromptForProviderWithRun(ctx, entry, modelPrompt, systemPrompt, timeoutSecs, run)
	})
	return results, nil
}

func defaultExecutePromptAgainstAllProviders(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	return executePromptAgainstAllProvidersCore(context.Background(), os.Stderr, promptText, systemPrompt, timeoutSecs, nil)
}

var executePromptAgainstAllProviders = defaultExecutePromptAgainstAllProviders

func executePromptAgainstAllProvidersTo(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	if reflect.ValueOf(executePromptAgainstAllProviders).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstAllProviders).Pointer() {
		return executePromptAgainstAllProviders(promptText, systemPrompt, timeoutSecs)
	}
	return executePromptAgainstAllProvidersCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, nil)
}

func executePromptAgainstSelectedModelsCore(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	if promptModels == "" && promptProviders == "" && !promptFreeOnly && promptMaxOutputCost <= 0 {
		if run == nil {
			return executePromptAgainstAllProvidersTo(ctx, errOut, promptText, systemPrompt, timeoutSecs)
		}
		return executePromptAgainstAllProvidersCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
	}
	selector := promptModels
	if selector == "" && promptFreeOnly {
		selector = "free"
	}

	globalCfg, err := providers.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	// Global cap from config applies when the per-call flag is unset (0).
	effectiveMaxOutputCost := promptMaxOutputCost
	effectiveIncludeUnknownCost := promptIncludeUnknownCost
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
	cat, err := catalog.Load(catPath)
	if err != nil {
		return nil, fmt.Errorf("load catalog: %w", err)
	}
	selected, err := catalog.ResolveModels(cat, globalCfg.Providers, costMap, catalog.SelectorOptions{
		Selector:           selector,
		ProviderFilter:     promptProviders,
		IncludeQuarantine:  promptIncludeQuarantine,
		FreeOnly:           promptFreeOnly,
		IncludeUnknownCost: effectiveIncludeUnknownCost,
		MaxOutputCost:      effectiveMaxOutputCost,
		Blocklist:          providers.NewBlocklist(globalCfg.Blocklist),
	})
	if err != nil {
		return nil, err
	}
	filtered := selected[:0]
	skippedNonChat := 0
	for _, target := range selected {
		if ok, reason := catalog.TextChatCapability(target.Provider, target.Model, modelEntryForTarget(cat, target.Provider, target.Model)); !ok {
			skippedNonChat++
			fmt.Fprintf(errOut, "Chat filter: skipped %s/%s (%s)\n", target.Provider, target.Model, reason)
			continue
		}
		filtered = append(filtered, target)
	}
	selected = filtered
	if skippedNonChat > 0 {
		fmt.Fprintf(errOut, "Chat filter: skipped %d non-text chat target(s)\n", skippedNonChat)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no text chat-capable models match selector %q", selector)
	}

	results := runOrderedParallel(selected, func(index int, target catalog.ModelTarget) PromptResult {
		modelPrompt := promptTextForModel(promptText, systemPrompt, index, len(selected), target.Provider, target.Model)
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

func executePromptAgainstSelectedModelsTo(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
	if reflect.ValueOf(executePromptAgainstSelectedModels).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstSelectedModels).Pointer() {
		return executePromptAgainstSelectedModels(promptText, systemPrompt, timeoutSecs)
	}
	return executePromptAgainstSelectedModelsCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, nil)
}

func executePromptAgainstSelectedModelsWithRun(ctx context.Context, errOut io.Writer, promptText, systemPrompt string, timeoutSecs int, run *promptRun) ([]PromptResult, error) {
	if reflect.ValueOf(executePromptAgainstSelectedModels).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstSelectedModels).Pointer() {
		return executePromptAgainstSelectedModels(promptText, systemPrompt, timeoutSecs)
	}
	return executePromptAgainstSelectedModelsCore(ctx, errOut, promptText, systemPrompt, timeoutSecs, run)
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
