package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type writingJudgmentJob struct {
	record     WritingPromptRecord
	generation WritingGenerationRecord
	criteria   []WritingCriterion
}

func RunWritingEvaluation(ctx context.Context, manifest WritingRunManifest, records []WritingPromptRecord, adapter WritingBenchmarkAdapter, executor WritingExecutor, store *WritingRunStore) error {
	if executor == nil || store == nil {
		return fmt.Errorf("writing executor and store are required")
	}
	if err := ValidateWritingPromptRecords(records, adapter); err != nil {
		return err
	}
	if err := store.CheckNoInDoubtDispatch(); err != nil {
		return err
	}
	budget := newWritingBudget(manifest.Identity.MaxRunCostUSD, store)
	type generationJob struct {
		record    WritingPromptRecord
		model     WritingModelSpec
		iteration int
	}
	jobs := make([]generationJob, 0, len(records)*len(manifest.Identity.Models)*manifest.Identity.Iterations)
	for _, record := range records {
		for _, model := range manifest.Identity.Models {
			for iteration := 1; iteration <= manifest.Identity.Iterations; iteration++ {
				key := writingGenerationKey(adapter.ID(), record.ID, model.ID(), iteration)
				if _, ok := store.Generation(key); !ok && store.NextGenerationAttempt(key) <= maxWritingGenerationAttempts {
					jobs = append(jobs, generationJob{record: record, model: model, iteration: iteration})
				}
			}
		}
	}
	if err := runWritingJobs(ctx, manifest.Identity.Concurrency, jobs, func(ctx context.Context, job generationJob) error {
		return executeWritingGeneration(ctx, manifest, adapter, executor, store, budget, job.record, job.model, job.iteration)
	}); err != nil {
		return err
	}

	judgmentJobs, err := pendingWritingJudgmentJobs(manifest, records, adapter, store)
	if err != nil {
		return err
	}
	if err := runWritingJobs(ctx, manifest.Identity.JudgeConcurrency, judgmentJobs, func(ctx context.Context, job writingJudgmentJob) error {
		return executeWritingJudgment(ctx, manifest, adapter, executor, store, budget, job.record, job.generation, job.criteria)
	}); err != nil {
		return err
	}
	if writingRunComplete(manifest, records, adapter, store) {
		return nil
	}
	return ErrWritingRunIncomplete
}

func pendingWritingJudgmentJobs(manifest WritingRunManifest, records []WritingPromptRecord, adapter WritingBenchmarkAdapter, store *WritingRunStore) ([]writingJudgmentJob, error) {
	jobs := make([]writingJudgmentJob, 0)
	for _, record := range records {
		criteria, err := adapter.Criteria(record)
		if err != nil {
			return nil, err
		}
		for _, model := range manifest.Identity.Models {
			for iteration := 1; iteration <= manifest.Identity.Iterations; iteration++ {
				generation, ok := store.Generation(writingGenerationKey(adapter.ID(), record.ID, model.ID(), iteration))
				if !ok {
					continue
				}
				key := writingCombinedJudgmentKey(generation.Key, generation.ContentSHA256, manifest.Identity.Judge.ID(), manifest.Identity.JudgePromptVersion)
				if _, ok := store.Judgment(key); !ok && store.NextJudgmentAttempt(key) <= maxWritingJudgmentAttempts {
					jobs = append(jobs, writingJudgmentJob{record: record, generation: generation, criteria: criteria})
				}
			}
		}
	}
	return jobs, nil
}

func executeWritingGeneration(ctx context.Context, manifest WritingRunManifest, adapter WritingBenchmarkAdapter, executor WritingExecutor, store *WritingRunStore, budget *writingBudget, record WritingPromptRecord, model WritingModelSpec, iteration int) error {
	key := writingGenerationKey(adapter.ID(), record.ID, model.ID(), iteration)
	attempt := store.NextGenerationAttempt(key)
	call := WritingExecutionCall{Kind: "generation", Model: model, SystemPrompt: strings.TrimSpace(writingGenerationSystemPrompt), UserPrompt: record.Prompt, MaxOutputTokens: model.MaxOutputTokens, Settings: manifest.Identity.Generation}
	estimate := estimateWritingCallCost(call.SystemPrompt+"\n"+call.UserPrompt, call.MaxOutputTokens, model)
	if err := budget.reserve(estimate); err != nil {
		return err
	}
	if err := store.AppendDispatchIntent(WritingDispatchIntent{Kind: "generation", Key: key, Attempt: attempt, CallSHA256: writingExecutionCallHash(call), DispatchedAt: time.Now().UTC()}); err != nil {
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
		recordOut.Status, recordOut.Error = "failed", callErr.Error()
		recordOut.Content = result.Content
	} else {
		recordOut.Status, recordOut.Content = "success", result.Content
		recordOut.ContentSHA256 = writingHash(result.Content)
	}
	appendErr := store.AppendGeneration(recordOut)
	if callErr != nil {
		return errors.Join(callErr, appendErr)
	}
	return appendErr
}

func executeWritingJudgment(ctx context.Context, manifest WritingRunManifest, adapter WritingBenchmarkAdapter, executor WritingExecutor, store *WritingRunStore, budget *writingBudget, record WritingPromptRecord, generation WritingGenerationRecord, criteria []WritingCriterion) error {
	key := writingCombinedJudgmentKey(generation.Key, generation.ContentSHA256, manifest.Identity.Judge.ID(), manifest.Identity.JudgePromptVersion)
	system, user, err := buildWritingAdapterJudgmentPrompt(adapter, record, generation.Content, criteria)
	if err != nil {
		return err
	}
	for attempt := store.NextJudgmentAttempt(key); attempt <= maxWritingJudgmentAttempts; attempt++ {
		settings, err := writingAdapterJudgmentSettings(adapter, manifest)
		if err != nil {
			return err
		}
		call := WritingExecutionCall{Kind: "judgment", Model: manifest.Identity.Judge, SystemPrompt: system, UserPrompt: user, MaxOutputTokens: manifest.Identity.Judge.MaxOutputTokens, Settings: settings}
		estimate := estimateWritingCallCost(system+"\n"+user, call.MaxOutputTokens, call.Model)
		if err := budget.reserve(estimate); err != nil {
			return err
		}
		if err := store.AppendDispatchIntent(WritingDispatchIntent{Kind: "judgment", Key: key, Attempt: attempt, CallSHA256: writingExecutionCallHash(call), DispatchedAt: time.Now().UTC()}); err != nil {
			return err
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
		if callErr == nil && writingFinishReasonTruncated(result.FinishReason) {
			callErr = fmt.Errorf("judgment returned truncating finish reason %q", result.FinishReason)
		}
		if callErr != nil {
			judgment.Status, judgment.Error = "failed", callErr.Error()
		} else {
			results, err := parseWritingAdapterJudgment(adapter, result.Content, generation.Content, criteria)
			if err != nil {
				judgment.Status, judgment.Error = "failed", err.Error()
			} else {
				judgment.Status = "success"
				judgment.Results = results
			}
		}
		if err := store.AppendJudgment(judgment); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if judgment.Status == "success" {
			return nil
		}
	}
	return nil
}

func writingFinishReasonTruncated(reason string) bool {
	reason = strings.ToLower(strings.TrimSpace(reason))
	return strings.Contains(reason, "truncat") || strings.Contains(reason, "max_token") || strings.Contains(reason, "token_limit") || reason == "length"
}

func writingExecutionCallHash(call WritingExecutionCall) string {
	data, err := json.Marshal(call)
	if err != nil {
		// All fields are controlled values, so this only guards a future type
		// change. A stable sentinel still makes the intent visibly invalid to a
		// reviewer rather than omitting the request fingerprint.
		return writingHash("marshal-error:" + err.Error())
	}
	return writingHash(string(data))
}

func runWritingJobs[T any](ctx context.Context, concurrency int, jobs []T, fn func(context.Context, T) error) error {
	if concurrency < 1 {
		return fmt.Errorf("writing concurrency must be positive")
	}
	jobCh := make(chan T)
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for job := range jobCh {
				if workerCtx.Err() != nil {
					return
				}
				if err := fn(workerCtx, job); err != nil {
					select {
					case errCh <- err:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}
	for _, job := range jobs {
		select {
		case jobCh <- job:
		case <-workerCtx.Done():
			close(jobCh)
			workers.Wait()
			select {
			case runErr := <-errCh:
				return runErr
			default:
				return workerCtx.Err()
			}
		}
	}
	close(jobCh)
	workers.Wait()
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func writingPromptDomains(record WritingPromptRecord) (string, string) {
	object, err := sourceObject(record.SourceRecord)
	if err != nil {
		return "", ""
	}
	return writingJSONFieldString(object, "domain1"), writingJSONFieldString(object, "domain2")
}

func writingRunComplete(manifest WritingRunManifest, records []WritingPromptRecord, adapter WritingBenchmarkAdapter, store *WritingRunStore) bool {
	for _, record := range records {
		_, err := adapter.Criteria(record)
		if err != nil {
			return false
		}
		for _, model := range manifest.Identity.Models {
			for iteration := 1; iteration <= manifest.Identity.Iterations; iteration++ {
				generation, ok := store.Generation(writingGenerationKey(adapter.ID(), record.ID, model.ID(), iteration))
				if !ok {
					return false
				}
				if _, ok := store.Judgment(writingCombinedJudgmentKey(generation.Key, generation.ContentSHA256, manifest.Identity.Judge.ID(), manifest.Identity.JudgePromptVersion)); !ok {
					return false
				}
			}
		}
	}
	return true
}

type writingBudget struct {
	mu       sync.Mutex
	max      float64
	spent    float64
	reserved float64
}

func newWritingBudget(max float64, store *WritingRunStore) *writingBudget {
	generations, judgments := store.Records()
	spent := 0.0
	for _, record := range generations {
		spent += writingPersistedAccountingCost(record.AccountingCostUSD, record.Usage)
	}
	for _, record := range judgments {
		spent += writingPersistedAccountingCost(record.AccountingCostUSD, record.Usage)
	}
	return &writingBudget{max: max, spent: spent}
}

func (b *writingBudget) reserve(estimate float64) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.max <= 0 {
		return fmt.Errorf("writing run cost limit must be positive")
	}
	if b.spent+b.reserved+estimate > b.max+1e-12 {
		return fmt.Errorf("writing run cost gate: estimated admission $%.6f would exceed $%.6f limit", b.spent+b.reserved+estimate, b.max)
	}
	b.reserved += estimate
	return nil
}

func (b *writingBudget) complete(estimate, actual float64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.reserved -= estimate
	if b.reserved < 0 {
		b.reserved = 0
	}
	b.spent += actual
}

func writingAccountingCost(usage WritingUsage, estimate float64) float64 {
	if usage.Known && (usage.CostKnown || (usage.CacheReadTokens == 0 && usage.CacheWriteTokens == 0)) {
		return usage.CostUSD
	}
	return estimate
}

func writingPersistedAccountingCost(accountingCost float64, usage WritingUsage) float64 {
	if accountingCost > 0 {
		return accountingCost
	}
	return usage.CostUSD
}
