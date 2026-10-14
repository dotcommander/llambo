package evals

import (
	"fmt"
	"math"
	"sort"
)

type sourceFingerprintRow struct {
	Key          string             `json:"key"`
	Name         string             `json:"name"`
	Organization string             `json:"organization"`
	LLM          *LLMStatsMetrics   `json:"llm,omitempty"`
	AA           *ArtificialMetrics `json:"aa,omitempty"`
}

func sourceFingerprints(models []Model) map[string]string {
	llmRows, aaRows := make([]sourceFingerprintRow, 0), make([]sourceFingerprintRow, 0)
	for _, model := range models {
		if model.LLMStats != nil {
			llmRows = append(llmRows, sourceFingerprintRow{Key: model.Key, Name: model.Name, Organization: model.Organization, LLM: model.LLMStats})
		}
		if model.AA != nil {
			aaRows = append(aaRows, artificialFingerprintRow(model))
		}
	}
	sort.Slice(llmRows, func(i, j int) bool { return llmRows[i].Key < llmRows[j].Key })
	sort.Slice(aaRows, func(i, j int) bool { return aaRows[i].Key < aaRows[j].Key })
	fingerprints := map[string]string{llmStatsKey: hashJSON(llmRows), artificialAnalysisKey: hashJSON(aaRows)}
	for _, source := range []struct{ key, benchmark string }{{writingbench1, writingbench1}, {eqbenchCreativeV3Key, eqbenchCreativeV3}, {ifevalOfficialKey, ifevalOfficial}} {
		for _, model := range models {
			if result, ok := model.Benchmarks[source.benchmark]; ok && result.ContentSHA != "" {
				fingerprints[source.key] = result.ContentSHA
				break
			}
		}
	}
	return fingerprints
}

func artificialFingerprintRow(model Model) sourceFingerprintRow {
	key := model.Key
	name, organization := model.AA.sourceName, model.AA.sourceOrganization
	if name == "" {
		name = model.Name
	}
	if organization == "" {
		organization = model.Organization
	}
	if model.AA.Slug != "" {
		key = model.AA.Slug
	}
	return sourceFingerprintRow{Key: key, Name: name, Organization: organization, AA: model.AA}
}

func evaluateReferenceDrift(models []Model, aaVersion float64, reference FormulaReference, currentValues map[string][]float64) DriftDiagnostics {
	drift := DriftDiagnostics{Status: "stable", CurrentFingerprints: sourceFingerprints(models)}
	if aaVersion != 0 && reference.AAVersion != 0 && aaVersion != reference.AAVersion {
		drift.Reasons = append(drift.Reasons, fmt.Sprintf("Artificial Analysis index changed from %.1f to %.1f", reference.AAVersion, aaVersion))
	}
	counts := sourceModelCounts(models)
	for _, source := range sortedStringMapKeys(reference.SourceFingerprints) {
		current := drift.CurrentFingerprints[source]
		if current != "" && current != reference.SourceFingerprints[source] {
			drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s source fingerprint changed", source))
		}
	}
	for _, source := range sortedIntMapKeys(reference.SourceModelCounts) {
		referenceCount := reference.SourceModelCounts[source]
		if referenceCount == 0 {
			continue
		}
		change := math.Abs(float64(counts[source]-referenceCount)) / float64(referenceCount)
		if change > .20 {
			drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s model count changed %.1f%%", source, change*100))
		}
	}
	evaluateMetricDrift(&drift, reference, currentValues, counts)
	if drift.MaxKS > .15 {
		drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s distribution KS %.3f exceeds 0.150", drift.WorstMetric, drift.MaxKS))
	}
	if len(drift.Reasons) > 0 {
		drift.Status = "drifted"
	}
	return drift
}

func evaluateMetricDrift(drift *DriftDiagnostics, reference FormulaReference, currentValues map[string][]float64, counts map[string]int) {
	metricSources := make(map[string]string, len(operationalMetrics))
	for _, spec := range operationalMetrics {
		metricSources[spec.name] = spec.source
	}
	for _, name := range []string{writingbenchOverall, writingbenchFormat, writingbenchLength, writingbenchDomain, writingbenchRequirements} {
		metricSources[name] = writingbench1
	}
	metricSources[eqbenchCreativeV3Key] = eqbenchCreativeV3Key
	metricSources[ifevalOfficialKey] = ifevalOfficialKey
	for _, name := range sortedMetricMapKeys(reference.Metrics) {
		referenceValues := reference.Metrics[name]
		current := currentValues[name]
		ks := twoSampleKS(referenceValues, current)
		if ks > drift.MaxKS {
			drift.MaxKS, drift.WorstMetric = ks, name
		}
		source := metricSources[name]
		referencePopulation, currentPopulation := reference.SourceModelCounts[source], counts[source]
		if referencePopulation > 0 && currentPopulation > 0 {
			referenceCoverage := float64(len(referenceValues)) / float64(referencePopulation)
			currentCoverage := float64(len(current)) / float64(currentPopulation)
			coverageChange := math.Abs(currentCoverage - referenceCoverage)
			if coverageChange > .10 {
				drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s coverage changed %.1f percentage points", name, coverageChange*100))
			}
		}
	}
}
