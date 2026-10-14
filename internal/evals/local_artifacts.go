package evals

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WriteLocalRunArtifacts publishes a fresh standalone run without overwriting evidence.
func WriteLocalRunArtifacts(dir string, m LocalRunManifest, calls []LocalCall, report LocalReport, finished time.Time) (LocalReceipt, error) {
	if err := ensureNewLocalOutput(dir, m); err != nil {
		return LocalReceipt{}, err
	}
	if report.RunID == "" {
		report.RunID = m.RunID
	}
	if _, err := os.Stat(filepath.Join(dir, "receipt.json")); err == nil {
		return LocalReceipt{}, errors.New("completed output directory is immutable; use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return LocalReceipt{}, err
	}
	report, err := localArtifactReport(m, calls, report)
	if err != nil {
		return LocalReceipt{}, err
	}
	if writeErr := writeJSON(filepath.Join(dir, manifestJSON), m); writeErr != nil {
		return LocalReceipt{}, writeErr
	}
	if writeErr := writeCalls(filepath.Join(dir, callsJsonl), calls); writeErr != nil {
		return LocalReceipt{}, writeErr
	}
	if writeErr := writeJSON(filepath.Join(dir, reportJSON1), report); writeErr != nil {
		return LocalReceipt{}, writeErr
	}
	md := fmt.Sprintf("# Local evaluation %s\n\nStatus: %s\n\nScore (%s): %.6f\n", m.RunID, report.Status, report.ScoreIdentity, report.Score)
	if writeErr := os.WriteFile(filepath.Join(dir, reportMd), []byte(md), 0o600); writeErr != nil { //nolint:gosec // reviewed file mode
		return LocalReceipt{}, writeErr
	}
	hashes, err := hashLocalArtifacts(dir, []string{manifestJSON, callsJsonl, reportJSON1, reportMd})
	if err != nil {
		return LocalReceipt{}, err
	}
	r := localCompletionReceipt(m, calls, report, finished, hashes)
	if err := writeJSON(filepath.Join(dir, "receipt.json"), r); err != nil {
		return LocalReceipt{}, err
	}
	return r, nil
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	b = append(b, '\n')
	return os.WriteFile(path, b, 0o600) //nolint:gosec // reviewed file mode
}
func writeCalls(path string, calls []LocalCall) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer func() { _ = f.Close() }()
	w := bufio.NewWriter(f)
	for _, c := range calls {
		b, e := json.Marshal(c)
		if e != nil {
			return e
		}
		if _, e = w.Write(append(b, '\n')); e != nil {
			return e
		}
	}
	return w.Flush()
}

// VerifyLocalReceipt checks a completed run directory's sealed receipt
// against its artifacts.
func VerifyLocalReceipt(dir string) error {
	b, e := os.ReadFile(filepath.Join(dir, "receipt.json"))
	if e != nil {
		return e
	}
	var r LocalReceipt
	if e = json.Unmarshal(b, &r); e != nil {
		return e
	}
	for name, want := range r.ArtifactSHA256 {
		got, e := os.ReadFile(filepath.Join(dir, name))
		if e != nil || localSHA(got) != want {
			return fmt.Errorf("artifact hash mismatch: %s", name)
		}
	}
	return nil
}

func localArtifactReport(m LocalRunManifest, calls []LocalCall, report LocalReport) (LocalReport, error) {
	if report.Suite == "" {
		report.Suite = m.Identity.Suite
	}
	report.CaseCount = len(calls)
	served := NewIdentityTracker(m.Identity.RequestedModel)
	for _, c := range calls {
		if c.Error == "" {
			if err := served.Record(c.ServedModel); err != nil {
				return LocalReport{}, err
			}
		}
		report.ObservedCostUSD += c.ObservedCostUSD
	}
	report.ServedModels = served.Served()
	if len(calls) != m.Identity.Limit || len(report.Errors) > 0 {
		report.Status = partial1
	} else if report.Status == "" {
		report.Status = completeQuality
	}
	return report, nil
}

func hashLocalArtifacts(dir string, names []string) (map[string]string, error) {
	hashes := make(map[string]string, len(names))
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		hashes[name] = localSHA(data)
	}
	return hashes, nil
}

func localCompletionReceipt(m LocalRunManifest, calls []LocalCall, report LocalReport, finished time.Time, hashes map[string]string) LocalReceipt {
	return LocalReceipt{SchemaVersion: localEvalSchemaVersion, RunID: m.RunID, Status: report.Status, RequestedModel: m.Identity.RequestedModel, ServedModels: report.ServedModels, IdentitySHA256: LocalRunIdentityHash(m), InputSHA256: m.Identity.InputSHA256, Suite: m.Identity.Suite, SuiteVersion: m.Identity.SuiteVersion, CaseCount: len(calls), ObservedCostUSD: report.ObservedCostUSD, CreatedAt: m.CreatedAt, FinishedAt: finished.UTC(), ArtifactSHA256: hashes, Errors: report.Errors}
}
