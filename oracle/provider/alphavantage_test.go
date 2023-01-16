package provider

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestAlphaVantageProvider_GetTickerAndCandlePrices(t *testing.T) {
	p := NewAlphaVantageProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{Base: "EUR", Quote: "USD"},
	)

	t.Run("valid_request_single_ticker_candle", func(t *testing.T) {
		lastPrice := sdk.MustNewDecFromStr("1.19000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		candleMap := map[string][]types.CandlePrice{}
		candleMap["EURUSD"] = append(candleMap["EURUSD"], types.CandlePrice{
			Price:  lastPrice,
			Volume: volume,
		})

		p.setCandleCache(candleMap)

		tickerPrices, err := p.GetTickerPrices(types.CurrencyPair{Base: "EUR", Quote: "USD"})
		require.NoError(t, err)
		candlePrices, err := p.GetCandlePrices(types.CurrencyPair{Base: "EUR", Quote: "USD"})
		require.NoError(t, err)
		require.Len(t, tickerPrices, 1)
		require.Len(t, candlePrices, 1)
		require.Equal(t, lastPrice, tickerPrices["EURUSD"].Price)
		require.Equal(t, lastPrice, candlePrices["EURUSD"][0].Price)
		require.Equal(t, volume, tickerPrices["EURUSD"].Volume)
		require.Equal(t, volume, candlePrices["EURUSD"][0].Volume)
	})

	t.Run("valid_request_multi_ticker_candle", func(t *testing.T) {
		lastPriceEUR := sdk.MustNewDecFromStr("1.19000000")
		lastPriceALL := sdk.MustNewDecFromStr("0.00940000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		candleMap := map[string][]types.CandlePrice{}
		candleMap["EURUSD"] = append(candleMap["EURUSD"], types.CandlePrice{
			Price:  lastPriceEUR,
			Volume: volume,
		})
		candleMap["ALLUSD"] = append(candleMap["ALLUSD"], types.CandlePrice{
			Price:  lastPriceALL,
			Volume: volume,
		})

		p.setCandleCache(candleMap)

		tickerPrices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "EUR", Quote: "USD"},
			types.CurrencyPair{Base: "ALL", Quote: "USD"},
		)
		candlePrices, err := p.GetCandlePrices(
			types.CurrencyPair{Base: "EUR", Quote: "USD"},
			types.CurrencyPair{Base: "ALL", Quote: "USD"},
		)
		require.NoError(t, err)
		require.Len(t, tickerPrices, 2)
		require.Len(t, candlePrices, 2)
		require.Equal(t, lastPriceEUR, tickerPrices["EURUSD"].Price)
		require.Equal(t, lastPriceEUR, candlePrices["EURUSD"][0].Price)
		require.Equal(t, volume, tickerPrices["EURUSD"].Volume)
		require.Equal(t, volume, candlePrices["EURUSD"][0].Volume)
		require.Equal(t, lastPriceALL, tickerPrices["ALLUSD"].Price)
		require.Equal(t, lastPriceALL, candlePrices["ALLUSD"][0].Price)
		require.Equal(t, volume, tickerPrices["ALLUSD"].Volume)
		require.Equal(t, volume, candlePrices["ALLUSD"][0].Volume)
	})

	t.Run("invalid_request_invalid_ticker_candle", func(t *testing.T) {
		tickerPrices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Error(t, err)
		require.Equal(t, "alphavantage failed to get ticker price for FOOBAR", err.Error())
		require.Nil(t, tickerPrices)
		candlePrices, err := p.GetCandlePrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Error(t, err)
		require.Equal(t, "alphavantage failed to get candle price for FOOBAR", err.Error())
		require.Nil(t, candlePrices)
	})
}
