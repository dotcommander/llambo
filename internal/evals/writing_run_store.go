package evals

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
)

const (
	maxWritingRunInputSize  = 128 << 20
	maxWritingRunRecordSize = 64 << 20
)

// WritingRunStore is the fsynced ledger for one writing run directory,
// enforcing manifest identity and attempt ordering.
type WritingRunStore struct {
	mu                 sync.Mutex
	dir                string
	generationFile     *os.File
	judgmentFile       *os.File
	intentFile         *os.File
	generations        []WritingGenerationRecord
	judgments          []WritingJudgmentRecord
	generationSuccess  map[string]WritingGenerationRecord
	generationAttempts map[string]int
	judgmentSuccess    map[string]WritingJudgmentRecord
	judgmentAttempts   map[string]int
	intents            map[string]WritingDispatchIntent
	lock               *os.File
}

// OpenWritingRunStore locks the run directory and opens or validates its
// ledger against the manifest identity.
func OpenWritingRunStore(dir string, manifest WritingRunManifest) (*WritingRunStore, error) {
	return acquireWritingRunStore(dir, manifest, os.OpenFile)
}

func acquireWritingRunStore(dir string, manifest WritingRunManifest, openLedger func(string, int, os.FileMode) (*os.File, error)) (*WritingRunStore, error) {
	dir = filepath.Clean(dir)
	if dir == "." || dir == "" || filepath.Dir(dir) == dir {
		return nil, errors.New("writing run output directory must be explicit")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create writing run directory: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, ".llambo-writing-run.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open writing run lock: %w", err)
	}
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = lock.Close()
		return nil, fmt.Errorf("writing output directory is already active: %s", dir)
	}
	lockOwned := true
	defer func() {
		if lockOwned {
			_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
			_ = lock.Close()
		}
	}()
	if manifestErr := initializeWritingManifest(dir, manifest); manifestErr != nil {
		return nil, manifestErr
	}
	if err = os.Chmod(dir, 0o700); err != nil { //nolint:gosec // directory, not file, permission
		return nil, fmt.Errorf("secure writing run directory: %w", err)
	}

	store := &WritingRunStore{
		dir:                dir,
		generationSuccess:  make(map[string]WritingGenerationRecord),
		generationAttempts: make(map[string]int),
		judgmentSuccess:    make(map[string]WritingJudgmentRecord),
		judgmentAttempts:   make(map[string]int),
		intents:            make(map[string]WritingDispatchIntent),
		lock:               lock,
	}
	initialized := false
	defer func() {
		if !initialized {
			_ = store.Close()
		}
	}()
	lockOwned = false
	if err = store.load(); err != nil {
		return nil, err
	}
	if err := store.openLedgers(openLedger); err != nil {
		return nil, err
	}
	initialized = true
	return store, nil
}

func sameWritingRunIdentity(existing, requested WritingRunManifest) error {
	if existing.SchemaVersion != requested.SchemaVersion {
		return fmt.Errorf("resume manifest schema mismatch: have %d, requested %d", existing.SchemaVersion, requested.SchemaVersion)
	}
	a, err := json.Marshal(normalizedWritingRunIdentity(existing.Identity))
	if err != nil {
		return err
	}
	b, err := json.Marshal(normalizedWritingRunIdentity(requested.Identity))
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return errors.New("resume manifest identity mismatch; use a new --output-dir")
	}
	return nil
}

func (s *WritingRunStore) load() error {
	if err := readWritingJSONL(filepath.Join(s.dir, intentsJsonl), s.loadWritingIntent); err != nil {
		return fmt.Errorf("load dispatch intent ledger: %w", err)
	}
	if err := readWritingJSONL(filepath.Join(s.dir, "generations.jsonl"), s.loadWritingGeneration); err != nil {
		return fmt.Errorf("load generation ledger: %w", err)
	}
	if err := readWritingJSONL(filepath.Join(s.dir, "judgments.jsonl"), s.loadWritingJudgment); err != nil {
		return fmt.Errorf("load judgment ledger: %w", err)
	}
	return nil
}

func writingIntentKey(kind, key string, attempt int) string {
	return kind + "\x00" + key + "\x00" + strconv.Itoa(attempt)
}

func validateWritingDispatchIntent(intent WritingDispatchIntent) error {
	if (intent.Kind != generation1 && intent.Kind != judgment1) || intent.Key == "" || intent.Attempt < 1 || len(intent.CallSHA256) != 64 || intent.DispatchedAt.IsZero() {
		return errors.New("invalid dispatch intent")
	}
	return nil
}

// AppendDispatchIntent must run immediately before executor dispatch. It is
// fsynced before returning so an interrupted call cannot be replayed later.
func (s *WritingRunStore) AppendDispatchIntent(intent WritingDispatchIntent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.intentFile == nil {
		return errors.New("writing run store is closed")
	}
	if err := validateWritingDispatchIntent(intent); err != nil {
		return err
	}
	key := writingIntentKey(intent.Kind, intent.Key, intent.Attempt)
	if _, exists := s.intents[key]; exists {
		return fmt.Errorf("dispatch intent already recorded for %s", key)
	}
	if err := appendSyncedJSON(s.intentFile, intent); err != nil {
		return err
	}
	s.intents[key] = intent
	return nil
}

// CheckNoInDoubtDispatch rejects resume when a durable intent has no matching
// generation or judgment result. This preserves the budget and evidence rather
// than risking a duplicate paid provider call.
func (s *WritingRunStore) CheckNoInDoubtDispatch() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for intentKey, intent := range s.intents {
		switch intent.Kind {
		case generation1:
			found := s.hasWritingGenerationResult(intent)
			if !found {
				return fmt.Errorf("dispatch %s has no durable generation result; attempt is in doubt and will not be resumed", intentKey)
			}
		case judgment1:
			found := s.hasWritingJudgmentResult(intent)
			if !found {
				return fmt.Errorf("dispatch %s has no durable judgment result; attempt is in doubt and will not be resumed", intentKey)
			}
		}
	}
	return nil
}

func readWritingJSONL(path string, consume func([]byte) error) error {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64<<10), maxWritingRunRecordSize)
	line := 0
	for scanner.Scan() {
		line++
		data := bytes.TrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}
		if err := consume(append([]byte(nil), data...)); err != nil {
			return fmt.Errorf("record %d: %w", line, err)
		}
	}
	return scanner.Err()
}

// AppendGeneration durably appends one generation record.
func (s *WritingRunStore) AppendGeneration(record WritingGenerationRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := appendSyncedJSON(s.generationFile, record); err != nil {
		return err
	}
	s.generations = append(s.generations, record)
	if record.Attempt > s.generationAttempts[record.Key] {
		s.generationAttempts[record.Key] = record.Attempt
	}
	if record.Status == success1 {
		s.generationSuccess[record.Key] = record
	}
	return nil
}

// AppendJudgment durably appends one judgment record.
func (s *WritingRunStore) AppendJudgment(record WritingJudgmentRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := appendSyncedJSON(s.judgmentFile, record); err != nil {
		return err
	}
	s.judgments = append(s.judgments, record)
	if record.Attempt > s.judgmentAttempts[record.Key] {
		s.judgmentAttempts[record.Key] = record.Attempt
	}
	if record.Status == success1 {
		s.judgmentSuccess[record.Key] = record
	}
	return nil
}

// Generation returns the latest generation record for a key.
func (s *WritingRunStore) Generation(key string) (WritingGenerationRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.generationSuccess[key]
	return record, ok
}

// Judgment returns the latest judgment record for a key.
func (s *WritingRunStore) Judgment(key string) (WritingJudgmentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.judgmentSuccess[key]
	return record, ok
}

// NextGenerationAttempt returns the next attempt number for a generation key.
func (s *WritingRunStore) NextGenerationAttempt(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generationAttempts[key] + 1
}

// NextJudgmentAttempt returns the next attempt number for a judgment key.
func (s *WritingRunStore) NextJudgmentAttempt(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.judgmentAttempts[key] + 1
}

// Records returns all ledgered generations and judgments.
func (s *WritingRunStore) Records() ([]WritingGenerationRecord, []WritingJudgmentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]WritingGenerationRecord(nil), s.generations...), append([]WritingJudgmentRecord(nil), s.judgments...)
}

// Close releases the run directory lock.
func (s *WritingRunStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	if s.generationFile != nil {
		errs = append(errs, s.generationFile.Close())
		s.generationFile = nil
	}
	if s.judgmentFile != nil {
		errs = append(errs, s.judgmentFile.Close())
		s.judgmentFile = nil
	}
	if s.intentFile != nil {
		errs = append(errs, s.intentFile.Close())
		s.intentFile = nil
	}
	if s.lock != nil {
		errs = append(errs, syscall.Flock(int(s.lock.Fd()), syscall.LOCK_UN), s.lock.Close())
		s.lock = nil
	}
	return errors.Join(errs...)
}

func appendSyncedJSON(f *os.File, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Sync()
}
