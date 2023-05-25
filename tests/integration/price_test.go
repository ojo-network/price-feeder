package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPriceAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := getLogger()
	cfg, err := config.ParseConfig("../../price-feeder.example.toml")
	require.NoError(t, err)

	providerTimeout, err := time.ParseDuration(cfg.ProviderTimeout)
	require.NoError(t, err)

	deviations, err := cfg.DeviationsMap()
	require.NoError(t, err)

	oracle := oracle.New(
		logger,
		client.OracleClient{},
		cfg.ProviderPairs(),
		providerTimeout,
		deviations,
		cfg.ProviderEndpointsMap(),
	)

	symbols := cfg.ExpectedSymbols()

	// first call to SetPrices starts the provider routines
	oracle.SetPrices(context.Background())

	time.Sleep(60 * time.Second)

	oracle.SetPrices(context.Background())
	oraclePrices := oracle.GetPrices()

	apiPrices, err := getCoinMarketCapPrices(
		symbols,
		cfg.ProviderEndpointsMap()["coinmarketcap"].Rest,
		cfg.ProviderEndpointsMap()["coinmarketcap"].APIKey,
	)
	require.NoError(t, err)

	for _, k := range symbols {
		if _, ok := apiPrices[k]; !ok {
			logger.Debug().Msg(fmt.Sprintf("%s API price not found", k))
			continue
		}

		if _, ok := oraclePrices[k]; !ok {
			logger.Debug().Msg(fmt.Sprintf("%s Oracle price not found", k))
			continue
		}

		v := oraclePrices[k]
		stdDeviation := calculateStandardDeviation([]float64{v.MustFloat64(), apiPrices[k]})
		if stdDeviation > 0.1 {
			assert.Fail(t, fmt.Sprintf("FAIL %s Oracle price: %s, API price: %f, Std Deviation: %f", k, v, apiPrices[k], stdDeviation))
		} else {
			logger.Info().Msg(fmt.Sprintf("PASS %s Oracle price: %s, API price: %f, Std Deviation: %f", k, v, apiPrices[k], stdDeviation))
		}
	}
}
