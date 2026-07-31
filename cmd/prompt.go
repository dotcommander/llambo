package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"time"
)

// PromptResult captures the result of sending a prompt to a single model
type PromptResult struct {
	Provider            string        // Name of the provider (e.g., "openai", "openrouter")
	Model               string        // Model identifier used
	Response            string        // Response text from the model
	Latency             time.Duration // Time taken to get the response
	Error               error         // Error if the request failed
	CostStatus          string
	InputCostPer1M      float64
	OutputCostPer1M     float64
	EstimatedInputCost  float64
	EstimatedOutputCost float64
}

var (
	systemPrompt             string
	outputFile               string
	timeoutSeconds           int
	promptModels             string
	promptProviders          string
	promptIncludeQuarantine  bool
	promptMaxOutputCost      float64
	promptFreeOnly           bool
	promptIncludeUnknownCost bool
	promptSmart              bool
	promptFuse               bool
	promptFuseModels         string
	promptFuseControl        bool
)

const defaultPromptFuseModels = "zai/GLM-5.2"
const defaultSmartPromptModels = "healthy"

func runPromptCommand(cmd *commandIO, args []string) error {
	promptText, err := promptTextFromArgsOrStdin(args)
	if err != nil {
		return err
	}
	applyPromptUXDefaults(cmd)

	// Get system prompt from flag
	sysPrompt := systemPrompt
	// Treat empty string as no system prompt
	if sysPrompt == "" {
		sysPrompt = "You are a helpful assistant."
	}
	var run *promptRun
	if !hasInjectedPromptExecutionSeam() {
		run, err = newPromptRun(cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		defer run.close()
	}

	var results []PromptResult
	if run == nil {
		results, err = executePromptAgainstSelectedModelsTo(cmd.Context(), cmd.ErrOrStderr(), promptText, sysPrompt, timeoutSeconds)
	} else {
		results, err = executePromptAgainstSelectedModelsWithRun(cmd.Context(), cmd.ErrOrStderr(), promptText, sysPrompt, timeoutSeconds, run)
	}
	if err != nil {
		return err
	}
	baseErr := checkAllFailed(results)
	var control *PromptFusionControl
	var controlErr error
	if promptFuseControl && baseErr == nil {
		control, controlErr = executePromptFusionControlWithRun(cmd.Context(), promptText, sysPrompt, timeoutSeconds, promptFuseModels, run)
	}
	var fusion *PromptFusion
	var fuseErr error
	if promptFuse && baseErr == nil {
		fusion, fuseErr = executePromptFusionResponseWithRun(cmd.Context(), promptText, sysPrompt, results, timeoutSeconds, promptFuseModels, run)
	}

	healthResults := results
	if control != nil {
		healthResults = append(append([]PromptResult(nil), healthResults...), control.Result)
	}
	if fusion != nil {
		healthResults = append(append([]PromptResult(nil), healthResults...), fusion.Result)
	}
	if err := recordPromptCatalogHealth(healthResults); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	}

	// If output file is specified, write to file
	if outputFile != "" {
		if err := writeResultsToFile(cmd, promptText, results, control, fusion, outputFile); err != nil {
			// Print error but continue to display results to stdout
			fmt.Fprintf(cmd.ErrOrStderr(), "Error writing to file %s: %v\n", outputFile, err)
			fmt.Fprintln(cmd.OutOrStdout(), "Displaying results to stdout instead:")
		} else {
			// Success message
			fmt.Fprintf(cmd.OutOrStdout(), "Results written to %s\n", outputFile)
			// Return early - don't print to stdout when file write succeeds
			if baseErr != nil {
				return baseErr
			}
			if controlErr != nil {
				return controlErr
			}
			return fuseErr
		}
	}

	// Output results to stdout (either no file specified or file write failed)
	outputPromptResults(cmd, promptText, results)
	if control != nil {
		outputPromptFusionControl(cmd, control)
	}
	if fusion != nil {
		outputPromptFusion(cmd, fusion)
	}

	if baseErr != nil {
		return baseErr
	}
	if controlErr != nil {
		return controlErr
	}
	return fuseErr
}

// hasInjectedPromptExecutionSeam preserves the package-level execution seams
// used by focused tests and legacy integrations. Those callers own execution
// and must not initialize config-backed prompt runtime resources.
func hasInjectedPromptExecutionSeam() bool {
	return reflect.ValueOf(executePromptAgainstAllProviders).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstAllProviders).Pointer() ||
		reflect.ValueOf(executePromptAgainstSelectedModels).Pointer() != reflect.ValueOf(defaultExecutePromptAgainstSelectedModels).Pointer() ||
		reflect.ValueOf(executePromptFusionControl).Pointer() != reflect.ValueOf(defaultExecutePromptFusionControl).Pointer() ||
		reflect.ValueOf(executePromptFusionResponse).Pointer() != reflect.ValueOf(defaultExecutePromptFusionResponse).Pointer()
}

func applyPromptUXDefaults(cmd *commandIO) {
	if promptSmart {
		if !cmd.FlagChanged("models") && promptModels == "" {
			promptModels = defaultSmartPromptModels
		}
		if !cmd.FlagChanged("fuse") {
			promptFuse = true
		}
	}

	if !cmd.FlagChanged("models") && promptModels == "" {
		promptModels = strings.TrimSpace(os.Getenv("LLAMBO_PROMPT_MODELS"))
	}
	if !cmd.FlagChanged("fuse") {
		if v, ok := boolEnv("LLAMBO_PROMPT_FUSE"); ok {
			promptFuse = v
		}
	}
	if !cmd.FlagChanged("fuse-models") {
		if v := strings.TrimSpace(os.Getenv("LLAMBO_PROMPT_FUSE_MODELS")); v != "" {
			promptFuseModels = v
		}
	}
}

func boolEnv(name string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		return false, false
	}
}

func promptTextFromArgsOrStdin(args []string) (string, error) {
	if len(args) > 0 {
		promptText := strings.TrimSpace(strings.Join(args, " "))
		if promptText == "" {
			return "", fmt.Errorf("prompt text cannot be empty")
		}
		return promptText, nil
	}

	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect stdin: %w", err)
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return "", fmt.Errorf("prompt text required - usage: llambo prompt <text>")
	}

	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	promptText := strings.TrimSpace(string(data))
	if promptText == "" {
		return "", fmt.Errorf("prompt text cannot be empty")
	}
	return promptText, nil
}

// checkAllFailed returns error if all providers failed
func checkAllFailed(results []PromptResult) error {
	allFailed := true
	for _, result := range results {
		if result.Error == nil {
			allFailed = false
			break
		}
	}
	if allFailed && len(results) > 0 {
		return fmt.Errorf("all providers failed")
	}
	return nil
}

// writeResultsToFile writes the markdown results to a file
func writeResultsToFile(_ *commandIO, promptText string, results []PromptResult, control *PromptFusionControl, fusion *PromptFusion, filePath string) error {
	// Create a buffer to capture the output
	var buf bytes.Buffer

	// Create a temporary command with buffer as output
	tempCmd := &commandIO{stdout: &buf, stderr: io.Discard}

	// Generate markdown output to buffer
	outputPromptResults(tempCmd, promptText, results)
	if control != nil {
		outputPromptFusionControl(tempCmd, control)
	}
	if fusion != nil {
		outputPromptFusion(tempCmd, fusion)
	}

	// Write buffer to file
	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil {
		return err
	}

	return nil
}

// executePromptAgainstAllProviders sends the prompt to all enabled providers concurrently
// This is a variable to allow mocking in tests
