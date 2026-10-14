package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

func validateWritingRunOptions(cmd *commandIO, options *evalsWritingRunCommand, limit int) error {
	for _, selector := range []string{options.Model, options.JudgeModel} {
		if isGPTProModel(selector) {
			return fmt.Errorf("local evaluation of GPT Pro models is prohibited by user policy: %s", selector)
		}
	}
	if limit < 1 || limit > 100 {
		return errors.New("--limit must be between 1 and 100")
	}
	if options.Concurrency < 1 || options.Concurrency > 8 {
		return errors.New("--concurrency must be between 1 and 8")
	}
	if options.Timeout <= 0 {
		return errors.New("--timeout must be positive")
	}
	if options.JudgeTokens < 1 || options.JudgeTokens > 65536 {
		return errors.New("--judge-max-output-tokens must be between 1 and 65536")
	}
	if err := validateWritingTokenControls(cmd, options); err != nil {
		return err
	}
	cleanOutputDir := filepath.Clean(options.OutputDir)
	if cleanOutputDir == "." || strings.TrimSpace(options.OutputDir) == "" || filepath.Dir(cleanOutputDir) == cleanOutputDir {
		return errors.New("--output-dir must be an explicit directory")
	}
	return nil
}

func validateWritingTokenControls(cmd *commandIO, options *evalsWritingRunCommand) error {
	if options.GenerationTokens < 0 || options.GenerationTokens > 65536 || (options.GenerationTokens == 0 && cmd.FlagChanged("generation-max-output-tokens")) {
		return errors.New("--generation-max-output-tokens must be between 1 and 65536")
	}
	if options.ThinkingBudget < 0 || options.ThinkingBudget > 65536 || (options.ThinkingBudget == 0 && cmd.FlagChanged("thinking-budget-tokens")) {
		return errors.New("--thinking-budget-tokens must be between 1 and 65536")
	}
	if options.ThinkingBudget > 0 && options.ReasoningEffort == off1 {
		return errors.New("--thinking-budget-tokens cannot be combined with --reasoning-effort off")
	}
	return nil
}

func validateWritingExecution(options *evalsWritingRunCommand, plan evals.WritingRunPlan) error {
	if options.MaxRunCost <= 0 {
		return errors.New("--execute requires a positive --max-run-cost")
	}
	if options.MaxCampaignCost <= 0 {
		return errors.New("--execute requires a positive --max-campaign-cost")
	}
	if (strings.HasPrefix(options.Model, "omlx/") || strings.HasPrefix(options.JudgeModel, "omlx/")) && strings.TrimSpace(options.LocalUseCase) == "" {
		return errors.New("local execution requires a specific --local-use-case")
	}
	if plan.WorstCaseCostUSD > options.MaxRunCost+1e-12 {
		return fmt.Errorf("worst-case run estimate $%.6f exceeds --max-run-cost $%.6f", plan.WorstCaseCostUSD, options.MaxRunCost)
	}
	return nil
}
