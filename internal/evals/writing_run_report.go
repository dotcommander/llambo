package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func BuildWritingRunReport(manifest WritingRunManifest, adapter WritingBenchmarkAdapter, generations []WritingGenerationRecord, judgments []WritingJudgmentRecord, complete bool) WritingRunReport {
	report := WritingRunReport{
		SchemaVersion: WritingRunSchemaVersion,
		RunID:         manifest.RunID,
		BenchmarkID:   manifest.Identity.BenchmarkID,
		ScoreIdentity: adapter.ScoreIdentity(),
		Complete:      complete,
	}
	for _, record := range generations {
		report.ObservedCostUSD += record.Usage.CostUSD
		if record.Status == "success" {
			report.Generations++
		} else {
			report.GenerationFailures++
		}
	}
	type accumulator struct {
		total int
		count int
	}
	byModel := make(map[string]*accumulator)
	byPrompt := make(map[string]*accumulator)
	promptGeneration := make(map[string]WritingGenerationRecord)
	for _, record := range judgments {
		report.ObservedCostUSD += record.Usage.CostUSD
		if record.Status != "success" {
			report.JudgmentFailures++
			continue
		}
		report.Judgments++
		generation := generationByKey(generations, record.GenerationKey)
		key := generation.Provider + "\x00" + generation.Model
		entry := byModel[key]
		if entry == nil {
			entry = &accumulator{}
			byModel[key] = entry
		}
		entry.total += record.Score
		entry.count++
		promptEntry := byPrompt[generation.Key]
		if promptEntry == nil {
			promptEntry = &accumulator{}
			byPrompt[generation.Key] = promptEntry
			promptGeneration[generation.Key] = generation
		}
		promptEntry.total += record.Score
		promptEntry.count++
		report.CriterionScores = append(report.CriterionScores, WritingCriterionScore{
			Provider: generation.Provider, Model: generation.Model, PromptID: generation.PromptID,
			Iteration: generation.Iteration, CriterionID: record.CriterionID, Score: record.Score,
		})
	}
	keys := make([]string, 0, len(byModel))
	for key := range byModel {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		provider, model, _ := strings.Cut(key, "\x00")
		entry := byModel[key]
		report.Scores = append(report.Scores, WritingScore{Provider: provider, Model: model, Score: float64(entry.total) / float64(entry.count), Judgments: entry.count})
	}
	promptKeys := make([]string, 0, len(byPrompt))
	for key := range byPrompt {
		promptKeys = append(promptKeys, key)
	}
	sort.Strings(promptKeys)
	for _, key := range promptKeys {
		generation := promptGeneration[key]
		entry := byPrompt[key]
		report.PromptScores = append(report.PromptScores, WritingPromptScore{
			Provider: generation.Provider, Model: generation.Model, PromptID: generation.PromptID,
			Iteration: generation.Iteration, Domain1: generation.Domain1, Domain2: generation.Domain2,
			Score: float64(entry.total) / float64(entry.count), Judgments: entry.count,
		})
	}
	sort.Slice(report.CriterionScores, func(i, j int) bool {
		a, b := report.CriterionScores[i], report.CriterionScores[j]
		return strings.Join([]string{a.Provider, a.Model, a.PromptID, fmt.Sprint(a.Iteration), a.CriterionID}, "\x00") < strings.Join([]string{b.Provider, b.Model, b.PromptID, fmt.Sprint(b.Iteration), b.CriterionID}, "\x00")
	})
	type domainAccumulator struct {
		total float64
		count int
	}
	byDomain := make(map[string]*domainAccumulator)
	for _, prompt := range report.PromptScores {
		for _, domain := range []string{domainLabel("domain1", prompt.Domain1), domainLabel("domain2", prompt.Domain2)} {
			if domain == "" {
				continue
			}
			key := strings.Join([]string{prompt.Provider, prompt.Model, domain}, "\x00")
			entry := byDomain[key]
			if entry == nil {
				entry = &domainAccumulator{}
				byDomain[key] = entry
			}
			entry.total += prompt.Score
			entry.count++
		}
	}
	domainKeys := make([]string, 0, len(byDomain))
	for key := range byDomain {
		domainKeys = append(domainKeys, key)
	}
	sort.Strings(domainKeys)
	for _, key := range domainKeys {
		parts := strings.Split(key, "\x00")
		entry := byDomain[key]
		report.DomainScores = append(report.DomainScores, WritingDomainScore{Provider: parts[0], Model: parts[1], Domain: parts[2], Score: entry.total / float64(entry.count), Prompts: entry.count})
	}
	return report
}

func domainLabel(level, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return level + ":" + value
}

func generationByKey(records []WritingGenerationRecord, key string) WritingGenerationRecord {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Key == key && records[i].Status == "success" {
			return records[i]
		}
	}
	return WritingGenerationRecord{}
}

func RenderWritingRunMarkdown(report WritingRunReport) string {
	var out strings.Builder
	out.WriteString("# Local writing evaluation\n\n")
	fmt.Fprintf(&out, "- **Run:** `%s`\n", report.RunID)
	fmt.Fprintf(&out, "- **Benchmark:** `%s`\n", report.BenchmarkID)
	fmt.Fprintf(&out, "- **Score identity:** `%s`\n", report.ScoreIdentity)
	fmt.Fprintf(&out, "- **Status:** `%s`\n", map[bool]string{true: "complete", false: "partial"}[report.Complete])
	fmt.Fprintf(&out, "- **Observed provider cost:** `$%.6f`\n\n", report.ObservedCostUSD)
	out.WriteString("| Provider | Model | Mean score (1–10) | Judgments |\n")
	out.WriteString("| --- | --- | ---: | ---: |\n")
	for _, score := range report.Scores {
		fmt.Fprintf(&out, "| %s | %s | %.3f | %d |\n", escapeWritingMarkdown(score.Provider), escapeWritingMarkdown(score.Model), score.Score, score.Judgments)
	}
	if len(report.DomainScores) > 0 {
		out.WriteString("\n## Domain scores\n\n| Provider | Model | Domain | Mean score | Prompts |\n| --- | --- | --- | ---: | ---: |\n")
		for _, score := range report.DomainScores {
			fmt.Fprintf(&out, "| %s | %s | %s | %.3f | %d |\n", escapeWritingMarkdown(score.Provider), escapeWritingMarkdown(score.Model), escapeWritingMarkdown(score.Domain), score.Score, score.Prompts)
		}
	}
	out.WriteString("\n## Prompt scores\n\n| Provider | Model | Prompt | Iteration | Mean score | Judgments |\n| --- | --- | --- | ---: | ---: | ---: |\n")
	for _, score := range report.PromptScores {
		fmt.Fprintf(&out, "| %s | %s | %s | %d | %.3f | %d |\n", escapeWritingMarkdown(score.Provider), escapeWritingMarkdown(score.Model), escapeWritingMarkdown(score.PromptID), score.Iteration, score.Score, score.Judgments)
	}
	fmt.Fprintf(&out, "\nSuccessful generations: **%d**; failed attempts: **%d**. Successful judgments: **%d**; failed attempts: **%d**.\n", report.Generations, report.GenerationFailures, report.Judgments, report.JudgmentFailures)
	if report.ScoreIdentity == "eqbench-creative-v3/local-rubric" {
		out.WriteString("\n> This is a local rubric score. It is not the official EQ-Bench Elo, Glicko, or leaderboard result.\n")
	}
	return out.String()
}

func WriteWritingRunArtifacts(dir string, manifest WritingRunManifest, adapter WritingBenchmarkAdapter, generations []WritingGenerationRecord, judgments []WritingJudgmentRecord, complete bool, now time.Time) (WritingRunReceipt, error) {
	report := BuildWritingRunReport(manifest, adapter, generations, judgments, complete)
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return WritingRunReceipt{}, err
	}
	reportJSON = append(reportJSON, '\n')
	reportMarkdown := []byte(RenderWritingRunMarkdown(report))
	quality := make([]WritingQualityImportRecord, 0, len(report.Scores))
	if complete {
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
		return WritingRunReceipt{}, err
	}
	qualityJSON = append(qualityJSON, '\n')
	artifacts := map[string][]byte{
		"report.json":         reportJSON,
		"report.md":           reportMarkdown,
		"quality-import.json": qualityJSON,
	}
	hashes := make(map[string]string, len(artifacts))
	for name, data := range artifacts {
		if err := writeSyncedBytes(filepath.Join(dir, name), data); err != nil {
			return WritingRunReceipt{}, fmt.Errorf("write %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	for _, name := range []string{"manifest.json", "generations.jsonl", "judgments.jsonl"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return WritingRunReceipt{}, fmt.Errorf("hash %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	status := "partial"
	if complete {
		status = "complete"
	}
	receipt := WritingRunReceipt{SchemaVersion: WritingRunSchemaVersion, RunID: manifest.RunID, CompletedAt: now.UTC(), Status: status, Report: report, Artifacts: hashes}
	if err := writeSyncedJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		return WritingRunReceipt{}, fmt.Errorf("write receipt: %w", err)
	}
	return receipt, nil
}

func writeSyncedBytes(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".llambo-writing-artifact-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
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

func escapeWritingMarkdown(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "|", "\\|"), "\n", " ")
}
