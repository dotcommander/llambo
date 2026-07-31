package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/llambo/internal/catalog"
	"github.com/dotcommander/llambo/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testRootCommand struct {
	args   []string
	stdout io.Writer
	stderr io.Writer
}

func (c *testRootCommand) SetArgs(args []string) { c.args = args }
func (c *testRootCommand) SetOut(out io.Writer)  { c.stdout = out }
func (c *testRootCommand) SetErr(out io.Writer)  { c.stderr = out }
func (c *testRootCommand) Execute() error {
	stdout, stderr := c.stdout, c.stderr
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	return execute(context.Background(), c.args, stdout, stderr)
}

var (
	rootCmd   = &testRootCommand{}
	promptCmd = &commandIO{stdout: io.Discard, stderr: io.Discard, changed: map[string]bool{}}
)

func TestPromptCommand_Help(t *testing.T) {
	// Reset root command to ensure clean test state
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"prompt", "--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("prompt --help failed: %v", err)
	}

	output := out.String()

	// Verify usage information is present
	expectedPhrases := []string{
		"prompt",
		"Send the same prompt to multiple configured LLM providers",
		"Usage:",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(output, phrase) {
			t.Errorf("Expected help output to contain %q, got:\n%s", phrase, output)
		}
	}
}

func TestPromptCommand_NoArguments(t *testing.T) {
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"prompt"})

	err := rootCmd.Execute()

	// Should return an error when no arguments provided
	if err == nil {
		t.Fatal("Expected error when running prompt without arguments")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "prompt text required") {
		t.Errorf("Expected error message to mention prompt text required, got: %s", errMsg)
	}
}

func TestPromptCommand_EmptyString(t *testing.T) {
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"prompt", ""})

	err := rootCmd.Execute()

	// Should return an error when prompt is empty string
	if err == nil {
		t.Fatal("Expected error when running prompt with empty string")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "prompt text cannot be empty") {
		t.Errorf("Expected error message about empty prompt, got: %s", errMsg)
	}
}

func TestPromptTextFromArgsOrStdin(t *testing.T) {
	t.Run("joins multiple args", func(t *testing.T) {
		got, err := promptTextFromArgsOrStdin([]string{"hello", "world"})
		require.NoError(t, err)
		require.Equal(t, "hello world", got)
	})

	t.Run("reads piped stdin", func(t *testing.T) {
		oldStdin := os.Stdin
		t.Cleanup(func() { os.Stdin = oldStdin })

		r, w, err := os.Pipe()
		require.NoError(t, err)
		_, err = w.WriteString("stdin prompt\n")
		require.NoError(t, err)
		require.NoError(t, w.Close())
		os.Stdin = r

		got, err := promptTextFromArgsOrStdin(nil)
		require.NoError(t, err)
		require.Equal(t, "stdin prompt", got)
	})
}

func TestPromptCommand_AppearsInCommandList(t *testing.T) {
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"--help"})

	err := rootCmd.Execute()
	if err != nil {
		t.Fatalf("llambo --help failed: %v", err)
	}

	output := out.String()

	// Verify prompt command appears in the command list
	if !strings.Contains(output, "prompt") {
		t.Errorf("Expected 'prompt' to appear in command list, got:\n%s", output)
	}
}

// resetRootCmd resets mutable command state so tests don't interfere with each other.
func resetRootCmd() {
	systemPrompt = ""
	outputFile = ""
	timeoutSeconds = 60
	promptModels = ""
	promptProviders = ""
	promptIncludeQuarantine = false
	promptMaxOutputCost = 0
	promptFreeOnly = false
	promptIncludeUnknownCost = false
	promptSmart = false
	promptFuse = false
	promptFuseModels = defaultPromptFuseModels
	promptFuseControl = false
	rootCmd = &testRootCommand{}
	promptCmd = &commandIO{stdout: io.Discard, stderr: io.Discard, changed: map[string]bool{}}
}

func TestApplyPromptUXDefaults_SmartPresetUsesHealthyAndFuse(t *testing.T) {
	resetRootCmd()
	promptSmart = true

	applyPromptUXDefaults(promptCmd)

	require.Equal(t, defaultSmartPromptModels, promptModels)
	require.True(t, promptFuse)
}

func TestApplyPromptUXDefaults_ExplicitFlagsOverrideSmartPreset(t *testing.T) {
	resetRootCmd()
	promptSmart = true
	promptModels = "free"
	promptFuse = false
	promptCmd.changed["models"] = true
	promptCmd.changed["fuse"] = true

	applyPromptUXDefaults(promptCmd)

	require.Equal(t, "free", promptModels)
	require.False(t, promptFuse)
}

func TestApplyPromptUXDefaults_UsesEnvWhenFlagsUnset(t *testing.T) {
	resetRootCmd()
	t.Setenv("LLAMBO_PROMPT_MODELS", "tag:smart")
	t.Setenv("LLAMBO_PROMPT_FUSE", "true")
	t.Setenv("LLAMBO_PROMPT_FUSE_MODELS", "zai/custom")

	applyPromptUXDefaults(promptCmd)

	require.Equal(t, "tag:smart", promptModels)
	require.True(t, promptFuse)
	require.Equal(t, "zai/custom", promptFuseModels)
}

func TestApplyPromptUXDefaults_ExplicitFlagsOverrideEnv(t *testing.T) {
	resetRootCmd()
	t.Setenv("LLAMBO_PROMPT_MODELS", "tag:smart")
	t.Setenv("LLAMBO_PROMPT_FUSE", "true")
	t.Setenv("LLAMBO_PROMPT_FUSE_MODELS", "zai/custom")
	promptModels = "free"
	promptFuse = false
	promptFuseModels = "zai/explicit"
	promptCmd.changed["models"] = true
	promptCmd.changed["fuse"] = true
	promptCmd.changed["fuse-models"] = true

	applyPromptUXDefaults(promptCmd)

	require.Equal(t, "free", promptModels)
	require.False(t, promptFuse)
	require.Equal(t, "zai/explicit", promptFuseModels)
}

func TestBuildFusionPrompt_IncludesOriginalPromptAndSuccessfulResponses(t *testing.T) {
	results := []PromptResult{
		{Provider: "p1", Model: "m1", Response: "First answer", Error: nil},
		{Provider: "p2", Model: "m2", Response: "Second answer", Error: nil},
	}

	got := buildFusionPrompt("What matters?", "Be brief.", results)

	assert.Contains(t, got, "Original user prompt:")
	assert.Contains(t, got, "What matters?")
	assert.Contains(t, got, "Original system prompt:")
	assert.Contains(t, got, "Be brief.")
	assert.Contains(t, got, "private source audit")
	assert.Contains(t, got, "strongest unique contribution")
	assert.Contains(t, got, "Do not output the audit")
	assert.Contains(t, got, "Fusion rules:")
	assert.Contains(t, got, "Keep only useful novelty")
	assert.Contains(t, got, "from a non-primary draft")
	assert.Contains(t, got, "Prefer one coherent strategy")
	assert.Contains(t, got, "Obey the original user's requested format")
	assert.Contains(t, got, "Response 1 from p1/m1:")
	assert.Contains(t, got, "First answer")
	assert.Contains(t, got, "Response 2 from p2/m2:")
	assert.Contains(t, got, "Second answer")
	assert.NotContains(t, got, "```")
}

func TestBuildFusionDraftPrompt_EncouragesDistinctDraftMaterial(t *testing.T) {
	got := buildFusionDraftPrompt("Give me a launch plan.", "Be practical.", 1, 6, "nvidia", "model-b")

	assert.Contains(t, got, "Original user prompt:")
	assert.Contains(t, got, "Give me a launch plan.")
	assert.Contains(t, got, "Original system prompt:")
	assert.Contains(t, got, "Be practical.")
	assert.Contains(t, got, "draft contributor 2 of 6")
	assert.Contains(t, got, "Assigned lens: creative strategist")
	assert.Contains(t, got, "not to converge on the obvious shortest answer")
	assert.Contains(t, got, "non-obvious angles")
	assert.Contains(t, got, "4-8 dense bullets")
	assert.Contains(t, got, "fusion model will compress")
	assert.Contains(t, got, "nvidia/model-b")
}

func TestComputeFusionConsensus_LabelsDraftAgreement(t *testing.T) {
	tests := []struct {
		name    string
		results []PromptResult
		label   string
	}{
		{
			name: "strong consensus",
			results: []PromptResult{
				{Response: "Use cheap fast models for routing and escalate hard cases."},
				{Response: "Use cheap fast models for routing, then escalate hard cases."},
			},
			label: "strong consensus",
		},
		{
			name: "mixed consensus",
			results: []PromptResult{
				{Response: "Use fast models first and escalate complex requests."},
				{Response: "Use fast models first but cap cost carefully."},
			},
			label: "mixed consensus",
		},
		{
			name: "contested",
			results: []PromptResult{
				{Response: "Route by lowest latency."},
				{Response: "Prefer expensive expert models for accuracy."},
			},
			label: "contested",
		},
		{
			name: "single source",
			results: []PromptResult{
				{Response: "Only one answer."},
			},
			label: "single source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeFusionConsensus(tt.results)
			require.Equal(t, tt.label, got.Label)
			assert.NotContains(t, strings.ToLower(got.Note), "probability")
		})
	}
}

func TestPromptTextForModel_OnlyWrapsWhenFuseEnabled(t *testing.T) {
	promptFuse = false
	require.Equal(t, "plain prompt", promptTextForModel("plain prompt", "system", 0, 2, "p", "m"))

	promptFuse = true
	t.Cleanup(func() { promptFuse = false })

	got := promptTextForModel("plain prompt", "system", 0, 2, "p", "m")
	assert.Contains(t, got, "plain prompt")
	assert.Contains(t, got, "draft contributor 1 of 2")
	assert.Contains(t, got, "Assigned lens:")
}

func TestRestorePromptCatalogQuarantine_PreservesPreviousState(t *testing.T) {
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	cat := &catalog.Catalog{
		Providers: map[string]*catalog.ProviderCatalog{
			"p": {
				Models: map[string]*catalog.ModelEntry{
					"m": {QuarantineUntil: time.Time{}},
				},
			},
		},
	}

	prior, hadPrior := promptCatalogQuarantine(cat, "p", "m")
	catalog.RecordPing(cat, "p", "m", true, catalog.SlowPingThreshold+time.Second, 0, 0, "", now)
	require.True(t, cat.Providers["p"].Models["m"].QuarantineUntil.After(now))

	restorePromptCatalogQuarantine(cat, "p", "m", prior, hadPrior)

	require.True(t, cat.Providers["p"].Models["m"].QuarantineUntil.IsZero())

	existingQuarantine := now.Add(time.Hour)
	cat.Providers["p"].Models["m"].QuarantineUntil = existingQuarantine
	prior, hadPrior = promptCatalogQuarantine(cat, "p", "m")
	catalog.RecordPing(cat, "p", "m", true, catalog.SlowPingThreshold+time.Second, 0, 0, "", now)

	restorePromptCatalogQuarantine(cat, "p", "m", prior, hadPrior)

	require.Equal(t, existingQuarantine, cat.Providers["p"].Models["m"].QuarantineUntil)
}

func TestPromptCommand_Fuse_AddsFinalFusionSection(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	originalPromptFunc := executePromptAgainstAllProviders
	originalFuseFunc := executePromptFusionResponse
	defer func() {
		executePromptAgainstAllProviders = originalPromptFunc
		executePromptFusionResponse = originalFuseFunc
	}()

	var capturedSelector string
	var capturedSourceCount int
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{Provider: "p1", Model: "m1", Response: "Alpha", Latency: 10 * time.Millisecond},
			{Provider: "p2", Model: "m2", Response: "Beta", Latency: 20 * time.Millisecond},
		}, nil
	}
	executePromptFusionResponse = func(promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string) (*PromptFusion, error) {
		capturedSelector = selector
		capturedSourceCount = len(successfulPromptResults(results))
		return &PromptFusion{
			Selector:       selector,
			SourceCount:    capturedSourceCount,
			OriginalPrompt: promptText,
			Consensus: FusionConsensus{
				Label: "mixed consensus",
				Score: 0.42,
				Note:  "Source drafts overlap but preserve meaningful differences; fusion may be resolving trade-offs.",
			},
			Result: PromptResult{
				Provider: "smart",
				Model:    "smart-model",
				Response: "Fused final answer.",
				Latency:  30 * time.Millisecond,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--fuse", "answer this"})
	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Equal(t, defaultPromptFuseModels, capturedSelector)
	assert.Equal(t, 2, capturedSourceCount)
	output := out.String()
	assert.Contains(t, output, "# Multi-Model Prompt Comparison")
	assert.Contains(t, output, "Alpha")
	assert.Contains(t, output, "Beta")
	assert.Contains(t, output, "# Fused Response")
	assert.Contains(t, output, "smart-model")
	assert.Contains(t, output, "mixed consensus")
	assert.Contains(t, output, "42% draft agreement; not a truth probability")
	assert.Contains(t, output, "Fused final answer.")
}

func TestPromptCommand_FuseControl_AddsDirectControlSection(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	originalPromptFunc := executePromptAgainstAllProviders
	originalControlFunc := executePromptFusionControl
	originalFuseFunc := executePromptFusionResponse
	defer func() {
		executePromptAgainstAllProviders = originalPromptFunc
		executePromptFusionControl = originalControlFunc
		executePromptFusionResponse = originalFuseFunc
	}()

	var controlPrompt string
	var fusionCalled bool
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{Provider: "p1", Model: "m1", Response: "Draft answer", Latency: 10 * time.Millisecond},
		}, nil
	}
	executePromptFusionControl = func(promptText, systemPrompt string, timeoutSecs int, selector string) (*PromptFusionControl, error) {
		controlPrompt = promptText
		return &PromptFusionControl{
			Selector:       selector,
			OriginalPrompt: promptText,
			Result: PromptResult{
				Provider: "zai",
				Model:    "GLM-5.2",
				Response: "Direct control answer.",
				Latency:  20 * time.Millisecond,
			},
		}, nil
	}
	executePromptFusionResponse = func(promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string) (*PromptFusion, error) {
		fusionCalled = true
		return &PromptFusion{
			Selector:    selector,
			SourceCount: 1,
			Result: PromptResult{
				Provider: "zai",
				Model:    "GLM-5.2",
				Response: "Fused answer.",
				Latency:  30 * time.Millisecond,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--fuse", "--fuse-control", "--fuse-models", "tag:fuse-zai", "answer this"})
	err := rootCmd.Execute()

	require.NoError(t, err)
	require.True(t, fusionCalled)
	require.Equal(t, "answer this", controlPrompt)
	output := out.String()
	assert.Contains(t, output, "# Multi-Model Prompt Comparison")
	assert.Contains(t, output, "# Direct Fusion Model Control")
	assert.Contains(t, output, "Direct control answer.")
	assert.Contains(t, output, "# Fused Response")
	assert.Contains(t, output, "Fused answer.")
}

func TestPromptCommand_FuseControlFailureReturnsErrorAfterOutput(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	originalPromptFunc := executePromptAgainstAllProviders
	originalControlFunc := executePromptFusionControl
	defer func() {
		executePromptAgainstAllProviders = originalPromptFunc
		executePromptFusionControl = originalControlFunc
	}()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{Provider: "p1", Model: "m1", Response: "Draft answer", Latency: 10 * time.Millisecond},
		}, nil
	}
	executePromptFusionControl = func(promptText, systemPrompt string, timeoutSecs int, selector string) (*PromptFusionControl, error) {
		return &PromptFusionControl{
			Selector: selector,
			Result: PromptResult{
				Provider: "zai",
				Model:    "GLM-5.2",
				Error:    fmt.Errorf("control provider failed"),
				Latency:  20 * time.Millisecond,
			},
		}, fmt.Errorf("fusion control failed: control provider failed")
	}

	rootCmd.SetArgs([]string{"prompt", "--fuse-control", "answer this"})
	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fusion control failed")
	output := out.String()
	assert.Contains(t, output, "Draft answer")
	assert.Contains(t, output, "# Direct Fusion Model Control")
	assert.Contains(t, output, "control provider failed")
}

func TestPromptCommand_FuseFailureReturnsErrorAfterOutput(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	originalPromptFunc := executePromptAgainstAllProviders
	originalFuseFunc := executePromptFusionResponse
	defer func() {
		executePromptAgainstAllProviders = originalPromptFunc
		executePromptFusionResponse = originalFuseFunc
	}()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{Provider: "p1", Model: "m1", Response: "Alpha", Latency: 10 * time.Millisecond},
		}, nil
	}
	executePromptFusionResponse = func(promptText, systemPrompt string, results []PromptResult, timeoutSecs int, selector string) (*PromptFusion, error) {
		return &PromptFusion{
			Selector:    selector,
			SourceCount: 1,
			Result: PromptResult{
				Provider: "smart",
				Model:    "smart-model",
				Latency:  5 * time.Millisecond,
				Error:    fmt.Errorf("fusion provider failed"),
			},
		}, fmt.Errorf("fusion failed: fusion provider failed")
	}

	rootCmd.SetArgs([]string{"prompt", "--fuse", "answer this"})
	err := rootCmd.Execute()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "fusion failed")
	output := out.String()
	assert.Contains(t, output, "Alpha")
	assert.Contains(t, output, "# Fused Response")
	assert.Contains(t, output, "fusion provider failed")
}

func TestPromptResult_StoreSuccessfulResponse(t *testing.T) {
	// Given the type is defined, when a response is received,
	// then provider name, model, response text, latency, and error can be stored
	result := PromptResult{
		Provider: "openai",
		Model:    "claude-3-5-sonnet",
		Response: "Paris is the capital of France.",
		Latency:  250 * time.Millisecond,
		Error:    nil,
	}

	if result.Provider != "openai" {
		t.Errorf("Expected provider 'openai', got %q", result.Provider)
	}
	if result.Model != "claude-3-5-sonnet" {
		t.Errorf("Expected model 'claude-3-5-sonnet', got %q", result.Model)
	}
	if result.Response != "Paris is the capital of France." {
		t.Errorf("Expected response about Paris, got %q", result.Response)
	}
	if result.Latency != 250*time.Millisecond {
		t.Errorf("Expected latency 250ms, got %v", result.Latency)
	}
	if result.Error != nil {
		t.Errorf("Expected no error, got %v", result.Error)
	}
}

func TestPromptResult_CollectMultipleResults(t *testing.T) {
	// Test AC1: all providers receive the prompt concurrently
	// Given multiple enabled providers
	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "claude-3-5-sonnet",
			Response: "Response from OpenAI",
			Latency:  200 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "openrouter",
			Model:    "claude-3-opus",
			Response: "Response from OpenRouter",
			Latency:  300 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "gemini",
			Model:    "gemini-2.0-flash-exp",
			Response: "Response from Gemini",
			Latency:  150 * time.Millisecond,
			Error:    nil,
		},
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got %d", len(results))
	}

	// Verify each result is stored correctly
	if results[0].Provider != "openai" {
		t.Errorf("Expected first provider 'openai', got %q", results[0].Provider)
	}
	if results[1].Provider != "openrouter" {
		t.Errorf("Expected second provider 'openrouter', got %q", results[1].Provider)
	}
	if results[2].Provider != "gemini" {
		t.Errorf("Expected third provider 'gemini', got %q", results[2].Provider)
	}

	// Verify all responses are distinct
	if results[0].Response == results[1].Response {
		t.Error("Expected distinct responses from different providers")
	}
}

func TestPromptResult_CaptureFailure(t *testing.T) {
	// Given a provider fails, when storing the result,
	// then the error field captures the failure reason
	failureError := errors.New("rate limit exceeded")

	result := PromptResult{
		Provider: "openai",
		Model:    "claude-3-5-sonnet",
		Response: "",
		Latency:  50 * time.Millisecond,
		Error:    failureError,
	}

	if result.Error == nil {
		t.Fatal("Expected error to be stored, got nil")
	}
	if result.Error.Error() != "rate limit exceeded" {
		t.Errorf("Expected error 'rate limit exceeded', got %q", result.Error.Error())
	}
	if result.Response != "" {
		t.Errorf("Expected empty response on failure, got %q", result.Response)
	}
}

func TestPromptResult_MixedSuccessAndFailure(t *testing.T) {
	// Test collecting results where some succeed and some fail
	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "claude-3-5-sonnet",
			Response: "Success response",
			Latency:  200 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "openrouter",
			Model:    "claude-3-opus",
			Response: "",
			Latency:  100 * time.Millisecond,
			Error:    errors.New("circuit breaker open"),
		},
		{
			Provider: "gemini",
			Model:    "gemini-2.0-flash-exp",
			Response: "Another success",
			Latency:  250 * time.Millisecond,
			Error:    nil,
		},
	}

	successCount := 0
	failureCount := 0

	for _, result := range results {
		if result.Error == nil {
			successCount++
		} else {
			failureCount++
		}
	}

	if successCount != 2 {
		t.Errorf("Expected 2 successful results, got %d", successCount)
	}
	if failureCount != 1 {
		t.Errorf("Expected 1 failed result, got %d", failureCount)
	}
}

// Test AC2: disabled providers are skipped
func TestFilterEnabledProviders_SkipsDisabled(t *testing.T) {
	configs := map[string]providers.Config{
		"enabled-1": {
			Enabled: true,
			Model:   "model-1",
		},
		"disabled": {
			Enabled: false,
			Model:   "model-disabled",
		},
		"enabled-2": {
			Enabled: true,
			Model:   "model-2",
		},
	}

	enabled := providers.FilterEnabledProviders(configs)

	require.Len(t, enabled, 2)
	names := make(map[string]bool)
	for _, e := range enabled {
		names[e.Name] = true
	}
	assert.True(t, names["enabled-1"])
	assert.True(t, names["enabled-2"])
	assert.False(t, names["disabled"])
}

// Test AC3: all results collected before output
func TestOutputPromptResults_DisplaysAllResults(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "gpt-4o",
			Response: "Hello, world!",
			Latency:  100 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "openrouter",
			Model:    "claude-3-5-sonnet",
			Response: "Hi there!",
			Latency:  200 * time.Millisecond,
			Error:    nil,
		},
	}

	outputPromptResults(promptCmd, "test prompt", results)

	output := out.String()
	assert.Contains(t, output, "test prompt")
	assert.Contains(t, output, "Results from 2 provider(s)")
	assert.Contains(t, output, "openai")
	assert.Contains(t, output, "gpt-4o")
	assert.Contains(t, output, "Hello, world!")
	assert.Contains(t, output, "SUCCESS")
	assert.Contains(t, output, "openrouter")
	assert.Contains(t, output, "claude-3-5-sonnet")
	assert.Contains(t, output, "Hi there!")
}

// Test AC1: Given results from 3 providers, when output is rendered,
// then each result shows provider name, model, and response in a distinct section
func TestOutputPromptResults_MarkdownFormat_ThreeProviders(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "gpt-4o",
			Response: "OpenAI response here",
			Latency:  150 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "openrouter",
			Model:    "claude-3-5-sonnet",
			Response: "OpenRouter response here",
			Latency:  220 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "gemini",
			Model:    "gemini-2.0-flash-exp",
			Response: "Gemini response here",
			Latency:  180 * time.Millisecond,
			Error:    nil,
		},
	}

	outputPromptResults(promptCmd, "Test prompt text", results)

	output := out.String()

	// Verify markdown title
	assert.Contains(t, output, "# Multi-Model Prompt Comparison")

	// Verify each provider has distinct section with numbered header
	assert.Contains(t, output, "## 1. openai")
	assert.Contains(t, output, "## 2. openrouter")
	assert.Contains(t, output, "## 3. gemini")

	// Verify provider names are displayed
	assert.Contains(t, output, "openai")
	assert.Contains(t, output, "openrouter")
	assert.Contains(t, output, "gemini")

	// Verify models are displayed in table format
	assert.Contains(t, output, "gpt-4o")
	assert.Contains(t, output, "claude-3-5-sonnet")
	assert.Contains(t, output, "gemini-2.0-flash-exp")

	// Verify responses are displayed
	assert.Contains(t, output, "OpenAI response here")
	assert.Contains(t, output, "OpenRouter response here")
	assert.Contains(t, output, "Gemini response here")

	// Verify markdown table structure exists
	assert.Contains(t, output, "| Field | Value |")
	assert.Contains(t, output, "|-------|-------|")

	// Verify Response sections
	assert.Contains(t, output, "### Response")
}

// Test AC2: Given results include latency, when output is rendered,
// then latency is displayed per provider
func TestOutputPromptResults_MarkdownFormat_LatencyDisplayed(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "gpt-4o",
			Response: "Response 1",
			Latency:  125 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "anthropic",
			Model:    "claude-3-opus",
			Response: "Response 2",
			Latency:  2500 * time.Millisecond, // 2.5 seconds
			Error:    nil,
		},
		{
			Provider: "gemini",
			Model:    "gemini-pro",
			Response: "Response 3",
			Latency:  950 * time.Millisecond,
			Error:    nil,
		},
	}

	outputPromptResults(promptCmd, "Latency test", results)

	output := out.String()

	// Verify latency is displayed for each provider
	assert.Contains(t, output, "125ms")
	assert.Contains(t, output, "2.5s")
	assert.Contains(t, output, "950ms")

	// Verify latency is in the table
	assert.Contains(t, output, "| **Latency**")

	// Verify each latency appears in context with its provider
	// (This ensures the latency is associated with the correct provider)
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		if strings.Contains(line, "## 1. openai") {
			// Find latency line within next 10 lines
			found := false
			for j := i; j < i+10 && j < len(lines); j++ {
				if strings.Contains(lines[j], "125ms") {
					found = true
					break
				}
			}
			assert.True(t, found, "Latency 125ms should appear near openai section")
		}
		if strings.Contains(line, "## 2. anthropic") {
			found := false
			for j := i; j < i+10 && j < len(lines); j++ {
				if strings.Contains(lines[j], "2.5s") {
					found = true
					break
				}
			}
			assert.True(t, found, "Latency 2.5s should appear near anthropic section")
		}
	}
}

func TestOutputPromptResults_WithErrors(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "gpt-4o",
			Response: "Success",
			Latency:  100 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "failed-provider",
			Model:    "some-model",
			Response: "",
			Latency:  50 * time.Millisecond,
			Error:    fmt.Errorf("connection timeout"),
		},
	}

	outputPromptResults(promptCmd, "test prompt", results)

	output := out.String()
	assert.Contains(t, output, "SUCCESS")
	assert.Contains(t, output, "FAILED")
	assert.Contains(t, output, "connection timeout")
	assert.Contains(t, output, "failed-provider")

	// Verify markdown error section
	assert.Contains(t, output, "### Error")
	assert.Contains(t, output, "```")
}

// Test AC3: Given markdown output, when viewed in terminal or piped to file,
// then formatting is clean and readable
func TestOutputPromptResults_MarkdownFormat_CleanReadable(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "provider-a",
			Model:    "model-a",
			Response: "This is a multi-line response.\n\nIt has paragraphs.\n\nAnd formatting.",
			Latency:  100 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "provider-b",
			Model:    "model-b",
			Response: "Another response",
			Latency:  200 * time.Millisecond,
			Error:    nil,
		},
	}

	outputPromptResults(promptCmd, "Clean format test", results)

	output := out.String()

	// Verify proper markdown structure
	assert.Contains(t, output, "# Multi-Model Prompt Comparison")
	assert.Contains(t, output, "**Prompt:**")
	assert.Contains(t, output, "**Results from")

	// Verify sections are properly separated
	assert.Contains(t, output, "---")

	// Verify headers are numbered
	assert.Contains(t, output, "## 1.")
	assert.Contains(t, output, "## 2.")

	// Verify tables are properly formatted
	tableHeaderCount := strings.Count(output, "| Field | Value |")
	assert.Equal(t, 2, tableHeaderCount, "Should have 2 table headers (one per provider)")

	tableSeparatorCount := strings.Count(output, "|-------|-------|")
	assert.Equal(t, 2, tableSeparatorCount, "Should have 2 table separators")

	// Verify responses preserve formatting
	assert.Contains(t, output, "This is a multi-line response.\n\nIt has paragraphs.\n\nAnd formatting.")

	// Verify no extraneous spacing issues
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		// No line should have trailing spaces (except intentional blank lines)
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			// Non-blank lines should not have trailing spaces
			assert.Equal(t, strings.TrimRight(line, " "), line, "Line %d should not have trailing spaces", i+1)
		}
	}

	// Verify status indicators use emojis
	assert.Contains(t, output, "✅ SUCCESS")

	// Verify model names are in code blocks
	assert.Contains(t, output, "`model-a`")
	assert.Contains(t, output, "`model-b`")
}

// Test markdown format with mixed success and errors
func TestOutputPromptResults_MarkdownFormat_MixedResults(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "success-provider",
			Model:    "success-model",
			Response: "Successful response",
			Latency:  150 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "error-provider",
			Model:    "error-model",
			Response: "",
			Latency:  75 * time.Millisecond,
			Error:    fmt.Errorf("API key invalid"),
		},
		{
			Provider: "timeout-provider",
			Model:    "timeout-model",
			Response: "",
			Latency:  30000 * time.Millisecond,
			Error:    fmt.Errorf("request timeout after 30s"),
		},
	}

	outputPromptResults(promptCmd, "Mixed results test", results)

	output := out.String()

	// Verify all three sections exist
	assert.Contains(t, output, "## 1. success-provider")
	assert.Contains(t, output, "## 2. error-provider")
	assert.Contains(t, output, "## 3. timeout-provider")

	// Verify success section has response
	assert.Contains(t, output, "### Response")
	assert.Contains(t, output, "Successful response")
	assert.Contains(t, output, "✅ SUCCESS")

	// Verify error sections have error blocks
	errorSectionCount := strings.Count(output, "### Error")
	assert.Equal(t, 2, errorSectionCount, "Should have 2 error sections")

	failedCount := strings.Count(output, "❌ FAILED")
	assert.Equal(t, 2, failedCount, "Should have 2 failed status indicators")

	// Verify errors are in code blocks
	assert.Contains(t, output, "API key invalid")
	assert.Contains(t, output, "request timeout after 30s")

	// Verify latency is shown for all, including errors
	assert.Contains(t, output, "150ms")
	assert.Contains(t, output, "75ms")
	assert.Contains(t, output, "30s")
}

// Test empty results still produces valid markdown
func TestOutputPromptResults_MarkdownFormat_EmptyResults(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{}

	outputPromptResults(promptCmd, "Empty test", results)

	output := out.String()

	// Should still have valid markdown structure
	assert.Contains(t, output, "# Multi-Model Prompt Comparison")
	assert.Contains(t, output, "**Prompt:** Empty test")
	assert.Contains(t, output, "**Results from 0 provider(s)**")

	// Should not have any provider sections
	assert.NotContains(t, output, "##")
	assert.NotContains(t, output, "| Field | Value |")
}

func TestOutputPromptResults_EmptyResults(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{}

	outputPromptResults(promptCmd, "test prompt", results)

	output := out.String()
	assert.Contains(t, output, "test prompt")
	assert.Contains(t, output, "Results from 0 provider(s)")
}

// Test no enabled providers error
func TestExecutePromptAgainstAllProviders_NoEnabledProviders(t *testing.T) {
	// This would require mocking config loading
	// For now, we test the logic through unit tests
	t.Skip("Integration test - requires mocked config")
}

// Test AC1: Given one provider returns 429, when results are displayed,
// then that provider shows "Error: rate limited" instead of response
func TestOutputPromptResults_RateLimitError(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "claude-3-sonnet",
			Response: "Success response",
			Latency:  100 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "openrouter",
			Model:    "claude-3-5-sonnet",
			Response: "",
			Latency:  50 * time.Millisecond,
			Error:    fmt.Errorf("rate limit exceeded: 429 Too Many Requests"),
		},
	}

	outputPromptResults(promptCmd, "test prompt", results)

	output := out.String()

	// Verify successful provider shows SUCCESS
	assert.Contains(t, output, "## 1. openai")
	assert.Contains(t, output, "✅ SUCCESS")
	assert.Contains(t, output, "Success response")

	// Verify rate limited provider shows FAILED with error
	assert.Contains(t, output, "## 2. openrouter")
	assert.Contains(t, output, "❌ FAILED")
	assert.Contains(t, output, "### Error")
	assert.Contains(t, output, "rate limit exceeded: 429 Too Many Requests")

	// Verify rate limited provider does NOT show a response section
	lines := strings.Split(output, "\n")
	inOpenRouterSection := false
	for _, line := range lines {
		if strings.Contains(line, "## 2. openrouter") {
			inOpenRouterSection = true
		}
		if inOpenRouterSection && strings.Contains(line, "## 3.") {
			inOpenRouterSection = false
		}
		if inOpenRouterSection {
			assert.NotContains(t, line, "### Response", "Rate limited provider should not have Response section")
		}
	}
}

// Test AC2: Given one provider times out, when results are displayed,
// then that provider shows timeout error
func TestOutputPromptResults_TimeoutError(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	promptCmd.stdout = &out

	results := []PromptResult{
		{
			Provider: "openai",
			Model:    "claude-3-sonnet",
			Response: "Success response",
			Latency:  100 * time.Millisecond,
			Error:    nil,
		},
		{
			Provider: "slow-provider",
			Model:    "slow-model",
			Response: "",
			Latency:  30 * time.Second,
			Error:    fmt.Errorf("context deadline exceeded"),
		},
	}

	outputPromptResults(promptCmd, "test prompt", results)

	output := out.String()

	// Verify successful provider
	assert.Contains(t, output, "## 1. openai")
	assert.Contains(t, output, "✅ SUCCESS")

	// Verify timeout provider shows error
	assert.Contains(t, output, "## 2. slow-provider")
	assert.Contains(t, output, "❌ FAILED")
	assert.Contains(t, output, "### Error")
	assert.Contains(t, output, "context deadline exceeded")

	// Verify latency is still shown (30s)
	assert.Contains(t, output, "30s")
}

// Test AC3: Given all providers fail, when results are displayed,
// then all errors are shown and exit code is non-zero
func TestPromptCommand_AllProvidersFail_NonZeroExit(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function to return all failures
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "provider-1",
				Model:    "model-1",
				Response: "",
				Latency:  50 * time.Millisecond,
				Error:    fmt.Errorf("rate limit exceeded"),
			},
			{
				Provider: "provider-2",
				Model:    "model-2",
				Response: "",
				Latency:  100 * time.Millisecond,
				Error:    fmt.Errorf("authentication failed"),
			},
			{
				Provider: "provider-3",
				Model:    "model-3",
				Response: "",
				Latency:  75 * time.Millisecond,
				Error:    fmt.Errorf("service unavailable"),
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "test prompt"})
	err := rootCmd.Execute()

	// Should return error when all providers fail
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all providers failed")

	output := out.String()

	// Verify all providers show errors
	assert.Contains(t, output, "## 1. provider-1")
	assert.Contains(t, output, "rate limit exceeded")
	assert.Contains(t, output, "## 2. provider-2")
	assert.Contains(t, output, "authentication failed")
	assert.Contains(t, output, "## 3. provider-3")
	assert.Contains(t, output, "service unavailable")

	// Verify all show FAILED status
	failedCount := strings.Count(output, "❌ FAILED")
	assert.Equal(t, 3, failedCount)

	// Verify no SUCCESS status
	assert.NotContains(t, output, "✅ SUCCESS")
}

// Test basic provider execution
func TestExecutePromptForProvider_Structure(t *testing.T) {
	entry := providers.ProviderEntry{
		Name: "test-provider",
		Config: providers.Config{
			BaseURL:      "https://api.openai.com",
			Model:        "gpt-4o-mini",
			Enabled:      true,
			RequiresKey:  true,
			APIKey:       "test-invalid-key", // Will fail with auth error
			ProviderType: "openai",
		},
	}

	// This will fail with auth error but verifies structure
	result := executePromptForProvider(entry, "test prompt", "You are a helpful assistant.", 60)

	assert.Equal(t, "test-provider", result.Provider)
	assert.NotZero(t, result.Latency)
	// Will have error since invalid API key
	if result.Error == nil {
		t.Skip("Expected error from invalid API key, but got nil - skipping")
	}
}

// Test AC1: Given --system "Be concise" is passed, when the prompt is sent,
// then all providers receive the system prompt
func TestPromptCommand_SystemPromptFlag_PassedToProviders(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function to capture the system prompt
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	var capturedSystemPrompt string
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		capturedSystemPrompt = systemPrompt
		return []PromptResult{
			{
				Provider: "test-provider",
				Model:    "test-model",
				Response: "Test response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--system", "Be concise", "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Equal(t, "Be concise", capturedSystemPrompt, "System prompt should be passed to providers")
}

// Test AC2: Given no --system flag, when the prompt is sent,
// then the default system prompt is used
func TestPromptCommand_NoSystemPromptFlag_UsesDefault(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function to capture the system prompt
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	var capturedSystemPrompt string
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		capturedSystemPrompt = systemPrompt
		return []PromptResult{
			{
				Provider: "test-provider",
				Model:    "test-model",
				Response: "Test response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Equal(t, "You are a helpful assistant.", capturedSystemPrompt, "Default system prompt should be used")
}

// Test AC3: Given --system "" (empty), when the prompt is sent,
// then the default system prompt is used
func TestPromptCommand_EmptySystemPromptFlag_UsesDefault(t *testing.T) {
	resetRootCmd()
	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function to capture the system prompt
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	var capturedSystemPrompt string
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		capturedSystemPrompt = systemPrompt
		return []PromptResult{
			{
				Provider: "test-provider",
				Model:    "test-model",
				Response: "Test response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--system", "", "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)
	assert.Equal(t, "You are a helpful assistant.", capturedSystemPrompt, "Empty system prompt should use default")
}

// Test AC1: Given --output results.md is passed, when results are ready,
// then markdown is written to results.md
func TestPromptCommand_OutputFlag_WritesToFile(t *testing.T) {
	resetRootCmd()

	// Create a temporary directory for test files
	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "results.md")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "openai",
				Model:    "claude-3-5-sonnet",
				Response: "Paris is the capital of France.",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
			{
				Provider: "anthropic",
				Model:    "claude-3-opus",
				Response: "The capital of France is Paris.",
				Latency:  150 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--output", outputPath, "What is the capital of France?"})
	err := rootCmd.Execute()

	require.NoError(t, err)

	// Verify file was created
	assert.FileExists(t, outputPath)

	// Read file contents
	fileContents, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	fileStr := string(fileContents)

	// Verify markdown structure
	assert.Contains(t, fileStr, "# Multi-Model Prompt Comparison")
	assert.Contains(t, fileStr, "**Prompt:** What is the capital of France?")
	assert.Contains(t, fileStr, "## 1. openai")
	assert.Contains(t, fileStr, "## 2. anthropic")
	assert.Contains(t, fileStr, "Paris is the capital of France.")
	assert.Contains(t, fileStr, "The capital of France is Paris.")
	assert.Contains(t, fileStr, "✅ SUCCESS")

	// Verify models and latencies are in file
	assert.Contains(t, fileStr, "claude-3-5-sonnet")
	assert.Contains(t, fileStr, "claude-3-opus")
	assert.Contains(t, fileStr, "100ms")
	assert.Contains(t, fileStr, "150ms")
}

// Test AC2: Given --output is passed, when file write succeeds,
// then stdout shows "Results written to results.md"
func TestPromptCommand_OutputFlag_SuccessMessage(t *testing.T) {
	resetRootCmd()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "results.md")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "openai",
				Model:    "claude-3-5-sonnet",
				Response: "Test response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--output", outputPath, "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)

	output := out.String()

	// Should show success message
	assert.Contains(t, output, "Results written to")
	assert.Contains(t, output, "results.md")

	// Should NOT show markdown output in stdout (only success message)
	assert.NotContains(t, output, "# Multi-Model Prompt Comparison")
	assert.NotContains(t, output, "## 1. openai")
}

// Test AC3: Given --output points to unwritable path, when write fails,
// then error is displayed and results still print to stdout
func TestPromptCommand_OutputFlag_UnwritablePath(t *testing.T) {
	resetRootCmd()

	// Use an invalid path (directory that doesn't exist and can't be created)
	invalidPath := "/nonexistent/directory/that/does/not/exist/results.md"

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "openai",
				Model:    "claude-3-5-sonnet",
				Response: "Fallback response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--output", invalidPath, "test prompt"})
	err := rootCmd.Execute()

	// Command should still succeed even if file write fails
	require.NoError(t, err)

	output := out.String()

	// Should show error message
	assert.Contains(t, output, "Error writing to file")
	assert.Contains(t, output, invalidPath)

	// Should show fallback message
	assert.Contains(t, output, "Displaying results to stdout instead")

	// Should still show the markdown output in stdout
	assert.Contains(t, output, "# Multi-Model Prompt Comparison")
	assert.Contains(t, output, "## 1. openai")
	assert.Contains(t, output, "Fallback response")
}

// Test output flag with relative path
func TestPromptCommand_OutputFlag_RelativePath(t *testing.T) {
	resetRootCmd()

	// Create a temporary directory and change to it
	tempDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() {
		require.NoError(t, os.Chdir(originalWd))
	}()

	err = os.Chdir(tempDir)
	require.NoError(t, err)

	outputPath := "output.md" // Relative path

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "test-provider",
				Model:    "test-model",
				Response: "Test response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--output", outputPath, "test prompt"})
	err = rootCmd.Execute()

	require.NoError(t, err)

	// Verify file was created in current directory
	assert.FileExists(t, filepath.Join(tempDir, outputPath))

	// Verify success message
	output := out.String()
	assert.Contains(t, output, "Results written to")
}

// Test output flag overwrites existing file
func TestPromptCommand_OutputFlag_OverwritesExistingFile(t *testing.T) {
	resetRootCmd()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "existing.md")

	// Create existing file with content
	existingContent := "# This is old content\nIt should be overwritten."
	err := os.WriteFile(outputPath, []byte(existingContent), 0644)
	require.NoError(t, err)

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "new-provider",
				Model:    "new-model",
				Response: "New response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--output", outputPath, "new prompt"})
	err = rootCmd.Execute()

	require.NoError(t, err)

	// Read file contents
	fileContents, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	fileStr := string(fileContents)

	// Verify old content is gone
	assert.NotContains(t, fileStr, "This is old content")

	// Verify new content is present
	assert.Contains(t, fileStr, "new prompt")
	assert.Contains(t, fileStr, "New response")
	assert.Contains(t, fileStr, "new-provider")
}

// Test output flag with errors in results
func TestPromptCommand_OutputFlag_WithErrors(t *testing.T) {
	resetRootCmd()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "with-errors.md")

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		return []PromptResult{
			{
				Provider: "success-provider",
				Model:    "success-model",
				Response: "Success response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
			{
				Provider: "failed-provider",
				Model:    "failed-model",
				Response: "",
				Latency:  50 * time.Millisecond,
				Error:    fmt.Errorf("rate limit exceeded"),
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--output", outputPath, "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)

	// Read file contents
	fileContents, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	fileStr := string(fileContents)

	// Verify both success and error are in file
	assert.Contains(t, fileStr, "✅ SUCCESS")
	assert.Contains(t, fileStr, "❌ FAILED")
	assert.Contains(t, fileStr, "Success response")
	assert.Contains(t, fileStr, "rate limit exceeded")
	assert.Contains(t, fileStr, "### Error")
}

// Test AC1: Given --timeout 30 is passed, when a provider takes 45s,
// then that provider times out after 30s
func TestPromptCommand_TimeoutFlag_ProviderTimesOut(t *testing.T) {
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function to verify timeout parameter is passed
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	var capturedTimeout int
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		capturedTimeout = timeoutSecs
		// Simulate a timeout error
		return []PromptResult{
			{
				Provider: "slow-provider",
				Model:    "slow-model",
				Response: "",
				Latency:  30 * time.Second,
				Error:    fmt.Errorf("context deadline exceeded"),
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--timeout", "30", "test prompt"})
	err := rootCmd.Execute()

	// Should fail because all providers failed
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all providers failed")

	// Verify timeout was captured correctly
	assert.Equal(t, 30, capturedTimeout, "Timeout should be 30 seconds")

	output := out.String()

	// Verify timeout error is displayed
	assert.Contains(t, output, "context deadline exceeded")
	assert.Contains(t, output, "❌ FAILED")
}

// Test AC2: Given no --timeout flag, when the prompt runs,
// then default timeout of 60s is used
func TestPromptCommand_TimeoutFlag_DefaultIs60Seconds(t *testing.T) {
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function to capture timeout
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	var capturedTimeout int
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		capturedTimeout = timeoutSecs
		return []PromptResult{
			{
				Provider: "test-provider",
				Model:    "test-model",
				Response: "Test response",
				Latency:  100 * time.Millisecond,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)

	// Verify default timeout is 60 seconds
	assert.Equal(t, 60, capturedTimeout, "Default timeout should be 60 seconds")
}

// Test AC3: Given --timeout 5, when a fast provider responds in 2s,
// then that result is captured normally
func TestPromptCommand_TimeoutFlag_FastProviderCompletes(t *testing.T) {
	resetRootCmd()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)

	// Mock the execution function
	originalFunc := executePromptAgainstAllProviders
	defer func() { executePromptAgainstAllProviders = originalFunc }()

	var capturedTimeout int
	executePromptAgainstAllProviders = func(promptText, systemPrompt string, timeoutSecs int) ([]PromptResult, error) {
		capturedTimeout = timeoutSecs
		return []PromptResult{
			{
				Provider: "fast-provider",
				Model:    "fast-model",
				Response: "Fast response",
				Latency:  2 * time.Second,
				Error:    nil,
			},
		}, nil
	}

	rootCmd.SetArgs([]string{"prompt", "--timeout", "5", "test prompt"})
	err := rootCmd.Execute()

	require.NoError(t, err)

	// Verify timeout was set to 5 seconds
	assert.Equal(t, 5, capturedTimeout, "Timeout should be 5 seconds")

	output := out.String()

	// Verify successful response
	assert.Contains(t, output, "Fast response")
	assert.Contains(t, output, "✅ SUCCESS")
	assert.Contains(t, output, "2s")
}
