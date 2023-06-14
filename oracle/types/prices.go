package types

import (
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

type (
	// ProviderName name of an oracle provider. Usually it is an exchange
	// but this can be any provider name that can give token prices
	// examples.: "binance", "osmosis", "kraken".
	ProviderName string

	PricesWithMutex struct {
		prices CurrencyPairDecByProvider
		mx     sync.RWMutex
	}

	// CurrencyPairDec is a map of sdk.Dec by CurrencyPair
	CurrencyPairDec map[CurrencyPair]sdk.Dec

	// CurrencyPairDecByProvider ia a map of CurrencyPairDec by provider name
	CurrencyPairDecByProvider map[ProviderName]CurrencyPairDec

	// CurrencyPairTickers is a map of TickerPrice by CurrencyPair
	CurrencyPairTickers map[CurrencyPair]TickerPrice

	// CurrencyPairTickersByProvider is a map of CandlePrice arrays by CurrencyPair
	CurrencyPairCandles map[CurrencyPair][]CandlePrice

	// AggregatedProviderPrices defines a type alias for a map
	// of provider -> currency pair -> TickerPrice
	AggregatedProviderPrices map[ProviderName]CurrencyPairTickers

	// AggregatedProviderCandles defines a type alias for a map
	// of provider -> currency pair -> []types.CandlePrice
	AggregatedProviderCandles map[ProviderName]CurrencyPairCandles
)

// SetPrices sets the PricesWithMutex.prices value surrounded by a write lock
func (pwm *PricesWithMutex) SetPrices(prices CurrencyPairDecByProvider) {
	pwm.mx.Lock()
	defer pwm.mx.Unlock()

	pwm.prices = prices
}

// GetPricesClone retrieves a clone of PricesWithMutex.prices
// surrounded by a read lock
func (pwm *PricesWithMutex) GetPricesClone() CurrencyPairDecByProvider {
	pwm.mx.RLock()
	defer pwm.mx.RUnlock()
	return pwm.clonePrices()
}

// clonePrices returns a deep copy of PricesWithMutex.prices
func (pwm *PricesWithMutex) clonePrices() CurrencyPairDecByProvider {
	clone := make(CurrencyPairDecByProvider, len(pwm.prices))
	for provider, prices := range pwm.prices {
		pricesClone := make(CurrencyPairDec, len(prices))
		for cp, price := range prices {
			pricesClone[cp] = price
		}
		clone[provider] = pricesClone
	}
	return clone
}

// String cast provider name to string.
func (n ProviderName) String() string {
	return string(n)
}
