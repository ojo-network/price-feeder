package oracle_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/stretchr/testify/require"
)

var (
	ATOMUSD = types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	OJOUSD  = types.CurrencyPair{Base: "OJO", Quote: "USD"}
	LUNAUSD = types.CurrencyPair{Base: "LUNA", Quote: "USD"}
)

func TestComputeVWAP(t *testing.T) {
	testCases := map[string]struct {
		prices   types.AggregatedProviderPrices
		expected types.CurrencyPairDec
	}{
		"empty prices": {
			prices:   make(types.AggregatedProviderPrices),
			expected: make(types.CurrencyPairDec),
		},
		"nil prices": {
			prices:   nil,
			expected: make(types.CurrencyPairDec),
		},
		"valid prices": {
			prices: types.AggregatedProviderPrices{
				provider.ProviderBinance: {
					ATOMUSD: types.TickerPrice{
						Price:  math.LegacyMustNewDecFromStr("28.21000000"),
						Volume: math.LegacyMustNewDecFromStr("2749102.78000000"),
					},
					OJOUSD: types.TickerPrice{
						Price:  math.LegacyMustNewDecFromStr("1.13000000"),
						Volume: math.LegacyMustNewDecFromStr("249102.38000000"),
					},
					LUNAUSD: types.TickerPrice{
						Price:  math.LegacyMustNewDecFromStr("64.87000000"),
						Volume: math.LegacyMustNewDecFromStr("7854934.69000000"),
					},
				},
				provider.ProviderKraken: {
					ATOMUSD: types.TickerPrice{
						Price:  math.LegacyMustNewDecFromStr("28.268700"),
						Volume: math.LegacyMustNewDecFromStr("178277.53314385"),
					},
					LUNAUSD: types.TickerPrice{
						Price:  math.LegacyMustNewDecFromStr("64.87853000"),
						Volume: math.LegacyMustNewDecFromStr("458917.46353577"),
					},
				},
				"FOO": {
					ATOMUSD: types.TickerPrice{
						Price:  math.LegacyMustNewDecFromStr("28.168700"),
						Volume: math.LegacyMustNewDecFromStr("4749102.53314385"),
					},
				},
			},
			expected: types.CurrencyPairDec{
				ATOMUSD: math.LegacyMustNewDecFromStr("28.185812745610043621"),
				OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
				LUNAUSD: math.LegacyMustNewDecFromStr("64.870470848638112395"),
			},
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			vwap := oracle.ComputeVWAP(tc.prices)
			require.Len(t, vwap, len(tc.expected))

			for k, v := range tc.expected {
				require.Equalf(t, v, vwap[k], "unexpected VWAP for %s", k)
			}
		})
	}
}

func TestComputeTVWAP(t *testing.T) {
	testCases := map[string]struct {
		candles  types.AggregatedProviderCandles
		expected types.CurrencyPairDec
	}{
		"empty prices": {
			candles:  make(types.AggregatedProviderCandles),
			expected: make(types.CurrencyPairDec),
		},
		"nil prices": {
			candles:  nil,
			expected: make(types.CurrencyPairDec),
		},
		"valid prices": {
			candles: types.AggregatedProviderCandles{
				provider.ProviderBinance: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("25.09183"),
							Volume:    math.LegacyMustNewDecFromStr("98444.123455"),
							TimeStamp: provider.PastUnixTime(1 * time.Minute),
						},
					},
				},
				provider.ProviderKraken: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("28.268700"),
							Volume:    math.LegacyMustNewDecFromStr("178277.53314385"),
							TimeStamp: provider.PastUnixTime(2 * time.Minute),
						},
					},
					OJOUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("1.13000000"),
							Volume:    math.LegacyMustNewDecFromStr("178277.53314385"),
							TimeStamp: provider.PastUnixTime(2 * time.Minute),
						},
					},
					LUNAUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("64.87853000"),
							Volume:    math.LegacyMustNewDecFromStr("458917.46353577"),
							TimeStamp: provider.PastUnixTime(1 * time.Minute),
						},
					},
				},
				"FOO": {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("28.168700"),
							Volume:    math.LegacyMustNewDecFromStr("4749102.53314385"),
							TimeStamp: provider.PastUnixTime(130 * time.Second),
						},
					},
				},
			},
			expected: types.CurrencyPairDec{
				ATOMUSD: math.LegacyMustNewDecFromStr("28.045149332478338614"),
				OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
				LUNAUSD: math.LegacyMustNewDecFromStr("64.878530000000000000"),
			},
		},
		"one expired price": {
			candles: types.AggregatedProviderCandles{
				provider.ProviderBinance: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("25.09183"),
							Volume:    math.LegacyMustNewDecFromStr("98444.123455"),
							TimeStamp: provider.PastUnixTime(1 * time.Minute),
						},
					},
				},
				provider.ProviderKraken: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("28.268700"),
							Volume:    math.LegacyMustNewDecFromStr("178277.53314385"),
							TimeStamp: provider.PastUnixTime(2 * time.Minute),
						},
					},
					OJOUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("1.13000000"),
							Volume:    math.LegacyMustNewDecFromStr("178277.53314385"),
							TimeStamp: provider.PastUnixTime(2 * time.Minute),
						},
					},
					LUNAUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("64.87853000"),
							Volume:    math.LegacyMustNewDecFromStr("458917.46353577"),
							TimeStamp: provider.PastUnixTime(1 * time.Minute),
						},
					},
				},
				"FOO": {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("28.168700"),
							Volume:    math.LegacyMustNewDecFromStr("4749102.53314385"),
							TimeStamp: provider.PastUnixTime(10 * time.Minute),
						},
					},
				},
			},
			expected: types.CurrencyPairDec{
				ATOMUSD: math.LegacyMustNewDecFromStr("26.601468076898424151"),
				OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
				LUNAUSD: math.LegacyMustNewDecFromStr("64.878530000000000000"),
			},
		},
		"all expired prices": {
			candles: types.AggregatedProviderCandles{
				provider.ProviderBinance: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("25.09183"),
							Volume:    math.LegacyMustNewDecFromStr("98444.123455"),
							TimeStamp: provider.PastUnixTime(10 * time.Minute),
						},
					},
				},
				provider.ProviderKraken: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("28.268700"),
							Volume:    math.LegacyMustNewDecFromStr("178277.53314385"),
							TimeStamp: provider.PastUnixTime(10 * time.Minute),
						},
					},
					OJOUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("1.13000000"),
							Volume:    math.LegacyMustNewDecFromStr("178277.53314385"),
							TimeStamp: provider.PastUnixTime(10 * time.Minute),
						},
					},
					LUNAUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("64.87853000"),
							Volume:    math.LegacyMustNewDecFromStr("458917.46353577"),
							TimeStamp: provider.PastUnixTime(10 * time.Minute),
						},
					},
				},
				"FOO": {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("28.168700"),
							Volume:    math.LegacyMustNewDecFromStr("4749102.53314385"),
							TimeStamp: provider.PastUnixTime(10 * time.Minute),
						},
					},
				},
			},
			expected: make(types.CurrencyPairDec),
		},
		"prices from the future": {
			candles: types.AggregatedProviderCandles{
				provider.ProviderBinance: {
					ATOMUSD: []types.CandlePrice{
						{
							Price:     math.LegacyMustNewDecFromStr("25.09183"),
							Volume:    math.LegacyMustNewDecFromStr("98444.123455"),
							TimeStamp: provider.PastUnixTime(-5 * time.Minute),
						},
					},
				},
			},
			expected: make(types.CurrencyPairDec),
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			vwap, err := oracle.ComputeTVWAP(tc.candles)
			require.NoError(t, err)
			require.Len(t, vwap, len(tc.expected))

			for k, v := range tc.expected {
				require.Equalf(t, v, vwap[k], "unexpected VWAP for %s", k)
			}
		})
	}
}

func TestStandardDeviation(t *testing.T) {
	type deviation struct {
		mean      math.LegacyDec
		deviation math.LegacyDec
	}
	testCases := map[string]struct {
		prices   types.CurrencyPairDecByProvider
		expected map[types.CurrencyPair]deviation
	}{
		"empty prices": {
			prices:   make(types.CurrencyPairDecByProvider),
			expected: map[types.CurrencyPair]deviation{},
		},
		"nil prices": {
			prices:   nil,
			expected: map[types.CurrencyPair]deviation{},
		},
		"not enough prices": {
			prices: types.CurrencyPairDecByProvider{
				provider.ProviderBinance: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.21000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.87000000"),
				},
				provider.ProviderKraken: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.23000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.13050000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.85000000"),
				},
			},
			expected: map[types.CurrencyPair]deviation{},
		},
		"some prices": {
			prices: types.CurrencyPairDecByProvider{
				provider.ProviderBinance: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.21000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.87000000"),
				},
				provider.ProviderKraken: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.23000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.13050000"),
				},
				provider.ProviderOsmosis: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.40000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.14000000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.10000000"),
				},
			},
			expected: map[types.CurrencyPair]deviation{
				ATOMUSD: {
					mean:      math.LegacyMustNewDecFromStr("28.28"),
					deviation: math.LegacyMustNewDecFromStr("0.085244745683629475"),
				},
				OJOUSD: {
					mean:      math.LegacyMustNewDecFromStr("1.1335"),
					deviation: math.LegacyMustNewDecFromStr("0.004600724580614014"),
				},
			},
		},

		"non empty prices": {
			prices: types.CurrencyPairDecByProvider{
				provider.ProviderBinance: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.21000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.87000000"),
				},
				provider.ProviderKraken: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.23000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.13050000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.85000000"),
				},
				provider.ProviderOsmosis: {
					ATOMUSD: math.LegacyMustNewDecFromStr("28.40000000"),
					OJOUSD:  math.LegacyMustNewDecFromStr("1.14000000"),
					LUNAUSD: math.LegacyMustNewDecFromStr("64.10000000"),
				},
			},
			expected: map[types.CurrencyPair]deviation{
				ATOMUSD: {
					mean:      math.LegacyMustNewDecFromStr("28.28"),
					deviation: math.LegacyMustNewDecFromStr("0.085244745683629475"),
				},
				OJOUSD: {
					mean:      math.LegacyMustNewDecFromStr("1.1335"),
					deviation: math.LegacyMustNewDecFromStr("0.004600724580614014"),
				},
				LUNAUSD: {
					mean:      math.LegacyMustNewDecFromStr("64.606666666666666666"),
					deviation: math.LegacyMustNewDecFromStr("0.358360464089193608"),
				},
			},
		},
	}

	for name, tc := range testCases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			deviation, mean, err := oracle.StandardDeviation(tc.prices)
			require.NoError(t, err)
			require.Len(t, deviation, len(tc.expected))
			require.Len(t, mean, len(tc.expected))

			for k, v := range tc.expected {
				require.Equalf(t, v.deviation, deviation[k], "unexpected deviation for %s", k)
				require.Equalf(t, v.mean, mean[k], "unexpected mean for %s", k)
			}
		})
	}
}
