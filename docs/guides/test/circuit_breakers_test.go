package guides

import (
	"os"
	"strings"
	"testing"
)

const circuitBreakersPath = "../circuit-breakers.md"

func TestCircuitBreakersGuideExists(t *testing.T) {
	_, err := os.Stat(circuitBreakersPath)
	if err != nil {
		t.Fatalf("docs/guides/circuit-breakers.md does not exist: %v", err)
	}
}

func TestCircuitBreakersGuideHasThreeFailureStates(t *testing.T) {
	content, err := os.ReadFile(circuitBreakersPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/circuit-breakers.md: %v", err)
	}

	text := string(content)

	// Check for all three failure states
	states := []string{
		"**Closed**",
		"**Open**",
		"**Half-Open**",
	}

	for _, state := range states {
		if !strings.Contains(text, state) {
			t.Errorf("Circuit breaker guide missing state: %s", state)
		}
	}

	// Check for state descriptions
	if !strings.Contains(text, "Healthy") || !strings.Contains(text, "Unhealthy") || !strings.Contains(text, "Testing") {
		t.Error("Circuit breaker guide missing state descriptions (Healthy, Unhealthy, Testing)")
	}
}

func TestCircuitBreakersGuideHasExactTriggers(t *testing.T) {
	content, err := os.ReadFile(circuitBreakersPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/circuit-breakers.md: %v", err)
	}

	text := string(content)

	// Check for exact triggers from CLAUDE.md
	triggers := []string{
		"HTTP 429",
		"rate limit",
		"quota",
		"3+ consecutive failures",
		"Success after cooldown",
	}

	for _, trigger := range triggers {
		if !strings.Contains(text, trigger) {
			t.Errorf("Circuit breaker guide missing trigger: %s", trigger)
		}
	}

	// Check for cooldown periods
	if !strings.Contains(text, "5 minutes") || !strings.Contains(text, "60 seconds") {
		t.Error("Circuit breaker guide missing cooldown periods (5 minutes, 60 seconds)")
	}
}

func TestCircuitBreakersGuideHasRecoveryBehavior(t *testing.T) {
	content, err := os.ReadFile(circuitBreakersPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/circuit-breakers.md: %v", err)
	}

	text := string(content)

	// Check for auto-recovery and cooldown explanations
	recoveryTerms := []string{
		"auto-recovery",
		"cooldown",
		"RateLimitCooldown",
		"FailureCooldown",
		"QuotaErrorThreshold",
	}

	for _, term := range recoveryTerms {
		if !strings.Contains(text, term) {
			t.Errorf("Circuit breaker guide missing recovery term: %s", term)
		}
	}

	// Check for cooldown logic explanation
	if !strings.Contains(text, "failures >= QuotaErrorThreshold") {
		t.Error("Circuit breaker guide missing cooldown logic explanation")
	}
}
