package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/dotcommander/llambo/internal/evals"
)

func encodeEvalsReport(report evals.Report, format string, limit int) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case markdown1, "md":
		return []byte(evals.RenderMarkdown(report, limit)), nil
	case "html":
		return []byte(evals.RenderHTML(report, limit)), nil
	case json1:
		limited := limitEvalsModels(report.Models, limit)
		report.Models = report.Models[:0]
		report.ProjectedModels = nil
		for _, model := range limited {
			if model.Projection == nil {
				report.Models = append(report.Models, model)
			} else {
				report.ProjectedModels = append(report.ProjectedModels, model)
			}
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return nil, err
		}
		return append(data, '\n'), nil
	default:
		return nil, fmt.Errorf("unsupported --format %q (supported: markdown, json, html)", format)
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

func writeEvalsReportTo(errOut io.Writer, path string, data []byte) error {
	if err := writeAtomicOutput(path, data); err != nil {
		return err
	}
	fmt.Fprintf(errOut, "Evaluation report written to %s\n", path)
	return nil
}

func writeAtomicOutput(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { //nolint:gosec // reviewed directory mode
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil { //nolint:gosec // reviewed file mode
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return nil
}
