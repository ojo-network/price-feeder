package oracle

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"

	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
)

// defaultDeviationThreshold defines how many 𝜎 a provider can be away
// from the mean without being considered faulty. This can be overridden
// in the config.
var defaultDeviationThreshold = sdk.MustNewDecFromStr("1.0")

// FilterTickerDeviations finds the standard deviations of the prices of
// all assets, and filters out any providers that are not within 2𝜎 of the mean.
func FilterTickerDeviations(
	logger zerolog.Logger,
	prices types.AggregatedProviderPrices,
	deviationThresholds map[string]sdk.Dec,
) (types.AggregatedProviderPrices, error) {
	var (
		filteredPrices = make(types.AggregatedProviderPrices)
		priceMap       = make(types.CurrencyPairDecByProvider)
	)

	for providerName, priceTickers := range prices {
		p, ok := priceMap[providerName]
		if !ok {
			p = map[types.CurrencyPair]sdk.Dec{}
			priceMap[providerName] = p
		}
		for base, tp := range priceTickers {
			p[base] = tp.Price
		}
	}

	deviations, means, err := StandardDeviation(priceMap)
	if err != nil {
		return nil, err
	}

	// We accept any prices that are within (2 * T)𝜎, or for which we couldn't get 𝜎.
	// T is defined as the deviation threshold, either set by the config
	// or defaulted to 1.
	for providerName, priceTickers := range prices {
		for cp, tp := range priceTickers {
			t := defaultDeviationThreshold
			if _, ok := deviationThresholds[cp.Base]; ok {
				t = deviationThresholds[cp.Base]
			}

			if d, ok := deviations[cp]; !ok || isBetween(tp.Price, means[cp], d.Mul(t)) {
				p, ok := filteredPrices[providerName]
				if !ok {
					p = make(types.CurrencyPairTickers)
					filteredPrices[providerName] = p
				}
				p[cp] = tp
			} else {
				provider.TelemetryFailure(providerName, provider.MessageTypeTicker)
				logger.Warn().
					Interface("currency_pair", cp).
					Str("provider", string(providerName)).
					Str("price", tp.Price.String()).
					Msg("provider deviating from other prices")
			}
		}
	}

	return filteredPrices, nil
}

// FilterCandleDeviations finds the standard deviations of the tvwaps of
// all assets, and filters out any providers that are not within 2𝜎 of the mean.
func FilterCandleDeviations(
	logger zerolog.Logger,
	candles types.AggregatedProviderCandles,
	deviationThresholds map[string]sdk.Dec,
) (types.AggregatedProviderCandles, error) {
	var (
		filteredCandles = make(types.AggregatedProviderCandles)
		tvwaps          = make(types.CurrencyPairDecByProvider)
	)

	for providerName, priceCandles := range candles {
		candlePrices := make(types.AggregatedProviderCandles)

		for currencyPair, candlePrice := range priceCandles {
			p, ok := candlePrices[providerName]
			if !ok {
				p = map[types.CurrencyPair][]types.CandlePrice{}
				candlePrices[providerName] = p
			}
			p[currencyPair] = candlePrice
		}

		tvwap, err := ComputeTVWAP(candlePrices)
		if err != nil {
			return nil, err
		}

		for cp, asset := range tvwap {
			if _, ok := tvwaps[providerName]; !ok {
				tvwaps[providerName] = make(types.CurrencyPairDec)
			}

			tvwaps[providerName][cp] = asset
		}
	}

	deviations, means, err := StandardDeviation(tvwaps)
	if err != nil {
		return nil, err
	}

	// We accept any prices that are within (2 * T)𝜎, or for which we couldn't get 𝜎.
	// T is defined as the deviation threshold, either set by the config
	// or defaulted to 1.
	for providerName, priceMap := range tvwaps {
		for cp, price := range priceMap {
			t := defaultDeviationThreshold
			if _, ok := deviationThresholds[cp.Base]; ok {
				t = deviationThresholds[cp.Base]
			}

			if d, ok := deviations[cp]; !ok || isBetween(price, means[cp], d.Mul(t)) {
				p, ok := filteredCandles[providerName]
				if !ok {
					p = make(types.CurrencyPairCandles)
					filteredCandles[providerName] = p
				}
				p[cp] = candles[providerName][cp]
			} else {
				provider.TelemetryFailure(providerName, provider.MessageTypeCandle)
				logger.Warn().
					Interface("currency_pair", cp).
					Str("provider", string(providerName)).
					Str("price", price.String()).
					Msg("provider deviating from other candles")
			}
		}
	}

	return filteredCandles, nil
}

func isBetween(p, mean, margin sdk.Dec) bool {
	return p.GTE(mean.Sub(margin)) &&
		p.LTE(mean.Add(margin))
}
