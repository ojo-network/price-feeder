package provider

import (
	"context"
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestPolygonProvider_GetTickerPrices(t *testing.T) {
	p, err := NewPolygonProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		EURUSD,
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := math.LegacyMustNewDecFromStr("1.19000000")
		volume := math.LegacyMustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]types.TickerPrice{}
		tickerMap["EUR/USD"] = types.TickerPrice{
			Price:  lastPrice,
			Volume: volume,
		}

		p.tickers = tickerMap

		prices, err := p.GetTickerPrices(EURUSD)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, lastPrice, prices[EURUSD].Price)
		require.Equal(t, volume, prices[EURUSD].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceEUR := math.LegacyMustNewDecFromStr("1.19000000")
		lastPriceJPY := math.LegacyMustNewDecFromStr("0.00820000")
		volume := math.LegacyMustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]types.TickerPrice{}
		tickerMap["EUR/USD"] = types.TickerPrice{
			Price:  lastPriceEUR,
			Volume: volume,
		}

		tickerMap["JPY/USD"] = types.TickerPrice{
			Price:  lastPriceJPY,
			Volume: volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			EURUSD,
			types.CurrencyPair{Base: "JPY", Quote: "USD"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, lastPriceEUR, prices[EURUSD].Price)
		require.Equal(t, volume, prices[EURUSD].Volume)
		require.Equal(t, lastPriceJPY, prices[JPYUSD].Price)
		require.Equal(t, volume, prices[JPYUSD].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, _ := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Empty(t, prices)
	})
}

func TestPolygonProvider_GetCandlePrices(t *testing.T) {
	p, err := NewPolygonProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{Base: "EUR", Quote: "USD"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := 1.190000000000000000
		volume := 2396974.000000000000000000
		timeStamp := int64(1000000000)

		data := PolygonAggregatesResponse{
			EV:        "CA",
			Pair:      "EUR/USD",
			Close:     price,
			Volume:    volume,
			Timestamp: timeStamp,
		}

		p.setCandlePair(data, data.Pair)

		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "EUR", Quote: "USD"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		priceDec, _ := math.LegacyNewDecFromStr(fmt.Sprintf("%f", price))
		volumeDec, _ := math.LegacyNewDecFromStr(fmt.Sprintf("%f", volume))

		require.Equal(t, priceDec, prices[EURUSD][0].Price)
		require.Equal(t, volumeDec, prices[EURUSD][0].Volume)
		require.Equal(t, timeStamp, prices[EURUSD][0].TimeStamp)
	})

	t.Run("invalid_request_invalid_candle", func(t *testing.T) {
		prices, _ := p.GetCandlePrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Empty(t, prices)
	})
}

func TestPolygonCurrencyPairToCryptoPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "EUR", Quote: "USD"}
	polygonSymbol := currencyPairToPolygonPair(cp)
	require.Equal(t, polygonSymbol, "EUR/USD")
}

func TestPolygonNewSubscriptionMsg(t *testing.T) {
	cps := []types.CurrencyPair{
		{Base: "EUR", Quote: "USD"},
		{Base: "ALL", Quote: "USD"},
		{Base: "JPY", Quote: "USD"},
	}
	subscriptionMsg := newPolygonSubscriptionMsg(cps)
	require.Equal(t, subscriptionMsg, PolygonSubscriptionMsg{
		Action: "subscribe",
		Params: "CA.EUR/USD,CA.ALL/USD,CA.JPY/USD",
	})
}
