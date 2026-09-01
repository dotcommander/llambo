package evals

import (
	"context"
	"encoding/json"
	"time"
)

const (
	WritingRunSchemaVersion        = 2
	WritingJudgePromptVersion      = "local-writing-judge-combined-v1"
	WritingGenerationPromptVersion = "local-writing-generation-v1"
)

type WritingModelSpec struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m,omitempty"`
	CacheWritePer1M float64 `json:"cache_write_per_1m,omitempty"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Workers         int     `json:"workers"`
	EvidenceClass   string  `json:"external_evidence_class,omitempty"`
	EvidenceSource  string  `json:"external_evidence_source,omitempty"`
	EvidenceNote    string  `json:"external_evidence_note,omitempty"`
}

func (m WritingModelSpec) ID() string { return m.Provider + "/" + m.Model }

type WritingGenerationSettings struct {
	Temperature    float64        `json:"temperature"`
	TemperatureSet bool           `json:"temperature_set,omitempty"`
	ExtraBody      map[string]any `json:"extra_body,omitempty"`
}

type WritingRunIdentity struct {
	InputSHA256             string                    `json:"input_sha256"`
	BenchmarkID             string                    `json:"benchmark_id"`
	AdapterVersion          string                    `json:"adapter_version"`
	JudgePromptVersion      string                    `json:"judge_prompt_version"`
	GenerationPromptVersion string                    `json:"generation_prompt_version"`
	PromptIDs               []string                  `json:"prompt_ids"`
	Models                  []WritingModelSpec        `json:"models"`
	Judge                   WritingModelSpec          `json:"judge"`
	Iterations              int                       `json:"iterations"`
	Concurrency             int                       `json:"concurrency"`
	TimeoutSeconds          int                       `json:"timeout_seconds"`
	MaxRunCostUSD           float64                   `json:"max_run_cost_usd"`
	Generation              WritingGenerationSettings `json:"generation"`
	JudgeExecution          string                    `json:"judge_execution,omitempty"`
	JudgeLayout             string                    `json:"judge_layout,omitempty"`
	JudgeThinkingLevel      string                    `json:"judge_thinking_level,omitempty"`
	JudgeConcurrency        int                       `json:"judge_concurrency,omitempty"`
	LocalUseCase            string                    `json:"local_use_case,omitempty"`
}

type WritingRunManifest struct {
	SchemaVersion int                `json:"schema_version"`
	RunID         string             `json:"run_id"`
	CreatedAt     time.Time          `json:"created_at"`
	InputPath     string             `json:"input_path"`
	Identity      WritingRunIdentity `json:"identity"`
}

type WritingUsage struct {
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	TotalTokens      int     `json:"total_tokens,omitempty"`
	CacheReadTokens  int     `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int     `json:"cache_write_tokens,omitempty"`
	ReasoningTokens  int     `json:"reasoning_tokens,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	Known            bool    `json:"known,omitempty"`
	CostKnown        bool    `json:"cost_known,omitempty"`
}

type WritingCriterionJudgment struct {
	CriterionID string              `json:"criterion_id"`
	Criterion   string              `json:"criterion"`
	Score       int                 `json:"score"`
	Reason      string              `json:"reason"`
	Applicable  bool                `json:"applicable,omitempty"`
	Evidence    []ProseEvidenceSpan `json:"evidence,omitempty"`
}

// WritingDispatchIntent is fsynced before each provider call. If a process
// stops after this record but before its matching result ledger entry, the
// attempt is intentionally in doubt and must never be replayed.
type WritingDispatchIntent struct {
	Kind         string    `json:"kind"`
	Key          string    `json:"key"`
	Attempt      int       `json:"attempt"`
	CallSHA256   string    `json:"call_sha256"`
	DispatchedAt time.Time `json:"dispatched_at"`
}

type WritingGenerationRecord struct {
	Key               string       `json:"key"`
	Attempt           int          `json:"attempt"`
	BenchmarkID       string       `json:"benchmark_id"`
	PromptID          string       `json:"prompt_id"`
	Domain1           string       `json:"domain1,omitempty"`
	Domain2           string       `json:"domain2,omitempty"`
	Provider          string       `json:"provider"`
	Model             string       `json:"model"`
	Iteration         int          `json:"iteration"`
	StartedAt         time.Time    `json:"started_at"`
	CompletedAt       time.Time    `json:"completed_at"`
	LatencyMS         int64        `json:"latency_ms"`
	Status            string       `json:"status"`
	Error             string       `json:"error,omitempty"`
	Content           string       `json:"content,omitempty"`
	ContentSHA256     string       `json:"content_sha256,omitempty"`
	ActualProvider    string       `json:"actual_provider,omitempty"`
	ActualModel       string       `json:"actual_model,omitempty"`
	FinishReason      string       `json:"finish_reason,omitempty"`
	Route             string       `json:"route,omitempty"`
	Usage             WritingUsage `json:"usage,omitzero"`
	AccountingCostUSD float64      `json:"accounting_cost_usd,omitempty"`
}

type WritingJudgmentRecord struct {
	Key               string                     `json:"key"`
	Attempt           int                        `json:"attempt"`
	GenerationKey     string                     `json:"generation_key"`
	ResponseSHA256    string                     `json:"response_sha256"`
	CriterionID       string                     `json:"criterion_id"`
	Criterion         string                     `json:"criterion"`
	JudgeProvider     string                     `json:"judge_provider"`
	JudgeModel        string                     `json:"judge_model"`
	StartedAt         time.Time                  `json:"started_at"`
	CompletedAt       time.Time                  `json:"completed_at"`
	LatencyMS         int64                      `json:"latency_ms"`
	Status            string                     `json:"status"`
	Error             string                     `json:"error,omitempty"`
	RawResponse       string                     `json:"raw_response,omitempty"`
	Score             int                        `json:"score,omitempty"`
	Reason            string                     `json:"reason,omitempty"`
	ActualProvider    string                     `json:"actual_provider,omitempty"`
	ActualModel       string                     `json:"actual_model,omitempty"`
	FinishReason      string                     `json:"finish_reason,omitempty"`
	Route             string                     `json:"route,omitempty"`
	Usage             WritingUsage               `json:"usage,omitzero"`
	AccountingCostUSD float64                    `json:"accounting_cost_usd,omitempty"`
	Results           []WritingCriterionJudgment `json:"results,omitempty"`
}

type WritingExecutionCall struct {
	Kind            string
	Model           WritingModelSpec
	SystemPrompt    string
	UserPrompt      string
	MaxOutputTokens int
	Settings        WritingGenerationSettings
}

type WritingExecutionResult struct {
	Content      string
	Provider     string
	Model        string
	FinishReason string
	Route        string
	Usage        WritingUsage
}

type WritingExecutor interface {
	Execute(context.Context, WritingExecutionCall) (WritingExecutionResult, error)
}

type WritingCriterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

type WritingRunPlan struct {
	BenchmarkID            string  `json:"benchmark_id"`
	ScoreIdentity          string  `json:"score_identity"`
	JudgeExecution         string  `json:"judge_execution"`
	PromptCount            int     `json:"prompt_count"`
	GenerationCalls        int     `json:"generation_calls"`
	JudgmentCalls          int     `json:"judgment_calls"`
	MaxJudgmentAttempts    int     `json:"max_judgment_attempts"`
	WorstCaseProviderCalls int     `json:"worst_case_provider_calls"`
	WorstCaseCostUSD       float64 `json:"worst_case_cost_usd"`
}

type WritingScore struct {
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	Score     float64 `json:"score"`
	Judgments int     `json:"judgments"`
}

type WritingCriterionScore struct {
	Provider    string              `json:"provider"`
	Model       string              `json:"model"`
	PromptID    string              `json:"prompt_id"`
	Iteration   int                 `json:"iteration"`
	CriterionID string              `json:"criterion_id"`
	Score       int                 `json:"score"`
	Applicable  bool                `json:"applicable,omitempty"`
	Evidence    []ProseEvidenceSpan `json:"evidence,omitempty"`
	Reason      string              `json:"reason,omitempty"`
}

type WritingPromptScore struct {
	Provider  string  `json:"provider"`
	Model     string  `json:"model"`
	PromptID  string  `json:"prompt_id"`
	Iteration int     `json:"iteration"`
	Domain1   string  `json:"domain1,omitempty"`
	Domain2   string  `json:"domain2,omitempty"`
	Score     float64 `json:"score"`
	Judgments int     `json:"judgments"`
}

type WritingDomainScore struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Domain   string  `json:"domain"`
	Score    float64 `json:"score"`
	Prompts  int     `json:"prompts"`
}

type WritingRunReport struct {
	SchemaVersion      int                      `json:"schema_version"`
	RunID              string                   `json:"run_id"`
	BenchmarkID        string                   `json:"benchmark_id"`
	ScoreIdentity      string                   `json:"score_identity"`
	Complete           bool                     `json:"complete"`
	Generations        int                      `json:"generations"`
	GenerationFailures int                      `json:"generation_failures"`
	Judgments          int                      `json:"judgments"`
	JudgmentFailures   int                      `json:"judgment_failures"`
	ObservedCostUSD    float64                  `json:"observed_cost_usd"`
	Scores             []WritingScore           `json:"scores"`
	PromptScores       []WritingPromptScore     `json:"prompt_scores"`
	DomainScores       []WritingDomainScore     `json:"domain_scores,omitempty"`
	CriterionScores    []WritingCriterionScore  `json:"criterion_scores"`
	PreScreenOnly      bool                     `json:"pre_screen_only,omitempty"`
	ModelDispersion    []WritingModelDispersion `json:"model_dispersion,omitempty"`
	ModelAggregates    []WritingModelAggregate  `json:"model_aggregates,omitempty"`
}

type WritingModelDispersion struct {
	Provider           string  `json:"provider"`
	Model              string  `json:"model"`
	Samples            int     `json:"samples"`
	Mean               float64 `json:"mean"`
	Median             float64 `json:"median"`
	Min                float64 `json:"min"`
	Max                float64 `json:"max"`
	Spread             float64 `json:"spread"`
	InsufficientSample bool    `json:"insufficient_sample"`
}

type WritingModelAggregate struct {
	Version       string  `json:"version"`
	Provider      string  `json:"provider"`
	Model         string  `json:"model"`
	Samples       int     `json:"samples"`
	Score         float64 `json:"score"`
	MechanicsCaps int     `json:"mechanics_caps"`
}

type WritingRunReceipt struct {
	SchemaVersion   int               `json:"schema_version"`
	RunID           string            `json:"run_id"`
	IdentitySHA256  string            `json:"identity_sha256"`
	InputSHA256     string            `json:"input_sha256"`
	RequestedModels []string          `json:"requested_models"`
	ServedModels    []string          `json:"served_models"`
	RequestedJudge  string            `json:"requested_judge"`
	ServedJudges    []string          `json:"served_judges"`
	CompletedAt     time.Time         `json:"completed_at"`
	Status          string            `json:"status"`
	Report          WritingRunReport  `json:"report"`
	Artifacts       map[string]string `json:"artifacts"`
}

type WritingQualityImportRecord struct {
	Provider string  `json:"provider"`
	Model    string  `json:"model"`
	Task     string  `json:"task"`
	Score    float64 `json:"score"`
	Source   string  `json:"source"`
	Notes    string  `json:"notes,omitempty"`
}

func cloneRawMessage(raw json.RawMessage) json.RawMessage {
	return append(json.RawMessage(nil), raw...)
}
