package evals

import "math"

func mean(values []float64) float64 {
	total := 0.0
	for _, value := range values {
		total += value
	}
	return total / float64(len(values))
}

func clamp(value float64) float64 { return math.Max(0, math.Min(100, value)) }
