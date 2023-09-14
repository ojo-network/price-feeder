package oracle

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"

	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	// paramsCacheInterval represents the amount of blocks
	// during which we will cache the oracle params.
	paramsCacheInterval = int64(200)
)

// ParamCache is used to cache oracle param data for
// an amount of blocks, defined by paramsCacheInterval.
type ParamCache struct {
	params           *oracletypes.Params
	lastUpdatedBlock int64
}

// Update retrieves the most recent oracle params and
// updates the instance.
func (paramCache *ParamCache) Update(currentBlockHeigh int64, params oracletypes.Params) {
	paramCache.lastUpdatedBlock = currentBlockHeigh
	paramCache.params = &params
}

// IsOutdated checks whether or not the current
// param data was fetched in the last 200 blocks.
func (paramCache *ParamCache) IsOutdated(currentBlockHeigh int64) bool {
	if paramCache.params == nil {
		return true
	}

	if currentBlockHeigh < paramsCacheInterval {
		return false
	}

	// This is an edge case, which should never happen.
	// The current blockchain height is lower
	// than the last updated block, to fix we should
	// just update the cached params again.
	if currentBlockHeigh < paramCache.lastUpdatedBlock {
		return true
	}

	return (currentBlockHeigh - paramCache.lastUpdatedBlock) > paramsCacheInterval
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
