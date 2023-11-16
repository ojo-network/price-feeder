package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/monitor"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/client"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/util"
)

const (
	maxCoeficientOfVariation = 0.75
)

var (
	KnownIncorrectAPIPrices = map[string]struct{}{
		"stATOM":  {},
		"stkATOM": {},
	}
)

// TestPriceAccuracy tests the accuracy of the final prices calculated by the oracle
// by comparing them to the prices from the CoinMarketCap API.
func TestPriceAccuracy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := getLogger()
	cfg, err := config.LoadConfigFromFlags(
		fmt.Sprintf("../../%s", config.SampleNodeConfigPath),
		"../../",
	)
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
		false,
	)

	symbols := cfg.ExpectedSymbols()

	// first call to SetPrices starts the provider routines
	oracle.SetPrices(context.Background())
	time.Sleep(60 * time.Second)

	oracle.SetPrices(context.Background())
	oraclePrices := oracle.GetPrices()

	apiPrices, err := monitor.GetCoinMarketCapPrices(symbols)
	require.NoError(t, err)

	checkPrices(t, symbols, oraclePrices, apiPrices)
}

func checkPrices(
	t *testing.T,
	expectedSymbols []string,
	oraclePrices types.CurrencyPairDec,
	apiPrices map[string]float64,
) {
	for _, denom := range expectedSymbols {
		cp := types.CurrencyPair{Base: denom, Quote: "USD"}

		if _, ok := oraclePrices[cp]; !ok {
			assert.Failf(t, "Oracle price not found", "currency_pair", cp)
			continue
		}
		oraclePrice := oraclePrices[cp].MustFloat64()

		if _, ok := apiPrices[denom]; !ok {
			t.Logf("SKIP %s API price not found; Oracle price: %f", denom, oraclePrice)
			continue
		}

		apiPrice := apiPrices[denom]
		cv := util.CalcCoeficientOfVariation([]float64{oraclePrice, apiPrice})

		if _, ok := KnownIncorrectAPIPrices[denom]; ok {
			t.Logf("SKIP %s Oracle price: %f, API price(inaccurate): %f, CV: %f", denom, oraclePrice, apiPrice, cv)
			continue
		}

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

func getLogger() zerolog.Logger {
	logWriter := zerolog.ConsoleWriter{Out: os.Stderr}
	logLvl := zerolog.DebugLevel
	zerolog.SetGlobalLevel(logLvl)
	return zerolog.New(logWriter).Level(logLvl).With().Timestamp().Logger()
}
