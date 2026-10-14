package evals

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"syscall"
	"time"
)

// Repeated string literals consolidated by goconst.
const (
	callsJsonl   = "calls.jsonl"
	intentsJsonl = "intents.jsonl"
)

const reportJSON1 = "report.json"

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

// LocalDispatchIntent is fsynced before each provider call; a stop after this
// record but before its call leaves the attempt intentionally in doubt.
type LocalDispatchIntent struct {
	CaseID      string    `json:"case_id"`
	InputSHA256 string    `json:"input_sha256"`
	At          time.Time `json:"at"`
}

// OpenLocalRunStore opens or validates the durable run ledger for a manifest.
func OpenLocalRunStore(dir string, manifest LocalRunManifest) (*LocalRunStore, error) {
	if dir == "" || filepath.Clean(dir) == "." {
		return nil, errors.New("--output-dir must be explicit")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil { //nolint:gosec // reviewed directory mode
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
	manifestPath := filepath.Join(s.dir, manifestJSON)
	b, err := os.ReadFile(manifestPath)
	switch {
	case os.IsNotExist(err):
		// The manifest is the durable start marker and must precede any call.
		if err = writeDurableLocalJSON(manifestPath, s.manifest); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
	case err != nil:
		return err
	default:
		var existing LocalRunManifest
		if err = json.Unmarshal(b, &existing); err != nil {
			return fmt.Errorf("parse existing manifest: %w", err)
		}
		if err = sameLocalRunIdentity(existing, s.manifest); err != nil {
			return err
		}
	}
	if _, err = os.Stat(filepath.Join(s.dir, "receipt.json")); err == nil {
		if err = VerifyLocalReceipt(s.dir); err != nil {
			return fmt.Errorf("completed output receipt is invalid: %w", err)
		}
		return errors.New("completed output directory is immutable; use a new --output-dir")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = s.loadCalls(); err != nil {
		return err
	}
	if err = s.loadIntents(); err != nil {
		return err
	}
	intents, err := os.OpenFile(filepath.Join(s.dir, intentsJsonl), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	s.intents = intents
	calls, err := os.OpenFile(filepath.Join(s.dir, callsJsonl), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	s.calls = calls
	return nil
}

func (s *LocalRunStore) loadIntents() error {
	f, err := os.Open(filepath.Join(s.dir, intentsJsonl))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
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

// AppendIntent durably records dispatch ownership before admitting it in memory.
func (s *LocalRunStore) AppendIntent(intent LocalDispatchIntent) error {
	if s.intents == nil {
		return errors.New("local run store is closed")
	}
	if err := s.validateCaseID(intent.CaseID); err != nil {
		return err
	}
	if _, exists := s.intentByCase[intent.CaseID]; exists {
		return fmt.Errorf("dispatch intent already recorded for case %s", intent.CaseID)
	}
	if err := appendLocalLedgerRecord(s.intents, intent); err != nil {
		return err
	}
	s.intentByCase[intent.CaseID] = intent
	return nil
}

func appendLocalLedgerRecord(file *os.File, record any) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func sameLocalRunIdentity(existing, requested LocalRunManifest) error {
	if existing.SchemaVersion != requested.SchemaVersion {
		return fmt.Errorf("resume manifest schema mismatch: have %d, requested %d", existing.SchemaVersion, requested.SchemaVersion)
	}
	if LocalRunIdentityHash(existing) != LocalRunIdentityHash(requested) {
		return errors.New("resume manifest identity mismatch; use a new --output-dir")
	}
	return nil
}

func (s *LocalRunStore) loadCalls() error {
	f, err := os.Open(filepath.Join(s.dir, callsJsonl))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
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

// AppendCall syncs a completed call before updating the in-memory case index.
func (s *LocalRunStore) AppendCall(call LocalCall) error {
	if s.calls == nil {
		return errors.New("local run store is closed")
	}
	if err := s.validateCaseID(call.CaseID); err != nil {
		return err
	}
	if _, exists := s.byCase[call.CaseID]; exists {
		return fmt.Errorf("local call already recorded for case %q", call.CaseID)
	}
	if err := appendLocalLedgerRecord(s.calls, call); err != nil {
		return err
	}
	s.byCase[call.CaseID] = call
	return nil
}

// Calls returns all ledgered calls.
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

// Close flushes and closes the run ledger.
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
	return writeDurableFile(path, data, 0o755, mode, ".llambo-local-*")
}

var _ io.Closer = (*LocalRunStore)(nil)
