package integration

import "math"

func calculateMean(numbers []float64) float64 {
	sum := 0.0
	for _, num := range numbers {
		sum += num
	}
	return sum / float64(len(numbers))
}

//lint:ignore U1000 helper function for integration tests
func calculateStandardDeviation(numbers []float64) float64 {
	mean := calculateMean(numbers)
	variance := 0.0
	for _, num := range numbers {
		diff := num - mean
		variance += diff * diff
	}
	variance /= float64(len(numbers))
	return math.Sqrt(variance)
}
