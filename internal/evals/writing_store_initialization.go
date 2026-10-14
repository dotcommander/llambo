package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func initializeWritingManifest(dir string, manifest WritingRunManifest) error {
	manifestPath := filepath.Join(dir, manifestJSON)
	data, err := os.ReadFile(manifestPath)
	if err == nil {
		return resumeWritingManifest(manifestPath, data, manifest)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read writing manifest: %w", err)
	}
	return createWritingManifest(dir, manifestPath, manifest)
}

func resumeWritingManifest(path string, data []byte, manifest WritingRunManifest) error {
	var existing WritingRunManifest
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("parse existing writing manifest: %w", err)
	}
	if err := sameWritingRunIdentity(existing, manifest); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure writing manifest: %w", err)
	}
	return nil
}

func createWritingManifest(dir, manifestPath string, manifest WritingRunManifest) error {
	for _, name := range []string{intentsJsonl, "generations.jsonl", "judgments.jsonl", "receipt.json", reportJSON1, reportMd, qualityImportJSON} {
		if _, statErr := os.Stat(filepath.Join(dir, name)); statErr == nil {
			return fmt.Errorf("writing run directory has %s but no manifest; use a new --output-dir", name)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return fmt.Errorf("inspect writing run directory: %w", statErr)
		}
	}
	if err := writeSyncedJSON(manifestPath, manifest); err != nil {
		return fmt.Errorf("write writing manifest: %w", err)
	}
	return nil
}

func (store *WritingRunStore) openLedgers(openLedger func(string, int, os.FileMode) (*os.File, error)) error {
	var err error
	dir := store.dir
	store.intentFile, err = openLedger(filepath.Join(dir, intentsJsonl), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open dispatch intent ledger: %w", err)
	}
	store.generationFile, err = openLedger(filepath.Join(dir, "generations.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open generation ledger: %w", err)
	}
	store.judgmentFile, err = openLedger(filepath.Join(dir, "judgments.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open judgment ledger: %w", err)
	}
	if err := store.intentFile.Chmod(0o600); err != nil {
		return fmt.Errorf("secure dispatch intent ledger: %w", err)
	}
	if err := store.generationFile.Chmod(0o600); err != nil {
		return fmt.Errorf("secure generation ledger: %w", err)
	}
	if err := store.judgmentFile.Chmod(0o600); err != nil {
		return fmt.Errorf("secure judgment ledger: %w", err)
	}
	return nil
}
