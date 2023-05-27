package oracle_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

func TestConvertRatesToUSD(t *testing.T) {
	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "BTC", Quote: "USD"}: sdk.NewDec(50000),
		types.CurrencyPair{Base: "ETH", Quote: "BTC"}: sdk.NewDecWithPrec(1, 18),
		types.CurrencyPair{Base: "ETH", Quote: "USD"}: sdk.NewDec(3000),
		types.CurrencyPair{Base: "LTC", Quote: "BTC"}: sdk.NewDecWithPrec(1, 8),
		types.CurrencyPair{Base: "LTC", Quote: "USD"}: sdk.NewDec(150),
	}

	expected := types.CurrencyPairDec{
		types.CurrencyPair{Base: "BTC", Quote: "USD"}: sdk.NewDec(50000),
		types.CurrencyPair{Base: "ETH", Quote: "USD"}: sdk.NewDec(150000000),
		types.CurrencyPair{Base: "LTC", Quote: "USD"}: sdk.NewDec(7500),
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

func TestCalcCurrencyPairRates(t *testing.T) {
	candles := types.AggregatedProviderCandles{
		"Provider1": {
			types.CurrencyPair{Base: "BTC", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(50000)},
				{TimeStamp: 2, Price: sdk.NewDec(51000)},
				{TimeStamp: 3, Price: sdk.NewDec(52000)},
			},
			types.CurrencyPair{Base: "ETH", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(3000)},
				{TimeStamp: 2, Price: sdk.NewDec(3100)},
				{TimeStamp: 3, Price: sdk.NewDec(3200)},
			},
		},
		"Provider2": {
			types.CurrencyPair{Base: "BTC", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(49500)},
				{TimeStamp: 2, Price: sdk.NewDec(50500)},
				{TimeStamp: 3, Price: sdk.NewDec(51500)},
			},
			types.CurrencyPair{Base: "ETH", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(2950)},
				{TimeStamp: 2, Price: sdk.NewDec(3050)},
				{TimeStamp: 3, Price: sdk.NewDec(3150)},
			},
		},
	}

	tickers := types.AggregatedProviderPrices{
		"Provider1": {
			types.CurrencyPair{Base: "LTC", Quote: "USD"}: types.TickerPrice{
				Price: sdk.NewDec(150),
			},
			types.CurrencyPair{Base: "XRP", Quote: "USD"}: types.TickerPrice{
				Price: sdk.NewDec(5),
			},
		},
		"Provider2": {
			types.CurrencyPair{Base: "LTC", Quote: "USD"}: types.TickerPrice{
				Price: sdk.NewDec(155),
			},
			types.CurrencyPair{Base: "XRP", Quote: "USD"}: types.TickerPrice{
				Price: sdk.NewDec(6),
			},
		},
	}

	deviationThresholds := map[string]sdk.Dec{
		"BTC": sdk.NewDecWithPrec(1, 4),
		"ETH": sdk.NewDecWithPrec(1, 6),
		"LTC": sdk.NewDecWithPrec(1, 2),
		"XRP": sdk.NewDecWithPrec(1, 3),
	}

	currencyPairs := []types.CurrencyPair{
		{Base: "BTC", Quote: "USD"},
		{Base: "ETH", Quote: "USD"},
		{Base: "LTC", Quote: "USD"},
		{Base: "XRP", Quote: "USD"},
	}

	logger := zerolog.Logger{}

	expectedConversionRates := types.CurrencyPairDec{
		{Base: "BTC", Quote: "USD"}: sdk.NewDec(50000),
		{Base: "ETH", Quote: "USD"}: sdk.NewDec(3000),
		{Base: "LTC", Quote: "USD"}: sdk.NewDec(150),
		{Base: "XRP", Quote: "USD"}: sdk.NewDec(5),
	}

	convertedRates, err := oracle.CalcCurrencyPairRates(candles, tickers, deviationThresholds, currencyPairs, logger)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(convertedRates) != len(expectedConversionRates) {
		t.Errorf("Unexpected length of converted rates. Expected: %d, Got: %d", len(expectedConversionRates), len(convertedRates))
	}

	for cp, expectedRate := range expectedConversionRates {
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
		"Provider1": {
			types.CurrencyPair{Base: "BTC", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(50000)},
				{TimeStamp: 2, Price: sdk.NewDec(51000)},
				{TimeStamp: 3, Price: sdk.NewDec(52000)},
			},
			types.CurrencyPair{Base: "ETH", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(3000)},
				{TimeStamp: 2, Price: sdk.NewDec(3100)},
				{TimeStamp: 3, Price: sdk.NewDec(3200)},
			},
		},
		"Provider2": {
			types.CurrencyPair{Base: "BTC", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(49500)},
				{TimeStamp: 2, Price: sdk.NewDec(50500)},
				{TimeStamp: 3, Price: sdk.NewDec(51500)},
			},
			types.CurrencyPair{Base: "ETH", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(2950)},
				{TimeStamp: 2, Price: sdk.NewDec(3050)},
				{TimeStamp: 3, Price: sdk.NewDec(3150)},
			},
		},
	}

	rates := types.CurrencyPairDec{
		{Base: "BTC", Quote: "USD"}: sdk.NewDec(50000),
		{Base: "ETH", Quote: "USD"}: sdk.NewDec(3000),
	}

	expectedConvertedCandles := types.AggregatedProviderCandles{
		"Provider1": {
			types.CurrencyPair{Base: "BTC", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(50000)},
				{TimeStamp: 2, Price: sdk.NewDec(51000)},
				{TimeStamp: 3, Price: sdk.NewDec(52000)},
			},
			types.CurrencyPair{Base: "ETH", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(90000000)},
				{TimeStamp: 2, Price: sdk.NewDec(93000000)},
				{TimeStamp: 3, Price: sdk.NewDec(96000000)},
			},
		},
		"Provider2": {
			types.CurrencyPair{Base: "BTC", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(99000000)},
				{TimeStamp: 2, Price: sdk.NewDec(101000000)},
				{TimeStamp: 3, Price: sdk.NewDec(103000000)},
			},
			types.CurrencyPair{Base: "ETH", Quote: "USD"}: []types.CandlePrice{
				{TimeStamp: 1, Price: sdk.NewDec(88500000)},
				{TimeStamp: 2, Price: sdk.NewDec(91500000)},
				{TimeStamp: 3, Price: sdk.NewDec(94500000)},
			},
		},
	}

	convertedCandles := oracle.ConvertAggregatedCandles(candles, rates)

	if len(convertedCandles) != len(expectedConvertedCandles) {
		t.Errorf("Unexpected length of converted candles. Expected: %d, Got: %d", len(expectedConvertedCandles), len(convertedCandles))
	}

	for provider, expectedCandles := range expectedConvertedCandles {
		convertedProviderCandles, ok := convertedCandles[provider]
		if !ok {
			t.Errorf("Missing converted candles for provider: %s", provider)
		}

		if len(convertedProviderCandles) != len(expectedCandles) {
			t.Errorf("Unexpected length of converted provider candles. Expected: %d, Got: %d", len(expectedCandles), len(convertedProviderCandles))
		}

		for cp, expectedCandlePrices := range expectedCandles {
			convertedCandlePrices, ok := convertedProviderCandles[cp]
			if !ok {
				t.Errorf("Missing converted candle prices for currency pair: %v", cp)
			}

			if len(convertedCandlePrices) != len(expectedCandlePrices) {
				t.Errorf("Unexpected length of converted candle prices. Expected: %d, Got: %d", len(expectedCandlePrices), len(convertedCandlePrices))
			}

			for i, expectedCandle := range expectedCandlePrices {
				convertedCandle := convertedCandlePrices[i]
				if convertedCandle.TimeStamp != expectedCandle.TimeStamp {
					t.Errorf("Unexpected converted candle TimeStamp. Expected: %d, Got: %d", expectedCandle.TimeStamp, convertedCandle.TimeStamp)
				}

				if !convertedCandle.Price.Equal(expectedCandle.Price) {
					t.Errorf("Unexpected converted candle price. Expected: %s, Got: %s", expectedCandle.Price.String(), convertedCandle.Price.String())
				}
			}
		}
	}
}
