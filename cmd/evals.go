package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dotcommander/llambo/internal/evals"
)

var (
	evalsRefresh        bool
	evalsFormat         string
	evalsOutput         string
	evalsLimit          int
	evalsPartial        bool
	evalsRankBy         string
	evalsOffline        bool
	evalsProjections    string
	evalsMinOverall     float64
	evalsMaxOutputPrice float64
	evalsOMLXURL        string
	evalsNoOMLX         bool
)

func runEvals(cmd *commandIO, _ []string) error {
	if evalsOffline && evalsRefresh {
		return fmt.Errorf("--offline and --refresh cannot be used together")
	}
	if math.IsNaN(evalsMinOverall) || math.IsInf(evalsMinOverall, 0) || evalsMinOverall < -1 || evalsMinOverall > 100 {
		return fmt.Errorf("--min-overall must be between -1 and 100")
	}
	if math.IsNaN(evalsMaxOutputPrice) || math.IsInf(evalsMaxOutputPrice, 0) || evalsMaxOutputPrice < -1 {
		return fmt.Errorf("--max-output-price must be -1 or greater")
	}
	if !evalsOffline && !evalsNoOMLX {
		if err := validateOMLXURL(evalsOMLXURL); err != nil {
			return err
		}
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return fmt.Errorf("resolve user cache directory: %w", err)
	}
	result, err := evals.Fetch(cmd.Context(), evalFetchOptions(filepath.Join(cacheRoot, "llambo", "evals")))
	if err != nil {
		return err
	}
	report, err := evals.BuildReportWithProjectionFile(result, strings.ToLower(strings.TrimSpace(evalsRankBy)), strings.TrimSpace(evalsProjections))
	if err != nil {
		return err
	}
	applyLiveOMLXDiscovery(cmd, &report)
	evals.ApplyEligibility(&report, evalsMinOverall, evalsMaxOutputPrice)
	data, err := encodeEvalsReport(report, evalsFormat, evalsLimit)
	if err != nil {
		return err
	}
	if evalsOutput == "" {
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}
	return writeEvalsReportTo(cmd.ErrOrStderr(), evalsOutput, data)
}

func applyLiveOMLXDiscovery(cmd *commandIO, report *evals.Report) {
	if evalsOffline || evalsNoOMLX {
		return
	}
	client := &http.Client{Timeout: 5 * time.Second}
	discovery, err := evals.DiscoverOMLXModels(cmd.Context(), evalsOMLXURL, os.Getenv("OMLX_API_KEY"), client)
	evals.FilterProjectedRowsToLiveOMLX(report, discovery, err)
}

func validateOMLXURL(value string) error {
	if err := evals.ValidateOMLXLoopbackBaseURL(value); err != nil {
		return fmt.Errorf("invalid --omlx-url: %w", err)
	}
	return nil
}

func evalFetchOptions(cacheDir string) evals.Options {
	return evals.Options{
		CacheDir:     cacheDir,
		Refresh:      evalsRefresh,
		Offline:      !evalsRefresh,
		AllowPartial: evalsPartial,
		AAAPIKey:     os.Getenv("AA_API_KEY"),
	}
}

func encodeEvalsReport(report evals.Report, format string, limit int) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "markdown", "md":
		return []byte(evals.RenderMarkdown(report, limit)), nil
	case "json":
		report.Models = limitEvalsModels(report.Models, limit)
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported --format %q (supported: markdown, json)", format)
	}
}

func limitEvalsModels(models []evals.ReportModel, limit int) []evals.ReportModel {
	if limit <= 0 {
		return models
	}
	limited := make([]evals.ReportModel, 0, min(len(models), limit))
	canonical := 0
	for _, model := range models {
		if model.Projection == nil {
			if canonical >= limit {
				continue
			}
			canonical++
		}
		limited = append(limited, model)
	}
	return limited
}

func writeEvalsReport(path string, data []byte) error {
	return writeEvalsReportTo(os.Stderr, path, data)
}

func writeEvalsReportTo(errOut io.Writer, path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	fmt.Fprintf(errOut, "Evaluation report written to %s\n", path)
	return nil
}
