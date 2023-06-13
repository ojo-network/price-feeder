package oracle_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/stretchr/testify/assert"
)

func TestConvertRatesToUSD(t *testing.T) {
	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "ATOM", Quote: "USD"}:  sdk.NewDec(10),
		types.CurrencyPair{Base: "OSMO", Quote: "ATOM"}: sdk.NewDec(3),
		types.CurrencyPair{Base: "JUNO", Quote: "ATOM"}: sdk.NewDec(20),
		types.CurrencyPair{Base: "LTC", Quote: "USDT"}:  sdk.NewDec(20),
	}

	expected := types.CurrencyPairDec{
		types.CurrencyPair{Base: "ATOM", Quote: "USD"}: sdk.NewDec(10),
		types.CurrencyPair{Base: "OSMO", Quote: "USD"}: sdk.NewDec(30),
		types.CurrencyPair{Base: "JUNO", Quote: "USD"}: sdk.NewDec(200),
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
				{Price: sdk.MustNewDecFromStr("35"), Volume: sdk.MustNewDecFromStr("1000"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("40"), Volume: sdk.MustNewDecFromStr("1500"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USDC"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("18"), Volume: sdk.MustNewDecFromStr("500"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("22"), Volume: sdk.MustNewDecFromStr("800"), TimeStamp: 2},
			},
		},
		"Provider2": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("30"), Volume: sdk.MustNewDecFromStr("800"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("35"), Volume: sdk.MustNewDecFromStr("1000"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USDT"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("5"), Volume: sdk.MustNewDecFromStr("200"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("6"), Volume: sdk.MustNewDecFromStr("300"), TimeStamp: 2},
			},
		},
	}

	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "USDT", Quote: "USD"}: sdk.MustNewDecFromStr("2"),
		types.CurrencyPair{Base: "USDC", Quote: "USD"}: sdk.MustNewDecFromStr("1"),
	}

	expectedResult := types.AggregatedProviderCandles{
		"Provider1": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("35"), Volume: sdk.MustNewDecFromStr("1000"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("40"), Volume: sdk.MustNewDecFromStr("1500"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USD"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("18"), Volume: sdk.MustNewDecFromStr("500"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("22"), Volume: sdk.MustNewDecFromStr("800"), TimeStamp: 2},
			},
		},
		"Provider2": types.CurrencyPairCandles{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("60"), Volume: sdk.MustNewDecFromStr("800"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("70"), Volume: sdk.MustNewDecFromStr("1000"), TimeStamp: 2},
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USD"}: []types.CandlePrice{
				{Price: sdk.MustNewDecFromStr("10"), Volume: sdk.MustNewDecFromStr("200"), TimeStamp: 1},
				{Price: sdk.MustNewDecFromStr("12"), Volume: sdk.MustNewDecFromStr("300"), TimeStamp: 2},
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
				Price: sdk.MustNewDecFromStr("35"), Volume: sdk.MustNewDecFromStr("1000"),
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USDC"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("18"), Volume: sdk.MustNewDecFromStr("500"),
			},
		},
		"Provider2": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("30"), Volume: sdk.MustNewDecFromStr("800"),
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USDT"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("5"), Volume: sdk.MustNewDecFromStr("200"),
			},
		},
	}

	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "USDT", Quote: "USD"}: sdk.MustNewDecFromStr("2"),
		types.CurrencyPair{Base: "USDC", Quote: "USD"}: sdk.MustNewDecFromStr("1"),
	}

	expectedResult := types.AggregatedProviderPrices{
		"Provider1": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("35"), Volume: sdk.MustNewDecFromStr("1000"),
			},
			types.CurrencyPair{Base: "UMEE", Quote: "USD"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("18"), Volume: sdk.MustNewDecFromStr("500"),
			},
		},
		"Provider2": types.CurrencyPairTickers{
			types.CurrencyPair{Base: "ATOM", Quote: "USD"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("60"), Volume: sdk.MustNewDecFromStr("800"),
			},
			types.CurrencyPair{Base: "JUNO", Quote: "USD"}: types.TickerPrice{
				Price: sdk.MustNewDecFromStr("10"), Volume: sdk.MustNewDecFromStr("200"),
			},
		},
	}

	result := oracle.ConvertAggregatedTickers(tickers, rates)

	assert.Equal(t, expectedResult, result, "The converted tickers do not match the expected result.")
}
