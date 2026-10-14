package writingcampaign

import (
	"context"
	"strings"
	"time"
)

func runModel(ctx context.Context, client *Client, input modelRun) Receipt {
	model, options := input.model, input.options
	source, systemPrompt := input.content.source, input.content.systemPrompt
	sourceHash, systemHash := input.content.sourceHash, input.content.systemHash
	if options.ModelTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.ModelTimeout)
		defer cancel()
	}
	receipt := Receipt{SchemaVersion: ReceiptSchemaVersion, ModelID: model.OpenRouterModelID, LlamboSelector: model.LlamboSelector, Status: "failed", SourceSHA256: sourceHash, SystemSHA256: systemHash, CreatedAt: time.Now().UTC()}
	initial := min(options.InitialTokens, model.MaxCompletionTokens)
	first := client.Execute(ctx, GenerationRequest{Model: model, SystemPrompt: systemPrompt, Source: source, MaxTokens: initial, Attempt: 1})
	receipt.Attempts = append(receipt.Attempts, first)
	if first.Error == "" && strings.EqualFold(first.FinishReason, "length") {
		retry := min(options.RetryTokens, model.MaxCompletionTokens)
		if retry > initial {
			receipt.RetryDisposition = "retried_with_higher_max_completion_tokens"
			receipt.Attempts = append(receipt.Attempts, client.Execute(ctx, GenerationRequest{Model: model, SystemPrompt: systemPrompt, Source: source, MaxTokens: retry, Attempt: 2}))
		} else {
			receipt.RetryDisposition = "length_retry_unavailable_at_model_limit"
		}
	}
	final := receipt.Attempts[len(receipt.Attempts)-1]
	receipt.FinalAttempt = final.Attempt
	receipt.OutputText = final.OutputText
	receipt.ServedModel = final.ServedModel
	receipt.ServedProvider = final.ServedProvider
	receipt.FinishReason = final.FinishReason
	for _, attempt := range receipt.Attempts {
		receipt.TotalLatencyMS += attempt.LatencyMS
		receipt.TotalCostUSD += attempt.Usage.CostUSD
	}
	if len(receipt.Attempts) > 0 {
		duration := final.CompletedAt.Sub(receipt.Attempts[0].StartedAt)
		receipt.TotalTimeToFinishMS = duration.Milliseconds()
		if duration > 0 && receipt.TotalTimeToFinishMS == 0 {
			receipt.TotalTimeToFinishMS = 1
		}
	}
	receipt.TokensPerSecond = final.Usage.TokensPerSecond
	if final.Error == "" && strings.TrimSpace(final.OutputText) != "" {
		receipt.Status = "complete"
	}
	return receipt
}
