package evals

import (
	"sort"
	"strings"
)

type writingScoreAccumulator struct {
	total int
	count int
}

type writingReportAccumulation struct {
	byModel          map[string]*writingScoreAccumulator
	byPrompt         map[string]*writingScoreAccumulator
	promptGeneration map[string]WritingGenerationRecord
}

func (state *writingReportAccumulation) consume(report *WritingRunReport, generations []WritingGenerationRecord, record WritingJudgmentRecord) {
	report.ObservedCostUSD += record.Usage.CostUSD
	if record.Status != success1 {
		report.JudgmentFailures++
		return
	}
	results := record.Results
	if len(results) == 0 {
		results = []WritingCriterionJudgment{{CriterionID: record.CriterionID, Criterion: record.Criterion, Score: record.Score, Reason: record.Reason}}
	}
	generation := generationByKey(generations, record.GenerationKey)
	key := generation.Provider + "\x00" + generation.Model
	entry := state.byModel[key]
	if entry == nil {
		entry = &writingScoreAccumulator{}
		state.byModel[key] = entry
	}
	for _, result := range results {
		report.Judgments++
		entry.total += result.Score
		entry.count++
	}
	promptEntry := state.byPrompt[generation.Key]
	if promptEntry == nil {
		promptEntry = &writingScoreAccumulator{}
		state.byPrompt[generation.Key] = promptEntry
		state.promptGeneration[generation.Key] = generation
	}
	for _, result := range results {
		promptEntry.total += result.Score
		promptEntry.count++
		report.CriterionScores = append(report.CriterionScores, WritingCriterionScore{Provider: generation.Provider, Model: generation.Model, PromptID: generation.PromptID, Iteration: generation.Iteration, CriterionID: result.CriterionID, Score: result.Score, Applicable: result.Applicable, Evidence: append([]ProseEvidenceSpan(nil), result.Evidence...), Reason: result.Reason})
	}
}

func appendWritingDomainScores(report *WritingRunReport) {
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
}
