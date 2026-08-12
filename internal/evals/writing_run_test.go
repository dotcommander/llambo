package evals

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fixtureWritingExecutor struct {
	mu        sync.Mutex
	active    int
	maxActive int
	calls     int
}

type cancelWritingExecutor struct {
	cancel context.CancelFunc
	calls  int
}

func (e *cancelWritingExecutor) Execute(ctx context.Context, call WritingExecutionCall) (WritingExecutionResult, error) {
	e.calls++
	e.cancel()
	return WritingExecutionResult{}, context.Canceled
}

func (e *fixtureWritingExecutor) Execute(ctx context.Context, call WritingExecutionCall) (WritingExecutionResult, error) {
	e.mu.Lock()
	e.active++
	e.calls++
	if e.active > e.maxActive {
		e.maxActive = e.active
	}
	e.mu.Unlock()
	select {
	case <-ctx.Done():
		return WritingExecutionResult{}, ctx.Err()
	default:
	}
	e.mu.Lock()
	e.active--
	e.mu.Unlock()
	if call.Kind == "judgment" {
		return WritingExecutionResult{Content: `{"score":8,"reason":"fixture reason"}`, Provider: call.Model.Provider, Model: call.Model.Model}, nil
	}
	return WritingExecutionResult{Content: "fixture response", Provider: call.Model.Provider, Model: call.Model.Model}, nil
}

func (e *fixtureWritingExecutor) stats() (int, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.calls, e.maxActive
}

func TestWritingRunCancellationStopsDispatch(t *testing.T) {
	t.Parallel()
	first := writingBenchFixture()
	second := writingBenchFixture()
	second.ID = "2"
	second.Prompt = "Write a second note."
	second.SourceRecord = []byte(`{"query":"Write a second note.","checklist":["one","two","three","four","five"]}`)
	records := []WritingPromptRecord{first, second}
	adapter := WritingBenchAdapter{}
	model := WritingModelSpec{Provider: "p", Model: "writer", MaxOutputTokens: 64}
	judge := WritingModelSpec{Provider: "p", Model: "judge", MaxOutputTokens: 32}
	manifest := NewWritingRunManifest("input", "hash", records, adapter, []WritingModelSpec{model}, judge, 1, 1, time.Minute, 1, time.Unix(1, 0))
	store, err := OpenWritingRunStore(t.TempDir(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithCancel(t.Context())
	executor := &cancelWritingExecutor{cancel: cancel}
	if err := RunWritingEvaluation(ctx, manifest, records, adapter, executor, store); !errors.Is(err, context.Canceled) {
		t.Fatalf("run error = %v, want context canceled", err)
	}
	if executor.calls != 1 {
		t.Fatalf("calls after cancellation = %d, want 1", executor.calls)
	}
}

func TestWritingRunBoundedAndResumable(t *testing.T) {
	t.Parallel()
	records := []WritingPromptRecord{writingBenchFixture()}
	adapter := WritingBenchAdapter{}
	model := WritingModelSpec{Provider: "p", Model: "writer", InputPer1M: 1, OutputPer1M: 2, MaxOutputTokens: 64, Workers: 2}
	judge := WritingModelSpec{Provider: "p", Model: "judge", InputPer1M: 1, OutputPer1M: 2, MaxOutputTokens: 32, Workers: 2}
	manifest := NewWritingRunManifest("input", "hash", records, adapter, []WritingModelSpec{model}, judge, 1, 2, time.Minute, 1, time.Unix(1, 0))
	store, err := OpenWritingRunStore(t.TempDir(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	executor := &fixtureWritingExecutor{}
	if err := RunWritingEvaluation(t.Context(), manifest, records, adapter, executor, store); err != nil {
		t.Fatal(err)
	}
	calls, maxActive := executor.stats()
	if maxActive > 2 {
		t.Fatalf("max concurrency = %d, want <= 2", maxActive)
	}
	if calls != 6 {
		t.Fatalf("calls = %d, want 1 generation + 5 judgments", calls)
	}
	generations, judgments := store.Records()
	for _, cost := range generations {
		if cost.Usage.CostUSD <= 0 {
			t.Fatal("generation without provider usage did not persist its conservative cost estimate")
		}
	}
	for _, cost := range judgments {
		if cost.Usage.CostUSD <= 0 {
			t.Fatal("judgment without provider usage did not persist its conservative cost estimate")
		}
	}
	if err := RunWritingEvaluation(t.Context(), manifest, records, adapter, executor, store); err != nil {
		t.Fatal(err)
	}
	calls, _ = executor.stats()
	if calls != 6 {
		t.Fatalf("resume made provider calls: %d", calls)
	}
}

func TestPlanWritingRunWorstCase(t *testing.T) {
	t.Parallel()
	records := []WritingPromptRecord{writingBenchFixture()}
	adapter := WritingBenchAdapter{}
	model := WritingModelSpec{Provider: "p", Model: "writer", InputPer1M: 1, OutputPer1M: 2, MaxOutputTokens: 64}
	judge := WritingModelSpec{Provider: "p", Model: "judge", InputPer1M: 1, OutputPer1M: 2, MaxOutputTokens: 32}
	manifest := NewWritingRunManifest("input", "hash", records, adapter, []WritingModelSpec{model}, judge, 1, 2, time.Minute, 1, time.Unix(1, 0))
	plan, err := PlanWritingRun(manifest, records, adapter)
	if err != nil {
		t.Fatal(err)
	}
	if plan.GenerationCalls != 1 || plan.JudgmentCalls != 5 || plan.WorstCaseProviderCalls != 16 || plan.WorstCaseCostUSD <= 0 {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestWritingJudgmentKeyIncludesGenerationIdentity(t *testing.T) {
	t.Parallel()
	one := writingJudgmentKey("generation-1", "same-response", "criterion", "judge", "prompt-v1")
	two := writingJudgmentKey("generation-2", "same-response", "criterion", "judge", "prompt-v1")
	if one == two {
		t.Fatal("judgment keys collided across generations")
	}
}

func TestObservedWritingCostUsesEstimateWhenUsageMissing(t *testing.T) {
	t.Parallel()
	if got := observedWritingCost(0, 1.25); got != 1.25 {
		t.Fatalf("observed cost = %v, want 1.25", got)
	}
	if got := observedWritingCost(0.5, 1.25); got != 0.5 {
		t.Fatalf("observed cost = %v, want 0.5", got)
	}
}
