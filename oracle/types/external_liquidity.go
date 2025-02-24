package types

import (
	"cosmossdk.io/math"
)

// ExternalLiquidity defines price, volume, and time information for an exchange rate.
type ExternalLiquidity struct {
	PoolID      uint64
	FeederAddr  string
	BaseAsset   string
	QuoteAsset  string
	BaseAmount  math.LegacyDec
	QuoteAmount math.LegacyDec
	BaseDepth   math.LegacyDec
	QuoteDepth  math.LegacyDec
}

// NewExternalLiquidity parses a new external liquidity object from string
func NewExternalLiquidity(
	poolID uint64,
	baseAsset, quoteAsset string, baseAmount, quoteAmount, baseDepth, quoteDepth math.LegacyDec,
) (ExternalLiquidity, error) {
	return ExternalLiquidity{
		PoolID:      poolID,
		BaseAsset:   baseAsset,
		QuoteAsset:  quoteAsset,
		BaseAmount:  baseAmount,
		QuoteAmount: quoteAmount,
		BaseDepth:   baseDepth,
		QuoteDepth:  quoteDepth,
	}, nil
}
