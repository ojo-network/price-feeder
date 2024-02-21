package types

import (
	"fmt"

	"cosmossdk.io/math"
)

// TickerPrice defines price and volume information for a symbol or ticker exchange rate.
type TickerPrice struct {
	Price  math.LegacyDec // last trade price
	Volume math.LegacyDec // 24h volume
}

// NewTickerPrice parses the lastPrice and volume to a decimal and returns a TickerPrice
func NewTickerPrice(lastPrice, volume string) (TickerPrice, error) {
	price, err := math.LegacyNewDecFromStr(lastPrice)
	if err != nil {
		return TickerPrice{}, fmt.Errorf("failed to parse ticker price (%s): %w", lastPrice, err)
	}

	volumeDec, err := math.LegacyNewDecFromStr(volume)
	if err != nil {
		return TickerPrice{}, fmt.Errorf("failed to parse ticker volume (%s): %w", volume, err)
	}

	return TickerPrice{Price: price, Volume: volumeDec}, nil
}
