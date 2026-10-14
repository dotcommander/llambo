package cmd

import (
	"errors"
	"math"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

func (cliOpts *invocationOptions) validateEvalsOptions() error {
	if cliOpts.evalsOffline && cliOpts.evalsRefresh {
		return errors.New("--offline and --refresh cannot be used together")
	}
	if cliOpts.evalsRefresh && cliOpts.evalsRefreshOfficialCards {
		return errors.New("--refresh and --refresh-official-model-cards cannot be used together")
	}
	if cliOpts.evalsOffline && cliOpts.evalsRefreshOfficialCards {
		return errors.New("--offline and --refresh-official-model-cards cannot be used together")
	}
	return cliOpts.validateEvalsRankingOptions()
}

func (cliOpts *invocationOptions) validateEvalsRankingOptions() error {
	if math.IsNaN(cliOpts.evalsMinScore) || math.IsInf(cliOpts.evalsMinScore, 0) || cliOpts.evalsMinScore < -1 || cliOpts.evalsMinScore > 100 {
		return errors.New("--min-score must be between -1 and 100")
	}
	if cliOpts.evalsMinScore >= 0 && !evals.IsCapabilityCategory(strings.ToLower(strings.TrimSpace(cliOpts.evalsRankBy))) {
		return errors.New("--min-score requires a capability --rank-by category")
	}
	if math.IsNaN(cliOpts.evalsMaxOutputPrice) || math.IsInf(cliOpts.evalsMaxOutputPrice, 0) || cliOpts.evalsMaxOutputPrice < -1 {
		return errors.New("--max-output-price must be -1 or greater")
	}
	return nil
}
