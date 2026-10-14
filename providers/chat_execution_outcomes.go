package providers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

func (c executionCoordinator) execute(ctx context.Context, plan chatExecutionPlan, failoverCallback FailoverCallback, request func(context.Context, chatAttempt) (chatRequestResult, error)) (chatExecutionOutcome, error) {
	result, err := request(ctx, plan.attemptFor(plan.selected))
	outcome := chatExecutionOutcome{provider: plan.selected, result: result, decision: plan.decision}
	if err == nil {
		c.recordSuccessfulAttempt(plan, plan.selected, result)
		c.checkCanaryAutoPromote(ctx)
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

		result, err = request(ctx, plan.attemptFor(info))
		outcome = chatExecutionOutcome{provider: info, result: result, decision: plan.decision}
		if err == nil {
			c.recordSuccessfulAttempt(plan, info, result)
			c.checkCanaryAutoPromote(ctx)
			return outcome, nil
		}
		c.recordAttemptFailure(info.name, result.usage, result.duration, err)
		if ctx.Err() != nil {
			return outcome, ctx.Err()
		}
		errors = append(errors, fmt.Sprintf("%s: %v", info.name, err))
	}

	c.logRouteOutcome(plan.failedRouteOutcome(strings.Join(errors, "; ")))
	return outcome, fmt.Errorf("all providers failed: %s", strings.Join(errors, "; "))
}

func (c executionCoordinator) executeStream(ctx context.Context, plan chatExecutionPlan, onChunk ChatStreamHandler, request func(context.Context, chatAttempt, ChatStreamHandler) (chatRequestResult, bool, error)) (chatExecutionOutcome, error) {
	result, emitted, err := request(ctx, plan.attemptFor(plan.selected), onChunk)
	outcome := chatExecutionOutcome{provider: plan.selected, result: result, decision: plan.decision}
	if err == nil {
		if result.finishReason == "" {
			c.promotionIneligible = true
		}
		c.recordSuccessfulAttempt(plan, plan.selected, result)
		if result.finishReason != "" {
			c.checkCanaryAutoPromote(ctx)
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
		c.logFailedStreamOutcome(plan.routeOutcome(plan.selected.name, plan.selected.cfg.Model, result.usage, result.duration, strings.Join(errStrings, "; ")))
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
		result, emitted, err = request(ctx, plan.attemptFor(info), onChunk)
		outcome = chatExecutionOutcome{provider: info, result: result, decision: plan.decision}
		if err == nil {
			if result.finishReason == "" {
				c.promotionIneligible = true
			}
			c.recordSuccessfulAttempt(plan, info, result)
			if result.finishReason != "" {
				c.checkCanaryAutoPromote(ctx)
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

	c.logFailedStreamOutcome(plan.failedRouteOutcome(strings.Join(errStrings, "; ")))
	return outcome, fmt.Errorf("all providers failed: %s", strings.Join(errStrings, "; "))
}

func (c executionCoordinator) recordSuccessfulAttempt(plan chatExecutionPlan, info providerInfo, result chatRequestResult) {
	c.recordSuccess(info.name, result.clientKey, result.usage, result.duration)
	c.recordQuality(info.name, plan.systemPrompt, plan.userContent, result.content, result.duration)
	success := plan.routeOutcome(info.name, info.cfg.Model, result.usage, result.duration, "")
	success.success = true
	success.isCanary = plan.isCanary && info.name == plan.selected.name
	c.logRouteOutcome(success)
}

// failedRouteOutcome derives the route-event record for a request where every
// provider attempt failed.
func (plan chatExecutionPlan) failedRouteOutcome(errMsg string) routeOutcome {
	return plan.routeOutcome("", "", nil, 0, errMsg)
}

// routeOutcome captures the route-event attributes recorded for one outcome.
type routeOutcome struct {
	intent          RoutingIntent
	estimatedTokens int
	plannedProvider string
	chosenProvider  string
	model           string
	usage           *LLMUsage
	duration        time.Duration
	success         bool
	errMsg          string
	candidates      []CandidateScore
	isCanary        bool
}

func (plan chatExecutionPlan) routeOutcome(chosenProvider, model string, usage *LLMUsage, duration time.Duration, errMsg string) routeOutcome {
	return routeOutcome{
		intent:          plan.intent,
		estimatedTokens: plan.estimatedTokens,
		plannedProvider: plan.plannedProvider,
		chosenProvider:  chosenProvider,
		model:           model,
		usage:           usage,
		duration:        duration,
		errMsg:          errMsg,
		candidates:      plan.candidates,
		isCanary:        plan.isCanary,
	}
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
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrBackendUnavailable) || IsConsumerError(err) || IsIncompleteGenerationError(err) {
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

func (c executionCoordinator) logRouteOutcome(outcome routeOutcome) {
	if c.oc.RouteEvents == nil {
		return
	}

	cost := 0.0
	if outcome.usage != nil && outcome.usage.Cost != nil {
		cost = outcome.usage.Cost.TotalCost
	}
	err := c.oc.RouteEvents.Log(RouteEvent{
		Mode:                c.routing.Mode,
		PromotionIneligible: c.promotionIneligible,
		Intent:              outcome.intent,
		EstimatedTokens:     outcome.estimatedTokens,
		PlannedProvider:     outcome.plannedProvider,
		ChosenProvider:      outcome.chosenProvider,
		Model:               outcome.model,
		LatencyMs:           outcome.duration.Milliseconds(),
		CostUSD:             cost,
		Success:             outcome.success,
		Error:               outcome.errMsg,
		Candidates:          outcome.candidates,
		IsCanary:            outcome.isCanary,
	})
	if err != nil {
		logger := slog.Default()
		if c.oc != nil {
			logger = c.oc.loggerOrDefault()
		}
		logger.Warn("write route event failed", "path", c.oc.RouteEvents.path, "error", err)
	}
}

func (c executionCoordinator) logFailedStreamOutcome(outcome routeOutcome) {
	c.promotionIneligible = true
	c.logRouteOutcome(outcome)
}
