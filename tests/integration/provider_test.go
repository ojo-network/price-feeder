package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
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

	cfg, err := config.LoadConfigFromFlags(
		fmt.Sprintf("../../%s", config.SampleNodeConfigPath),
		"../../",
	)
	require.NoError(s.T(), err)

	endpoints := cfg.ProviderEndpointsMap()

	var waitGroup sync.WaitGroup
	for key, pairs := range cfg.ProviderPairs() {
		providerName := key
		currencyPairs := pairs

		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			endpoint := endpoints[providerName]
			ctx, cancel := context.WithCancel(context.Background())
			s.T().Logf("Checking %s provider with currency pairs %+v", providerName, currencyPairs)
			pvd, err := oracle.NewProvider(ctx, providerName, getLogger(), endpoint, currencyPairs...)
			require.NoError(s.T(), err)
			pvd.StartConnections()
			time.Sleep(60 * time.Second) // wait for provider to connect and receive some prices
			checkForPrices(s.T(), pvd, currencyPairs, providerName.String())
			cancel()
		}()
	}
	waitGroup.Wait()
}

func checkForPrices(t *testing.T, pvd provider.Provider, currencyPairs []types.CurrencyPair, providerName string) {
	tickerPrices, err := pvd.GetTickerPrices(currencyPairs...)
	require.NoError(t, err)

	candlePrices, err := pvd.GetCandlePrices(currencyPairs...)
	require.NoError(t, err)

	for _, cp := range currencyPairs {
		currencyPairKey := cp.String()

		if tickerPrices[cp].Price.IsNil() {
			assert.Failf(t,
				"no ticker price",
				"provider %s pair %s",
				providerName,
				currencyPairKey,
			)
		} else {
			assert.True(t,
				tickerPrices[cp].Price.GT(math.LegacyNewDec(0)),
				"ticker price is zero for %s pair %s",
				providerName,
				currencyPairKey,
			)
		}

		if len(candlePrices[cp]) == 0 {
			assert.Failf(t,
				"no candle prices",
				"provider %s pair %s",
				providerName,
				currencyPairKey,
			)
		} else {
			assert.True(t,
				candlePrices[cp][0].Price.GT(math.LegacyNewDec(0)),
				"candle price is zero for %s pair %s",
				providerName,
				currencyPairKey,
			)
		}
	}
}
