package oracle_test

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/stretchr/testify/assert"
)

func TestConvertRatesToUSD(t *testing.T) {
	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "ATOM", Quote: "USD"}:  math.LegacyNewDec(10),
		types.CurrencyPair{Base: "OSMO", Quote: "ATOM"}: math.LegacyNewDec(3),
		types.CurrencyPair{Base: "JUNO", Quote: "ATOM"}: math.LegacyNewDec(20),
		types.CurrencyPair{Base: "LTC", Quote: "USDT"}:  math.LegacyNewDec(20),
	}

	expected := types.CurrencyPairDec{
		types.CurrencyPair{Base: "ATOM", Quote: "USD"}: math.LegacyNewDec(10),
		types.CurrencyPair{Base: "OSMO", Quote: "USD"}: math.LegacyNewDec(30),
		types.CurrencyPair{Base: "JUNO", Quote: "USD"}: math.LegacyNewDec(200),
	}

	convertedRates := oracle.ConvertRatesToUSD(rates)

	if len(convertedRates) != len(expected) {
		t.Errorf("Unexpected length of converted rates. Expected: %d, Got: %d", len(expected), len(convertedRates))
	}

	for cp, expectedRate := range expected {
		convertedRate, ok := convertedRates[cp]
		if !ok {
			t.Errorf("Missing converted rate for currency pair: %v", cp)
		}

		if !convertedRate.Equal(expectedRate) {
			t.Errorf("Unexpected converted rate for currency pair: %v. Expected: %s, Got: %s", cp, expectedRate.String(), convertedRate.String())
		}
	}
}

func TestConvertAggregatedCandles(t *testing.T) {

	candles := types.AggregatedProviderCandles{
		"Provider1": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USDC"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("35"), Volume: math.LegacyMustNewDecFromStr("1000"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("40"), Volume: math.LegacyMustNewDecFromStr("1500"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USDC"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("18"), Volume: math.LegacyMustNewDecFromStr("500"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("22"), Volume: math.LegacyMustNewDecFromStr("800"), TimeStamp: 2},
			},
		},
		"Provider2": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("30"), Volume: math.LegacyMustNewDecFromStr("800"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("35"), Volume: math.LegacyMustNewDecFromStr("1000"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USDT"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("5"), Volume: math.LegacyMustNewDecFromStr("200"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("6"), Volume: math.LegacyMustNewDecFromStr("300"), TimeStamp: 2},
			},
		},
	}

	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "USDT", Quote: "USD"}: math.LegacyMustNewDecFromStr("2"),
		types.CurrencyPair{Base: "USDC", Quote: "USD"}: math.LegacyMustNewDecFromStr("1"),
	}

	expectedResult := types.AggregatedProviderCandles{
		"Provider1": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("35"), Volume: math.LegacyMustNewDecFromStr("1000"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("40"), Volume: math.LegacyMustNewDecFromStr("1500"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USD"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("18"), Volume: math.LegacyMustNewDecFromStr("500"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("22"), Volume: math.LegacyMustNewDecFromStr("800"), TimeStamp: 2},
			},
		},
		"Provider2": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("60"), Volume: math.LegacyMustNewDecFromStr("800"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("70"), Volume: math.LegacyMustNewDecFromStr("1000"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USD"}: []types.CandlePrice{
				{Price: math.LegacyMustNewDecFromStr("10"), Volume: math.LegacyMustNewDecFromStr("200"), TimeStamp: 1},
				{Price: math.LegacyMustNewDecFromStr("12"), Volume: math.LegacyMustNewDecFromStr("300"), TimeStamp: 2},
			},
		},
	}

	result := oracle.ConvertAggregatedCandles(candles, rates)

	assert.Equal(t, expectedResult, result, "The converted candles do not match the expected result.")
}

func TestConvertAggregatedTickers(t *testing.T) {

	tickers := types.AggregatedProviderPrices{
		"Provider1": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USDC"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("35"), Volume: math.LegacyMustNewDecFromStr("1000"),
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USDC"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("18"), Volume: math.LegacyMustNewDecFromStr("500"),
			},
		},
		"Provider2": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("30"), Volume: math.LegacyMustNewDecFromStr("800"),
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USDT"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("5"), Volume: math.LegacyMustNewDecFromStr("200"),
			},
		},
	}

	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "USDT", Quote: "USD"}: math.LegacyMustNewDecFromStr("2"),
		types.CurrencyPair{Base: "USDC", Quote: "USD"}: math.LegacyMustNewDecFromStr("1"),
	}

	expectedResult := types.AggregatedProviderPrices{
		"Provider1": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("35"), Volume: math.LegacyMustNewDecFromStr("1000"),
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USD"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("18"), Volume: math.LegacyMustNewDecFromStr("500"),
			},
		},
		"Provider2": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("60"), Volume: math.LegacyMustNewDecFromStr("800"),
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USD"}: types.TickerPrice{
				Price: math.LegacyMustNewDecFromStr("10"), Volume: math.LegacyMustNewDecFromStr("200"),
			},
		},
	}

	result := oracle.ConvertAggregatedTickers(tickers, rates)

	assert.Equal(t, expectedResult, result, "The converted tickers do not match the expected result.")
}
