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

const (
	maxCoeficientOfVariation = 0.1
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

		checkPrices(t, symbols, oraclePrices, apiPrices)
	}
}

func checkPrices(
	t *testing.T,
	expectedSymbols []string,
	oraclePrices map[string]sdk.Dec,
	apiPrices map[string]float64,
) {
	for _, denom := range expectedSymbols {
		if _, ok := apiPrices[denom]; !ok {
			t.Logf("%s API price not found", denom)
			continue
		}

		if _, ok := oraclePrices[denom]; !ok {
			t.Logf("%s Oracle price not found", denom)
			continue
		}

		oraclePrice := oraclePrices[denom].MustFloat64()
		apiPrice := apiPrices[denom]
		cv := calcCoeficientOfVariation([]float64{oraclePrice, apiPrice})

		if cv > maxCoeficientOfVariation {
			assert.Fail(t, fmt.Sprintf(
				"FAIL %s Oracle price: %f, API price: %f, CV: %f > %f",
				denom, oraclePrice, apiPrice, cv, maxCoeficientOfVariation,
			))
		} else {
			t.Logf(
				"PASS %s Oracle price: %f, API price: %f, CV: %f < %f",
				denom, oraclePrice, apiPrice, cv, maxCoeficientOfVariation)
		}
	}
}