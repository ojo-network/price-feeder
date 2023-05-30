package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
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
	time.Sleep(40 * time.Second)

	for i := 0; i < 3; i++ {
		time.Sleep(5 * time.Second)

		oracle.SetPrices(context.Background())
		oraclePrices := oracle.GetPrices()

		apiPrices, err := getCoinMarketCapPrices(symbols)
		require.NoError(t, err)

		checkPrices(t, symbols, deviations, oraclePrices, apiPrices)
	}
}

func checkPrices(
	t *testing.T,
	expectedSymbols []string,
	deviations map[string]sdk.Dec,
	oraclePrices map[string]sdk.Dec,
	apiPrices map[string]float64,
) {
	for _, k := range expectedSymbols {
		if _, ok := apiPrices[k]; !ok {
			t.Logf("%s API price not found", k)
			continue
		}

		if _, ok := oraclePrices[k]; !ok {
			t.Logf("%s Oracle price not found", k)
			continue
		}

		v := oraclePrices[k]
		stdDeviation := calculateStandardDeviation([]float64{v.MustFloat64(), apiPrices[k]})
		stdDeviationMax := deviations[k].MustFloat64() * 2
		if stdDeviation > deviations[k].MustFloat64() {
			assert.Fail(t, fmt.Sprintf("FAIL %s Oracle price: %f, API price: %f, Std Deviation: %f > %f", k, v, apiPrices[k], stdDeviation, stdDeviationMax))
		} else {
			t.Logf("PASS %s Oracle price: %f, API price: %f, Std Deviation: %f < %f", k, v, apiPrices[k], stdDeviation, stdDeviationMax)
		}
	}
}
