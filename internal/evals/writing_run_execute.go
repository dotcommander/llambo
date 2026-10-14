package evals

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

// Repeated string literals consolidated by goconst.
const (
	failed1     = "failed"
	generation1 = "generation"
	judgment1   = "judgment"
)

type writingJudgmentJob struct {
	record     WritingPromptRecord
	generation WritingGenerationRecord
	criteria   []WritingCriterion
}

// WritingEvaluation binds execution dependencies to one sealed manifest.
type WritingEvaluation struct {
	Manifest WritingRunManifest
	Adapter  WritingBenchmarkAdapter
	Executor WritingExecutor
	Store    *WritingRunStore
}

type writingExecution struct {
	manifest WritingRunManifest
	adapter  WritingBenchmarkAdapter
	executor WritingExecutor
	store    *WritingRunStore
	budget   *writingBudget
}

type writingGenerationJob struct {
	record    WritingPromptRecord
	model     WritingModelSpec
	iteration int
}

type writingJudgmentAttempt struct {
	key          string
	attempt      int
	system, user string
	job          writingJudgmentJob
}

// RunWritingEvaluation resumes determinate generations and judgments within the sealed budget.
func RunWritingEvaluation(ctx context.Context, records []WritingPromptRecord, input WritingEvaluation) error {
	manifest, adapter, executor, store := input.Manifest, input.Adapter, input.Executor, input.Store
	if executor == nil || store == nil {
		return errors.New("writing executor and store are required")
	}
	if err := ValidateWritingPromptRecords(records, adapter); err != nil {
		return err
	}
	if err := store.CheckNoInDoubtDispatch(); err != nil {
		return err
	}
	budget := newWritingBudget(manifest.Identity.MaxRunCostUSD, store)
	execution := writingExecution{manifest: manifest, adapter: adapter, executor: executor, store: store, budget: budget}
	jobs := make([]writingGenerationJob, 0, len(records)*len(manifest.Identity.Models)*manifest.Identity.Iterations)
	for _, record := range records {
		for _, model := range manifest.Identity.Models {
			for iteration := 1; iteration <= manifest.Identity.Iterations; iteration++ {
				key := writingGenerationKey(adapter.ID(), record.ID, model.ID(), iteration)
				if _, ok := store.Generation(key); !ok && store.NextGenerationAttempt(key) <= maxWritingGenerationAttempts {
					jobs = append(jobs, writingGenerationJob{record: record, model: model, iteration: iteration})
				}
			}
		}
	}
	if err := runWritingJobs(ctx, manifest.Identity.Concurrency, jobs, func(ctx context.Context, job writingGenerationJob) error {
		return executeWritingGeneration(ctx, execution, job)
	}); err != nil {
		return err
	}

	judgmentJobs, err := pendingWritingJudgmentJobs(manifest, records, adapter, store)
	if err != nil {
		return err
	}
	if err := runWritingJobs(ctx, manifest.Identity.JudgeConcurrency, judgmentJobs, func(ctx context.Context, job writingJudgmentJob) error {
		return executeWritingJudgment(ctx, execution, job)
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

func writingPromptDomains(record WritingPromptRecord) (primary, secondary string) {
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
