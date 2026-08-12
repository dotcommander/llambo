package evals

import (
	"context"
	"encoding/json"
	"time"
)

const (
	WritingRunSchemaVersion        = 1
	WritingJudgePromptVersion      = "local-writing-judge-v2"
	WritingGenerationPromptVersion = "local-writing-generation-v1"
)

type WritingModelSpec struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	MaxOutputTokens int     `json:"max_output_tokens"`
	Workers         int     `json:"workers"`
}

func (m WritingModelSpec) ID() string { return m.Provider + "/" + m.Model }

type WritingGenerationSettings struct {
	Temperature float64        `json:"temperature"`
	ExtraBody   map[string]any `json:"extra_body,omitempty"`
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
	CostUSD          float64 `json:"cost_usd,omitempty"`
}

type WritingGenerationRecord struct {
	Key            string       `json:"key"`
	Attempt        int          `json:"attempt"`
	BenchmarkID    string       `json:"benchmark_id"`
	PromptID       string       `json:"prompt_id"`
	Domain1        string       `json:"domain1,omitempty"`
	Domain2        string       `json:"domain2,omitempty"`
	Provider       string       `json:"provider"`
	Model          string       `json:"model"`
	Iteration      int          `json:"iteration"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    time.Time    `json:"completed_at"`
	LatencyMS      int64        `json:"latency_ms"`
	Status         string       `json:"status"`
	Error          string       `json:"error,omitempty"`
	Content        string       `json:"content,omitempty"`
	ContentSHA256  string       `json:"content_sha256,omitempty"`
	ActualProvider string       `json:"actual_provider,omitempty"`
	ActualModel    string       `json:"actual_model,omitempty"`
	FinishReason   string       `json:"finish_reason,omitempty"`
	Route          string       `json:"route,omitempty"`
	Usage          WritingUsage `json:"usage,omitzero"`
}

type WritingJudgmentRecord struct {
	Key            string       `json:"key"`
	Attempt        int          `json:"attempt"`
	GenerationKey  string       `json:"generation_key"`
	ResponseSHA256 string       `json:"response_sha256"`
	CriterionID    string       `json:"criterion_id"`
	Criterion      string       `json:"criterion"`
	JudgeProvider  string       `json:"judge_provider"`
	JudgeModel     string       `json:"judge_model"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    time.Time    `json:"completed_at"`
	LatencyMS      int64        `json:"latency_ms"`
	Status         string       `json:"status"`
	Error          string       `json:"error,omitempty"`
	RawResponse    string       `json:"raw_response,omitempty"`
	Score          int          `json:"score,omitempty"`
	Reason         string       `json:"reason,omitempty"`
	ActualProvider string       `json:"actual_provider,omitempty"`
	ActualModel    string       `json:"actual_model,omitempty"`
	FinishReason   string       `json:"finish_reason,omitempty"`
	Route          string       `json:"route,omitempty"`
	Usage          WritingUsage `json:"usage,omitzero"`
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
	Provider    string `json:"provider"`
	Model       string `json:"model"`
	PromptID    string `json:"prompt_id"`
	Iteration   int    `json:"iteration"`
	CriterionID string `json:"criterion_id"`
	Score       int    `json:"score"`
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
	SchemaVersion      int                     `json:"schema_version"`
	RunID              string                  `json:"run_id"`
	BenchmarkID        string                  `json:"benchmark_id"`
	ScoreIdentity      string                  `json:"score_identity"`
	Complete           bool                    `json:"complete"`
	Generations        int                     `json:"generations"`
	GenerationFailures int                     `json:"generation_failures"`
	Judgments          int                     `json:"judgments"`
	JudgmentFailures   int                     `json:"judgment_failures"`
	ObservedCostUSD    float64                 `json:"observed_cost_usd"`
	Scores             []WritingScore          `json:"scores"`
	PromptScores       []WritingPromptScore    `json:"prompt_scores"`
	DomainScores       []WritingDomainScore    `json:"domain_scores,omitempty"`
	CriterionScores    []WritingCriterionScore `json:"criterion_scores"`
}

type WritingRunReceipt struct {
	SchemaVersion int               `json:"schema_version"`
	RunID         string            `json:"run_id"`
	CompletedAt   time.Time         `json:"completed_at"`
	Status        string            `json:"status"`
	Report        WritingRunReport  `json:"report"`
	Artifacts     map[string]string `json:"artifacts"`
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
