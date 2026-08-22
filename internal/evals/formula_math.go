package evals

import "math"

func scoreConfidence(coverage float64, disagreement *float64, sourceCount, expectedSources int) string {
	if coverage >= .80 && sourceCount >= expectedSources && (disagreement == nil || *disagreement <= 10) {
		return "high"
	}
	if coverage >= .60 && (disagreement == nil || *disagreement <= 20) {
		return "medium"
	}
	return "low"
}

func mean(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func minValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		result = math.Min(result, value)
	}
	return result
}

func maxValue(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	result := values[0]
	for _, value := range values[1:] {
		result = math.Max(result, value)
	}
	return result
}

func clamp(value float64) float64 { return math.Max(0, math.Min(100, value)) }

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
