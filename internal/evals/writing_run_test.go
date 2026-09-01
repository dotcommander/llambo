package evals

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

type recordingProseScreenExecutor struct {
	calls            []WritingExecutionCall
	generationFinish string
	judgmentFinish   string
}

func (e *recordingProseScreenExecutor) Execute(_ context.Context, call WritingExecutionCall) (WritingExecutionResult, error) {
	e.calls = append(e.calls, call)
	if call.Kind == "generation" {
		finish := e.generationFinish
		if finish == "" {
			finish = "stop"
		}
		return WritingExecutionResult{Content: proseInput().Prose, Provider: call.Model.Provider, Model: call.Model.Model, FinishReason: finish}, nil
	}
	data, _ := json.Marshal(validProseEvaluation())
	finish := e.judgmentFinish
	if finish == "" {
		finish = "stop"
	}
	return WritingExecutionResult{Content: string(data), Provider: call.Model.Provider, Model: call.Model.Model, FinishReason: finish}, nil
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
		return WritingExecutionResult{Content: `{"results":[{"criterion_id":"checklist-1","score":8,"reason":"fixture"},{"criterion_id":"checklist-2","score":8,"reason":"fixture"},{"criterion_id":"checklist-3","score":8,"reason":"fixture"},{"criterion_id":"checklist-4","score":8,"reason":"fixture"},{"criterion_id":"checklist-5","score":8,"reason":"fixture"}]}`, Provider: call.Model.Provider, Model: call.Model.Model}, nil
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

func TestProseScreenRunUsesAdapterOwnedSchemaAndControls(t *testing.T) {
	adapter := ProseScreenAdapter{}
	record := WritingPromptRecord{BenchmarkID: adapter.ID(), ID: "case", Prompt: "Write a deployment note.", SourceRecord: []byte(`{"prompt":"Write a deployment note.","task":"Explain deployment safety.","reader_profile":"An operator."}`)}
	model := WritingModelSpec{Provider: "p", Model: "writer", MaxOutputTokens: 64}
	judge := WritingModelSpec{Provider: "p", Model: "judge", MaxOutputTokens: DefaultProseEvaluationMaxTokens}
	manifest := NewWritingRunManifest("input", "hash", []WritingPromptRecord{record}, adapter, []WritingModelSpec{model}, judge, 1, 1, time.Minute, 1, time.Unix(1, 0))
	store, err := OpenWritingRunStore(t.TempDir(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	executor := &recordingProseScreenExecutor{}
	if err := RunWritingEvaluation(t.Context(), manifest, []WritingPromptRecord{record}, adapter, executor, store); err != nil {
		t.Fatal(err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("calls=%d", len(executor.calls))
	}
	for _, call := range executor.calls {
		if !call.Settings.TemperatureSet || call.Settings.Temperature != 0 {
			t.Fatalf("call settings=%#v", call.Settings)
		}
	}
	judgment := executor.calls[1]
	if judgment.MaxOutputTokens != DefaultProseEvaluationMaxTokens {
		t.Fatalf("judge tokens=%d", judgment.MaxOutputTokens)
	}
	generationConfig, ok := judgment.Settings.ExtraBody["generationConfig"].(map[string]any)
	if !ok || generationConfig["responseMimeType"] != "application/json" {
		t.Fatalf("settings=%#v", judgment.Settings)
	}
	schemaJSON, err := json.Marshal(generationConfig["responseSchema"])
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"$schema", "$id", "definitions", "additionalProperties", "$ref"} {
		if strings.Contains(string(schemaJSON), absent) {
			t.Fatalf("projected schema retained %q: %s", absent, schemaJSON)
		}
	}
	for _, required := range []string{"depth_and_development", "applicable", "evidence", "minimum", "maximum", "required"} {
		if !strings.Contains(string(schemaJSON), required) {
			t.Fatalf("projected schema lost %q: %s", required, schemaJSON)
		}
	}
	if strings.Contains(string(schemaJSON), "criterion_id") {
		t.Fatalf("schema=%s", schemaJSON)
	}
	generations, judgments := store.Records()
	if len(generations) != 1 || generations[0].Status != "success" || len(judgments) != 1 || judgments[0].Status != "success" || len(judgments[0].Results) != 6 {
		t.Fatalf("records=%#v %#v", generations, judgments)
	}
}

func TestWritingFinishReasonTruncated(t *testing.T) {
	for _, reason := range []string{"length", "max_tokens", "truncated", "token_limit"} {
		if !writingFinishReasonTruncated(reason) {
			t.Fatalf("%q was accepted", reason)
		}
	}
	for _, reason := range []string{"", "stop", "end_turn"} {
		if writingFinishReasonTruncated(reason) {
			t.Fatalf("%q was rejected", reason)
		}
	}
}

func TestProseScreenRunRejectsTruncatedGenerationAndJudgment(t *testing.T) {
	newRun := func(t *testing.T, executor *recordingProseScreenExecutor) ([]WritingGenerationRecord, []WritingJudgmentRecord, error) {
		t.Helper()
		adapter := ProseScreenAdapter{}
		record := WritingPromptRecord{BenchmarkID: adapter.ID(), ID: "case", Prompt: "Write a deployment note.", SourceRecord: []byte(`{"prompt":"Write a deployment note.","task":"Explain deployment safety.","reader_profile":"An operator."}`)}
		model := WritingModelSpec{Provider: "p", Model: "writer", MaxOutputTokens: 64}
		judge := WritingModelSpec{Provider: "p", Model: "judge", MaxOutputTokens: DefaultProseEvaluationMaxTokens}
		manifest := NewWritingRunManifest("input", "hash", []WritingPromptRecord{record}, adapter, []WritingModelSpec{model}, judge, 1, 1, time.Minute, 1, time.Unix(1, 0))
		store, err := OpenWritingRunStore(t.TempDir(), manifest)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		runErr := RunWritingEvaluation(t.Context(), manifest, []WritingPromptRecord{record}, adapter, executor, store)
		generations, judgments := store.Records()
		return generations, judgments, runErr
	}
	gens, judgments, err := newRun(t, &recordingProseScreenExecutor{generationFinish: "length"})
	if err == nil || !strings.Contains(err.Error(), "truncating finish") {
		t.Fatalf("generation error=%v", err)
	}
	if len(gens) != 1 || gens[0].Status != "failed" || len(judgments) != 0 {
		t.Fatalf("generation truncation records=%#v %#v", gens, judgments)
	}
	gens, judgments, err = newRun(t, &recordingProseScreenExecutor{judgmentFinish: "max_tokens"})
	if !errors.Is(err, ErrWritingRunIncomplete) {
		t.Fatalf("judgment error=%v", err)
	}
	if len(gens) != 1 || gens[0].Status != "success" || len(judgments) != 1 || judgments[0].Status != "failed" || len(judgments[0].Results) != 0 {
		t.Fatalf("judgment truncation records=%#v %#v", gens, judgments)
	}
}

func TestWritingRunRefusesInDoubtDispatchIntent(t *testing.T) {
	t.Parallel()
	records := []WritingPromptRecord{writingBenchFixture()}
	adapter := WritingBenchAdapter{}
	model := WritingModelSpec{Provider: "p", Model: "writer", MaxOutputTokens: 64}
	judge := WritingModelSpec{Provider: "p", Model: "judge", MaxOutputTokens: 32}
	manifest := NewWritingRunManifest("input", "hash", records, adapter, []WritingModelSpec{model}, judge, 1, 1, time.Minute, 1, time.Unix(1, 0))
	store, err := OpenWritingRunStore(t.TempDir(), manifest)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	key := writingGenerationKey(adapter.ID(), records[0].ID, model.ID(), 1)
	if err := store.AppendDispatchIntent(WritingDispatchIntent{Kind: "generation", Key: key, Attempt: 1, CallSHA256: writingHash("call"), DispatchedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	executor := &fixtureWritingExecutor{}
	err = RunWritingEvaluation(t.Context(), manifest, records, adapter, executor, store)
	if err == nil || !strings.Contains(err.Error(), "in doubt") {
		t.Fatalf("run error = %v, want in-doubt refusal", err)
	}
	if calls, _ := executor.stats(); calls != 0 {
		t.Fatalf("in-doubt resume dispatched %d calls", calls)
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
	if calls != 2 {
		t.Fatalf("calls = %d, want 1 generation + 1 combined judgment", calls)
	}
	generations, judgments := store.Records()
	for _, record := range generations {
		if record.Usage.Known || record.Usage.CostUSD != 0 {
			t.Fatal("generation without provider usage was misreported as observed cost")
		}
		if record.AccountingCostUSD <= 0 {
			t.Fatal("generation did not persist its conservative accounting cost")
		}
	}
	for _, record := range judgments {
		if record.Usage.Known || record.Usage.CostUSD != 0 {
			t.Fatal("judgment without provider usage was misreported as observed cost")
		}
		if record.AccountingCostUSD <= 0 {
			t.Fatal("judgment did not persist its conservative accounting cost")
		}
	}
	if err := RunWritingEvaluation(t.Context(), manifest, records, adapter, executor, store); err != nil {
		t.Fatal(err)
	}
	calls, _ = executor.stats()
	if calls != 2 {
		t.Fatalf("resume made provider calls: %d", calls)
	}
}

func TestWritingPersistedAccountingCostPrefersSealedAdmissionCost(t *testing.T) {
	t.Parallel()
	usage := WritingUsage{Known: true, CostUSD: 0.2}
	if got := writingPersistedAccountingCost(1.25, usage); got != 1.25 {
		t.Fatalf("persisted accounting cost = %v, want 1.25", got)
	}
	if got := writingPersistedAccountingCost(0, usage); got != 0.2 {
		t.Fatalf("legacy accounting fallback = %v, want 0.2", got)
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
	if plan.GenerationCalls != 1 || plan.JudgmentCalls != 1 || plan.WorstCaseProviderCalls != 2 || plan.WorstCaseCostUSD <= 0 {
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

func TestWritingAccountingCostUsesEstimateOnlyForAdmissionAccounting(t *testing.T) {
	t.Parallel()
	if got := writingAccountingCost(WritingUsage{}, 1.25); got != 1.25 {
		t.Fatalf("observed cost = %v, want 1.25", got)
	}
	if got := writingAccountingCost(WritingUsage{CostUSD: 0.5, Known: true}, 1.25); got != 0.5 {
		t.Fatalf("observed cost = %v, want 0.5", got)
	}
	if got := writingAccountingCost(WritingUsage{Known: true}, 1.25); got != 0 {
		t.Fatalf("known zero cost = %v, want 0", got)
	}
	if got := writingAccountingCost(WritingUsage{Known: true, CacheReadTokens: 10}, 1.25); got != 1.25 {
		t.Fatalf("unknown cached-token cost = %v, want conservative estimate 1.25", got)
	}
	if got := writingAccountingCost(WritingUsage{Known: true, CostKnown: true, CacheReadTokens: 10, CostUSD: 0.2}, 1.25); got != 0.2 {
		t.Fatalf("known cached-token cost = %v, want 0.2", got)
	}
}
