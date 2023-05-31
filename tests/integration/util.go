package integration

import (
	"math"
	"os"

	"github.com/rs/zerolog"
)

func calcMean(numbers []float64) float64 {
	sum := 0.0
	for _, num := range numbers {
		sum += num
	}
	return sum / float64(len(numbers))
}

func calcStandardDeviation(numbers []float64) float64 {
	mean := calcMean(numbers)
	variance := 0.0
	for _, num := range numbers {
		diff := num - mean
		variance += diff * diff
	}
	variance /= float64(len(numbers))
	return math.Sqrt(variance)
}

func calcCoeficientOfVariation(numbers []float64) float64 {
	mean := calcMean(numbers)
	stdDev := calcStandardDeviation(numbers)
	return (stdDev / mean) * 100
}

func getLogger() zerolog.Logger {
	logWriter := zerolog.ConsoleWriter{Out: os.Stderr}
	logLvl := zerolog.DebugLevel
	zerolog.SetGlobalLevel(logLvl)
	return zerolog.New(logWriter).Level(logLvl).With().Timestamp().Logger()
}
