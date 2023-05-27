package oracle

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/ojo-network/price-feeder/oracle/client"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
)

var (
	OJOUSDC = types.CurrencyPair{Base: "OJO", Quote: "USDC"}
	OJOUSDT = types.CurrencyPair{Base: "OJO", Quote: "USDT"}
	OJOUSDX = types.CurrencyPair{Base: "OJO", Quote: "USDX"}

	OJOUSD  = types.CurrencyPair{Base: "OJO", Quote: "USD"}
	ATOMUSD = types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	OSMOUSD = types.CurrencyPair{Base: "OSMO", Quote: "USD"}

	USDCUSD = types.CurrencyPair{Base: "USDC", Quote: "USD"}
	USDTUSD = types.CurrencyPair{Base: "USDT", Quote: "USD"}

	XBTUSDT = types.CurrencyPair{Base: "XBT", Quote: "USDT"}
	XBTUSD  = types.CurrencyPair{Base: "XBT", Quote: "USD"}

	BTCETH = types.CurrencyPair{Base: "BTC", Quote: "ETH"}
	BTCUSD = types.CurrencyPair{Base: "BTC", Quote: "USD"}
	ETHUSD = types.CurrencyPair{Base: "ETH", Quote: "USD"}
	DAIUSD = types.CurrencyPair{Base: "DAI", Quote: "USD"}
)

type mockProvider struct {
	prices types.CurrencyPairTickers
}

func (m mockProvider) StartConnections() {}

func (m mockProvider) GetTickerPrices(_ ...types.CurrencyPair) (types.CurrencyPairTickers, error) {
	return m.prices, nil
}

func (m mockProvider) GetCandlePrices(_ ...types.CurrencyPair) (types.CurrencyPairCandles, error) {
	candles := make(types.CurrencyPairCandles)
	for pair, price := range m.prices {
		candles[pair] = []types.CandlePrice{
			{
				Price:     price.Price,
				TimeStamp: provider.PastUnixTime(1 * time.Minute),
				Volume:    price.Volume,
			},
		}
	}
	return candles, nil
}

func (m mockProvider) SubscribeCurrencyPairs(...types.CurrencyPair) {}

func (m mockProvider) GetAvailablePairs() (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

type failingProvider struct {
	prices types.CurrencyPairTickers
}

func (m failingProvider) StartConnections() {}

func (m failingProvider) GetTickerPrices(_ ...types.CurrencyPair) (types.CurrencyPairTickers, error) {
	return nil, fmt.Errorf("unable to get ticker prices")
}

func (m failingProvider) GetCandlePrices(_ ...types.CurrencyPair) (types.CurrencyPairCandles, error) {
	return nil, fmt.Errorf("unable to get candle prices")
}

func (m failingProvider) SubscribeCurrencyPairs(...types.CurrencyPair) {}

func (m failingProvider) GetAvailablePairs() (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

type OracleTestSuite struct {
	suite.Suite

	oracle *Oracle
}

// SetupSuite executes once before the suite's tests are executed.
func (ots *OracleTestSuite) SetupSuite() {
	ots.oracle = New(
		zerolog.Nop(),
		client.OracleClient{},
		map[types.ProviderName][]types.CurrencyPair{
			provider.ProviderBinance: {
				{
					Base:  "OJO",
					Quote: "USDT",
				},
			},
			provider.ProviderKraken: {
				{
					Base:  "OJO",
					Quote: "USDC",
				},
			},
			provider.ProviderHuobi: {
				{
					Base:  "USDC",
					Quote: "USD",
				},
			},
			provider.ProviderCoinbase: {
				{
					Base:  "USDT",
					Quote: "USD",
				},
			},
		},
		time.Millisecond*100,
		make(map[string]sdk.Dec),
		make(map[types.ProviderName]provider.Endpoint),
	)
}

func TestServiceTestSuite(t *testing.T) {
	suite.Run(t, new(OracleTestSuite))
}

func (ots *OracleTestSuite) TestStop() {
	ots.Eventually(
		func() bool {
			ots.oracle.Stop()
			return true
		},
		5*time.Second,
		time.Second,
	)
}

func (ots *OracleTestSuite) TestGetLastPriceSyncTimestamp() {
	// when no tick() has been invoked, assume zero value
	ots.Require().Equal(time.Time{}, ots.oracle.GetLastPriceSyncTimestamp())
}

func (ots *OracleTestSuite) TestPrices() {
	// initial prices should be empty (not set)
	ots.Require().Empty(ots.oracle.GetPrices())

	// Use a mock provider with exchange rates that are not specified in
	// configuration.
	ots.oracle.priceProviders = map[types.ProviderName]provider.Provider{
		provider.ProviderBinance: mockProvider{
			prices: types.CurrencyPairTickers{
				types.CurrencyPair{Base: "OJO", Quote: "USDX"}: {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			}, /*  */
		},
		provider.ProviderKraken: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDX: {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	ots.Require().Empty(ots.oracle.GetPrices())

	// use a mock provider without a conversion rate for these stablecoins
	ots.oracle.priceProviders = map[types.ProviderName]provider.Provider{
		provider.ProviderBinance: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDT: {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		provider.ProviderKraken: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDC: {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	prices := ots.oracle.GetPrices()
	ots.Require().Len(prices, 0)

	// use a mock provider to provide prices for the configured exchange pairs
	ots.oracle.priceProviders = map[types.ProviderName]provider.Provider{
		provider.ProviderBinance: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDT: {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		provider.ProviderKraken: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDC: {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		provider.ProviderHuobi: mockProvider{
			prices: types.CurrencyPairTickers{
				USDCUSD: {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		provider.ProviderCoinbase: mockProvider{
			prices: types.CurrencyPairTickers{
				USDTUSD: {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		provider.ProviderOsmosisV2: mockProvider{
			prices: types.CurrencyPairTickers{
				XBTUSDT: {
					Price:  sdk.MustNewDecFromStr("3.717"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))

	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.710916056220858266"), prices[OJOUSDC])
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices[XBTUSDT])
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices[USDCUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices[USDTUSD])

	// use one working provider and one provider with an incorrect exchange rate
	ots.oracle.priceProviders = map[types.ProviderName]provider.Provider{
		provider.ProviderBinance: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDX: {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		provider.ProviderKraken: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDC: {
					Price:  sdk.MustNewDecFromStr("3.70"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		provider.ProviderHuobi: mockProvider{
			prices: types.CurrencyPairTickers{
				USDCUSD: {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		provider.ProviderCoinbase: mockProvider{
			prices: types.CurrencyPairTickers{
				USDTUSD: {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		provider.ProviderOsmosisV2: mockProvider{
			prices: types.CurrencyPairTickers{
				XBTUSDT: {
					Price:  sdk.MustNewDecFromStr("3.717"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.70"), prices[OJOUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices[XBTUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices[USDCUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices[USDTUSD])

	// use one working provider and one provider that fails
	ots.oracle.priceProviders = map[types.ProviderName]provider.Provider{
		provider.ProviderBinance: failingProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDC: {
					Price:  sdk.MustNewDecFromStr("3.72"),
					Volume: sdk.MustNewDecFromStr("2396974.02000000"),
				},
			},
		},
		provider.ProviderKraken: mockProvider{
			prices: types.CurrencyPairTickers{
				OJOUSDC: {
					Price:  sdk.MustNewDecFromStr("3.71"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		provider.ProviderHuobi: mockProvider{
			prices: types.CurrencyPairTickers{
				USDCUSD: {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("2396974.34000000"),
				},
			},
		},
		provider.ProviderCoinbase: mockProvider{
			prices: types.CurrencyPairTickers{
				USDTUSD: {
					Price:  sdk.MustNewDecFromStr("1"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
		provider.ProviderOsmosisV2: mockProvider{
			prices: types.CurrencyPairTickers{
				XBTUSDT: {
					Price:  sdk.MustNewDecFromStr("3.717"),
					Volume: sdk.MustNewDecFromStr("1994674.34000000"),
				},
			},
		},
	}

	ots.Require().NoError(ots.oracle.SetPrices(context.TODO()))
	prices = ots.oracle.GetPrices()
	ots.Require().Len(prices, 4)
	ots.Require().Equal(sdk.MustNewDecFromStr("3.71"), prices[OJOUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("3.717"), prices[XBTUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices[USDCUSD])
	ots.Require().Equal(sdk.MustNewDecFromStr("1"), prices[USDTUSD])
}

func TestGenerateSalt(t *testing.T) {
	salt, err := GenerateSalt(0)
	require.Error(t, err)
	require.Empty(t, salt)

	salt, err = GenerateSalt(32)
	require.NoError(t, err)
	require.NotEmpty(t, salt)
}

func TestGenerateExchangeRatesString(t *testing.T) {
	testCases := map[string]struct {
		input    types.CurrencyPairDec
		expected string
	}{
		"empty input": {
			input:    make(types.CurrencyPairDec),
			expected: "",
		},
		"single denom": {
			input: types.CurrencyPairDec{
				OJOUSD: sdk.MustNewDecFromStr("3.72"),
			},
			expected: "OJO:3.720000000000000000",
		},
		"multi denom": {
			input: types.CurrencyPairDec{
				OJOUSD:  sdk.MustNewDecFromStr("3.72"),
				ATOMUSD: sdk.MustNewDecFromStr("40.13"),
				OSMOUSD: sdk.MustNewDecFromStr("8.69"),
			},
			expected: "ATOMUSD:40.130000000000000000,OJOUSD:3.720000000000000000,OSMOUSD:8.690000000000000000",
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			out := GenerateExchangeRatesString(tc.input)
			require.Equal(t, tc.expected, out)
		})
	}
}

func TestSuccessSetProviderTickerPricesAndCandles(t *testing.T) {
	providerPrices := make(types.AggregatedProviderPrices, 1)
	providerCandles := make(types.AggregatedProviderCandles, 1)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USDT",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("894123.00")

	prices := make(types.CurrencyPairTickers, 1)
	prices[pair] = types.TickerPrice{
		Price:  atomPrice,
		Volume: atomVolume,
	}

	candles := make(types.CurrencyPairCandles, 1)
	candles[pair] = []types.CandlePrice{
		{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}

	success := SetProviderTickerPricesAndCandles(
		provider.ProviderGate,
		providerPrices,
		providerCandles,
		prices,
		candles,
		pair,
	)

	require.True(t, success, "It should successfully set the prices")
	require.Equal(t, atomPrice, providerPrices[provider.ProviderGate][pair].Price)
	require.Equal(t, atomPrice, providerCandles[provider.ProviderGate][pair][0].Price)
}

func TestFailedSetProviderTickerPricesAndCandles(t *testing.T) {
	success := SetProviderTickerPricesAndCandles(
		provider.ProviderCoinbase,
		make(types.AggregatedProviderPrices, 1),
		make(types.AggregatedProviderCandles, 1),
		make(types.CurrencyPairTickers, 1),
		make(types.CurrencyPairCandles, 1),
		types.CurrencyPair{
			Base:  "ATOM",
			Quote: "USDT",
		},
	)

	require.False(t, success, "It should failed to set the prices, prices and candle are empty")
}

func (ots *OracleTestSuite) TestSuccessGetComputedPricesCandles() {
	providerCandles := make(types.AggregatedProviderCandles, 1)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USD",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("894123.00")

	candles := make(types.CurrencyPairCandles, 1)
	candles[pair] = []types.CandlePrice{
		{
			Price:     atomPrice,
			Volume:    atomVolume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[provider.ProviderBinance] = candles

	providerPair := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {pair},
	}

	prices, err := ots.oracle.GetComputedPrices(
		providerCandles,
		make(types.AggregatedProviderPrices, 1),
		providerPair,
		make(map[string]sdk.Dec),
	)

	require.NoError(ots.T(), err, "It should successfully get computed candle prices")
	require.Equal(ots.T(), prices[pair], atomPrice)
}

func (ots *OracleTestSuite) TestSuccessGetComputedPricesTickers() {
	providerPrices := make(types.AggregatedProviderPrices, 1)
	pair := types.CurrencyPair{
		Base:  "ATOM",
		Quote: "USD",
	}

	atomPrice := sdk.MustNewDecFromStr("29.93")
	atomVolume := sdk.MustNewDecFromStr("894123.00")

	tickerPrices := make(types.CurrencyPairTickers, 1)
	tickerPrices[pair] = types.TickerPrice{
		Price:  atomPrice,
		Volume: atomVolume,
	}
	providerPrices[provider.ProviderBinance] = tickerPrices

	providerPair := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {pair},
	}

	prices, err := ots.oracle.GetComputedPrices(
		make(types.AggregatedProviderCandles, 1),
		providerPrices,
		providerPair,
		make(map[string]sdk.Dec),
	)

	require.NoError(ots.T(), err, "It should successfully get computed ticker prices")
	require.Equal(ots.T(), prices[pair], atomPrice)
}

func (ots *OracleTestSuite) TestGetComputedPricesCandlesConversion() {
	btcPair := types.CurrencyPair{
		Base:  "BTC",
		Quote: "ETH",
	}
	btcUSDPair := types.CurrencyPair{
		Base:  "BTC",
		Quote: "USD",
	}
	ethPair := types.CurrencyPair{
		Base:  "ETH",
		Quote: "USD",
	}
	btcEthPrice := sdk.MustNewDecFromStr("17.55")
	btcUSDPrice := sdk.MustNewDecFromStr("20962.601")
	ethUsdPrice := sdk.MustNewDecFromStr("1195.02")
	volume := sdk.MustNewDecFromStr("894123.00")
	providerCandles := make(types.AggregatedProviderCandles, 4)

	// normal rates
	binanceCandles := make(types.CurrencyPairCandles, 2)
	binanceCandles[btcPair] = []types.CandlePrice{
		{
			Price:     btcEthPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	binanceCandles[ethPair] = []types.CandlePrice{
		{
			Price:     ethUsdPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[provider.ProviderBinance] = binanceCandles

	// normal rates
	gateCandles := make(types.CurrencyPairCandles, 1)
	gateCandles[ethPair] = []types.CandlePrice{
		{
			Price:     ethUsdPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	gateCandles[btcPair] = []types.CandlePrice{
		{
			Price:     btcEthPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[provider.ProviderGate] = gateCandles

	// abnormal eth rate
	okxCandles := make(types.CurrencyPairCandles, 1)
	okxCandles[ethPair] = []types.CandlePrice{
		{
			Price:     sdk.MustNewDecFromStr("1.0"),
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[provider.ProviderOkx] = okxCandles

	// btc / usd rate
	krakenCandles := make(types.CurrencyPairCandles, 1)
	krakenCandles[btcUSDPair] = []types.CandlePrice{
		{
			Price:     btcUSDPrice,
			Volume:    volume,
			TimeStamp: provider.PastUnixTime(1 * time.Minute),
		},
	}
	providerCandles[provider.ProviderKraken] = krakenCandles

	providerPair := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {btcPair, ethPair},
		provider.ProviderGate:    {ethPair},
		provider.ProviderOkx:     {ethPair},
		provider.ProviderKraken:  {btcUSDPair},
	}

	prices, err := ots.oracle.GetComputedPrices(
		providerCandles,
		make(types.AggregatedProviderPrices, 1),
		providerPair,
		make(map[string]sdk.Dec),
	)

	require.NoError(ots.T(), err,
		"It should successfully filter out bad candles and convert everything to USD",
	)
	require.Equal(ots.T(),
		ethUsdPrice.Mul(
			btcEthPrice).Add(btcUSDPrice).Quo(sdk.MustNewDecFromStr("2")),
		prices[btcPair],
	)
}

func (ots *OracleTestSuite) TestGetComputedPricesTickersConversion() {
	volume := sdk.MustNewDecFromStr("881272.00")
	btcEthPrice := sdk.MustNewDecFromStr("72.55")
	ethUsdPrice := sdk.MustNewDecFromStr("9989.02")
	btcUSDPrice := sdk.MustNewDecFromStr("724603.401")
	providerPrices := make(types.AggregatedProviderPrices, 1)

	// normal rates
	binanceTickerPrices := make(types.CurrencyPairTickers, 2)
	binanceTickerPrices[BTCETH] = types.TickerPrice{
		Price:  btcEthPrice,
		Volume: volume,
	}
	binanceTickerPrices[ETHUSD] = types.TickerPrice{
		Price:  ethUsdPrice,
		Volume: volume,
	}
	providerPrices[provider.ProviderBinance] = binanceTickerPrices

	// normal rates
	gateTickerPrices := make(types.CurrencyPairTickers, 4)
	gateTickerPrices[BTCETH] = types.TickerPrice{
		Price:  btcEthPrice,
		Volume: volume,
	}
	gateTickerPrices[ETHUSD] = types.TickerPrice{
		Price:  ethUsdPrice,
		Volume: volume,
	}
	providerPrices[provider.ProviderGate] = gateTickerPrices

	// abnormal eth rate
	okxTickerPrices := make(types.CurrencyPairTickers, 1)
	okxTickerPrices[ETHUSD] = types.TickerPrice{
		Price:  sdk.MustNewDecFromStr("1.0"),
		Volume: volume,
	}
	providerPrices[provider.ProviderOkx] = okxTickerPrices

	// btc / usd rate
	krakenTickerPrices := make(types.CurrencyPairTickers, 1)
	krakenTickerPrices[BTCUSD] = types.TickerPrice{
		Price:  btcUSDPrice,
		Volume: volume,
	}
	providerPrices[provider.ProviderKraken] = krakenTickerPrices

	providerPair := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderBinance: {ETHUSD, BTCETH},
		provider.ProviderGate:    {ETHUSD},
		provider.ProviderOkx:     {ETHUSD},
		provider.ProviderKraken:  {BTCUSD},
	}

	prices, err := ots.oracle.GetComputedPrices(
		make(types.AggregatedProviderCandles, 1),
		providerPrices,
		providerPair,
		make(map[string]sdk.Dec),
	)

	require.NoError(ots.T(), err,
		"It should successfully filter out bad tickers and convert everything to USD",
	)
	require.Equal(ots.T(),
		ethUsdPrice.Mul(
			btcEthPrice).Add(btcUSDPrice).Quo(sdk.MustNewDecFromStr("2")),
		prices[BTCETH],
	)
}

func (ots *OracleTestSuite) TestGetComputedPricesEmptyTvwap() {
	symbolUSDT := "USDT"
	symbolUSD := "USD"
	symbolDAI := "DAI"
	symbolETH := "ETH"

	pairETHtoUSDT := types.CurrencyPair{
		Base:  symbolETH,
		Quote: symbolUSDT,
	}
	pairETHtoDAI := types.CurrencyPair{
		Base:  symbolETH,
		Quote: symbolDAI,
	}
	pairETHtoUSD := types.CurrencyPair{
		Base:  symbolETH,
		Quote: symbolUSD,
	}
	basePairsETH := []types.CurrencyPair{
		pairETHtoUSDT,
		pairETHtoDAI,
	}
	krakenPairsETH := append(basePairsETH, pairETHtoUSD)

	pairUSDTtoUSD := types.CurrencyPair{
		Base:  symbolUSDT,
		Quote: symbolUSD,
	}
	pairDAItoUSD := types.CurrencyPair{
		Base:  symbolDAI,
		Quote: symbolUSD,
	}
	stablecoinPairs := []types.CurrencyPair{
		pairUSDTtoUSD,
		pairDAItoUSD,
	}

	krakenPairs := append(krakenPairsETH, stablecoinPairs...)

	volume := sdk.MustNewDecFromStr("881272.00")
	ethUsdPrice := sdk.MustNewDecFromStr("9989.02")
	daiUsdPrice := sdk.MustNewDecFromStr("999890000000000000")
	ethTime := provider.PastUnixTime(1 * time.Minute)

	ethCandle := []types.CandlePrice{
		{
			Price:     ethUsdPrice,
			Volume:    volume,
			TimeStamp: ethTime,
		},
		{
			Price:     ethUsdPrice,
			Volume:    volume,
			TimeStamp: ethTime,
		},
	}
	daiCandle := []types.CandlePrice{
		{
			Price:     daiUsdPrice,
			Volume:    volume,
			TimeStamp: 1660829520000,
		},
	}

	prices := types.AggregatedProviderPrices{}

	pairs := map[types.ProviderName][]types.CurrencyPair{
		provider.ProviderKraken: krakenPairs,
	}

	testCases := map[string]struct {
		candles   types.AggregatedProviderCandles
		prices    types.AggregatedProviderPrices
		pairs     map[types.ProviderName][]types.CurrencyPair
		numPrices int
	}{
		"Empty tvwap": {
			candles: types.AggregatedProviderCandles{
				provider.ProviderKraken: {
					USDTUSD: ethCandle,
					ETHUSD:  ethCandle,
					DAIUSD:  daiCandle,
				},
			},
			prices:    prices,
			pairs:     pairs,
			numPrices: 2,
		},
		"No valid conversion rates DAI": {
			candles: types.AggregatedProviderCandles{
				provider.ProviderKraken: {
					USDTUSD: ethCandle,
					ETHUSD:  ethCandle,
				},
			},
			prices:    prices,
			pairs:     pairs,
			numPrices: 2,
		},
	}

	for name, tc := range testCases {
		tc := tc

		ots.Run(name, func() {
			prices, _ := ots.oracle.GetComputedPrices(
				tc.candles,
				tc.prices,
				tc.pairs,
				make(map[string]sdk.Dec),
			)

			require.Equal(ots.T(), tc.numPrices, len(prices))
		})
	}
}
