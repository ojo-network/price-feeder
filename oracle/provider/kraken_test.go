package provider

import (
	"context"
	"encoding/json"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestKrakenProvider_GetTickerPrices(t *testing.T) {
	p, err := NewKrakenProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		BTCUSDT,
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := sdk.MustNewDecFromStr("34.69000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]types.TickerPrice{}
		tickerMap["ATOMUSDT"] = types.TickerPrice{
			Price:  lastPrice,
			Volume: volume,
		}

		p.tickers = tickerMap

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, lastPrice, prices[ATOMUSDT].Price)
		require.Equal(t, volume, prices[ATOMUSDT].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := sdk.MustNewDecFromStr("34.69000000")
		lastPriceLuna := sdk.MustNewDecFromStr("41.35000000")
		volume := sdk.MustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]types.TickerPrice{}
		tickerMap["ATOMUSDT"] = types.TickerPrice{
			Price:  lastPriceAtom,
			Volume: volume,
		}

		tickerMap["LUNAUSDT"] = types.TickerPrice{
			Price:  lastPriceLuna,
			Volume: volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "LUNA", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, lastPriceAtom, prices[ATOMUSDT].Price)
		require.Equal(t, volume, prices[ATOMUSDT].Volume)
		require.Equal(t, lastPriceLuna, prices[LUNAUSDT].Price)
		require.Equal(t, volume, prices[LUNAUSDT].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, _ := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Empty(t, prices)
	})
}

func TestKrakenPairToCurrencyPairSymbol(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	currencyPairSymbol := krakenPairToCurrencyPairSymbol("ATOM/USDT")
	require.Equal(t, cp.String(), currencyPairSymbol)
}

func TestKrakenCurrencyPairToKrakenPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	krakenSymbol := currencyPairToKrakenPair(cp)
	require.Equal(t, krakenSymbol, "ATOM/USDT")
}

func TestNormalizeKrakenBTCPair(t *testing.T) {
	btcSymbol := normalizeKrakenBTCPair("XBT/USDT")
	require.Equal(t, btcSymbol, "BTC/USDT")

	atomSymbol := normalizeKrakenBTCPair("ATOM/USDT")
	require.Equal(t, atomSymbol, "ATOM/USDT")
}

func TestKrakenProvider_getSubscriptionMsgs(t *testing.T) {
	provider := &KrakenProvider{}
	cps := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
	}
	subMsgs := provider.getSubscriptionMsgs(cps...)

	msg, _ := json.Marshal(subMsgs[0])
	require.Equal(t, "{\"event\":\"subscribe\",\"pair\":[\"ATOM/USDT\"],\"subscription\":{\"name\":\"ticker\"}}", string(msg))

	msg, _ = json.Marshal(subMsgs[1])
	require.Equal(t, "{\"event\":\"subscribe\",\"pair\":[\"ATOM/USDT\"],\"subscription\":{\"name\":\"ohlc\"}}", string(msg))
}
