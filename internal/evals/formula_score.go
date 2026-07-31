package evals

import (
	"math"
	"sort"
)

type normalizedMetric struct {
	spec       metricSpec
	percentile map[int]float64
}

type scoreAccumulator struct {
	weighted float64
	weight   float64
	max      float64
	signals  int
}

func normalizeExternalMetrics(models []Model, reference FormulaReference) ([]normalizedMetric, []MetricDiagnostic, map[string][]float64) {
	normalized := make([]normalizedMetric, 0, len(externalMetrics))
	diagnostics := make([]MetricDiagnostic, 0, len(externalMetrics))
	currentValues := make(map[string][]float64, len(externalMetrics))
	for _, spec := range externalMetrics {
		values := make([]float64, 0, len(models))
		byModel := make(map[int]float64)
		for i, model := range models {
			value, ok := spec.value(model)
			if !ok || value == nil || math.IsNaN(*value) || math.IsInf(*value, 0) {
				continue
			}
			values = append(values, *value)
			byModel[i] = *value
		}
		sort.Float64s(values)
		currentValues[spec.name] = values
		referenceValues := reference.Metrics[spec.name]
		percentiles := make(map[int]float64, len(byModel))
		for i, value := range byModel {
			percentiles[i] = empiricalPercentile(referenceValues, value, spec.higher)
		}
		normalized = append(normalized, normalizedMetric{spec: spec, percentile: percentiles})
		direction := "lower"
		if spec.higher {
			direction = "higher"
		}
		diagnostics = append(diagnostics, MetricDiagnostic{Name: spec.name, Dimension: spec.dimension, Source: spec.source, Weight: spec.weight, Direction: direction, Population: len(values), ReferencePopulation: len(referenceValues)})
	}
	return normalized, diagnostics, currentValues
}

func empiricalPercentile(sortedValues []float64, value float64, higher bool) float64 {
	if len(sortedValues) <= 1 {
		return 50
	}
	left := sort.SearchFloat64s(sortedValues, value)
	right := sort.Search(len(sortedValues), func(i int) bool { return sortedValues[i] > value })
	percentile := 100 * (float64(left) + .5*float64(right-left)) / float64(len(sortedValues))
	if !higher {
		percentile = 100 - percentile
	}
	return clamp(percentile)
}

func scoreDimension(modelIndex int, dimension string, metrics []normalizedMetric, omitMetric string, minimumCoverage float64) *ExternalScore {
	sources := map[string]*scoreAccumulator{}
	for _, metric := range metrics {
		if metric.spec.dimension != dimension || metric.spec.name == omitMetric {
			continue
		}
		acc := sources[metric.spec.source]
		if acc == nil {
			acc = &scoreAccumulator{}
			sources[metric.spec.source] = acc
		}
		acc.max += metric.spec.weight
		if percentile, ok := metric.percentile[modelIndex]; ok {
			acc.weighted += percentile * metric.spec.weight
			acc.weight += metric.spec.weight
			acc.signals++
		}
	}
	if len(sources) == 0 {
		return nil
	}
	var sourceScores, sourceLows, sourceHighs []float64
	var sourceNames []string
	coverageSum := 0.0
	signals := 0
	for _, source := range sortedAccumulatorKeys(sources) {
		acc := sources[source]
		if acc.weight == 0 {
			continue
		}
		coverage := acc.weight / acc.max
		if coverage < minimumCoverage {
			continue
		}
		sourceScores = append(sourceScores, (acc.weighted+50*(acc.max-acc.weight))/acc.max)
		sourceLows = append(sourceLows, acc.weighted/acc.max)
		sourceHighs = append(sourceHighs, (acc.weighted+100*(acc.max-acc.weight))/acc.max)
		sourceNames = append(sourceNames, source)
		coverageSum += coverage
		signals += acc.signals
	}
	if len(sourceScores) == 0 {
		return nil
	}
	sort.Strings(sourceNames)
	raw := mean(sourceScores)
	coverage := coverageSum / float64(len(sourceScores))
	if coverage < minimumCoverage {
		return nil
	}
	var disagreement *float64
	if len(sourceScores) > 1 {
		value := maxValue(sourceScores) - minValue(sourceScores)
		disagreement = &value
	}
	return &ExternalScore{Score: clamp(raw), RawScore: raw, Low: mean(sourceLows), High: mean(sourceHighs), Coverage: coverage, Confidence: scoreConfidence(coverage, disagreement, len(sourceNames), len(sources)), Signals: signals, Sources: sourceNames, Disagreement: disagreement}
}

func scoreComposite(scores map[string]*ExternalScore, weights map[string]float64, omit string, minimumCoverage float64) *ExternalScore {
	weighted, lowWeighted, highWeighted, totalWeight := 0.0, 0.0, 0.0, 0.0
	coverageWeighted := 0.0
	signals := 0
	sourceSet := map[string]bool{}
	disagreementWeighted, disagreementWeight := 0.0, 0.0
	for _, name := range sortedWeightKeys(weights) {
		weight := weights[name]
		if name == omit {
			continue
		}
		totalWeight += weight
		score := scores[name]
		if score == nil {
			weighted += 50 * weight
			highWeighted += 100 * weight
			continue
		}
		weighted += score.Score * weight
		lowWeighted += score.Low * weight
		highWeighted += score.High * weight
		coverageWeighted += score.Coverage * weight
		signals += score.Signals
		for _, source := range score.Sources {
			sourceSet[source] = true
		}
		if score.Disagreement != nil {
			disagreementWeighted += *score.Disagreement * weight
			disagreementWeight += weight
		}
	}
	if totalWeight == 0 {
		return nil
	}
	raw := weighted / totalWeight
	coverage := coverageWeighted / totalWeight
	if coverage < minimumCoverage {
		return nil
	}
	var disagreement *float64
	if disagreementWeight > 0 {
		value := disagreementWeighted / disagreementWeight
		disagreement = &value
	}
	sources := make([]string, 0, len(sourceSet))
	for source := range sourceSet {
		sources = append(sources, source)
	}
	sort.Strings(sources)
	return &ExternalScore{Score: clamp(raw), RawScore: raw, Low: lowWeighted / totalWeight, High: highWeighted / totalWeight, Coverage: coverage, Confidence: scoreConfidence(coverage, disagreement, len(sources), 2), Signals: signals, Sources: sources, Disagreement: disagreement}
}

func sortedAccumulatorKeys(values map[string]*scoreAccumulator) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
