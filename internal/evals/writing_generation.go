package evals

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

func executeWritingGeneration(ctx context.Context, execution writingExecution, job writingGenerationJob) error {
	manifest, adapter, executor, store, budget := execution.manifest, execution.adapter, execution.executor, execution.store, execution.budget
	record, model, iteration := job.record, job.model, job.iteration
	key := writingGenerationKey(adapter.ID(), record.ID, model.ID(), iteration)
	attempt := store.NextGenerationAttempt(key)
	call := WritingExecutionCall{Kind: generation1, Model: model, SystemPrompt: strings.TrimSpace(writingGenerationSystemPrompt), UserPrompt: record.Prompt, MaxOutputTokens: model.MaxOutputTokens, Settings: manifest.Identity.Generation}
	estimate := estimateWritingCallCost(call.SystemPrompt+"\n"+call.UserPrompt, call.MaxOutputTokens, model)
	if err := budget.reserve(estimate); err != nil {
		return err
	}
	if err := store.AppendDispatchIntent(WritingDispatchIntent{Kind: generation1, Key: key, Attempt: attempt, CallSHA256: writingExecutionCallHash(call), DispatchedAt: time.Now().UTC()}); err != nil {
		return err
	}
	started := time.Now().UTC()
	result, callErr := executor.Execute(ctx, call)
	completed := time.Now().UTC()
	accountingCost := writingAccountingCost(result.Usage, estimate)
	budget.complete(estimate, accountingCost)
	recordOut := WritingGenerationRecord{
		Key: key, Attempt: attempt, BenchmarkID: adapter.ID(), PromptID: record.ID,
		Provider: model.Provider, Model: model.Model, Iteration: iteration,
		StartedAt: started, CompletedAt: completed, LatencyMS: completed.Sub(started).Milliseconds(),
		ActualProvider: result.Provider, ActualModel: result.Model, FinishReason: result.FinishReason, Route: result.Route, Usage: result.Usage,
		AccountingCostUSD: accountingCost,
	}
	recordOut.Domain1, recordOut.Domain2 = writingPromptDomains(record)
	if callErr == nil && writingFinishReasonTruncated(result.FinishReason) {
		callErr = fmt.Errorf("generation returned truncating finish reason %q", result.FinishReason)
	}
	if callErr != nil {
		recordOut.Status, recordOut.Error = failed1, callErr.Error()
		recordOut.Content = result.Content
	} else {
		recordOut.Status, recordOut.Content = success1, result.Content
		recordOut.ContentSHA256 = writingHash(result.Content)
	}
	appendErr := store.AppendGeneration(recordOut)
	if callErr != nil {
		return errors.Join(callErr, appendErr)
	}
	return appendErr
}
