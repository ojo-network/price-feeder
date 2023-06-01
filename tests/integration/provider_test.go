package integration

import (
	"context"
	"sync"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
)

type IntegrationTestSuite struct {
	suite.Suite

	logger zerolog.Logger
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.logger = getLogger()
}

func TestServiceTestSuite(t *testing.T) {
	suite.Run(t, new(IntegrationTestSuite))
}

// TestWebsocketProviders tests that we receive pricing information for
// every webssocket provider and each of their currency pairs.
func (s *IntegrationTestSuite) TestWebsocketProviders() {
	if testing.Short() {
		s.T().Skip("skipping integration test in short mode")
	}

	cfg, err := config.ParseConfig("../../price-feeder.example.toml")
	require.NoError(s.T(), err)

	endpoints := cfg.ProviderEndpointsMap()

	var waitGroup sync.WaitGroup
	for key, pairs := range cfg.ProviderPairs() {
		waitGroup.Add(1)
		providerName := key
		currencyPairs := pairs

		go func() {
			defer waitGroup.Done()
			endpoint := endpoints[providerName]
			ctx, cancel := context.WithCancel(context.Background())
			s.T().Logf("Checking %s provider with currency pairs %+v", providerName, currencyPairs)
			pvd, _ := oracle.NewProvider(ctx, providerName, getLogger(), endpoint, currencyPairs...)
			pvd.StartConnections()
			time.Sleep(60 * time.Second) // wait for provider to connect and receive some prices
			checkForPrices(s.T(), pvd, currencyPairs, providerName.String())
			cancel()
		}()
	}
	waitGroup.Wait()
}

func (s *IntegrationTestSuite) TestSubscribeCurrencyPairs() {
	if testing.Short() {
		s.T().Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithCancel(context.Background())
	currencyPairs := []types.CurrencyPair{{Base: "USDT", Quote: "USD"}}
	pvd, _ := provider.NewKrakenProvider(ctx, getLogger(), provider.Endpoint{}, currencyPairs...)
	pvd.StartConnections()

	time.Sleep(5 * time.Second)

	newPairs := []types.CurrencyPair{{Base: "ATOM", Quote: "USD"}}
	pvd.SubscribeCurrencyPairs(newPairs...)
	currencyPairs = append(currencyPairs, newPairs...)

	time.Sleep(25 * time.Second)

	checkForPrices(s.T(), pvd, currencyPairs, "Kraken")

	cancel()
}

func checkForPrices(t *testing.T, pvd provider.Provider, currencyPairs []types.CurrencyPair, providerName string) {
	tickerPrices, err := pvd.GetTickerPrices(currencyPairs...)
	require.NoError(t, err)

	candlePrices, err := pvd.GetCandlePrices(currencyPairs...)
	require.NoError(t, err)

	for _, cp := range currencyPairs {
		currencyPairKey := cp.String()

		require.False(t,
			tickerPrices[currencyPairKey].Price.IsNil(),
			"no ticker price for %s pair %s",
			providerName,
			currencyPairKey,
		)

		require.True(t,
			tickerPrices[currencyPairKey].Price.GT(sdk.NewDec(0)),
			"ticker price is zero for %s pair %s",
			providerName,
			currencyPairKey,
		)

		require.NotEmpty(t,
			candlePrices[currencyPairKey],
			"no candle prices for %s pair %s",
			providerName,
			currencyPairKey,
		)

		require.True(t,
			candlePrices[currencyPairKey][0].Price.GT(sdk.NewDec(0)),
			"candle price is zero for %s pair %s",
			providerName,
			currencyPairKey,
		)
	}
}
