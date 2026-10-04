package cmd

import (
	"bytes"
	"errors"
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

const defaultPromptFuseModels = "zai/GLM-5.2"
const defaultSmartPromptModels = "healthy"

func (cliOpts *invocationOptions) runPromptCommand(cmd *commandIO, args []string) error {
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	promptText, err := promptTextFromArgsOrStdin(args)
	if err != nil {
		return err
	}
	cliOpts.applyPromptUXDefaults(cmd)

	// Get system prompt from flag
	sysPrompt := cliOpts.systemPrompt
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
		results, err = cliOpts.executePromptAgainstSelectedModelsTo(cmd.Context(), cmd.ErrOrStderr(), promptText, sysPrompt, cliOpts.timeoutSeconds)
	} else {
		results, err = cliOpts.executePromptAgainstSelectedModelsWithRun(cmd.Context(), cmd.ErrOrStderr(), promptText, sysPrompt, cliOpts.timeoutSeconds, run)
	}
	if err != nil {
		return err
	}
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	baseErr := checkAllFailed(results)
	var control *PromptFusionControl
	var controlErr error
	if cliOpts.promptFuseControl && baseErr == nil {
		control, controlErr = cliOpts.executePromptFusionControlWithRun(cmd.Context(), promptText, sysPrompt, cliOpts.timeoutSeconds, cliOpts.promptFuseModels, run)
	}
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	var fusion *PromptFusion
	var fuseErr error
	if cliOpts.promptFuse && baseErr == nil {
		fusion, fuseErr = cliOpts.executePromptFusionResponseWithRun(cmd.Context(), promptText, sysPrompt, results, cliOpts.timeoutSeconds, cliOpts.promptFuseModels, run)
	}

	if err := cmd.Context().Err(); err != nil {
		return err
	}
	healthResults := results
	if control != nil {
		healthResults = append(append([]PromptResult(nil), healthResults...), control.Result)
	}
	if fusion != nil {
		healthResults = append(append([]PromptResult(nil), healthResults...), fusion.Result)
	}
	if err := recordPromptCatalogHealth(cmd.Context(), healthResults); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	}

	var outputErr error
	// If output file is specified, write to file
	if cliOpts.outputFile != "" {
		if err := writeResultsToFile(cmd, promptText, results, control, fusion, cliOpts.outputFile); err != nil {
			outputErr = err
			// Print error but continue to display results to stdout
			fmt.Fprintf(cmd.ErrOrStderr(), "Error writing to file %s: %v\n", cliOpts.outputFile, err)
			fmt.Fprintln(cmd.OutOrStdout(), "Displaying results to stdout instead:")
		} else {
			// Success message
			fmt.Fprintf(cmd.OutOrStdout(), "Results written to %s\n", cliOpts.outputFile)
			// Return early - don't print to stdout when file write succeeds
			return errors.Join(baseErr, controlErr, fuseErr)
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

	return errors.Join(baseErr, controlErr, fuseErr, outputErr)
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

func (cliOpts *invocationOptions) applyPromptUXDefaults(cmd *commandIO) {
	if cliOpts.promptSmart {
		if !cmd.FlagChanged("models") && cliOpts.promptModels == "" {
			cliOpts.promptModels = defaultSmartPromptModels
		}
		if !cmd.FlagChanged("fuse") {
			cliOpts.promptFuse = true
		}
	}

	if !cmd.FlagChanged("models") && cliOpts.promptModels == "" {
		cliOpts.promptModels = strings.TrimSpace(os.Getenv("LLAMBO_PROMPT_MODELS"))
	}
	if !cmd.FlagChanged("fuse") {
		if v, ok := boolEnv("LLAMBO_PROMPT_FUSE"); ok {
			cliOpts.promptFuse = v
		}
	}
	if !cmd.FlagChanged("fuse-models") {
		if v := strings.TrimSpace(os.Getenv("LLAMBO_PROMPT_FUSE_MODELS")); v != "" {
			cliOpts.promptFuseModels = v
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

// Scalar helpers retain their signatures with independent default options.
func applyPromptUXDefaults(cmd *commandIO) {
	defaultInvocationOptions().applyPromptUXDefaults(cmd)
}

func runPromptCommand(cmd *commandIO, args []string) error {
	return defaultInvocationOptions().runPromptCommand(cmd, args)
}
