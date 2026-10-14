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

	// Get system prompt from flag; treat empty string as no system prompt.
	sysPrompt := cliOpts.systemPrompt
	if sysPrompt == "" {
		sysPrompt = "You are a helpful assistant."
	}
	req := promptRequest{promptText: promptText, systemPrompt: sysPrompt, timeoutSecs: cliOpts.timeoutSeconds}

	var run *promptRun
	if !hasInjectedPromptExecutionSeam() {
		run, err = newPromptRun(cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		defer run.close()
	}

	results, err := cliOpts.executePromptResults(cmd, req, run)
	if err != nil {
		return err
	}
	if err := cmd.Context().Err(); err != nil {
		return err
	}

	stages := cliOpts.executePromptFusionStages(cmd, req, results, run)
	if err := cmd.Context().Err(); err != nil {
		return err
	}
	if err := recordPromptCatalogHealth(cmd.Context(), promptHealthResults(results, stages)); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %v\n", err)
	}

	output := resultsOutput{promptText: promptText, results: results, control: stages.control, fusion: stages.fusion}
	written, outputErr := writePromptOutputFile(cmd, cliOpts, output)
	if written {
		// Don't print to stdout when the file write succeeds.
		return errors.Join(stages.baseErr, stages.controlErr, stages.fuseErr)
	}

	// Output results to stdout (either no file specified or file write failed)
	outputPromptResults(cmd, promptText, results)
	if stages.control != nil {
		outputPromptFusionControl(cmd, stages.control)
	}
	if stages.fusion != nil {
		outputPromptFusion(cmd, stages.fusion)
	}

	return errors.Join(stages.baseErr, stages.controlErr, stages.fuseErr, outputErr)
}

// executePromptResults runs the primary multi-provider fanout through the
// active execution seam.
func (cliOpts *invocationOptions) executePromptResults(cmd *commandIO, req promptRequest, run *promptRun) ([]PromptResult, error) {
	if run == nil {
		return cliOpts.executePromptAgainstSelectedModelsTo(cmd.Context(), cmd.ErrOrStderr(), req)
	}
	return cliOpts.executePromptAgainstSelectedModelsWithRun(cmd.Context(), cmd.ErrOrStderr(), req, run)
}

// promptFusionStages bundles the optional fusion-stage outputs after a
// primary fanout.
type promptFusionStages struct {
	baseErr    error
	control    *PromptFusionControl
	controlErr error
	fusion     *PromptFusion
	fuseErr    error
}

// executePromptFusionStages runs optional fusion control and response stages
// only when the fanout produced at least one success, stopping at the
// cancellation fence between stages.
func (cliOpts *invocationOptions) executePromptFusionStages(cmd *commandIO, req promptRequest, results []PromptResult, run *promptRun) promptFusionStages {
	var stages promptFusionStages
	stages.baseErr = checkAllFailed(results)
	if stages.baseErr != nil {
		return stages
	}
	if cliOpts.promptFuseControl {
		stages.control, stages.controlErr = cliOpts.executePromptFusionControlWithRun(cmd.Context(), req, cliOpts.promptFuseModels, run)
	}
	if cmd.Context().Err() != nil {
		return stages
	}
	if cliOpts.promptFuse {
		stages.fusion, stages.fuseErr = cliOpts.executePromptFusionResponseWithRun(cmd.Context(), req, results, cliOpts.promptFuseModels, run)
	}
	return stages
}

// promptHealthResults extends the primary results with successful fusion-stage
// results for catalog health recording.
func promptHealthResults(results []PromptResult, stages promptFusionStages) []PromptResult {
	healthResults := results
	if stages.control != nil {
		healthResults = append(append([]PromptResult(nil), healthResults...), stages.control.Result)
	}
	if stages.fusion != nil {
		healthResults = append(append([]PromptResult(nil), healthResults...), stages.fusion.Result)
	}
	return healthResults
}

// writePromptOutputFile writes markdown results when --output is set. It
// reports whether stdout display can be skipped; write failures fall back to
// stdout display and are returned for exit-status joining.
func writePromptOutputFile(cmd *commandIO, cliOpts *invocationOptions, output resultsOutput) (written bool, err error) {
	if cliOpts.outputFile == "" {
		return false, nil
	}
	if writeErr := writeResultsToFile(cmd, output, cliOpts.outputFile); writeErr != nil {
		// Print error but continue to display results to stdout.
		fmt.Fprintf(cmd.ErrOrStderr(), "Error writing to file %s: %v\n", cliOpts.outputFile, writeErr)
		fmt.Fprintln(cmd.OutOrStdout(), "Displaying results to stdout instead:")
		return false, writeErr
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Results written to %s\n", cliOpts.outputFile)
	return true, nil
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

func boolEnv(name string) (value, ok bool) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", yes1, "on":
		return true, true
	case "0", "false", no1, off1:
		return false, true
	default:
		return false, false
	}
}

func promptTextFromArgsOrStdin(args []string) (string, error) {
	if len(args) > 0 {
		promptText := strings.TrimSpace(strings.Join(args, " "))
		if promptText == "" {
			return "", errors.New("prompt text cannot be empty")
		}
		return promptText, nil
	}

	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect stdin: %w", err)
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("prompt text required - usage: llambo prompt <text>")
	}

	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	promptText := strings.TrimSpace(string(data))
	if promptText == "" {
		return "", errors.New("prompt text cannot be empty")
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
		return errors.New("all providers failed")
	}
	return nil
}

// resultsOutput bundles everything written by the markdown file exporter.
type resultsOutput struct {
	promptText string
	results    []PromptResult
	control    *PromptFusionControl
	fusion     *PromptFusion
}

// writeResultsToFile writes the markdown results to a file
func writeResultsToFile(_ *commandIO, out resultsOutput, filePath string) error {
	// Create a buffer to capture the output
	var buf bytes.Buffer

	// Create a temporary command with buffer as output
	tempCmd := &commandIO{stdout: &buf, stderr: io.Discard}

	// Generate markdown output to buffer
	outputPromptResults(tempCmd, out.promptText, out.results)
	if out.control != nil {
		outputPromptFusionControl(tempCmd, out.control)
	}
	if out.fusion != nil {
		outputPromptFusion(tempCmd, out.fusion)
	}

	// Write buffer to file
	if err := os.WriteFile(filePath, buf.Bytes(), 0644); err != nil { //nolint:gosec // reviewed file mode
		return err
	}

	return nil
}

// executePromptAgainstAllProviders sends the prompt to all enabled providers concurrently
// This is a variable to allow mocking in tests
