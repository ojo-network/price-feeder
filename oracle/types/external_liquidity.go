package types

import (
	"fmt"

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
	baseAsset, quoteAsset, baseAmount, quoteAmount, baseDepth, quoteDepth string,
) (ExternalLiquidity, error) {
	baseAmountInt, err := math.LegacyNewDecFromStr(baseAmount)
	if err != nil {
		return ExternalLiquidity{}, fmt.Errorf("failed to parse base amount (%s): %w", baseAmount, err)
	}

	quoteAmountInt, err := math.LegacyNewDecFromStr(quoteAmount)
	if err != nil {
		return ExternalLiquidity{}, fmt.Errorf("failed to parse quote amount (%s): %w", quoteAmount, err)
	}

	baseDepthDec, err := math.LegacyNewDecFromStr(baseDepth)
	if err != nil {
		return ExternalLiquidity{}, fmt.Errorf("failed to parse base depth (%s): %w", baseDepth, err)
	}

	quoteDepthDec, err := math.LegacyNewDecFromStr(quoteDepth)
	if err != nil {
		return ExternalLiquidity{}, fmt.Errorf("failed to parse quote depth (%s): %w", quoteDepth, err)
	}

	return ExternalLiquidity{
		PoolID:      poolID,
		BaseAsset:   baseAsset,
		QuoteAsset:  quoteAsset,
		BaseAmount:  baseAmountInt,
		QuoteAmount: quoteAmountInt,
		BaseDepth:   baseDepthDec,
		QuoteDepth:  quoteDepthDec,
	}, nil
}
