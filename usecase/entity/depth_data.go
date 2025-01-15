package entity

type DepthData struct {
	Asks [][2]float64
	Bids [][2]float64
}

type ExternalLiquidity struct {
	BaseAmount  float64
	QuoteAmount float64
	BaseDepth   float64
	QuoteDepth  float64
}
