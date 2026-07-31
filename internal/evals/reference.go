package evals

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// les-v1-reference.json is generated from cached snapshots only with:
// go run ./internal/evals/cmd/generate-reference
//
//go:embed testdata/les-v1-reference.json
var formulaReferenceJSON []byte

type FormulaReference struct {
	FormulaVersion     string               `json:"formula_version"`
	CreatedAt          time.Time            `json:"created_at"`
	AAVersion          float64              `json:"artificial_analysis_index_version"`
	SourceModelCounts  map[string]int       `json:"source_model_counts"`
	SourceFingerprints map[string]string    `json:"source_fingerprints"`
	Metrics            map[string][]float64 `json:"metrics"`
}

type FormulaReferenceSummary struct {
	CreatedAt          time.Time         `json:"created_at"`
	AAVersion          float64           `json:"artificial_analysis_index_version"`
	SourceModelCounts  map[string]int    `json:"source_model_counts"`
	SourceFingerprints map[string]string `json:"source_fingerprints"`
}

type DriftDiagnostics struct {
	Status              string            `json:"status"`
	Reasons             []string          `json:"reasons,omitempty"`
	MaxKS               float64           `json:"max_ks"`
	WorstMetric         string            `json:"worst_metric,omitempty"`
	CurrentFingerprints map[string]string `json:"current_source_fingerprints"`
}

func loadFormulaReference() (FormulaReference, error) {
	var reference FormulaReference
	if err := json.Unmarshal(formulaReferenceJSON, &reference); err != nil {
		return FormulaReference{}, fmt.Errorf("decode formula reference: %w", err)
	}
	if reference.FormulaVersion != FormulaVersion || len(reference.Metrics) == 0 {
		return FormulaReference{}, fmt.Errorf("formula reference is missing or does not match %s", FormulaVersion)
	}
	for _, spec := range externalMetrics {
		values := reference.Metrics[spec.name]
		if len(values) == 0 {
			return FormulaReference{}, fmt.Errorf("formula reference metric %s is missing", spec.name)
		}
		for i, value := range values {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return FormulaReference{}, fmt.Errorf("formula reference metric %s contains a non-finite value", spec.name)
			}
			if i > 0 && values[i-1] > value {
				return FormulaReference{}, fmt.Errorf("formula reference metric %s is not sorted", spec.name)
			}
		}
	}
	return reference, nil
}

func GenerateFormulaReference(result Result) ([]byte, error) {
	createdAt := time.Time{}
	for _, source := range result.Sources {
		if source.FetchedAt.After(createdAt) {
			createdAt = source.FetchedAt
		}
	}
	if createdAt.IsZero() {
		createdAt = result.GeneratedAt
	}
	reference := FormulaReference{
		FormulaVersion: FormulaVersion, CreatedAt: createdAt.UTC(), AAVersion: result.AAVersion,
		SourceModelCounts: sourceModelCounts(result.Models), SourceFingerprints: sourceFingerprints(result.Models),
		Metrics: make(map[string][]float64, len(externalMetrics)),
	}
	for _, spec := range externalMetrics {
		values := make([]float64, 0, len(result.Models))
		for _, model := range result.Models {
			value, ok := spec.value(model)
			if ok && value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) {
				values = append(values, *value)
			}
		}
		sort.Float64s(values)
		reference.Metrics[spec.name] = values
	}
	data, err := json.MarshalIndent(reference, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func sourceModelCounts(models []Model) map[string]int {
	counts := map[string]int{"llm_stats": 0, "artificial_analysis": 0}
	for _, model := range models {
		if model.LLMStats != nil {
			counts["llm_stats"]++
		}
		if model.AA != nil {
			counts["artificial_analysis"]++
		}
	}
	return counts
}

func sourceFingerprints(models []Model) map[string]string {
	type row struct {
		Key          string             `json:"key"`
		Name         string             `json:"name"`
		Organization string             `json:"organization"`
		LLM          *LLMStatsMetrics   `json:"llm,omitempty"`
		AA           *ArtificialMetrics `json:"aa,omitempty"`
	}
	llmRows, aaRows := make([]row, 0), make([]row, 0)
	for _, model := range models {
		if model.LLMStats != nil {
			llmRows = append(llmRows, row{Key: model.Key, Name: model.Name, Organization: model.Organization, LLM: model.LLMStats})
		}
		if model.AA != nil {
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
			aaRows = append(aaRows, row{Key: key, Name: name, Organization: organization, AA: model.AA})
		}
	}
	sort.Slice(llmRows, func(i, j int) bool { return llmRows[i].Key < llmRows[j].Key })
	sort.Slice(aaRows, func(i, j int) bool { return aaRows[i].Key < aaRows[j].Key })
	return map[string]string{"llm_stats": hashJSON(llmRows), "artificial_analysis": hashJSON(aaRows)}
}

func hashJSON(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func evaluateReferenceDrift(models []Model, aaVersion float64, reference FormulaReference, currentValues map[string][]float64) DriftDiagnostics {
	drift := DriftDiagnostics{Status: "stable", CurrentFingerprints: sourceFingerprints(models)}
	if aaVersion != 0 && reference.AAVersion != 0 && aaVersion != reference.AAVersion {
		drift.Reasons = append(drift.Reasons, fmt.Sprintf("Artificial Analysis index changed from %.1f to %.1f", reference.AAVersion, aaVersion))
	}
	counts := sourceModelCounts(models)
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
	metricSources := make(map[string]string, len(externalMetrics))
	for _, spec := range externalMetrics {
		metricSources[spec.name] = spec.source
	}
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
	if drift.MaxKS > .15 {
		drift.Reasons = append(drift.Reasons, fmt.Sprintf("%s distribution KS %.3f exceeds 0.150", drift.WorstMetric, drift.MaxKS))
	}
	if len(drift.Reasons) > 0 {
		drift.Status = "drifted"
	}
	return drift
}

func sortedIntMapKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedMetricMapKeys(values map[string][]float64) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func twoSampleKS(left, right []float64) float64 {
	if len(left) == 0 || len(right) == 0 {
		if len(left) == len(right) {
			return 0
		}
		return 1
	}
	i, j, maximum := 0, 0, 0.0
	for i < len(left) || j < len(right) {
		var value float64
		if j >= len(right) || (i < len(left) && left[i] <= right[j]) {
			value = left[i]
		} else {
			value = right[j]
		}
		for i < len(left) && left[i] <= value {
			i++
		}
		for j < len(right) && right[j] <= value {
			j++
		}
		delta := math.Abs(float64(i)/float64(len(left)) - float64(j)/float64(len(right)))
		maximum = math.Max(maximum, delta)
	}
	return maximum
}
