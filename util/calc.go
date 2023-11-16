package util

import (
	"math"
)

func CalcMean(numbers []float64) float64 {
	sum := 0.0
	for _, num := range numbers {
		sum += num
	}
	return sum / float64(len(numbers))
}

func CalcStandardDeviation(numbers []float64) float64 {
	mean := CalcMean(numbers)
	variance := 0.0
	for _, num := range numbers {
		diff := num - mean
		variance += diff * diff
	}
	variance /= float64(len(numbers))
	return math.Sqrt(variance)
}

func CalcCoeficientOfVariation(numbers []float64) float64 {
	mean := CalcMean(numbers)
	stdDev := CalcStandardDeviation(numbers)
	return (stdDev / mean) * 100
}
