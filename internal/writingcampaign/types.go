package writingcampaign

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ReceiptSchemaVersion = 1

type Roster struct {
	SchemaVersion int            `json:"schema_version"`
	Locked        bool           `json:"locked"`
	Assets        Assets         `json:"assets"`
	Pricing       PricingPolicy  `json:"pricing"`
	Policy        CampaignPolicy `json:"policy"`
	Models        []Model        `json:"models"`
}

type Assets struct {
	SourceInput      string           `json:"source_input"`
	SystemPrompt     string           `json:"system_prompt"`
	ComparisonPage   string           `json:"comparison_page"`
	RawOutputDir     string           `json:"raw_output_directory"`
	FutureMultistage FutureMultistage `json:"future_multistage"`
}

type FutureMultistage struct {
	Enabled            bool   `json:"enabled"`
	OutlinePrompt      string `json:"outline_prompt"`
	WriterPrompt       string `json:"writer_prompt"`
	OutlinePlaceholder string `json:"writer_outline_placeholder"`
}

type PricingPolicy struct {
	MaximumOutputPer1M float64 `json:"maximum_output_per_1m"`
}

type CampaignPolicy struct {
	Provider              string `json:"provider"`
	ExactModelIDsRequired bool   `json:"exact_model_ids_required"`
	LocalModelsAllowed    bool   `json:"local_models_allowed"`
	AliasesAllowed        bool   `json:"aliases_allowed"`
}

type Model struct {
	OpenRouterModelID     string  `json:"openrouter_model_id"`
	LlamboSelector        string  `json:"llambo_selector"`
	InputPer1M            float64 `json:"input_per_1m"`
	OutputPer1M           float64 `json:"output_per_1m"`
	DynamicRouting        bool    `json:"dynamic_routing,omitempty"`
	RoutingMaxInputPer1M  float64 `json:"routing_max_input_per_1m,omitempty"`
	RoutingMaxOutputPer1M float64 `json:"routing_max_output_per_1m,omitempty"`
	ContextLength         int     `json:"context_length"`
	MaxCompletionTokens   int     `json:"max_completion_tokens"`
	PriorEvidence         string  `json:"prior_evidence"`
	Disabled              bool    `json:"disabled,omitempty"`
	DisabledReason        string  `json:"disabled_reason,omitempty"`
}

type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalTokens      int     `json:"total_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	TokensPerSecond  float64 `json:"tokens_per_second"`
}

type Attempt struct {
	Attempt             int                 `json:"attempt"`
	MaxCompletionTokens int                 `json:"max_completion_tokens"`
	StartedAt           time.Time           `json:"started_at"`
	CompletedAt         time.Time           `json:"completed_at"`
	LatencyMS           int64               `json:"latency_ms"`
	HTTPStatus          int                 `json:"http_status"`
	ResponseHeaders     map[string][]string `json:"response_headers"`
	RequestedModel      string              `json:"requested_model"`
	ServedModel         string              `json:"served_model,omitempty"`
	ServedProvider      string              `json:"served_provider,omitempty"`
	FinishReason        string              `json:"finish_reason,omitempty"`
	OutputText          string              `json:"output_text,omitempty"`
	Usage               Usage               `json:"usage"`
	Error               string              `json:"error,omitempty"`
	RawResponse         json.RawMessage     `json:"raw_response,omitempty"`
}

type Receipt struct {
	SchemaVersion       int       `json:"schema_version"`
	ModelID             string    `json:"model_id"`
	LlamboSelector      string    `json:"llambo_selector"`
	Status              string    `json:"status"`
	SourceSHA256        string    `json:"source_sha256"`
	SystemSHA256        string    `json:"system_prompt_sha256"`
	CreatedAt           time.Time `json:"created_at"`
	Attempts            []Attempt `json:"attempts"`
	FinalAttempt        int       `json:"final_attempt"`
	OutputText          string    `json:"output_text,omitempty"`
	ServedModel         string    `json:"served_model,omitempty"`
	ServedProvider      string    `json:"served_provider,omitempty"`
	FinishReason        string    `json:"finish_reason,omitempty"`
	TotalLatencyMS      int64     `json:"total_latency_ms"`
	TotalTimeToFinishMS int64     `json:"total_time_to_finish_ms"`
	TotalCostUSD        float64   `json:"total_cost_usd"`
	TokensPerSecond     float64   `json:"tokens_per_second"`
	RetryDisposition    string    `json:"retry_disposition,omitempty"`
}

func LoadRoster(path string) (Roster, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Roster{}, fmt.Errorf("read roster: %w", err)
	}
	var roster Roster
	if err := json.Unmarshal(data, &roster); err != nil {
		return Roster{}, fmt.Errorf("parse roster: %w", err)
	}
	if err := roster.Validate(); err != nil {
		return Roster{}, err
	}
	return roster, nil
}

func (r Roster) Validate() error {
	if r.SchemaVersion != 1 || !r.Locked {
		return fmt.Errorf("roster must be locked schema version 1")
	}
	if r.Policy.Provider != "openrouter" || r.Policy.LocalModelsAllowed || r.Policy.AliasesAllowed {
		return fmt.Errorf("roster must require exact OpenRouter models without local routes or aliases")
	}
	for name, path := range map[string]string{"source input": r.Assets.SourceInput, "system prompt": r.Assets.SystemPrompt, "comparison page": r.Assets.ComparisonPage, "raw output directory": r.Assets.RawOutputDir} {
		if strings.TrimSpace(path) == "" || filepath.Clean(path) == "." {
			return fmt.Errorf("%s path is required", name)
		}
	}
	seen := make(map[string]struct{}, len(r.Models))
	for _, model := range r.Models {
		if strings.TrimSpace(model.OpenRouterModelID) == "" || model.LlamboSelector != "openrouter/"+model.OpenRouterModelID {
			return fmt.Errorf("invalid exact OpenRouter selector for %q", model.OpenRouterModelID)
		}
		if _, ok := seen[model.OpenRouterModelID]; ok {
			return fmt.Errorf("duplicate model %q", model.OpenRouterModelID)
		}
		seen[model.OpenRouterModelID] = struct{}{}
		if model.InputPer1M < 0 || model.OutputPer1M < 0 || model.OutputPer1M > r.Pricing.MaximumOutputPer1M {
			return fmt.Errorf("model %q violates pricing policy", model.OpenRouterModelID)
		}
		if model.DynamicRouting {
			if model.OpenRouterModelID != "openrouter/auto" || model.RoutingMaxInputPer1M <= 0 || model.RoutingMaxOutputPer1M <= 0 || model.RoutingMaxOutputPer1M > r.Pricing.MaximumOutputPer1M {
				return fmt.Errorf("model %q has invalid dynamic-routing price limits", model.OpenRouterModelID)
			}
		} else if model.RoutingMaxInputPer1M != 0 || model.RoutingMaxOutputPer1M != 0 {
			return fmt.Errorf("model %q has routing limits without dynamic routing", model.OpenRouterModelID)
		}
		if model.MaxCompletionTokens < 1 {
			return fmt.Errorf("model %q has no completion-token limit", model.OpenRouterModelID)
		}
		if model.Disabled && strings.TrimSpace(model.DisabledReason) == "" {
			return fmt.Errorf("disabled model %q has no reason", model.OpenRouterModelID)
		}
		if !model.Disabled && strings.TrimSpace(model.DisabledReason) != "" {
			return fmt.Errorf("enabled model %q has a disabled reason", model.OpenRouterModelID)
		}
	}
	if len(r.Models) == 0 {
		return fmt.Errorf("roster has no models")
	}
	return nil
}
