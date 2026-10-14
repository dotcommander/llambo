package cmd

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

// Repeated string literals consolidated by goconst.
const (
	omlx1 = "omlx"
	ping1 = "ping"
)

var (
	omlxDiscoveryHTTPClient = &http.Client{Timeout: 5 * time.Second}
	fetchEvalsReport        = evals.Fetch
	prepareEvalsOMLXScores  = prepareOMLXScores
)

func (cliOpts *invocationOptions) runEvals(cmd *commandIO, _ []string) error {
	if err := cliOpts.validateEvalsOptions(); err != nil {
		return err
	}
	cacheDir, err := evaluationCacheDirectory(strings.TrimSpace(cliOpts.evalsCacheDir))
	if err != nil {
		return err
	}
	result, err := fetchEvalsReport(cmd.Context(), cliOpts.evalFetchOptions(cacheDir))
	if err != nil {
		return err
	}
	report, err := evals.BuildReportWithProjectionFile(result, strings.ToLower(strings.TrimSpace(cliOpts.evalsRankBy)), strings.TrimSpace(cliOpts.evalsProjections))
	if err != nil {
		return err
	}
	if strings.TrimSpace(cliOpts.evalsValidationReceipts) != "" {
		if err = evals.ApplyValidationReceipts(&report, strings.TrimSpace(cliOpts.evalsValidationReceipts)); err != nil {
			return err
		}
	}
	// Live OMLX discovery is an explicit inventory boundary, never an
	// evaluation. Ordinary cache-only runs keep the configured/snapshot set.
	discovery, liveInventory, err := cliOpts.applyLiveOMLXDiscovery(cmd, &report)
	if err != nil {
		return err
	}
	pending, err := prepareEvalsOMLXScores(&report, discovery, liveInventory)
	if err != nil {
		return err
	}
	evals.ApplyCategoryEligibility(&report, strings.ToLower(strings.TrimSpace(cliOpts.evalsRankBy)), cliOpts.evalsMinScore, cliOpts.evalsMaxOutputPrice)
	data, err := encodeEvalsReport(report, cliOpts.evalsFormat, cliOpts.evalsLimit)
	if err != nil {
		return err
	}
	if cliOpts.evalsOutput == "" {
		_, err = cmd.OutOrStdout().Write(data)
		if err != nil {
			return err
		}
		return publishOMLXScores(pending)
	}
	return writeEvalsThenPublish(cmd.ErrOrStderr(), cliOpts.evalsOutput, data, pending)
}

func (cliOpts *invocationOptions) applyLiveOMLXDiscovery(cmd *commandIO, report *evals.Report) (evals.OMLXDiscovery, bool, error) {
	if cliOpts.evalsOffline || cliOpts.evalsNoOMLX {
		return evals.OMLXDiscovery{}, false, nil
	}
	discovery, err := evals.DiscoverOMLXModels(cmd.Context(), cliOpts.evalsOMLXURL, os.Getenv("OMLX_API_KEY"), omlxDiscoveryHTTPClient)
	if err != nil {
		return discovery, false, fmt.Errorf("discover live OMLX inventory: %w", err)
	}
	evals.FilterProjectedRowsToLiveOMLX(report, discovery, nil)
	return discovery, true, nil
}

func validateOMLXURL(value string) error {
	if err := evals.ValidateOMLXLoopbackBaseURL(value); err != nil {
		return fmt.Errorf("invalid --omlx-url: %w", err)
	}
	return nil
}

func (cliOpts *invocationOptions) evalFetchOptions(cacheDir string) evals.Options {
	return evals.Options{
		CacheDir:             cacheDir,
		Refresh:              cliOpts.evalsRefresh,
		RefreshOfficialCards: cliOpts.evalsRefreshOfficialCards,
		Offline:              !cliOpts.evalsRefresh && !cliOpts.evalsRefreshOfficialCards,
		AllowPartial:         cliOpts.evalsPartial,
		IngestLLMBenchmarks:  cliOpts.evalsRefresh,
		AAAPIKey:             os.Getenv("AA_API_KEY"),
		LLMStatsAPIKey:       os.Getenv("LLM_STATS_KEY"),
	}
}
