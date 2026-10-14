package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// WritingRunArtifactInput binds all receipt projections to one sealed run.
type WritingRunArtifactInput struct {
	Manifest    WritingRunManifest
	Adapter     WritingBenchmarkAdapter
	Generations []WritingGenerationRecord
	Judgments   []WritingJudgmentRecord
	Complete    bool
	Now         time.Time
}

// WriteWritingRunArtifacts seals derived projections and ledger hashes in a receipt.
func WriteWritingRunArtifacts(dir string, input WritingRunArtifactInput) (WritingRunReceipt, error) {
	manifest, adapter := input.Manifest, input.Adapter
	generations, judgments, complete, now := input.Generations, input.Judgments, input.Complete, input.Now
	if err := validateWritingArtifactTarget(dir); err != nil {
		return WritingRunReceipt{}, err
	}
	report := BuildWritingRunReport(manifest, adapter, generations, judgments, complete)
	artifacts, err := writingDerivedArtifacts(report, complete)
	if err != nil {
		return WritingRunReceipt{}, err
	}
	hashes, err := writeWritingDerivedArtifacts(dir, artifacts)
	if err != nil {
		return WritingRunReceipt{}, err
	}
	if err := hashWritingLedgers(dir, hashes); err != nil {
		return WritingRunReceipt{}, err
	}
	status := partial1
	if complete && !report.PreScreenOnly {
		status = completeQuality
	} else if complete && report.PreScreenOnly {
		status = "complete_pre_screen"
	}
	identities := writingReceiptIdentities(manifest, generations, judgments)
	receipt := WritingRunReceipt{SchemaVersion: WritingRunSchemaVersion, RunID: manifest.RunID, IdentitySHA256: WritingRunIdentityHash(manifest), InputSHA256: manifest.Identity.InputSHA256, RequestedModels: sortedUnique(identities.requested), ServedModels: sortedUnique(identities.served), RequestedJudge: manifest.Identity.Judge.ID(), ServedJudges: sortedUnique(identities.servedJudges), CompletedAt: now.UTC(), Status: status, Report: report, Artifacts: hashes}
	if err := writeSyncedJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		return WritingRunReceipt{}, fmt.Errorf("write receipt: %w", err)
	}
	return receipt, nil
}

func validateWritingArtifactTarget(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "receipt.json")); err == nil {
		return errors.New("completed writing output is immutable; use a new --output-dir")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, name := range []string{reportJSON1, reportMd, qualityImportJSON} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return fmt.Errorf("writing output has %s without a receipt; preserve it and use a new --output-dir", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func writingDerivedArtifacts(report WritingRunReport, complete bool) (map[string][]byte, error) {
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return nil, err
	}
	reportJSON = append(reportJSON, '\n')
	reportMarkdown := []byte(RenderWritingRunMarkdown(report))
	quality := make([]WritingQualityImportRecord, 0, len(report.Scores))
	if complete && !report.PreScreenOnly {
		for _, score := range report.Scores {
			quality = append(quality, WritingQualityImportRecord{
				Provider: score.Provider,
				Model:    score.Model,
				Task:     report.ScoreIdentity,
				Score:    score.Score / 10,
				Source:   "llambo-writing-run/" + report.RunID,
				Notes:    fmt.Sprintf("%d local judge criteria; explicit import required", score.Judgments),
			})
		}
	}
	qualityJSON, err := json.MarshalIndent(quality, "", "  ")
	if err != nil {
		return nil, err
	}
	qualityJSON = append(qualityJSON, '\n')
	artifacts := map[string][]byte{
		reportJSON1:       reportJSON,
		reportMd:          reportMarkdown,
		qualityImportJSON: qualityJSON,
	}
	return artifacts, nil
}

func writeWritingDerivedArtifacts(dir string, artifacts map[string][]byte) (map[string]string, error) {
	hashes := make(map[string]string, len(artifacts))
	for name, data := range artifacts {
		if err := writeSyncedBytes(filepath.Join(dir, name), data); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	return hashes, nil
}

func hashWritingLedgers(dir string, hashes map[string]string) error {
	for _, name := range []string{manifestJSON, intentsJsonl, "generations.jsonl", "judgments.jsonl"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("hash %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	return nil
}

type writingRunIdentities struct {
	requested, served, servedJudges []string
}

func writingReceiptIdentities(manifest WritingRunManifest, generations []WritingGenerationRecord, judgments []WritingJudgmentRecord) writingRunIdentities {
	requested, served, servedJudges := make([]string, 0, len(manifest.Identity.Models)), []string{}, []string{}
	for _, model := range manifest.Identity.Models {
		requested = append(requested, model.ID())
	}
	for _, record := range generations {
		if record.Status == success1 {
			if record.ActualProvider != "" && record.ActualModel != "" {
				served = append(served, record.ActualProvider+"/"+record.ActualModel)
			} else {
				served = append(served, record.Provider+"/"+record.Model)
			}
		}
	}
	for _, record := range judgments {
		if record.Status == success1 {
			if record.ActualProvider != "" && record.ActualModel != "" {
				servedJudges = append(servedJudges, record.ActualProvider+"/"+record.ActualModel)
			} else {
				servedJudges = append(servedJudges, record.JudgeProvider+"/"+record.JudgeModel)
			}
		}
	}
	return writingRunIdentities{requested: requested, served: served, servedJudges: servedJudges}
}
