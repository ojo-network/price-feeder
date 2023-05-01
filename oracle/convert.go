package oracle

import (
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

// getUSDBasedProviders retrieves which providers for an asset have a USD-based pair,
// given the asset and the map of providers to currency pairs.
func getUSDBasedProviders(
	asset string,
	providerPairs map[provider.Name][]types.CurrencyPair,
) (map[provider.Name]struct{}, error) {
	conversionProviders := make(map[provider.Name]struct{})

	for provider, pairs := range providerPairs {
		for _, pair := range pairs {
			if strings.ToUpper(pair.Quote) == config.DenomUSD && strings.ToUpper(pair.Base) == asset {
				conversionProviders[provider] = struct{}{}
			}
			// If asset is known to not have a USD price feed, include it if its accepted USD quoted quote is
			// set e.g. BCRE/USDC.
			if _, ok := config.NonUSDQuotedPriceQuotes[asset]; ok {
				if strings.ToUpper(pair.Base) == asset && strings.ToUpper(pair.Quote) == config.NonUSDQuotedPriceQuotes[asset] {
					conversionProviders[provider] = struct{}{}
				}
			}
		}
	}
	if len(conversionProviders) == 0 {
		return nil, fmt.Errorf("no providers have a usd conversion for %s", asset)
	}

	return conversionProviders, nil
}

// ConvertCandlesToUSD converts any candles which are not quoted in USD
// to USD by other price feeds. Filters out any candles unable to convert
// to USD. It will also filter out any candles not within the deviation threshold set by the config.
// TODO - Refactor with: https://github.com/ojo-network/price-feeder/issues/105
func ConvertCandlesToUSD(
	logger zerolog.Logger,
	candles provider.AggregatedProviderCandles,
	providerPairs map[provider.Name][]types.CurrencyPair,
	deviationThresholds map[string]sdk.Dec,
) provider.AggregatedProviderCandles {
	if len(candles) == 0 {
		return candles
	}

	conversionRates := make(map[string]sdk.Dec)
	requiredConversions := make(map[provider.Name][]types.CurrencyPair)

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
				validCandleList := provider.AggregatedProviderCandles{}
				for providerName, candleSet := range candles {
					if _, ok := validProviders[providerName]; ok {
						for base, candles := range candleSet {
							if base == pair.Quote {
								if _, ok := validCandleList[providerName]; !ok {
									validCandleList[providerName] = make(map[string][]types.CandlePrice)
								}

								// If asset does not have a USD feed, use latest USD quoted quote asset's candle price
								// to convert candle price to USD e.g. BCRE/USDC to BCRE/USD.
								if _, ok := config.NonUSDQuotedPriceQuotes[base]; ok {
									if candles, ok := candleSet[config.NonUSDQuotedPriceQuotes[base]]; !ok || len(candles) == 0 {
										logger.Error().Err(fmt.Errorf(
											"%s candle cannot be converted to USD without a %s/%s feed",
											base,
											base,
											config.NonUSDQuotedPriceQuotes[base],
										))
										continue
									}
									for i := range candles {
										candles[i].Price = candles[i].Price.Mul(candleSet[config.NonUSDQuotedPriceQuotes[base]][0].Price)
									}
								}

								validCandleList[providerName][base] = candles
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

				cvRate, ok := tvwap[pair.Quote]
				if !ok {
					logger.Error().Err(fmt.Errorf("error on computing tvwap for quote: %s, base: %s", pair.Quote, pair.Base))
					continue
				}

				conversionRates[pair.Quote] = cvRate
			}
		}
	}

	// Convert assets to USD and filter out any unable to convert.
	convertedCandles := make(provider.AggregatedProviderCandles)
	for provider, assetMap := range candles {
		convertedCandles[provider] = make(map[string][]types.CandlePrice)
		for asset, assetCandles := range assetMap {
			conversionAttempted := false
			for _, requiredConversion := range requiredConversions[provider] {
				if requiredConversion.Base == asset {
					conversionAttempted = true
					// candles are filtered out when conversion rate is not found
					if conversionRate, ok := conversionRates[requiredConversion.Quote]; ok {
						convertedCandles[provider][asset] = append(
							convertedCandles[provider][asset],
							convertCandles(assetCandles, conversionRate)...,
						)
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
	tickers provider.AggregatedProviderPrices,
	providerPairs map[provider.Name][]types.CurrencyPair,
	deviationThresholds map[string]sdk.Dec,
) provider.AggregatedProviderPrices {
	if len(tickers) == 0 {
		return tickers
	}

	conversionRates := make(map[string]sdk.Dec)
	requiredConversions := make(map[provider.Name][]types.CurrencyPair)

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
				validTickerList := provider.AggregatedProviderPrices{}
				for providerName, tickerSet := range tickers {
					// Find tickers which we can use for conversion, and calculate the vwap
					// to find the conversion rate.
					if _, ok := validProviders[providerName]; ok {
						for base, ticker := range tickerSet {
							if base == pair.Quote {
								if _, ok := validTickerList[providerName]; !ok {
									validTickerList[providerName] = make(map[string]types.TickerPrice)
								}

								// If asset does not have a USD feed, use the USD quoted quote asset's ticker price
								// to convert ticker price to USD e.g. BCRE/USDC to BCRE/USD.
								if _, ok := config.NonUSDQuotedPriceQuotes[base]; ok {
									if _, ok := tickerSet[config.NonUSDQuotedPriceQuotes[base]]; !ok {
										logger.Error().Err(fmt.Errorf(
											"%s ticker cannot be converted to USD without a %s/%s feed",
											base,
											base,
											config.NonUSDQuotedPriceQuotes[base],
										))
										continue
									}
									ticker.Price = ticker.Price.Mul(tickerSet[config.NonUSDQuotedPriceQuotes[base]].Price)
								}

								validTickerList[providerName][base] = ticker
							}
						}
					}
				}

				if len(validTickerList) == 0 {
					logger.Error().Err(fmt.Errorf("there are no valid conversion rates for %s", pair.Quote))
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

				conversionRates[pair.Quote] = vwap[pair.Quote]
			}
		}
	}

	// Convert assets to USD and filter out any unable to convert.
	convertedTickers := make(provider.AggregatedProviderPrices)
	for provider, assetMap := range tickers {
		convertedTickers[provider] = make(map[string]types.TickerPrice)
		for asset, ticker := range assetMap {
			conversionAttempted := false
			for _, requiredConversion := range requiredConversions[provider] {
				if requiredConversion.Base == asset {
					conversionAttempted = true
					// ticker is filtered out when conversion rate is not found
					if conversionRate, ok := conversionRates[requiredConversion.Quote]; ok {
						ticker.Price = ticker.Price.Mul(conversionRate)
						convertedTickers[provider][asset] = ticker
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
