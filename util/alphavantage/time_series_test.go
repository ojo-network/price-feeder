package alphavantage

import (
	"testing"
)

func BenchmarkParseTimeSeriesData(b *testing.B) {
	buff := NewBuffCloser(sampleTimeSeriesData)
	for i := 0; i < b.N; i++ {
		buff.Restart()
		if _, err := ParseTimeSeriesData(buff); err != nil {
			b.Fatalf("error parsing series: %v", err)
		}
	}
}
