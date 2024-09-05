package oracle

import (
	"fmt"
	"sort"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"

	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
)

var (
<<<<<<< HEAD
	minimumTimeWeight   = sdk.MustNewDecFromStr("0.2000")
	minimumCandleVolume = sdk.MustNewDecFromStr("0.0001")
=======
	minimumTimeWeight   = math.LegacyMustNewDecFromStr("0.2000")
	minimumTickerVolume = math.LegacyMustNewDecFromStr("0.000000000000001")
	minimumCandleVolume = math.LegacyMustNewDecFromStr("0.0001")
>>>>>>> 8876d36 (feat: Add WSTETH/WETH as supported conversion (#410))
)

const (
	// tvwapCandlePeriod represents the time period we use for tvwap in minutes
	tvwapCandlePeriod = 10 * time.Minute
)

// compute VWAP for each base by dividing the Σ {P * V} by Σ {V}
func vwap(weightedPrices, volumeSum types.CurrencyPairDec) types.CurrencyPairDec {
	vwap := make(types.CurrencyPairDec)

	for base, p := range weightedPrices {
		if !volumeSum[base].Equal(sdk.ZeroDec()) {
			if _, ok := vwap[base]; !ok {
				vwap[base] = sdk.ZeroDec()
			}

			vwap[base] = p.Quo(volumeSum[base])
		}
	}

	return vwap
}

// ComputeVWAP computes the volume weighted average price for all price points
// for each ticker/exchange pair. The provided prices argument reflects a mapping
// of provider => {<base> => <TickerPrice>, ...}.
//
// Ref: https://en.wikipedia.org/wiki/Volume-weighted_average_price
func ComputeVWAP(prices types.AggregatedProviderPrices) types.CurrencyPairDec {
	var (
		weightedPrices = make(types.CurrencyPairDec)
		volumeSum      = make(types.CurrencyPairDec)
	)

	for _, providerPrices := range prices {
		for base, tp := range providerPrices {
			if _, ok := weightedPrices[base]; !ok {
				weightedPrices[base] = sdk.ZeroDec()
			}
			if _, ok := volumeSum[base]; !ok {
				volumeSum[base] = sdk.ZeroDec()
			}
			if tp.Volume.LT(minimumTickerVolume) {
				tp.Volume = minimumTickerVolume
			}

			// weightedPrices[base] = Σ {P * V} for all TickerPrice
			weightedPrices[base] = weightedPrices[base].Add(tp.Price.Mul(tp.Volume))

			// track total volume for each base
			volumeSum[base] = volumeSum[base].Add(tp.Volume)
		}
	}

	return vwap(weightedPrices, volumeSum)
}

// ComputeTVWAP computes the time volume weighted average price for all points
// for each exchange pair. Filters out any candles that did not occur within
// timePeriod. The provided prices argument reflects a mapping of
// provider => {<base> => <TickerPrice>, ...}.
//
// Ref : https://en.wikipedia.org/wiki/Time-weighted_average_price
func ComputeTVWAP(prices types.AggregatedProviderCandles) (types.CurrencyPairDec, error) {
	var (
		weightedPrices = make(types.CurrencyPairDec)
		volumeSum      = make(types.CurrencyPairDec)
		now            = provider.PastUnixTime(0)
		timePeriod     = provider.PastUnixTime(tvwapCandlePeriod)
	)

	for _, providerPrices := range prices {
		for base := range providerPrices {
			cp := providerPrices[base]
			if len(cp) == 0 {
				continue
			}

			if _, ok := weightedPrices[base]; !ok {
				weightedPrices[base] = sdk.ZeroDec()
			}
			if _, ok := volumeSum[base]; !ok {
				volumeSum[base] = sdk.ZeroDec()
			}

			// Sort by timestamp old -> new
			sort.SliceStable(cp, func(i, j int) bool {
				return cp[i].TimeStamp < cp[j].TimeStamp
			})

			period := sdk.NewDec(now - cp[0].TimeStamp)
			if period.Equal(sdk.ZeroDec()) {
				return nil, fmt.Errorf("unable to divide by zero")
			}
			// weightUnit = (1 - minimumTimeWeight) / period
			weightUnit := sdk.OneDec().Sub(minimumTimeWeight).Quo(period)

			// get weighted prices, and sum of volumes
			for _, candle := range cp {
				// we only want candles within the last timePeriod
				if timePeriod < candle.TimeStamp && candle.TimeStamp <= now {
					// timeDiff = now - candle.TimeStamp
					timeDiff := sdk.NewDec(now - candle.TimeStamp)
					// set minimum candle volume for low-trading assets
					if candle.Volume.Equal(sdk.ZeroDec()) {
						candle.Volume = minimumCandleVolume
					}

					// volume = candle.Volume * (weightUnit * (period - timeDiff) + minimumTimeWeight)
					volume := candle.Volume.Mul(
						weightUnit.Mul(period.Sub(timeDiff).Add(minimumTimeWeight)),
					)
					volumeSum[base] = volumeSum[base].Add(volume)
					weightedPrices[base] = weightedPrices[base].Add(candle.Price.Mul(volume))
				}
			}

		}
	}

	return vwap(weightedPrices, volumeSum), nil
}

// StandardDeviation returns maps of the standard deviations and means of assets.
// Will skip calculating for an asset if there are less than 3 prices.
func StandardDeviation(
	prices types.CurrencyPairDecByProvider,
) (types.CurrencyPairDec, types.CurrencyPairDec, error) {
	var (
		deviations = make(types.CurrencyPairDec)
		means      = make(types.CurrencyPairDec)
		priceSlice = make(map[types.CurrencyPair][]sdk.Dec)
		priceSums  = make(types.CurrencyPairDec)
	)

	for _, providerPrices := range prices {
		for base, p := range providerPrices {
			if _, ok := priceSums[base]; !ok {
				priceSums[base] = sdk.ZeroDec()
			}
			if _, ok := priceSlice[base]; !ok {
				priceSlice[base] = []sdk.Dec{}
			}

			priceSums[base] = priceSums[base].Add(p)
			priceSlice[base] = append(priceSlice[base], p)
		}
	}

	for base, sum := range priceSums {
		// Skip if standard deviation would not be meaningful
		if len(priceSlice[base]) < 3 {
			continue
		}
		if _, ok := deviations[base]; !ok {
			deviations[base] = sdk.ZeroDec()
		}
		if _, ok := means[base]; !ok {
			means[base] = sdk.ZeroDec()
		}

		numPrices := int64(len(priceSlice[base]))
		means[base] = sum.QuoInt64(numPrices)
		varianceSum := sdk.ZeroDec()

		for _, price := range priceSlice[base] {
			deviation := price.Sub(means[base])
			varianceSum = varianceSum.Add(deviation.Mul(deviation))
		}

		variance := varianceSum.QuoInt64(numPrices)

		standardDeviation, err := variance.ApproxSqrt()
		if err != nil {
			return make(types.CurrencyPairDec), make(types.CurrencyPairDec), err
		}

		deviations[base] = standardDeviation
	}

	return deviations, means, nil
}

// ComputeTvwapsByProvider computes the tvwap prices from candles for each provider separately and returns them
// in a map separated by provider name
func ComputeTvwapsByProvider(prices types.AggregatedProviderCandles) (types.CurrencyPairDecByProvider, error) {
	tvwaps := make(types.CurrencyPairDecByProvider)
	var err error

	for providerName, candles := range prices {
		singleProviderCandles := types.AggregatedProviderCandles{"providerName": candles}
		tvwaps[providerName], err = ComputeTVWAP(singleProviderCandles)
		if err != nil {
			return nil, err
		}
	}
	return tvwaps, nil
}

// ComputeVwapsByProvider computes the vwap prices from tickers for each provider separately and returns them
// in a map separated by provider name
func ComputeVwapsByProvider(prices types.AggregatedProviderPrices) types.CurrencyPairDecByProvider {
	vwaps := make(types.CurrencyPairDecByProvider)

	for providerName, tickers := range prices {
		singleProviderCandles := types.AggregatedProviderPrices{"providerName": tickers}
		vwaps[providerName] = ComputeVWAP(singleProviderCandles)
	}
	return vwaps
}

// createPairProvidersFromCurrencyPairProvidersList will create the pair providers
// map used by the price feeder Oracle from a CurrencyPairProvidersList defined by
// Ojo's oracle module.
func createPairProvidersFromCurrencyPairProvidersList(
	currencyPairs oracletypes.CurrencyPairProvidersList,
) map[types.ProviderName][]types.CurrencyPair {
	providerPairs := make(map[types.ProviderName][]types.CurrencyPair)

	for _, pair := range currencyPairs {
		for _, provider := range pair.Providers {
			if len(pair.PairAddress) > 0 {
				for _, uniPair := range pair.PairAddress {
					if (uniPair.AddressProvider == provider) && (uniPair.Address != "") {
						providerPairs[types.ProviderName(uniPair.AddressProvider)] = append(
							providerPairs[types.ProviderName(uniPair.AddressProvider)],
							types.CurrencyPair{
								Base:    pair.BaseDenom,
								Quote:   pair.QuoteDenom,
								Address: uniPair.Address,
							},
						)
					}
				}
			} else {
				providerPairs[types.ProviderName(provider)] = append(
					providerPairs[types.ProviderName(provider)],
					types.CurrencyPair{
						Base:  pair.BaseDenom,
						Quote: pair.QuoteDenom,
					},
				)
			}
		}
	}

	return providerPairs
}

// createDeviationsFromCurrencyDeviationThresholdList will create the deviations
// map used by the price feeder Oracle from a CurrencyDeviationThresholdList defined by
// Ojo's oracle module.
func createDeviationsFromCurrencyDeviationThresholdList(
	deviationList oracletypes.CurrencyDeviationThresholdList,
) (map[string]sdk.Dec, error) {
	deviations := make(map[string]sdk.Dec, len(deviationList))

	for _, deviation := range deviationList {
		threshold, err := sdk.NewDecFromStr(deviation.Threshold)
		if err != nil {
			return nil, err
		}
		deviations[deviation.BaseDenom] = threshold
	}

	return deviations, nil
}
