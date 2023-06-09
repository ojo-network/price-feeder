package oracle

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

// ConvertRatesToUSD converts the rates to USD and updates the currency pair
// with a USD quote. If no conversion exists the rate is omitted in the return.
func ConvertRatesToUSD(rates types.CurrencyPairDec) types.CurrencyPairDec {
	convertedRates := make(types.CurrencyPairDec)
	for cp, rate := range rates {
		if cp.Quote == config.DenomUSD {
			convertedRates[cp] = rate
			continue
		}

		for cpConvert, rateConvert := range rates {
			if cpConvert.Quote == config.DenomUSD && cpConvert.Base == cp.Quote {
				convertedPair := types.CurrencyPair{Base: cp.Base, Quote: config.DenomUSD}
				convertedRates[convertedPair] = rate.Mul(rateConvert)
			}
		}
	}

	return convertedRates
}

// CalcCurrencyPairRates filters the candles and tickers to the currency pair
// list provided, then filters candles/tickers outside of the deviation threshold,
// and finally computes the rates for the given currency pairs using TVWAP for candles
// and VWAP for tickers. It will first compute rates with candles and then attempt
// to fill in any missing prices with ticker data.
func CalcCurrencyPairRates(
	candles types.AggregatedProviderCandles,
	tickers types.AggregatedProviderPrices,
	deviationThresholds map[string]sdk.Dec,
	currencyPairs []types.CurrencyPair,
	logger zerolog.Logger,
) (types.CurrencyPairDec, error) {

	candlesFilteredByCP := make(types.AggregatedProviderCandles)
	for _, ratePair := range currencyPairs {
		for provider, cpCandles := range candles {
			for cp, candles := range cpCandles {
				if cp == ratePair {
					if _, ok := candlesFilteredByCP[provider]; !ok {
						candlesFilteredByCP[provider] = make(types.CurrencyPairCandles)
					}
					candlesFilteredByCP[provider][cp] = candles
				}
			}
		}
	}

	candlesFilteredByDeviation, err := FilterCandleDeviations(
		logger,
		candlesFilteredByCP,
		deviationThresholds,
	)
	if err != nil {
		return nil, err
	}

	conversionRates, err := ComputeTVWAP(candlesFilteredByDeviation)
	if err != nil {
		return nil, err
	}

	// select tickers that matche the currencyPairs and also do
	// not already exist in the conversionRates array
	tickersFilteredByCP := make(types.AggregatedProviderPrices)
	for _, ratePair := range currencyPairs {
		if _, ok := conversionRates[ratePair]; ok {
			continue
		}
		for provider, cpTickers := range tickers {
			for cp, tickers := range cpTickers {
				if cp == ratePair {
					if _, ok := tickersFilteredByCP[provider]; !ok {
						tickersFilteredByCP[provider] = make(types.CurrencyPairTickers)
					}
					tickersFilteredByCP[provider][cp] = tickers
				}
			}
		}
	}

	tickersFilteredByDeviation, err := FilterTickerDeviations(
		logger,
		tickersFilteredByCP,
		deviationThresholds,
	)
	if err != nil {
		return nil, err
	}

	vwap := ComputeVWAP(tickersFilteredByDeviation)

	for cp, rate := range vwap {
		conversionRates[cp] = rate
	}

	return conversionRates, nil
}

// ConvertAggregatedCandles converts the candles to USD and updates the currency pair
// with a USD quote. If no conversion exists the rate is omitted in the return.
func ConvertAggregatedCandles(
	candles types.AggregatedProviderCandles,
	rates types.CurrencyPairDec,
) types.AggregatedProviderCandles {
	convertedCandles := make(types.AggregatedProviderCandles)

	for provider, cpCandles := range candles {
		for cp, candles := range cpCandles {

			if cp.Quote == config.DenomUSD {
				if _, ok := convertedCandles[provider]; !ok {
					convertedCandles[provider] = make(types.CurrencyPairCandles)
				}
				convertedCandles[provider][cp] = candles
				continue
			}

			for rateCP, rate := range rates {
				if cp.Quote == rateCP.Base {
					if _, ok := convertedCandles[provider]; !ok {
						convertedCandles[provider] = make(types.CurrencyPairCandles)
					}
					newCP := types.CurrencyPair{Base: cp.Base, Quote: config.DenomUSD}
					convertedCandles[provider][newCP] = convertCandles(candles, rate)
				}
			}
		}
	}
	return convertedCandles
}

func convertCandles(candles []types.CandlePrice, rate sdk.Dec) []types.CandlePrice {
	convertedCandles := []types.CandlePrice{}
	for _, candle := range candles {
		candle.Price = candle.Price.Mul(rate)
		convertedCandles = append(convertedCandles, candle)
	}
	return convertedCandles
}

// ConvertAggregatedTickers converts the tickers to USD and updates the currency pair
// with a USD quote. If no conversion exists the rate is omitted in the return.
func ConvertAggregatedTickers(
	tickers types.AggregatedProviderPrices,
	rates types.CurrencyPairDec,
) types.AggregatedProviderPrices {
	convertedTickers := make(types.AggregatedProviderPrices)

	for provider, cpTickers := range tickers {
		for cp, ticker := range cpTickers {

			if cp.Quote == config.DenomUSD {
				if _, ok := convertedTickers[provider]; !ok {
					convertedTickers[provider] = make(types.CurrencyPairTickers)
				}
				convertedTickers[provider][cp] = ticker
				continue
			}

			for rateCP, rate := range rates {
				if cp.Quote == rateCP.Base {
					if _, ok := convertedTickers[provider]; !ok {
						convertedTickers[provider] = make(types.CurrencyPairTickers)
					}
					newCP := types.CurrencyPair{Base: cp.Base, Quote: config.DenomUSD}
					convertedTickers[provider][newCP] = convertTicker(ticker, rate)
				}
			}
		}
	}
	return convertedTickers
}

func convertTicker(ticker types.TickerPrice, rate sdk.Dec) types.TickerPrice {
	ticker.Price = ticker.Price.Mul(rate)
	return ticker
}
