package evals

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const localRunLockName = ".llambo-local-run.lock"

// LocalRunStore owns an output directory for the life of a process. Its lock
// prevents concurrent calls against one attempt; calls are append-only and
// synced after every case so only truly missing cases run after a restart.
type LocalRunStore struct {
	dir          string
	manifest     LocalRunManifest
	lock         *os.File
	calls        *os.File
	intents      *os.File
	byCase       map[string]LocalCall
	intentByCase map[string]LocalDispatchIntent
}

type LocalDispatchIntent struct {
	CaseID      string    `json:"case_id"`
	InputSHA256 string    `json:"input_sha256"`
	At          time.Time `json:"at"`
}

func OpenLocalRunStore(dir string, manifest LocalRunManifest) (*LocalRunStore, error) {
	if dir == "" || filepath.Clean(dir) == "." {
		return nil, fmt.Errorf("--output-dir must be explicit")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, localRunLockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("output directory is already active: %s", dir)
		}
		return nil, fmt.Errorf("lock output directory: %w", err)
	}
	s := &LocalRunStore{dir: dir, manifest: manifest, lock: lock, byCase: make(map[string]LocalCall), intentByCase: make(map[string]LocalDispatchIntent)}
	if err := s.open(); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

func (s *LocalRunStore) open() error {
	manifestPath := filepath.Join(s.dir, "manifest.json")
	b, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		// The manifest is the durable start marker and must precede any call.
		if err := writeDurableLocalJSON(manifestPath, s.manifest); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
	} else if err != nil {
		return err
	} else {
		var existing LocalRunManifest
		if err := json.Unmarshal(b, &existing); err != nil {
			return fmt.Errorf("parse existing manifest: %w", err)
		}
		if err := sameLocalRunIdentity(existing, s.manifest); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(s.dir, "receipt.json")); err == nil {
		if err := VerifyLocalReceipt(s.dir); err != nil {
			return fmt.Errorf("completed output receipt is invalid: %w", err)
		}
		return fmt.Errorf("completed output directory is immutable; use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := s.loadCalls(); err != nil {
		return err
	}
	if err := s.loadIntents(); err != nil {
		return err
	}
	intents, err := os.OpenFile(filepath.Join(s.dir, "intents.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	s.intents = intents
	calls, err := os.OpenFile(filepath.Join(s.dir, "calls.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	s.calls = calls
	return nil
}

func (s *LocalRunStore) loadIntents() error {
	f, err := os.Open(filepath.Join(s.dir, "intents.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	line := 0
	for scanner.Scan() {
		line++
		var intent LocalDispatchIntent
		if err := json.Unmarshal(bytes.TrimSpace(scanner.Bytes()), &intent); err != nil {
			return fmt.Errorf("read intent %d: %w", line, err)
		}
		if err := s.validateCaseID(intent.CaseID); err != nil {
			return err
		}
		if _, exists := s.intentByCase[intent.CaseID]; exists {
			return fmt.Errorf("duplicate dispatch intent for case %s", intent.CaseID)
		}
		s.intentByCase[intent.CaseID] = intent
	}
	return scanner.Err()
}

func (s *LocalRunStore) AppendIntent(intent LocalDispatchIntent) error {
	if s.intents == nil {
		return fmt.Errorf("local run store is closed")
	}
	if err := s.validateCaseID(intent.CaseID); err != nil {
		return err
	}
	if _, exists := s.intentByCase[intent.CaseID]; exists {
		return fmt.Errorf("dispatch intent already recorded for case %s", intent.CaseID)
	}
	b, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	if _, err := s.intents.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := s.intents.Sync(); err != nil {
		return err
	}
	s.intentByCase[intent.CaseID] = intent
	return nil
}

func sameLocalRunIdentity(existing, requested LocalRunManifest) error {
	if existing.SchemaVersion != requested.SchemaVersion {
		return fmt.Errorf("resume manifest schema mismatch: have %d, requested %d", existing.SchemaVersion, requested.SchemaVersion)
	}
	if LocalRunIdentityHash(existing) != LocalRunIdentityHash(requested) {
		return fmt.Errorf("resume manifest identity mismatch; use a new --output-dir")
	}
	return nil
}

func (s *LocalRunStore) loadCalls() error {
	f, err := os.Open(filepath.Join(s.dir, "calls.jsonl"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), 8<<20)
	line := 0
	for scanner.Scan() {
		line++
		data := bytes.TrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}
		var call LocalCall
		if err := json.Unmarshal(data, &call); err != nil {
			return fmt.Errorf("read calls ledger record %d: %w", line, err)
		}
		if err := s.validateCaseID(call.CaseID); err != nil {
			return fmt.Errorf("read calls ledger record %d: %w", line, err)
		}
		if _, exists := s.byCase[call.CaseID]; exists {
			return fmt.Errorf("read calls ledger record %d: duplicate case id %q", line, call.CaseID)
		}
		s.byCase[call.CaseID] = call
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read calls ledger: %w", err)
	}
	return nil
}

func (s *LocalRunStore) AppendCall(call LocalCall) error {
	if s.calls == nil {
		return fmt.Errorf("local run store is closed")
	}
	if err := s.validateCaseID(call.CaseID); err != nil {
		return err
	}
	if _, exists := s.byCase[call.CaseID]; exists {
		return fmt.Errorf("local call already recorded for case %q", call.CaseID)
	}
	b, err := json.Marshal(call)
	if err != nil {
		return err
	}
	if _, err := s.calls.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := s.calls.Sync(); err != nil {
		return err
	}
	s.byCase[call.CaseID] = call
	return nil
}

func (s *LocalRunStore) Calls() []LocalCall {
	calls := make([]LocalCall, 0, len(s.byCase))
	for _, call := range s.byCase {
		calls = append(calls, call)
	}
	sort.Slice(calls, func(i, j int) bool {
		left, _ := strconv.Atoi(calls[i].CaseID)
		right, _ := strconv.Atoi(calls[j].CaseID)
		return left < right
	})
	return calls
}

func (s *LocalRunStore) validateCaseID(caseID string) error {
	i, err := strconv.Atoi(caseID)
	if err != nil || i < 1 || i > s.manifest.Identity.Limit || caseID != strconv.Itoa(i) {
		return fmt.Errorf("invalid case id %q for this run", caseID)
	}
	return nil
}

// RunStoredLocalEvaluation resumes a matching output directory. A persisted
// failed call is still a completed case: it is preserved in the receipt and is
// never silently retried from the same attempt.
func RunStoredLocalEvaluation(ctx context.Context, manifest LocalRunManifest, in LocalInputManifest, exec LocalExecutor, store *LocalRunStore) ([]LocalCall, error) {
	if exec == nil || store == nil {
		return nil, fmt.Errorf("local executor and run store are required")
	}
	if LocalRunIdentityHash(manifest) != LocalRunIdentityHash(store.manifest) {
		return nil, fmt.Errorf("run store does not belong to manifest identity")
	}
	if _, err := PlanLocalRun(manifest, in); err != nil {
		return nil, err
	}
	tracker := NewIdentityTracker(manifest.Identity.RequestedModel)
	for _, call := range store.Calls() {
		caseIndex, _ := strconv.Atoi(call.CaseID)
		if call.RequestedModel != manifest.Identity.RequestedModel {
			return store.Calls(), fmt.Errorf("stored case %s has a different requested model", call.CaseID)
		}
		if !bytes.Equal(call.Input, in.Cases[caseIndex-1]) {
			return store.Calls(), fmt.Errorf("stored case %s does not match sealed input", call.CaseID)
		}
		if call.Error == "" {
			if err := tracker.Record(call.ServedModel); err != nil {
				return store.Calls(), err
			}
		}
	}
	for i := 0; i < manifest.Identity.Limit; i++ {
		caseID := fmt.Sprintf("%d", i+1)
		if _, exists := store.byCase[caseID]; exists {
			continue
		}
		if _, inDoubt := store.intentByCase[caseID]; inDoubt {
			if store.intentByCase[caseID].InputSHA256 != localSHA(in.Cases[i]) {
				return store.Calls(), fmt.Errorf("case %s dispatch intent does not match sealed input", caseID)
			}
			return store.Calls(), fmt.Errorf("case %s has a dispatch intent without a durable result; attempt is in doubt and will not be retried", caseID)
		}
		if err := store.AppendIntent(LocalDispatchIntent{CaseID: caseID, InputSHA256: localSHA(in.Cases[i]), At: time.Now().UTC()}); err != nil {
			return store.Calls(), err
		}
		call, err := exec.ExecuteLocal(ctx, manifest, in.Cases[i])
		call.CaseID = caseID
		call.RequestedModel = manifest.Identity.RequestedModel
		call.Input = append(json.RawMessage(nil), in.Cases[i]...)
		call.At = time.Now().UTC()
		if priceErr := ApplyLocalCallPricing(manifest.Identity.Pricing, &call); priceErr != nil {
			return store.Calls(), priceErr
		}
		if err != nil {
			call.Error = err.Error()
			if appendErr := store.AppendCall(call); appendErr != nil {
				return store.Calls(), errors.Join(err, appendErr)
			}
			return store.Calls(), err
		}
		if call.Error == "" {
			if err := tracker.Record(call.ServedModel); err != nil {
				call.Error = err.Error()
				if appendErr := store.AppendCall(call); appendErr != nil {
					return store.Calls(), errors.Join(err, appendErr)
				}
				return store.Calls(), err
			}
		}
		if err := store.AppendCall(call); err != nil {
			return store.Calls(), err
		}
	}
	return store.Calls(), nil
}

// Finalize writes all derived artifacts only once. It refuses an existing
// receipt unless that receipt verifies, in which case callers must use a new
// output directory rather than overwrite completed evidence.
func (s *LocalRunStore) Finalize(report LocalReport, finished time.Time) (LocalReceipt, error) {
	if s.calls == nil {
		return LocalReceipt{}, fmt.Errorf("local run store is closed")
	}
	if err := s.calls.Sync(); err != nil {
		return LocalReceipt{}, err
	}
	if _, err := os.Stat(filepath.Join(s.dir, "receipt.json")); err == nil {
		if err := VerifyLocalReceipt(s.dir); err != nil {
			return LocalReceipt{}, fmt.Errorf("completed output receipt is invalid: %w", err)
		}
		return LocalReceipt{}, fmt.Errorf("completed output directory is immutable; use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return LocalReceipt{}, err
	}
	if _, err := os.Stat(filepath.Join(s.dir, "report.json")); err == nil {
		return LocalReceipt{}, fmt.Errorf("output directory already has a report without a receipt; preserve it and use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return LocalReceipt{}, err
	}
	calls := s.Calls()
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
				return LocalReceipt{}, err
			}
		}
		if call.ObservedCostKnown {
			report.ObservedCostUSD += call.ObservedCostUSD
		}
	}
	report.ServedModels = served.Served()
	if len(calls) != s.manifest.Identity.Limit || len(report.Errors) > 0 || hasLocalCallError(calls) {
		report.Status = "partial"
	} else if report.Status == "" {
		report.Status = "complete_quality"
	}
	if err := writeDurableLocalJSON(filepath.Join(s.dir, "report.json"), report); err != nil {
		return LocalReceipt{}, err
	}
	var md strings.Builder
	fmt.Fprintf(&md, "# Local evaluation %s\n\nStatus: %s\n\nScore (%s): %.6f\n", s.manifest.RunID, report.Status, report.ScoreIdentity, report.Score)
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
	if err := writeDurableLocalFile(filepath.Join(s.dir, "report.md"), []byte(md.String()), 0o644); err != nil {
		return LocalReceipt{}, err
	}
	hashes := make(map[string]string, 5)
	for _, name := range []string{"manifest.json", "intents.jsonl", "calls.jsonl", "report.json", "report.md"} {
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			return LocalReceipt{}, err
		}
		hashes[name] = localSHA(data)
	}
	receipt := LocalReceipt{SchemaVersion: localEvalSchemaVersion, RunID: s.manifest.RunID, Status: report.Status, RequestedModel: s.manifest.Identity.RequestedModel, ServedModels: report.ServedModels, IdentitySHA256: LocalRunIdentityHash(s.manifest), InputSHA256: s.manifest.Identity.InputSHA256, Suite: s.manifest.Identity.Suite, SuiteVersion: s.manifest.Identity.SuiteVersion, CaseCount: len(calls), ObservedCostUSD: report.ObservedCostUSD, CreatedAt: s.manifest.CreatedAt, FinishedAt: finished.UTC(), ArtifactSHA256: hashes, Errors: report.Errors}
	if err := writeDurableLocalJSON(filepath.Join(s.dir, "receipt.json"), receipt); err != nil {
		return LocalReceipt{}, err
	}
	return receipt, nil
}

func hasLocalCallError(calls []LocalCall) bool {
	for _, call := range calls {
		if call.Error != "" {
			return true
		}
	}
	return false
}

func (s *LocalRunStore) Close() error {
	var errs []error
	if s.calls != nil {
		errs = append(errs, s.calls.Close())
		s.calls = nil
	}
	if s.intents != nil {
		errs = append(errs, s.intents.Close())
		s.intents = nil
	}
	if s.lock != nil {
		errs = append(errs, syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN), s.lock.Close())
		s.lock = nil
	}
	return errors.Join(errs...)
}

func writeDurableLocalJSON(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return writeDurableLocalFile(path, append(b, '\n'), 0o600)
}

func writeDurableLocalFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".llambo-local-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

var _ io.Closer = (*LocalRunStore)(nil)
