package guides

import (
	"os"
	"strings"
	"testing"
)

const keyRotationPath = "../key-rotation.md"

func TestKeyRotationGuideExists(t *testing.T) {
	_, err := os.Stat(keyRotationPath)
	if err != nil {
		t.Fatalf("docs/guides/key-rotation.md does not exist: %v", err)
	}
}

func TestKeyRotationGuideHasMultiKeyConfiguration(t *testing.T) {
	content, err := os.ReadFile(keyRotationPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/key-rotation.md: %v", err)
	}

	text := string(content)

	// Check for multi-key configuration format
	configChecks := []string{
		"`api_keys`",
		"api_keys array",
		"[\"sk-",
		"Multiple API keys",
	}

	for _, check := range configChecks {
		if !strings.Contains(text, check) {
			t.Errorf("Key rotation guide missing multi-key configuration reference: %s", check)
		}
	}

	// Check for configuration rules table
	if !strings.Contains(text, "| Field | Required | Description |") {
		t.Error("Key rotation guide missing configuration rules table")
	}
}

func TestKeyRotationGuideHas429RotateRetryFlow(t *testing.T) {
	content, err := os.ReadFile(keyRotationPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/key-rotation.md: %v", err)
	}

	text := string(content)

	// Check for 429 → rotate → retry flow explanation
	flowTerms := []string{
		"HTTP 429",
		"rotate",
		"retry",
		"MarkRateLimited",
		"rate-limited",
		"RateLimited = true",
	}

	for _, term := range flowTerms {
		if !strings.Contains(text, term) {
			t.Errorf("Key rotation guide missing 429 → rotate → retry flow term: %s", term)
		}
	}

	// Check for step-by-step flow
	if !strings.Contains(text, "Step-by-Step Flow") {
		t.Error("Key rotation guide missing step-by-step flow section")
	}

	// Check for key rotation flowchart or diagram
	if !strings.Contains(text, "graph TD") && !strings.Contains(text, "flowchart") {
		t.Error("Key rotation guide missing rotation flowchart or diagram")
	}
}

func TestKeyRotationGuideHasExhaustionBehavior(t *testing.T) {
	content, err := os.ReadFile(keyRotationPath)
	if err != nil {
		t.Fatalf("Failed to read docs/guides/key-rotation.md: %v", err)
	}

	text := string(content)

	// Check for exhaustion behavior documentation
	exhaustionTerms := []string{
		"all keys exhausted",
		"all keys rate-limited",
		"Circuit-break provider",
		"RateLimitCooldown",
		"cooldown expires",
		"keys automatically recover",
	}

	for _, term := range exhaustionTerms {
		if !strings.Contains(text, term) {
			t.Errorf("Key rotation guide missing exhaustion behavior term: %s", term)
		}
	}

	// Check for exhaustion scenarios or examples
	if !strings.Contains(text, "All exhausted") || !strings.Contains(text, "All providers exhausted") {
		t.Error("Key rotation guide missing exhaustion scenarios")
	}

	// Check for recovery after exhaustion
	if !strings.Contains(text, "Recovery After Exhaustion") && !strings.Contains(text, "recover after exhaustion") {
		t.Error("Key rotation guide missing recovery after exhaustion section")
	}
}
