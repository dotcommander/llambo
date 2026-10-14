package evals

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Repeated string literals consolidated by goconst.
const (
	qualityImportJSON = "quality-import.json"
	success1          = "success"
)

// BuildWritingRunReport derives scores and costs from the recorded run outcomes.
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
		if record.Status == success1 {
			report.Generations++
		} else {
			report.GenerationFailures++
		}
	}
	if specialized, ok := adapter.(writingRunReportAdapter); ok {
		return specialized.buildRunReport(report, generations, judgments)
	}
	state := writingReportAccumulation{byModel: make(map[string]*writingScoreAccumulator), byPrompt: make(map[string]*writingScoreAccumulator), promptGeneration: make(map[string]WritingGenerationRecord)}
	for _, record := range judgments {
		state.consume(&report, generations, record)
	}
	keys := make([]string, 0, len(state.byModel))
	for key := range state.byModel {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		provider, model, _ := strings.Cut(key, "\x00")
		entry := state.byModel[key]
		if entry.count > 0 {
			report.Scores = append(report.Scores, WritingScore{Provider: provider, Model: model, Score: float64(entry.total) / float64(entry.count), Judgments: entry.count})
		}
	}
	promptKeys := make([]string, 0, len(state.byPrompt))
	for key := range state.byPrompt {
		promptKeys = append(promptKeys, key)
	}
	sort.Strings(promptKeys)
	for _, key := range promptKeys {
		generation := state.promptGeneration[key]
		entry := state.byPrompt[key]
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
		return strings.Join([]string{a.Provider, a.Model, a.PromptID, strconv.Itoa(a.Iteration), a.CriterionID}, "\x00") < strings.Join([]string{b.Provider, b.Model, b.PromptID, strconv.Itoa(b.Iteration), b.CriterionID}, "\x00")
	})
	appendWritingDomainScores(&report)
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
		if record.Status != success1 {
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
		stats := proseScreenDistribution(values)
		mean, median, min, max := stats.mean, stats.median, stats.min, stats.max
		report.Scores = append(report.Scores, WritingScore{Provider: provider, Model: model, Score: mean, Judgments: len(values)})
		report.ModelAggregates = append(report.ModelAggregates, WritingModelAggregate{Version: "prose-evaluation-aggregate-v2", Provider: provider, Model: model, Samples: len(values), Score: mean, MechanicsCaps: caps})
		report.ModelDispersion = append(report.ModelDispersion, WritingModelDispersion{Provider: provider, Model: model, Samples: len(values), Mean: mean, Median: median, Min: min, Max: max, Spread: max - min, InsufficientSample: len(values) < 2})
	}
	sort.Slice(report.PromptScores, func(i, j int) bool {
		a, b := report.PromptScores[i], report.PromptScores[j]
		return strings.Join([]string{a.Provider, a.Model, a.PromptID, strconv.Itoa(a.Iteration)}, "\x00") < strings.Join([]string{b.Provider, b.Model, b.PromptID, strconv.Itoa(b.Iteration)}, "\x00")
	})
	sort.Slice(report.CriterionScores, func(i, j int) bool {
		a, b := report.CriterionScores[i], report.CriterionScores[j]
		return strings.Join([]string{a.Provider, a.Model, a.PromptID, strconv.Itoa(a.Iteration), a.CriterionID}, "\x00") < strings.Join([]string{b.Provider, b.Model, b.PromptID, strconv.Itoa(b.Iteration), b.CriterionID}, "\x00")
	})
	return report
}

type proseScreenStatistics struct {
	mean, median, min, max float64
}

func proseScreenDistribution(values []float64) proseScreenStatistics {
	var stats proseScreenStatistics
	if len(values) == 0 {
		return stats
	}
	for _, value := range values {
		stats.mean += value
	}
	stats.mean /= float64(len(values))
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	stats.min, stats.max = sorted[0], sorted[len(sorted)-1]
	middle := len(sorted) / 2
	stats.median = sorted[middle]
	if len(sorted)%2 == 0 {
		stats.median = (sorted[middle-1] + sorted[middle]) / 2
	}
	return stats
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
		if records[i].Key == key && records[i].Status == success1 {
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

// RenderWritingRunMarkdown renders a writing run report as Markdown.
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

func writeSyncedBytes(path string, data []byte) error {
	return writeDurableFile(path, data, 0o700, 0o600, ".llambo-writing-artifact-*")
}

func escapeWritingMarkdown(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(value), "|", "\\|"), "\n", " ")
}
