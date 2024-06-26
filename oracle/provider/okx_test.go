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

func TestOkxProvider_GetTickerPrices(t *testing.T) {
	p, err := NewOkxProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   lastPrice,
			Vol24h: volume,
		}

		for _, okxTicker := range syncMap {
			p.setTickerPair(okxTicker, okxTicker.OkxInstID.InstID)
		}

		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, math.LegacyMustNewDecFromStr(lastPrice), prices[ATOMUSDT].Price)
		require.Equal(t, math.LegacyMustNewDecFromStr(volume), prices[ATOMUSDT].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := "34.69000000"
		lastPriceLuna := "41.35000000"
		volume := "2396974.02000000"

		syncMap := map[string]OkxTickerPair{}
		syncMap["ATOM-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "ATOM-USDT",
			},
			Last:   lastPriceAtom,
			Vol24h: volume,
		}

		syncMap["LUNA-USDT"] = OkxTickerPair{
			OkxInstID: OkxInstID{
				InstID: "LUNA-USDT",
			},
			Last:   lastPriceLuna,
			Vol24h: volume,
		}

		for _, okxTicker := range syncMap {
			p.setTickerPair(okxTicker, okxTicker.OkxInstID.InstID)
		}

		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "LUNA", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, math.LegacyMustNewDecFromStr(lastPriceAtom), prices[ATOMUSDT].Price)
		require.Equal(t, math.LegacyMustNewDecFromStr(volume), prices[ATOMUSDT].Volume)
		require.Equal(t, math.LegacyMustNewDecFromStr(lastPriceLuna), prices[LUNAUSDT].Price)
		require.Equal(t, math.LegacyMustNewDecFromStr(volume), prices[LUNAUSDT].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, _ := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Empty(t, prices)
	})
}

func TestOkxCurrencyPairToOkxPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	okxSymbol := currencyPairToOkxPair(cp)
	require.Equal(t, okxSymbol, "ATOM-USDT")
}

func TestOkxProvider_getSubscriptionMsgs(t *testing.T) {
	provider := &OkxProvider{}
	cps := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
	}
	subMsgs := provider.getSubscriptionMsgs(cps...)

	msg, _ := json.Marshal(subMsgs[0])
	require.Equal(t, "{\"op\":\"subscribe\",\"args\":[{\"channel\":\"candle1m\",\"instId\":\"ATOM-USDT\"}]}", string(msg))

	msg, _ = json.Marshal(subMsgs[1])
	require.Equal(t, "{\"op\":\"subscribe\",\"args\":[{\"channel\":\"tickers\",\"instId\":\"ATOM-USDT\"}]}", string(msg))
}
