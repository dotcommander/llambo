package evals

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	if specialized, ok := adapter.(writingRunReportAdapter); ok {
		return specialized.buildRunReport(report, generations, judgments)
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
		results := record.Results
		if len(results) == 0 {
			results = []WritingCriterionJudgment{{CriterionID: record.CriterionID, Criterion: record.Criterion, Score: record.Score, Reason: record.Reason}}
		}
		generation := generationByKey(generations, record.GenerationKey)
		key := generation.Provider + "\x00" + generation.Model
		entry := byModel[key]
		if entry == nil {
			entry = &accumulator{}
			byModel[key] = entry
		}
		for _, result := range results {
			report.Judgments++
			entry.total += result.Score
			entry.count++
		}
		promptEntry := byPrompt[generation.Key]
		if promptEntry == nil {
			promptEntry = &accumulator{}
			byPrompt[generation.Key] = promptEntry
			promptGeneration[generation.Key] = generation
		}
		for _, result := range results {
			promptEntry.total += result.Score
			promptEntry.count++
			report.CriterionScores = append(report.CriterionScores, WritingCriterionScore{Provider: generation.Provider, Model: generation.Model, PromptID: generation.PromptID, Iteration: generation.Iteration, CriterionID: result.CriterionID, Score: result.Score, Applicable: result.Applicable, Evidence: append([]ProseEvidenceSpan(nil), result.Evidence...), Reason: result.Reason})
		}
	}
	keys := make([]string, 0, len(byModel))
	for key := range byModel {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		provider, model, _ := strings.Cut(key, "\x00")
		entry := byModel[key]
		if entry.count > 0 {
			report.Scores = append(report.Scores, WritingScore{Provider: provider, Model: model, Score: float64(entry.total) / float64(entry.count), Judgments: entry.count})
		}
	}
	promptKeys := make([]string, 0, len(byPrompt))
	for key := range byPrompt {
		promptKeys = append(promptKeys, key)
	}
	sort.Strings(promptKeys)
	for _, key := range promptKeys {
		generation := promptGeneration[key]
		entry := byPrompt[key]
		if entry.count == 0 {
			continue
		}
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

func buildProseScreenRunReport(report WritingRunReport, generations []WritingGenerationRecord, judgments []WritingJudgmentRecord) WritingRunReport {
	type caseScore struct {
		generation WritingGenerationRecord
		aggregate  ProseEvaluationAggregate
	}
	byModel := map[string][]caseScore{}
	for _, record := range judgments {
		report.ObservedCostUSD += record.Usage.CostUSD
		if record.Status != "success" {
			report.JudgmentFailures++
			continue
		}
		aggregate, ok := proseAggregateFromJudgments(record.Results)
		if !ok {
			report.JudgmentFailures++
			continue
		}
		generation := generationByKey(generations, record.GenerationKey)
		if generation.Key == "" {
			report.JudgmentFailures++
			continue
		}
		report.Judgments++
		for _, result := range record.Results {
			report.CriterionScores = append(report.CriterionScores, WritingCriterionScore{Provider: generation.Provider, Model: generation.Model, PromptID: generation.PromptID, Iteration: generation.Iteration, CriterionID: result.CriterionID, Score: result.Score, Applicable: result.Applicable, Evidence: append([]ProseEvidenceSpan(nil), result.Evidence...), Reason: result.Reason})
		}
		key := generation.Provider + "\x00" + generation.Model
		byModel[key] = append(byModel[key], caseScore{generation: generation, aggregate: aggregate})
		report.PromptScores = append(report.PromptScores, WritingPromptScore{Provider: generation.Provider, Model: generation.Model, PromptID: generation.PromptID, Iteration: generation.Iteration, Domain1: generation.Domain1, Domain2: generation.Domain2, Score: aggregate.Score, Judgments: 1})
	}
	keys := make([]string, 0, len(byModel))
	for key := range byModel {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		provider, model, _ := strings.Cut(key, "\x00")
		cases := byModel[key]
		values := make([]float64, 0, len(cases))
		caps := 0
		for _, item := range cases {
			values = append(values, item.aggregate.Score)
			if item.aggregate.MechanicsCapApplied {
				caps++
			}
		}
		mean, median, min, max := proseScreenDistribution(values)
		report.Scores = append(report.Scores, WritingScore{Provider: provider, Model: model, Score: mean, Judgments: len(values)})
		report.ModelAggregates = append(report.ModelAggregates, WritingModelAggregate{Version: "prose-evaluation-aggregate-v2", Provider: provider, Model: model, Samples: len(values), Score: mean, MechanicsCaps: caps})
		report.ModelDispersion = append(report.ModelDispersion, WritingModelDispersion{Provider: provider, Model: model, Samples: len(values), Mean: mean, Median: median, Min: min, Max: max, Spread: max - min, InsufficientSample: len(values) < 2})
	}
	sort.Slice(report.PromptScores, func(i, j int) bool {
		a, b := report.PromptScores[i], report.PromptScores[j]
		return strings.Join([]string{a.Provider, a.Model, a.PromptID, fmt.Sprint(a.Iteration)}, "\x00") < strings.Join([]string{b.Provider, b.Model, b.PromptID, fmt.Sprint(b.Iteration)}, "\x00")
	})
	sort.Slice(report.CriterionScores, func(i, j int) bool {
		a, b := report.CriterionScores[i], report.CriterionScores[j]
		return strings.Join([]string{a.Provider, a.Model, a.PromptID, fmt.Sprint(a.Iteration), a.CriterionID}, "\x00") < strings.Join([]string{b.Provider, b.Model, b.PromptID, fmt.Sprint(b.Iteration), b.CriterionID}, "\x00")
	})
	return report
}

func proseScreenDistribution(values []float64) (mean, median, min, max float64) {
	if len(values) == 0 {
		return 0, 0, 0, 0
	}
	for _, value := range values {
		mean += value
	}
	mean /= float64(len(values))
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	min, max = sorted[0], sorted[len(sorted)-1]
	middle := len(sorted) / 2
	median = sorted[middle]
	if len(sorted)%2 == 0 {
		median = (sorted[middle-1] + sorted[middle]) / 2
	}
	return mean, median, min, max
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
			record := records[i]
			if record.ActualProvider != "" {
				record.Provider = record.ActualProvider
			}
			if record.ActualModel != "" {
				record.Model = record.ActualModel
			}
			return record
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
	if report.PreScreenOnly {
		out.WriteString("> **Pre-screen only:** this report cannot establish factual support, semantic qualification, or writer promotion. TLDW remains authoritative for those decisions.\n\n")
	}
	scoreScale := "1–10"
	if report.PreScreenOnly {
		scoreScale = "1–5"
	}
	fmt.Fprintf(&out, "| Provider | Model | Mean score (%s) | Judgments |\n", scoreScale)
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
	if len(report.ModelDispersion) > 0 {
		out.WriteString("\n## Model dispersion\n\n| Provider | Model | Cases | Mean | Median | Min | Max | Spread | Status |\n| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- |\n")
		for _, item := range report.ModelDispersion {
			status := "sufficient"
			if item.InsufficientSample {
				status = "insufficient sample"
			}
			fmt.Fprintf(&out, "| %s | %s | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %s |\n", escapeWritingMarkdown(item.Provider), escapeWritingMarkdown(item.Model), item.Samples, item.Mean, item.Median, item.Min, item.Max, item.Spread, status)
		}
	}
	if len(report.ModelAggregates) > 0 {
		out.WriteString("\n## Go-computed prose aggregates\n\n| Version | Provider | Model | Cases | Aggregate | Mechanics caps |\n| --- | --- | --- | ---: | ---: | ---: |\n")
		for _, item := range report.ModelAggregates {
			fmt.Fprintf(&out, "| %s | %s | %s | %d | %.3f | %d |\n", escapeWritingMarkdown(item.Version), escapeWritingMarkdown(item.Provider), escapeWritingMarkdown(item.Model), item.Samples, item.Score, item.MechanicsCaps)
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
	if _, err := os.Stat(filepath.Join(dir, "receipt.json")); err == nil {
		return WritingRunReceipt{}, fmt.Errorf("completed writing output is immutable; use a new --output-dir")
	} else if !errors.Is(err, os.ErrNotExist) {
		return WritingRunReceipt{}, err
	}
	for _, name := range []string{"report.json", "report.md", "quality-import.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return WritingRunReceipt{}, fmt.Errorf("writing output has %s without a receipt; preserve it and use a new --output-dir", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return WritingRunReceipt{}, err
		}
	}
	report := BuildWritingRunReport(manifest, adapter, generations, judgments, complete)
	reportJSON, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return WritingRunReceipt{}, err
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
	for _, name := range []string{"manifest.json", "intents.jsonl", "generations.jsonl", "judgments.jsonl"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return WritingRunReceipt{}, fmt.Errorf("hash %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		hashes[name] = hex.EncodeToString(sum[:])
	}
	status := "partial"
	if complete && !report.PreScreenOnly {
		status = "complete_quality"
	} else if complete && report.PreScreenOnly {
		status = "complete_pre_screen"
	}
	requested, served, servedJudges := make([]string, 0, len(manifest.Identity.Models)), []string{}, []string{}
	for _, model := range manifest.Identity.Models {
		requested = append(requested, model.ID())
	}
	for _, record := range generations {
		if record.Status == "success" {
			if record.ActualProvider != "" && record.ActualModel != "" {
				served = append(served, record.ActualProvider+"/"+record.ActualModel)
			} else {
				served = append(served, record.Provider+"/"+record.Model)
			}
		}
	}
	for _, record := range judgments {
		if record.Status == "success" {
			if record.ActualProvider != "" && record.ActualModel != "" {
				servedJudges = append(servedJudges, record.ActualProvider+"/"+record.ActualModel)
			} else {
				servedJudges = append(servedJudges, record.JudgeProvider+"/"+record.JudgeModel)
			}
		}
	}
	receipt := WritingRunReceipt{SchemaVersion: WritingRunSchemaVersion, RunID: manifest.RunID, IdentitySHA256: WritingRunIdentityHash(manifest), InputSHA256: manifest.Identity.InputSHA256, RequestedModels: sortedUnique(requested), ServedModels: sortedUnique(served), RequestedJudge: manifest.Identity.Judge.ID(), ServedJudges: sortedUnique(servedJudges), CompletedAt: now.UTC(), Status: status, Report: report, Artifacts: hashes}
	if err := writeSyncedJSON(filepath.Join(dir, "receipt.json"), receipt); err != nil {
		return WritingRunReceipt{}, fmt.Errorf("write receipt: %w", err)
	}
	return receipt, nil
}

func writeSyncedBytes(path string, data []byte) error {
	return writeDurableFile(path, data, 0o700, 0o600, ".llambo-writing-artifact-*")
}

func escapeWritingMarkdown(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "|", "\\|"), "\n", " ")
}
