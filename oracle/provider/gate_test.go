package provider

import (
	"context"
	"encoding/json"
	"testing"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestGateProvider_GetTickerPrices(t *testing.T) {
	p, err := NewGateProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		tickerMap := map[string]GateTicker{}
		tickerMap["ATOM_USDT"] = GateTicker{
			Symbol: "ATOM_USDT",
			Last:   lastPrice,
			Vol:    volume,
		}

		for _, ticker := range tickerMap {
			p.setTickerPair(ticker, ticker.Symbol)
		}

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, math.LegacyMustNewDecFromStr(lastPrice), prices[ATOMUSDT].Price)
		require.Equal(t, math.LegacyMustNewDecFromStr(volume), prices[ATOMUSDT].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := "34.69000000"
		lastPriceOJO := "41.35000000"
		volume := "2396974.02000000"

		tickerMap := map[string]GateTicker{}
		tickerMap["ATOM_USDT"] = GateTicker{
			Symbol: "ATOM_USDT",
			Last:   lastPriceAtom,
			Vol:    volume,
		}

		tickerMap["OJO_USDT"] = GateTicker{
			Symbol: "OJO_USDT",
			Last:   lastPriceOJO,
			Vol:    volume,
		}

		for _, ticker := range tickerMap {
			p.setTickerPair(ticker, ticker.Symbol)
		}

		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "OJO", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, math.LegacyMustNewDecFromStr(lastPriceAtom), prices[ATOMUSDT].Price)
		require.Equal(t, math.LegacyMustNewDecFromStr(volume), prices[ATOMUSDT].Volume)
		require.Equal(t, math.LegacyMustNewDecFromStr(lastPriceOJO), prices[OJOUSDT].Price)
		require.Equal(t, math.LegacyMustNewDecFromStr(volume), prices[OJOUSDT].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, _ := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Empty(t, prices)
	})
}

func TestGateCurrencyPairToGatePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	GateSymbol := currencyPairToGatePair(cp)
	require.Equal(t, GateSymbol, "ATOM_USDT")
}

func TestGateProvider_getSubscriptionMsgs(t *testing.T) {
	provider := &GateProvider{}
	cps := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
	}
	subMsgs := provider.getSubscriptionMsgs(cps...)

	msg, _ := json.Marshal(subMsgs[0])
	require.Equal(t, "{\"method\":\"ticker.subscribe\",\"params\":[\"ATOM_USDT\"],\"id\":1}", string(msg))

	msg, _ = json.Marshal(subMsgs[1])
	require.Equal(t, "{\"method\":\"kline.subscribe\",\"params\":[\"ATOM_USDT\",60],\"id\":2}", string(msg))
}
