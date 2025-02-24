package entity

import (
	"cosmossdk.io/math"
)

type DepthData struct {
	Asks [][2]float64
	Bids [][2]float64
}

type ExternalLiquidity struct {
	BaseAmount  math.LegacyDec
	QuoteAmount math.LegacyDec
	BaseDepth   math.LegacyDec
	QuoteDepth  math.LegacyDec
}
