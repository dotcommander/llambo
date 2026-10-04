package cmd

import (
	"context"
	"fmt"
	"reflect"

	"github.com/dotcommander/llambo/providers"
)

const defaultFusionSystemPrompt = "You synthesize diverse model drafts into one final response. Preserve correct details, use only valuable novelty, resolve conflicts, omit duplication, and answer the user's original prompt directly."

var fusionDraftLenses = []string{
	"practical operator: concrete steps, trade-offs, and failure modes",
	"creative strategist: non-obvious options, reframes, and second-order effects",
	"skeptical reviewer: weak assumptions, risks, edge cases, and what could be wrong",
	"domain specialist: technical depth, constraints, and implementation details",
	"minimalist editor: simplest useful answer, priorities, and what to omit",
	"explorer: unusual but plausible ideas, adjacent approaches, and hidden opportunities",
}

// PromptFusion captures the final response produced from successful base model results.
type PromptFusion struct {
	Selector       string
	SourceCount    int
	OriginalPrompt string
	Consensus      FusionConsensus
	Result         PromptResult
}

// FusionConsensus describes agreement among source drafts. It is an agreement
// signal for the merge input, not a probability that the final answer is true.
type FusionConsensus struct {
	Label     string
	Score     float64
	Agreement float64
	Note      string
}

// PromptFusionControl captures the fusion model's direct answer to the original prompt.
type PromptFusionControl struct {
	Selector       string
	OriginalPrompt string
	Result         PromptResult
}

func (cliOpts *invocationOptions) executePromptFusionControlCore(ctx context.Context, promptText, systemPrompt string, timeoutSecs int, selector string, run *promptRun) (*PromptFusionControl, error) {
	target, err := cliOpts.resolveFirstFusionTarget(selector)
	if err != nil {
		return nil, err
	}
	result := executePromptForProviderWithRun(
		ctx,
		providers.ProviderEntry{Name: target.Provider, Config: target.Config},
		promptText,
		systemPrompt,
		timeoutSecs,
		run,
	)
	result.CostStatus = string(target.CostStatus)
	result.InputCostPer1M = target.InputPer1M
	result.OutputCostPer1M = target.OutputPer1M
	result.EstimatedInputCost = estimateTokenCost(estimateTokens(systemPrompt)+estimateTokens(promptText), target.InputPer1M)
	result.EstimatedOutputCost = estimateTokenCost(target.Config.MaxTokens, target.OutputPer1M)

	control := &PromptFusionControl{
		Selector:       normalizedFusionSelector(selector),
		OriginalPrompt: promptText,
		Result:         result,
	}
	if result.Error != nil {
		return control, fmt.Errorf("fusion control failed: %w", result.Error)
	}
	return control, nil
}

func defaultExecutePromptFusionControl(promptText, systemPrompt string, timeoutSecs int, selector string) (*PromptFusionControl, error) {
	return executePromptFusionControlCore(context.Background(), promptText, systemPrompt, timeoutSecs, selector, nil)
}

var executePromptFusionControl = defaultExecutePromptFusionControl

func (cliOpts *invocationOptions) executePromptFusionControlContext(ctx context.Context, promptText, systemPrompt string, timeoutSecs int, selector string) (*PromptFusionControl, error) {
	if reflect.ValueOf(executePromptFusionControl).Pointer() != reflect.ValueOf(defaultExecutePromptFusionControl).Pointer() {
		return executePromptFusionControl(promptText, systemPrompt, timeoutSecs, selector)
	}
	return cliOpts.executePromptFusionControlCore(ctx, promptText, systemPrompt, timeoutSecs, selector, nil)
}

func (cliOpts *invocationOptions) executePromptFusionControlWithRun(ctx context.Context, promptText, systemPrompt string, timeoutSecs int, selector string, run *promptRun) (*PromptFusionControl, error) {
	if reflect.ValueOf(executePromptFusionControl).Pointer() != reflect.ValueOf(defaultExecutePromptFusionControl).Pointer() {
		return executePromptFusionControl(promptText, systemPrompt, timeoutSecs, selector)
	}
	return cliOpts.executePromptFusionControlCore(ctx, promptText, systemPrompt, timeoutSecs, selector, run)
}

func (cliOpts *invocationOptions) executePromptFusionResponseCore(ctx context.Context, promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string, run *promptRun) (*PromptFusion, error) {
	successful := successfulPromptResults(results)
	if len(successful) == 0 {
		return nil, fmt.Errorf("cannot fuse responses: no successful model responses")
	}

	target, err := cliOpts.resolveFirstFusionTarget(selector)
	if err != nil {
		return nil, err
	}

	fusionPrompt := buildFusionPrompt(promptText, systemPrompt, successful)
	result := executePromptForProviderWithRun(
		ctx,
		providers.ProviderEntry{Name: target.Provider, Config: target.Config},
		fusionPrompt,
		defaultFusionSystemPrompt,
		timeoutSecs,
		run,
	)
	result.CostStatus = string(target.CostStatus)
	result.InputCostPer1M = target.InputPer1M
	result.OutputCostPer1M = target.OutputPer1M
	result.EstimatedInputCost = estimateTokenCost(estimateTokens(defaultFusionSystemPrompt)+estimateTokens(fusionPrompt), target.InputPer1M)
	result.EstimatedOutputCost = estimateTokenCost(target.Config.MaxTokens, target.OutputPer1M)

	fusion := &PromptFusion{
		Selector:       normalizedFusionSelector(selector),
		SourceCount:    len(successful),
		OriginalPrompt: promptText,
		Consensus:      computeFusionConsensus(successful),
		Result:         result,
	}
	if result.Error != nil {
		return fusion, fmt.Errorf("fusion failed: %w", result.Error)
	}
	return fusion, nil
}

func defaultExecutePromptFusionResponse(promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string) (*PromptFusion, error) {
	return executePromptFusionResponseCore(context.Background(), promptText, systemPrompt, results, timeoutSecs, selector, nil)
}

var executePromptFusionResponse = defaultExecutePromptFusionResponse

func (cliOpts *invocationOptions) executePromptFusionResponseContext(ctx context.Context, promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string) (*PromptFusion, error) {
	if reflect.ValueOf(executePromptFusionResponse).Pointer() != reflect.ValueOf(defaultExecutePromptFusionResponse).Pointer() {
		return executePromptFusionResponse(promptText, systemPrompt, results, timeoutSecs, selector)
	}
	return cliOpts.executePromptFusionResponseCore(ctx, promptText, systemPrompt, results, timeoutSecs, selector, nil)
}

func (cliOpts *invocationOptions) executePromptFusionResponseWithRun(ctx context.Context, promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string, run *promptRun) (*PromptFusion, error) {
	if reflect.ValueOf(executePromptFusionResponse).Pointer() != reflect.ValueOf(defaultExecutePromptFusionResponse).Pointer() {
		return executePromptFusionResponse(promptText, systemPrompt, results, timeoutSecs, selector)
	}
	return cliOpts.executePromptFusionResponseCore(ctx, promptText, systemPrompt, results, timeoutSecs, selector, run)
}

func outputPromptFusionControl(cmd *commandIO, control *PromptFusionControl) {
	if control == nil {
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "---")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "# Direct Fusion Model Control")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "| Field | Value |")
	fmt.Fprintln(out, "|-------|-------|")
	fmt.Fprintf(out, "| **Fuse Selector** | `%s` |\n", control.Selector)
	fmt.Fprintf(out, "| **Provider** | `%s` |\n", control.Result.Provider)
	fmt.Fprintf(out, "| **Model** | `%s` |\n", control.Result.Model)
	fmt.Fprintf(out, "| **Latency** | `%v` |\n", control.Result.Latency)
	if control.Result.CostStatus != "" {
		fmt.Fprintf(out, "| **Cost** | `%s` |\n", promptCostLabel(control.Result))
	}
	if control.Result.Error != nil {
		fmt.Fprintln(out, "| **Status** | ❌ FAILED |")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "### Error")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "```")
		fmt.Fprintf(out, "%v\n", control.Result.Error)
		fmt.Fprintln(out, "```")
		return
	}
	fmt.Fprintln(out, "| **Status** | ✅ SUCCESS |")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "### Response")
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "%s\n", control.Result.Response)
}

func outputPromptFusion(cmd *commandIO, fusion *PromptFusion) {
	if fusion == nil {
		return
	}
	out := cmd.OutOrStdout()
	fmt.Fprintln(out, "---")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "# Fused Response")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "| Field | Value |")
	fmt.Fprintln(out, "|-------|-------|")
	fmt.Fprintf(out, "| **Fuse Selector** | `%s` |\n", fusion.Selector)
	fmt.Fprintf(out, "| **Source Responses** | `%d` |\n", fusion.SourceCount)
	if fusion.Consensus.Label != "" {
		fmt.Fprintf(out, "| **Consensus** | `%s` (%.0f%% draft agreement; not a truth probability) |\n", fusion.Consensus.Label, fusion.Consensus.Score*100)
	}
	fmt.Fprintf(out, "| **Provider** | `%s` |\n", fusion.Result.Provider)
	fmt.Fprintf(out, "| **Model** | `%s` |\n", fusion.Result.Model)
	fmt.Fprintf(out, "| **Latency** | `%v` |\n", fusion.Result.Latency)
	if fusion.Result.CostStatus != "" {
		fmt.Fprintf(out, "| **Cost** | `%s` |\n", promptCostLabel(fusion.Result))
	}
	if fusion.Result.Error != nil {
		fmt.Fprintln(out, "| **Status** | ❌ FAILED |")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "### Error")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "```")
		fmt.Fprintf(out, "%v\n", fusion.Result.Error)
		fmt.Fprintln(out, "```")
		return
	}
	fmt.Fprintln(out, "| **Status** | ✅ SUCCESS |")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "### Response")
	if fusion.Consensus.Note != "" {
		fmt.Fprintln(out, "")
		fmt.Fprintf(out, "_%s_\n", fusion.Consensus.Note)
	}
	fmt.Fprintln(out, "")
	fmt.Fprintf(out, "%s\n", fusion.Result.Response)
}

// Scalar helpers retain their signatures with independent default options.
func executePromptFusionResponseWithRun(ctx context.Context, promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string, run *promptRun) (*PromptFusion, error) {
	return defaultInvocationOptions().executePromptFusionResponseWithRun(ctx, promptText, systemPrompt, results, timeoutSecs, selector, run)
}

func executePromptFusionResponseContext(ctx context.Context, promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string) (*PromptFusion, error) {
	return defaultInvocationOptions().executePromptFusionResponseContext(ctx, promptText, systemPrompt, results, timeoutSecs, selector)
}

func executePromptFusionResponseCore(ctx context.Context, promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string, run *promptRun) (*PromptFusion, error) {
	return defaultInvocationOptions().executePromptFusionResponseCore(ctx, promptText, systemPrompt, results, timeoutSecs, selector, run)
}

func executePromptFusionControlWithRun(ctx context.Context, promptText, systemPrompt string, timeoutSecs int, selector string, run *promptRun) (*PromptFusionControl, error) {
	return defaultInvocationOptions().executePromptFusionControlWithRun(ctx, promptText, systemPrompt, timeoutSecs, selector, run)
}

func executePromptFusionControlContext(ctx context.Context, promptText, systemPrompt string, timeoutSecs int, selector string) (*PromptFusionControl, error) {
	return defaultInvocationOptions().executePromptFusionControlContext(ctx, promptText, systemPrompt, timeoutSecs, selector)
}

func executePromptFusionControlCore(ctx context.Context, promptText, systemPrompt string, timeoutSecs int, selector string, run *promptRun) (*PromptFusionControl, error) {
	return defaultInvocationOptions().executePromptFusionControlCore(ctx, promptText, systemPrompt, timeoutSecs, selector, run)
}
