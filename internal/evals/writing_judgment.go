package evals

import (
	"context"
	"fmt"
	"time"
)

func executeWritingJudgment(ctx context.Context, execution writingExecution, job writingJudgmentJob) error {
	manifest, adapter, store := execution.manifest, execution.adapter, execution.store
	record, generation, criteria := job.record, job.generation, job.criteria
	key := writingCombinedJudgmentKey(generation.Key, generation.ContentSHA256, manifest.Identity.Judge.ID(), manifest.Identity.JudgePromptVersion)
	system, user, err := buildWritingAdapterJudgmentPrompt(adapter, record, generation.Content, criteria)
	if err != nil {
		return err
	}
	for attempt := store.NextJudgmentAttempt(key); attempt <= maxWritingJudgmentAttempts; attempt++ {
		complete, err := executeWritingJudgmentAttempt(ctx, execution, writingJudgmentAttempt{key: key, attempt: attempt, system: system, user: user, job: job})
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if complete {
			return nil
		}
	}
	return nil
}

func executeWritingJudgmentAttempt(ctx context.Context, execution writingExecution, input writingJudgmentAttempt) (bool, error) {
	manifest, adapter, executor, store, budget := execution.manifest, execution.adapter, execution.executor, execution.store, execution.budget
	key, attempt, system, user := input.key, input.attempt, input.system, input.user
	generation := input.job.generation
	settings, err := writingAdapterJudgmentSettings(adapter, manifest)
	if err != nil {
		return false, err
	}
	call := WritingExecutionCall{Kind: judgment1, Model: manifest.Identity.Judge, SystemPrompt: system, UserPrompt: user, MaxOutputTokens: manifest.Identity.Judge.MaxOutputTokens, Settings: settings}
	estimate := estimateWritingCallCost(system+"\n"+user, call.MaxOutputTokens, call.Model)
	if err := budget.reserve(estimate); err != nil {
		return false, err
	}
	if err := store.AppendDispatchIntent(WritingDispatchIntent{Kind: judgment1, Key: key, Attempt: attempt, CallSHA256: writingExecutionCallHash(call), DispatchedAt: time.Now().UTC()}); err != nil {
		return false, err
	}
	started := time.Now().UTC()
	result, callErr := executor.Execute(ctx, call)
	completed := time.Now().UTC()
	accountingCost := writingAccountingCost(result.Usage, estimate)
	budget.complete(estimate, accountingCost)
	judgment := WritingJudgmentRecord{
		Key: key, Attempt: attempt, GenerationKey: generation.Key, ResponseSHA256: generation.ContentSHA256,
		JudgeProvider: call.Model.Provider, JudgeModel: call.Model.Model,
		StartedAt: started, CompletedAt: completed, LatencyMS: completed.Sub(started).Milliseconds(),
		RawResponse: result.Content, ActualProvider: result.Provider, ActualModel: result.Model,
		FinishReason: result.FinishReason, Route: result.Route, Usage: result.Usage,
		AccountingCostUSD: accountingCost,
	}
	classifyWritingJudgment(adapter, &judgment, input.job, callErr)
	if err := store.AppendJudgment(judgment); err != nil {
		return false, err
	}
	return judgment.Status == success1, nil
}

func classifyWritingJudgment(adapter WritingBenchmarkAdapter, judgment *WritingJudgmentRecord, job writingJudgmentJob, callErr error) {
	generation, criteria := job.generation, job.criteria
	if callErr == nil && writingFinishReasonTruncated(judgment.FinishReason) {
		callErr = fmt.Errorf("judgment returned truncating finish reason %q", judgment.FinishReason)
	}
	if callErr != nil {
		judgment.Status, judgment.Error = failed1, callErr.Error()
	} else {
		results, err := parseWritingAdapterJudgment(adapter, judgment.RawResponse, generation.Content, criteria)
		if err != nil {
			judgment.Status, judgment.Error = failed1, err.Error()
		} else {
			judgment.Status = success1
			judgment.Results = results
		}
	}
}
