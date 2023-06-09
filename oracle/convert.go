package oracle

// Everything assumes that there are only two hops

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

// ConvertRatesToUSD converts the rates to USD. If no conversion exists the
// rate is omitted in the return.
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

// CalcCurrencyPairRates computes the conversion rates for the given currency pairs
// Limited to only the currencyPairs passed in
// If the rate does not exist after computing candles it falls back to tickers
func CalcCurrencyPairRates(
	candles types.AggregatedProviderCandles,
	tickers types.AggregatedProviderPrices,
	deviationThresholds map[string]sdk.Dec,
	currencyPairs []types.CurrencyPair,
	logger zerolog.Logger,
) (types.CurrencyPairDec, error) {

	// Select candles that matches the currencyPairs and fill conversionCandles with them
	conversionCandles := make(types.AggregatedProviderCandles)
	for _, ratePair := range currencyPairs {
		for provider, cpCandles := range candles {
			for cp, candles := range cpCandles {
				if cp == ratePair {
					if _, ok := conversionCandles[provider]; !ok {
						conversionCandles[provider] = make(types.CurrencyPairCandles)
					}
					conversionCandles[provider][cp] = candles
				}
			}
		}
	}

	filteredCandles, err := FilterCandleDeviations(
		logger,
		conversionCandles,
		deviationThresholds,
	)
	if err != nil {
		return nil, err
	}

	conversionRates, err := ComputeTVWAP(filteredCandles)
	if err != nil {
		return nil, err
	}

	// Select tickers that matches the currencyPairs and also does not already exist in the conversionRates
	// array and fill conversionTickers with them
	conversionTickers := make(types.AggregatedProviderPrices)
	for _, ratePair := range currencyPairs {
		if _, ok := conversionRates[ratePair]; ok {
			continue
		}
		for provider, cpTickers := range tickers {
			for cp, tickers := range cpTickers {
				if cp == ratePair {
					if _, ok := conversionTickers[provider]; !ok {
						conversionTickers[provider] = make(types.CurrencyPairTickers)
					}
					conversionTickers[provider][cp] = tickers
				}
			}
		}
	}

	filteredTickers, err := FilterTickerDeviations(
		logger,
		conversionTickers,
		deviationThresholds,
	)
	if err != nil {
		return nil, err
	}

	vwap := ComputeVWAP(filteredTickers)

	for cp, rate := range vwap {
		conversionRates[cp] = rate
	}

	return conversionRates, nil
}

// Assumes all rate pairs have a quote of USD (called after ConvertRatesToUSD)
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
