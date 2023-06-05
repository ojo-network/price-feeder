package oracle

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

// getUSDBasedProviders retrieves which providers for an asset have a USD-based pair,
// given the asset and the map of providers to currency pairs.
func getUSDBasedProviders(
	asset string,
	providerPairs map[types.ProviderName][]types.CurrencyPair,
) (map[types.ProviderName]struct{}, error) {
	conversionProviders := make(map[types.ProviderName]struct{})

	for provider, pairs := range providerPairs {
		for _, pair := range pairs {
			if strings.ToUpper(pair.Quote) == config.DenomUSD && strings.ToUpper(pair.Base) == asset {
				conversionProviders[provider] = struct{}{}
			}
		}
	}
	if len(conversionProviders) == 0 {
		return nil, fmt.Errorf("no providers have a usd conversion for this asset")
	}

	return conversionProviders, nil
}

// ConvertCandlesToUSD converts any candles which are not quoted in USD
// to USD by other price feeds. Filters out any candles unable to convert
// to USD. It will also filter out any candles not within the deviation threshold set by the config.
// TODO - Refactor with: https://github.com/ojo-network/price-feeder/issues/105
func ConvertCandlesToUSD(
	logger zerolog.Logger,
	candles types.AggregatedProviderCandles,
	providerPairs map[types.ProviderName][]types.CurrencyPair,
	deviationThresholds map[string]sdk.Dec,
) types.AggregatedProviderCandles {
	if len(candles) == 0 {
		return candles
	}

	conversionRates := make(map[string]sdk.Dec)
	requiredConversions := make(map[types.ProviderName][]types.CurrencyPair)

	for pairProviderName, pairs := range providerPairs {
		for _, pair := range pairs {
			if strings.ToUpper(pair.Quote) != config.DenomUSD {
				requiredConversions[pairProviderName] = append(requiredConversions[pairProviderName], pair)

				// Get valid providers and use them to generate a USD-based price for this asset.
				validProviders, err := getUSDBasedProviders(pair.Quote, providerPairs)
				if err != nil {
					logger.Error().Err(err).Msg("error on getting usd based providers")
					continue
				}

				// Find candles which we can use for conversion, and calculate the tvwap
				// to find the conversion rate.
				validCandleList := types.AggregatedProviderCandles{}
				for providerName, candleSet := range candles {
					if _, ok := validProviders[providerName]; ok {
						for cp, candles := range candleSet {
							if cp.Base == pair.Quote {
								if _, ok := validCandleList[providerName]; !ok {
									validCandleList[providerName] = make(map[types.CurrencyPair][]types.CandlePrice)
								}

								validCandleList[providerName][cp] = candles
							}
						}
					}
				}

				if len(validCandleList) == 0 {
					logger.Error().Err(fmt.Errorf("there are no valid conversion rates for %s", pair.Quote))
					continue
				}

				filteredCandles, err := FilterCandleDeviations(
					logger,
					validCandleList,
					deviationThresholds,
				)
				if err != nil {
					logger.Error().Err(err).Msg("error on filtering candle deviations")
					continue
				}

				// TODO: we should revise ComputeTVWAP to avoid return empty slices
				// Ref: https://github.com/ojo-network/ojo/issues/1261
				tvwap, err := ComputeTVWAP(filteredCandles)
				if err != nil {
					logger.Error().Err(fmt.Errorf("error on computing tvwap for quote: %s, base: %s", pair.Quote, pair.Base))
					continue
				}

				conversionPair := types.CurrencyPair{Base: pair.Quote, Quote: config.DenomUSD}
				cvRate, ok := tvwap[conversionPair]
				if !ok {
					logger.Error().Err(fmt.Errorf("error on computing tvwap for quote: %s, base: %s", pair.Quote, pair.Base))
					continue
				}

				conversionRates[pair.Quote] = cvRate
			}
		}
	}

	fmt.Println(conversionRates)

	// Convert assets to USD and filter out any unable to convert.
	convertedCandles := make(types.AggregatedProviderCandles)
	for provider, assetMap := range candles {
		convertedCandles[provider] = make(types.CurrencyPairCandles)
		for asset, assetCandles := range assetMap {
			conversionAttempted := false
			for _, requiredConversion := range requiredConversions[provider] {
				if requiredConversion == asset {
					conversionAttempted = true
					// candles are filtered out when conversion rate is not found
					if conversionRate, ok := conversionRates[asset.Quote]; ok {
						newCurrencyPair := types.CurrencyPair{Base: asset.Base, Quote: "USD"}
						convertedCandles[provider][newCurrencyPair] = append(
							convertedCandles[provider][newCurrencyPair],
							convertCandles(assetCandles, conversionRate)...,
						)
					} else {
						logger.Error().Msgf("no valid conversion rate found for %s", asset)
					}
					break
				}
			}
			if !conversionAttempted {
				convertedCandles[provider][asset] = append(
					convertedCandles[provider][asset],
					assetCandles...,
				)
			}
		}
	}

	return convertedCandles
}

func convertCandles(candles []types.CandlePrice, conversionRate sdk.Dec) (ret []types.CandlePrice) {
	for _, candle := range candles {
		candle.Price = candle.Price.Mul(conversionRate)
		ret = append(ret, candle)
	}
	return
}

// ConvertTickersToUSD converts any tickers which are not quoted in USD to USD,
// using the conversion rates of other tickers. It will also filter out any tickers
// not within the deviation threshold set by the config.
// TODO - Refactor with: https://github.com/ojo-network/price-feeder/issues/105
func ConvertTickersToUSD(
	logger zerolog.Logger,
	tickers types.AggregatedProviderPrices,
	providerPairs map[types.ProviderName][]types.CurrencyPair,
	deviationThresholds map[string]sdk.Dec,
) types.AggregatedProviderPrices {
	if len(tickers) == 0 {
		return tickers
	}

	conversionRates := make(map[string]sdk.Dec)
	requiredConversions := make(map[types.ProviderName][]types.CurrencyPair)

	for pairProviderName, pairs := range providerPairs {
		for _, pair := range pairs {
			if strings.ToUpper(pair.Quote) != config.DenomUSD {
				requiredConversions[pairProviderName] = append(requiredConversions[pairProviderName], pair)

				// Get valid providers and use them to generate a USD-based price for this asset.
				validProviders, err := getUSDBasedProviders(pair.Quote, providerPairs)
				if err != nil {
					logger.Error().Err(err).Msg("error on getting USD based providers")
					continue
				}

				// Find valid candles, and then let's re-compute the tvwap.
				validTickerList := types.AggregatedProviderPrices{}
				for providerName, tickerSet := range tickers {
					// Find tickers which we can use for conversion, and calculate the vwap
					// to find the conversion rate.
					if _, ok := validProviders[providerName]; ok {
						for cp, ticker := range tickerSet {
							if cp.Base == pair.Quote {
								if _, ok := validTickerList[providerName]; !ok {
									validTickerList[providerName] = make(types.CurrencyPairTickers)
								}
								validTickerList[providerName][cp] = ticker
							}
						}
					}
				}

				if len(validTickerList) == 0 {
					logger.Error().Err(fmt.Errorf("there are no valid conversion rates for %s", pair.Base))
					continue
				}

				filteredTickers, err := FilterTickerDeviations(
					logger,
					validTickerList,
					deviationThresholds,
				)
				if err != nil {
					logger.Error().Err(err).Msg("error on filtering candle deviations")
					continue
				}

				vwap := ComputeVWAP(filteredTickers)

				conversionPair := types.CurrencyPair{Base: pair.Quote, Quote: config.DenomUSD}
				conversionRates[pair.Quote] = vwap[conversionPair]
			}
		}
	}

	// Convert assets to USD and filter out any unable to convert.
	convertedTickers := make(types.AggregatedProviderPrices)
	for provider, assetMap := range tickers {
		convertedTickers[provider] = make(types.CurrencyPairTickers)
		for asset, ticker := range assetMap {
			conversionAttempted := false
			for _, requiredConversion := range requiredConversions[provider] {
				if requiredConversion.Base == asset.Base {
					conversionAttempted = true
					// ticker is filtered out when conversion rate is not found
					if conversionRate, ok := conversionRates[asset.Quote]; ok {
						ticker.Price = ticker.Price.Mul(conversionRate)
						newCurrencyPair := types.CurrencyPair{Base: asset.Base, Quote: "USD"}
						convertedTickers[provider][newCurrencyPair] = ticker
					}
					break
				}
			}
			if !conversionAttempted {
				convertedTickers[provider][asset] = ticker
			}
		}
	}

	return convertedTickers
}
