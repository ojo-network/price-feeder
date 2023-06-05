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

var (
	atomPrice  = sdk.MustNewDecFromStr("29.93")
	atomVolume = sdk.MustNewDecFromStr("894123.00")
	usdtPrice  = sdk.MustNewDecFromStr("0.98")
	usdtVolume = sdk.MustNewDecFromStr("894123.00")

	atomPair = types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USDT",
	}
	usdtPair = types.CurrencyPair{
		Base:  "USDT",
		Quote: "USD",
	}
)

func TestGetUSDBasedProviders(t *testing.T) {
	providerPairs := make(map[types.ProviderName][]types.CurrencyPair, 3)
	providerPairs[provider.ProviderCoinbase] = []types.CurrencyPair{
		{
			Base:  "FOO",
			Quote: "USD",
		},
	}
	providerPairs[provider.ProviderHuobi] = []types.CurrencyPair{
		{
			Base:  "FOO",
			Quote: "USD",
		},
	}
	providerPairs[provider.ProviderKraken] = []types.CurrencyPair{
		{
			Base:  "FOO",
			Quote: "USDT",
		},
	}
	providerPairs[provider.ProviderBinance] = []types.CurrencyPair{
		{
			Base:  "USDT",
			Quote: "USD",
		},
	}

	pairs, err := getUSDBasedProviders("FOO", providerPairs)
	require.NoError(t, err)
	expectedPairs := map[types.ProviderName]struct{}{
		provider.ProviderCoinbase: {},
		provider.ProviderHuobi:    {},
	}
	require.Equal(t, pairs, expectedPairs)

	pairs, err = getUSDBasedProviders("USDT", providerPairs)
	require.NoError(t, err)
	expectedPairs = map[types.ProviderName]struct{}{
		provider.ProviderBinance: {},
	}
	require.Equal(t, pairs, expectedPairs)

	_, err = getUSDBasedProviders("BAR", providerPairs)
	require.Error(t, err)
}

func TestConvertCandlesToUSD(t *testing.T) {
	providerCandles := make(types.AggregatedProviderCandles, 2)

	binanceCandles := types.CurrencyPairCandles{
		atomPair: {{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderBinance] = binanceCandles

	krakenCandles := types.CurrencyPairCandles{
		usdtPair: {{
			Price:     usdtPrice,
			Volume:    usdtVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderKraken] = krakenCandles

	providerPairs := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {atomPair},
		provider.ProviderKraken:  {usdtPair},
	}

	convertedCandles := ConvertCandlesToUSD(
		zerolog.Nop(),
		providerCandles,
		providerPairs,
		make(map[string]sdk.Dec),
	)

	convertedPair := types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	require.Equal(
		t,
		atomPrice.Mul(usdtPrice),
		convertedCandles[provider.ProviderBinance][convertedPair][0].Price,
	)
}

func TestConvertCandlesToUSDFiltering(t *testing.T) {
	providerCandles := make(types.AggregatedProviderCandles, 2)

	binanceCandles := types.CurrencyPairCandles{
		atomPair: {{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderBinance] = binanceCandles

	krakenCandles := types.CurrencyPairCandles{
		usdtPair: {{
			Price:     usdtPrice,
			Volume:    usdtVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderKraken] = krakenCandles

	gateCandles := types.CurrencyPairCandles{
		usdtPair: {{
			Price:     usdtPrice,
			Volume:    usdtVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderGate] = gateCandles

	okxCandles := types.CurrencyPairCandles{
		usdtPair: {{
			Price:     sdk.MustNewDecFromStr("100.0"),
			Volume:    usdtVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderOkx] = okxCandles

	providerPairs := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {atomPair},
		provider.ProviderKraken:  {usdtPair},
		provider.ProviderGate:    {usdtPair},
		provider.ProviderOkx:     {usdtPair},
	}

	convertedCandles := ConvertCandlesToUSD(
		zerolog.Nop(),
		providerCandles,
		providerPairs,
		make(map[string]sdk.Dec),
	)

	convertedPair := types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	require.Equal(
		t,
		atomPrice.Mul(usdtPrice),
		convertedCandles[provider.ProviderBinance][convertedPair][0].Price,
	)
}

func TestConvertCandlesToUSDNotFound(t *testing.T) {
	providerCandles := make(types.AggregatedProviderCandles, 2)

	binanceCandles := types.CurrencyPairCandles{
		atomPair: {{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		}},
	}
	providerCandles[provider.ProviderBinance] = binanceCandles

	providerPairs := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {atomPair},
		provider.ProviderKraken:  {usdtPair},
		provider.ProviderGate:    {usdtPair},
		provider.ProviderOkx:     {usdtPair},
	}

	convertedCandles := ConvertCandlesToUSD(
		zerolog.Nop(),
		providerCandles,
		providerPairs,
		make(map[string]sdk.Dec),
	)

	require.Empty(
		t,
		convertedCandles[provider.ProviderBinance][atomPair],
	)
}

func TestConvertTickersToUSD(t *testing.T) {
	providerPrices := make(types.AggregatedProviderPrices, 2)

	binanceTickers := types.CurrencyPairTickers{
		atomPair: {
			Price:  atomPrice,
			Volume: atomVolume,
		},
	}
	providerPrices[provider.ProviderBinance] = binanceTickers

	krakenTicker := types.CurrencyPairTickers{
		usdtPair: {
			Price:  usdtPrice,
			Volume: usdtVolume,
		},
	}
	providerPrices[provider.ProviderKraken] = krakenTicker

	providerPairs := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {atomPair},
		provider.ProviderKraken:  {usdtPair},
	}

	convertedTickers := ConvertTickersToUSD(
		zerolog.Nop(),
		providerPrices,
		providerPairs,
		make(map[string]sdk.Dec),
	)

	convertedPair := types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	require.Equal(
		t,
		atomPrice.Mul(usdtPrice),
		convertedTickers[provider.ProviderBinance][convertedPair].Price,
	)
}

func TestConvertTickersToUSDFiltering(t *testing.T) {
	providerPrices := make(types.AggregatedProviderPrices, 2)

	binanceTickers := types.CurrencyPairTickers{
		atomPair: {
			Price:  atomPrice,
			Volume: atomVolume,
		},
	}
	providerPrices[provider.ProviderBinance] = binanceTickers

	krakenTicker := types.CurrencyPairTickers{
		usdtPair: {
			Price:  usdtPrice,
			Volume: usdtVolume,
		},
	}
	providerPrices[provider.ProviderKraken] = krakenTicker

	gateTicker := types.CurrencyPairTickers{
		usdtPair: krakenTicker[usdtPair],
	}
	providerPrices[provider.ProviderGate] = gateTicker

	huobiTicker := types.CurrencyPairTickers{
		usdtPair: {
			Price:  sdk.MustNewDecFromStr("10000"),
			Volume: usdtVolume,
		},
	}
	providerPrices[provider.ProviderHuobi] = huobiTicker

	providerPairs := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {atomPair},
		provider.ProviderKraken:  {usdtPair},
		provider.ProviderGate:    {usdtPair},
		provider.ProviderHuobi:   {usdtPair},
	}

	covertedDeviation := ConvertTickersToUSD(
		zerolog.Nop(),
		providerPrices,
		providerPairs,
		make(map[string]sdk.Dec),
	)

	convertedPair := types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	require.Equal(
		t,
		atomPrice.Mul(usdtPrice),
		covertedDeviation[provider.ProviderBinance][convertedPair].Price,
	)
}
