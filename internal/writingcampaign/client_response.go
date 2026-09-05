package writingcampaign

import (
	"fmt"
	"strings"
)

type chatResponse struct {
	Model    string `json:"model"`
	Provider string `json:"provider"`
	Choices  []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int      `json:"prompt_tokens"`
		CompletionTokens int      `json:"completion_tokens"`
		TotalTokens      int      `json:"total_tokens"`
		Cost             *float64 `json:"cost"`
	} `json:"usage"`
}

func applyChatResponse(result Attempt, model Model, decoded chatResponse) Attempt {
	if len(decoded.Choices) != 1 {
		result.Error = fmt.Sprintf("expected one response choice, got %d", len(decoded.Choices))
		return result
	}
	result.ServedModel = strings.TrimSpace(decoded.Model)
	result.ServedProvider = strings.TrimSpace(decoded.Provider)
	result.FinishReason = strings.TrimSpace(decoded.Choices[0].FinishReason)
	result.OutputText = decoded.Choices[0].Message.Content
	result.Usage.PromptTokens = decoded.Usage.PromptTokens
	result.Usage.CompletionTokens = decoded.Usage.CompletionTokens
	result.Usage.TotalTokens = decoded.Usage.TotalTokens
	if decoded.Usage.Cost != nil {
		result.Usage.CostUSD = *decoded.Usage.Cost
	} else if model.DynamicRouting {
		result.Error = "auto-router response omitted usage.cost"
	} else if model.InputPer1M > 0 || model.OutputPer1M > 0 {
		result.Usage.CostUSD = float64(result.Usage.PromptTokens)*model.InputPer1M/1_000_000 + float64(result.Usage.CompletionTokens)*model.OutputPer1M/1_000_000
	}
	if model.DynamicRouting && result.ServedModel == "" {
		result.Error = "auto-router response omitted the routed model identity"
	} else if !model.DynamicRouting && result.ServedModel != model.OpenRouterModelID {
		result.Error = fmt.Sprintf("served model %q does not match requested exact model %q", result.ServedModel, model.OpenRouterModelID)
	}
	return result
}
