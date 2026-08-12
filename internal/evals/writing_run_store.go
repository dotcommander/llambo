package evals

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	maxWritingRunInputSize  = 128 << 20
	maxWritingRunRecordSize = 64 << 20
)

type WritingRunStore struct {
	mu                 sync.Mutex
	dir                string
	generationFile     *os.File
	judgmentFile       *os.File
	generations        []WritingGenerationRecord
	judgments          []WritingJudgmentRecord
	generationSuccess  map[string]WritingGenerationRecord
	generationAttempts map[string]int
	judgmentSuccess    map[string]WritingJudgmentRecord
	judgmentAttempts   map[string]int
}

func OpenWritingRunStore(dir string, manifest WritingRunManifest) (*WritingRunStore, error) {
	dir = filepath.Clean(dir)
	if dir == "." || dir == "" || filepath.Dir(dir) == dir {
		return nil, fmt.Errorf("writing run output directory must be explicit")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create writing run directory: %w", err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if data, err := os.ReadFile(manifestPath); err == nil {
		var existing WritingRunManifest
		if err := json.Unmarshal(data, &existing); err != nil {
			return nil, fmt.Errorf("parse existing writing manifest: %w", err)
		}
		if err := sameWritingRunIdentity(existing, manifest); err != nil {
			return nil, err
		}
		if err := os.Chmod(manifestPath, 0o600); err != nil {
			return nil, fmt.Errorf("secure writing manifest: %w", err)
		}
		manifest = existing
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read writing manifest: %w", err)
	} else {
		for _, name := range []string{"generations.jsonl", "judgments.jsonl", "receipt.json", "report.json", "report.md", "quality-import.json"} {
			if _, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
				return nil, fmt.Errorf("writing run directory has %s but no manifest; use a new --output-dir", name)
			} else if !errors.Is(statErr, os.ErrNotExist) {
				return nil, fmt.Errorf("inspect writing run directory: %w", statErr)
			}
		}
		if err := writeSyncedJSON(manifestPath, manifest); err != nil {
			return nil, fmt.Errorf("write writing manifest: %w", err)
		}
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure writing run directory: %w", err)
	}

	store := &WritingRunStore{
		dir:                dir,
		generationSuccess:  make(map[string]WritingGenerationRecord),
		generationAttempts: make(map[string]int),
		judgmentSuccess:    make(map[string]WritingJudgmentRecord),
		judgmentAttempts:   make(map[string]int),
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	var err error
	store.generationFile, err = os.OpenFile(filepath.Join(dir, "generations.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open generation ledger: %w", err)
	}
	store.judgmentFile, err = os.OpenFile(filepath.Join(dir, "judgments.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		_ = store.generationFile.Close()
		return nil, fmt.Errorf("open judgment ledger: %w", err)
	}
	if err := store.generationFile.Chmod(0o600); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("secure generation ledger: %w", err)
	}
	if err := store.judgmentFile.Chmod(0o600); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("secure judgment ledger: %w", err)
	}
	return store, nil
}

func sameWritingRunIdentity(existing, requested WritingRunManifest) error {
	if existing.SchemaVersion != requested.SchemaVersion {
		return fmt.Errorf("resume manifest schema mismatch: have %d, requested %d", existing.SchemaVersion, requested.SchemaVersion)
	}
	a, err := json.Marshal(existing.Identity)
	if err != nil {
		return err
	}
	b, err := json.Marshal(requested.Identity)
	if err != nil {
		return err
	}
	if !bytes.Equal(a, b) {
		return fmt.Errorf("resume manifest identity mismatch; use a new --output-dir")
	}
	return nil
}

func (s *WritingRunStore) load() error {
	if err := readWritingJSONL(filepath.Join(s.dir, "generations.jsonl"), func(data []byte) error {
		var record WritingGenerationRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		s.generations = append(s.generations, record)
		if record.Attempt > s.generationAttempts[record.Key] {
			s.generationAttempts[record.Key] = record.Attempt
		}
		if record.Status == "success" {
			s.generationSuccess[record.Key] = record
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load generation ledger: %w", err)
	}
	if err := readWritingJSONL(filepath.Join(s.dir, "judgments.jsonl"), func(data []byte) error {
		var record WritingJudgmentRecord
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		s.judgments = append(s.judgments, record)
		if record.Attempt > s.judgmentAttempts[record.Key] {
			s.judgmentAttempts[record.Key] = record.Attempt
		}
		if record.Status == "success" {
			s.judgmentSuccess[record.Key] = record
		}
		return nil
	}); err != nil {
		return fmt.Errorf("load judgment ledger: %w", err)
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
	defer f.Close()
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
	if record.Status == "success" {
		s.generationSuccess[record.Key] = record
	}
	return nil
}

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
	if record.Status == "success" {
		s.judgmentSuccess[record.Key] = record
	}
	return nil
}

func (s *WritingRunStore) Generation(key string) (WritingGenerationRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.generationSuccess[key]
	return record, ok
}

func (s *WritingRunStore) Judgment(key string) (WritingJudgmentRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.judgmentSuccess[key]
	return record, ok
}

func (s *WritingRunStore) NextGenerationAttempt(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generationAttempts[key] + 1
}

func (s *WritingRunStore) NextJudgmentAttempt(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.judgmentAttempts[key] + 1
}

func (s *WritingRunStore) Records() ([]WritingGenerationRecord, []WritingJudgmentRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]WritingGenerationRecord(nil), s.generations...), append([]WritingJudgmentRecord(nil), s.judgments...)
}

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
