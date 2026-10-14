package evals

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Finalize publishes derived artifacts only after determinate ledger validation.
func (s *LocalRunStore) Finalize(report LocalReport, finished time.Time) (LocalReceipt, error) {
	if s.calls == nil {
		return LocalReceipt{}, errors.New("local run store is closed")
	}
	if err := s.calls.Sync(); err != nil {
		return LocalReceipt{}, err
	}
	if _, err := os.Stat(filepath.Join(s.dir, "receipt.json")); err == nil {
		if err = VerifyLocalReceipt(s.dir); err != nil {
			return LocalReceipt{}, fmt.Errorf("completed output receipt is invalid: %w", err)
		}
		return LocalReceipt{}, errors.New("completed output directory is immutable; use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return LocalReceipt{}, err
	}
	if _, err := os.Stat(filepath.Join(s.dir, reportJSON1)); err == nil {
		return LocalReceipt{}, errors.New("output directory already has a report without a receipt; preserve it and use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return LocalReceipt{}, err
	}
	calls := s.Calls()
	report, err := s.completedLocalReport(report, calls)
	if err != nil {
		return LocalReceipt{}, err
	}
	if writeErr := writeDurableLocalJSON(filepath.Join(s.dir, reportJSON1), report); writeErr != nil {
		return LocalReceipt{}, writeErr
	}
	md := renderStoredLocalReport(s.manifest, report)
	if writeErr := writeDurableLocalFile(filepath.Join(s.dir, reportMd), []byte(md), 0o644); writeErr != nil {
		return LocalReceipt{}, writeErr
	}
	hashes, err := hashLocalArtifacts(s.dir, []string{manifestJSON, intentsJsonl, callsJsonl, reportJSON1, reportMd})
	if err != nil {
		return LocalReceipt{}, err
	}
	receipt := localCompletionReceipt(s.manifest, calls, report, finished, hashes)
	if err := writeDurableLocalJSON(filepath.Join(s.dir, "receipt.json"), receipt); err != nil {
		return LocalReceipt{}, err
	}
	return receipt, nil
}

func (s *LocalRunStore) completedLocalReport(report LocalReport, calls []LocalCall) (LocalReport, error) {
	if report.RunID == "" {
		report.RunID = s.manifest.RunID
	}
	if report.Suite == "" {
		report.Suite = s.manifest.Identity.Suite
	}
	report.CaseCount = len(calls)
	served := NewIdentityTracker(s.manifest.Identity.RequestedModel)
	for _, call := range calls {
		if call.Error == "" {
			if err := served.Record(call.ServedModel); err != nil {
				return LocalReport{}, err
			}
		}
		if call.ObservedCostKnown {
			report.ObservedCostUSD += call.ObservedCostUSD
		}
	}
	report.ServedModels = served.Served()
	if len(calls) != s.manifest.Identity.Limit || len(report.Errors) > 0 || hasLocalCallError(calls) {
		report.Status = partial1
	} else if report.Status == "" {
		report.Status = completeQuality
	}
	return report, nil
}

func renderStoredLocalReport(manifest LocalRunManifest, report LocalReport) string {
	var md strings.Builder
	fmt.Fprintf(&md, "# Local evaluation %s\n\nStatus: %s\n\nScore (%s): %.6f\n", manifest.RunID, report.Status, report.ScoreIdentity, report.Score)
	metricKeys := make([]string, 0, len(report.Metrics))
	for key := range report.Metrics {
		metricKeys = append(metricKeys, key)
	}
	sort.Strings(metricKeys)
	if len(metricKeys) > 0 {
		md.WriteString("\n## Metrics\n")
	}
	for _, key := range metricKeys {
		fmt.Fprintf(&md, "\n- %s: %.6f", key, report.Metrics[key])
	}
	if len(metricKeys) > 0 {
		md.WriteByte('\n')
	}
	return md.String()
}

func hasLocalCallError(calls []LocalCall) bool {
	for _, call := range calls {
		if call.Error != "" {
			return true
		}
	}
	return false
}
