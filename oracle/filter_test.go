package oracle

import (
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestSuccessFilterCandleDeviations(t *testing.T) {
	providerCandles := make(types.AggregatedProviderCandles, 4)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USDT",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("1994674.34000000")

	atomCandlePrice := []types.CandlePrice{
		{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}

	providerCandles[provider.ProviderBinance] = types.CurrencyPairCandles{
		pair: atomCandlePrice,
	}
	providerCandles[provider.ProviderHuobi] = types.CurrencyPairCandles{
		pair: atomCandlePrice,
	}
	providerCandles[provider.ProviderKraken] = types.CurrencyPairCandles{
		pair: atomCandlePrice,
	}
	providerCandles[provider.ProviderCoinbase] = types.CurrencyPairCandles{
		pair: {
			{
				Price:     sdk.MustNewDecFromStr("27.1"),
				Volume:    atomVolume,
				TimeStamp: provider.PastUnixTime(1 * time.Minute),
			},
		},
	}

	pricesFiltered, err := FilterCandleDeviations(
		zerolog.Nop(),
		providerCandles,
		make(map[string]sdk.Dec),
	)

	_, ok := pricesFiltered[provider.ProviderCoinbase]
	require.NoError(t, err, "It should successfully filter out the provider using candles")
	require.False(t, ok, "The filtered candle deviation price at coinbase should be empty")

	customDeviations := make(map[string]sdk.Dec, 1)
	customDeviations[pair.Base] = sdk.NewDec(2)

	pricesFilteredCustom, err := FilterCandleDeviations(
		zerolog.Nop(),
		providerCandles,
		customDeviations,
	)

	_, ok = pricesFilteredCustom[provider.ProviderCoinbase]
	require.NoError(t, err, "It should successfully not filter out coinbase")
	require.True(t, ok, "The filtered candle deviation price of coinbase should remain")
}

func TestSuccessFilterTickerDeviations(t *testing.T) {
	providerTickers := make(types.AggregatedProviderPrices, 4)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USDT",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("1994674.34000000")

	atomTickerPrice := types.TickerPrice{
		Price:  atomPrice,
		Volume: atomVolume,
	}

	providerTickers[provider.ProviderBinance] = types.CurrencyPairTickers{
		pair: atomTickerPrice,
	}
	providerTickers[provider.ProviderHuobi] = types.CurrencyPairTickers{
		pair: atomTickerPrice,
	}
	providerTickers[provider.ProviderKraken] = types.CurrencyPairTickers{
		pair: atomTickerPrice,
	}
	providerTickers[provider.ProviderCoinbase] = types.CurrencyPairTickers{
		pair: {
			Price:  sdk.MustNewDecFromStr("27.1"),
			Volume: atomVolume,
		},
	}

	pricesFiltered, err := FilterTickerDeviations(
		zerolog.Nop(),
		providerTickers,
		make(map[string]sdk.Dec),
	)

	_, ok := pricesFiltered[provider.ProviderCoinbase]
	require.NoError(t, err, "It should successfully filter out the provider using tickers")
	require.False(t, ok, "The filtered ticker deviation price at coinbase should be empty")

	customDeviations := make(map[string]sdk.Dec, 1)
	customDeviations[pair.Base] = sdk.NewDec(2)

	pricesFilteredCustom, err := FilterTickerDeviations(
		zerolog.Nop(),
		providerTickers,
		customDeviations,
	)

	_, ok = pricesFilteredCustom[provider.ProviderCoinbase]
	require.NoError(t, err, "It should successfully not filter out coinbase")
	require.True(t, ok, "The filtered candle deviation price of coinbase should remain")
}
