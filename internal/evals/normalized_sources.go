package evals

import (
	"sort"
	"strings"
)

func normalizedSourceRecords(result Result, dataset normalizedDataset) []NormalizedSourceRecord {
	counts := normalizedSourceCounts(dataset)
	records := make([]NormalizedSourceRecord, 0, len(result.Sources))
	for _, status := range result.Sources {
		sourceID := normalizedStableSourceID(status.Name)
		roles := counts.roles(sourceID)
		records = append(records, NormalizedSourceRecord{
			SourceID: sourceID, Name: status.Name, URL: status.URL, Cache: status.Cache,
			FetchedAt: status.FetchedAt, Models: status.Models, Observations: counts.observation[sourceID],
			OperationalMetrics: counts.operational[sourceID], ScoreContributions: counts.contribution[sourceID],
			FrozenCohorts: counts.frozen[sourceID], Version: status.Version, CommitSHA: status.CommitSHA,
			ContentSHA256: status.ContentSHA, Methodology: status.Methodology, RegistryVersion: status.RegistryVersion,
			EvidenceGrade: status.EvidenceGrade, Error: status.Error, Roles: roles,
		})
	}
	sort.Slice(records, func(i, j int) bool { return records[i].SourceID < records[j].SourceID })
	return records
}

func normalizedStableSourceID(name string) string {
	switch name {
	case "LLM Stats":
		return llmStats
	case "LLM Stats frozen cohorts":
		return "llm-stats-frozen-cohorts"
	case artificialAnalysisName:
		return artificialAnalysis
	case writingBench1:
		return writingbench1
	case eQBenchCreativeV3:
		return eqbenchCreativeV3
	case "Official IFEval":
		return ifevalOfficial
	case liquidAILFM2X5X2X6BCard:
		return "liquidai-lfm25-2.6b-card"
	case qwen3X8X27BCard:
		return "qwen3.8-27b-card"
	case openAIGptOssModelCard:
		return "openai-gpt-oss-model-card"
	case liquidAILFM2X5VLX3BCard:
		return "liquidai-lfm25-vl-3b-card"
	case googleGemmaX4ModelCard:
		return "google-gemma4-model-card"
	default:
		return strings.ToLower(strings.Join(strings.Fields(name), "-"))
	}
}

func normalizedStableContributionSource(sourceID string) string {
	if sourceID == llmStatsBenchmarkResults || sourceID == llmStatsStatsV1SourceID {
		return llmStats
	}
	return sourceID
}

type normalizedSourceCountSet struct{ observation, operational, contribution, frozen, drift map[string]int }

func normalizedSourceCounts(dataset normalizedDataset) normalizedSourceCountSet {
	observationCounts := make(map[string]int)
	for _, row := range dataset.SourceObservations {
		observationCounts[row.SourceID]++
		for _, mirror := range row.Mirrors {
			observationCounts[mirror.SourceID]++
		}
	}
	operationalCounts := make(map[string]int)
	for _, row := range dataset.OperationalMetrics {
		operationalCounts[row.SourceID]++
	}
	contributionCounts := make(map[string]int)
	for _, row := range dataset.ScoreContributions {
		contributionCounts[normalizedStableContributionSource(row.SourceID)]++
	}
	frozenCounts := make(map[string]int)
	for _, row := range dataset.FrozenCohorts {
		for _, sourceID := range row.SourceIDs {
			if sourceID == "llm-stats-frozen-cohorts" {
				frozenCounts[sourceID]++
			}
		}
	}
	driftCounts := make(map[string]int)
	for _, row := range dataset.DriftDiagnostics {
		if row.SourceID != "" {
			driftCounts[row.SourceID]++
		}
	}
	return normalizedSourceCountSet{observationCounts, operationalCounts, contributionCounts, frozenCounts, driftCounts}
}

func (counts normalizedSourceCountSet) roles(sourceID string) []string {
	roles := make([]string, 0, 5)
	if counts.contribution[sourceID] > 0 {
		roles = append(roles, "capability_evidence")
	}
	if counts.frozen[sourceID] > 0 {
		roles = append(roles, "frozen_cohort")
	}
	if counts.observation[sourceID] > 0 {
		roles = append(roles, "source_observation")
	}
	if counts.operational[sourceID] > 0 {
		roles = append(roles, "operational_metadata")
	}
	if counts.drift[sourceID] > 0 {
		roles = append(roles, "drift_diagnostic")
	}
	if len(roles) == 0 {
		roles = append(roles, "manifest_only")
	}
	return roles
}
