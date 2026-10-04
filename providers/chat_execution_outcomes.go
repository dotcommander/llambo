package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (c executionCoordinator) execute(ctx context.Context, plan chatExecutionPlan, systemPrompt, userContent string, failoverCallback FailoverCallback, request func(context.Context, providerInfo, string, string) (chatRequestResult, error)) (chatExecutionOutcome, error) {
	result, err := request(ctx, plan.selected, systemPrompt, userContent)
	outcome := chatExecutionOutcome{provider: plan.selected, result: result, decision: plan.decision}
	if err == nil {
		c.recordSuccessfulAttempt(plan, plan.selected, systemPrompt, userContent, result)
		c.checkCanaryAutoPromote(ctx, plan)
		return outcome, nil
	}

	c.recordAttemptFailure(plan.selected.name, result.usage, result.duration, err)
	if ctx.Err() != nil {
		return outcome, ctx.Err()
	}
	errors := []string{fmt.Sprintf("%s: %v", plan.selected.name, err)}

	if failoverCallback != nil {
		failoverCallback(FailoverEvent{FromBackend: plan.selected.name, Error: err})
	}

	for _, info := range plan.enabled {
		if info.name == plan.selected.name {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		if failoverCallback != nil {
			failoverCallback(FailoverEvent{FromBackend: plan.selected.name, ToBackend: info.name, ToModel: NormalizeModelName(info.cfg.Model)})
		}

		result, err = request(ctx, info, systemPrompt, userContent)
		outcome = chatExecutionOutcome{provider: info, result: result, decision: plan.decision}
		if err == nil {
			c.recordSuccessfulAttempt(plan, info, systemPrompt, userContent, result)
			c.checkCanaryAutoPromote(ctx, plan)
			return outcome, nil
		}
		c.recordAttemptFailure(info.name, result.usage, result.duration, err)
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		errors = append(errors, fmt.Sprintf("%s: %v", info.name, err))
	}

	c.logRouteOutcome(plan.intent, plan.estimatedTokens, plan.plannedProvider, "", "", nil, 0, false, strings.Join(errors, "; "), plan.candidates, plan.isCanary)
	return outcome, fmt.Errorf("all providers failed: %s", strings.Join(errors, "; "))
}

func (c executionCoordinator) executeStream(ctx context.Context, plan chatExecutionPlan, systemPrompt, userContent string, onChunk ChatStreamHandler, request func(context.Context, providerInfo, string, string, ChatStreamHandler) (chatRequestResult, bool, error)) (chatExecutionOutcome, error) {
	result, emitted, err := request(ctx, plan.selected, systemPrompt, userContent, onChunk)
	outcome := chatExecutionOutcome{provider: plan.selected, result: result, decision: plan.decision}
	if err == nil {
		if result.finishReason == "" {
			c.promotionIneligible = true
		}
		c.recordSuccessfulAttempt(plan, plan.selected, systemPrompt, userContent, result)
		if result.finishReason != "" {
			c.checkCanaryAutoPromote(ctx, plan)
		}
		return outcome, nil
	}

	c.recordAttemptFailure(plan.selected.name, result.usage, result.duration, err)
	if IsConsumerError(err) {
		return outcome, err
	}
	if ctx.Err() != nil {
		return outcome, ctx.Err()
	}
	errStrings := []string{fmt.Sprintf("%s: %v", plan.selected.name, err)}
	if emitted {
		c.logFailedStreamOutcome(plan.intent, plan.estimatedTokens, plan.plannedProvider, plan.selected.name, plan.selected.cfg.Model, result.usage, result.duration, false, strings.Join(errStrings, "; "), plan.candidates, plan.isCanary)
		return outcome, fmt.Errorf("stream failed after emission: %w", err)
	}

	for _, info := range plan.enabled {
		if info.name == plan.selected.name {
			continue
		}
		if ctx.Err() != nil {
			break
		}

		if c.failoverCallback != nil {
			c.failoverCallback(FailoverEvent{FromBackend: outcome.provider.name, ToBackend: info.name, ToModel: info.cfg.Model, Error: err})
		}
		result, emitted, err = request(ctx, info, systemPrompt, userContent, onChunk)
		outcome = chatExecutionOutcome{provider: info, result: result, decision: plan.decision}
		if err == nil {
			if result.finishReason == "" {
				c.promotionIneligible = true
			}
			c.recordSuccessfulAttempt(plan, info, systemPrompt, userContent, result)
			if result.finishReason != "" {
				c.checkCanaryAutoPromote(ctx, plan)
			}
			return outcome, nil
		}

		c.recordAttemptFailure(info.name, result.usage, result.duration, err)
		if IsConsumerError(err) {
			return outcome, err
		}
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		errStrings = append(errStrings, fmt.Sprintf("%s: %v", info.name, err))
		if emitted || IsConsumerError(err) {
			return outcome, err
		}
	}

	c.logFailedStreamOutcome(plan.intent, plan.estimatedTokens, plan.plannedProvider, "", "", nil, 0, false, strings.Join(errStrings, "; "), plan.candidates, plan.isCanary)
	return outcome, fmt.Errorf("all providers failed: %s", strings.Join(errStrings, "; "))
}

func (c executionCoordinator) recordSuccessfulAttempt(plan chatExecutionPlan, info providerInfo, systemPrompt, userContent string, result chatRequestResult) {
	c.recordSuccess(info.name, result.clientKey, result.usage, result.duration)
	c.recordQuality(info.name, systemPrompt, userContent, result.content, result.duration)
	c.logRouteOutcome(plan.intent, plan.estimatedTokens, plan.plannedProvider, info.name, info.cfg.Model, result.usage, result.duration, true, "", plan.candidates, plan.isCanary && info.name == plan.selected.name)
}

func (c executionCoordinator) recordSuccess(provider, key string, usage *LLMUsage, duration time.Duration) {
	c.oc.CircuitBreaker.RecordSuccess(provider)
	if c.oc.KeyRotator != nil && key != "" {
		c.oc.KeyRotator.RecordSuccessForKey(provider, key)
	}
	c.oc.CostTracker.Record(provider, usage)
	if c.oc.RoutingMetrics != nil {
		c.oc.RoutingMetrics.Record(provider, duration, usage, nil)
	}
}

func (c executionCoordinator) recordFailure(provider string, usage *LLMUsage, duration time.Duration, err error) {
	c.oc.CircuitBreaker.RecordFailure(provider, err)
	if c.oc.RoutingMetrics != nil {
		c.oc.RoutingMetrics.Record(provider, duration, usage, err)
	}
}

func (c executionCoordinator) recordAttemptFailure(provider string, usage *LLMUsage, duration time.Duration, err error) {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBackendUnavailable) || IsConsumerError(err) {
		return
	}
	c.recordFailure(provider, usage, duration, err)
}

func (c executionCoordinator) recordQuality(provider, systemPrompt, userContent, responseContent string, duration time.Duration) {
	if c.oc.RoutingMetrics == nil {
		return
	}
	promptLen := len(systemPrompt) + len(userContent)
	responseLen := len(responseContent)
	score := ComputeResponseQuality(promptLen, responseLen, duration.Milliseconds())
	c.oc.RoutingMetrics.RecordQuality(provider, score)
}

func (c executionCoordinator) logRouteOutcome(intent RoutingIntent, estimatedTokens int, plannedProvider, chosenProvider, model string, usage *LLMUsage, duration time.Duration, success bool, errMsg string, candidates []CandidateScore, isCanary bool) {
	if c.oc.RouteEvents == nil {
		return
	}

	cost := 0.0
	if usage != nil && usage.Cost != nil {
		cost = usage.Cost.TotalCost
	}
	err := c.oc.RouteEvents.Log(RouteEvent{
		Mode:                c.routing.Mode,
		PromotionIneligible: c.promotionIneligible,
		Intent:              intent,
		EstimatedTokens:     estimatedTokens,
		PlannedProvider:     plannedProvider,
		ChosenProvider:      chosenProvider,
		Model:               model,
		LatencyMs:           duration.Milliseconds(),
		CostUSD:             cost,
		Success:             success,
		Error:               errMsg,
		Candidates:          candidates,
		IsCanary:            isCanary,
	})
	if err != nil {
		logger := slog.Default()
		if c.oc != nil {
			logger = c.oc.loggerOrDefault()
		}
		logger.Warn("write route event failed", "path", c.oc.RouteEvents.path, "error", err)
	}
}

func (c executionCoordinator) logFailedStreamOutcome(intent RoutingIntent, estimatedTokens int, plannedProvider, chosenProvider, model string, usage *LLMUsage, duration time.Duration, success bool, errMsg string, candidates []CandidateScore, isCanary bool) {
	c.promotionIneligible = true
	c.logRouteOutcome(intent, estimatedTokens, plannedProvider, chosenProvider, model, usage, duration, success, errMsg, candidates, isCanary)
}
