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

func TestBinanceProvider_GetTickerPrices(t *testing.T) {
	p, err := NewBinanceProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		true,
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"

		tickerMap := map[string]BinanceTicker{}
		tickerMap["ATOMUSDT"] = BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: lastPrice,
			Volume:    volume,
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
		lastPriceLuna := "41.35000000"
		volume := "2396974.02000000"

		tickerMap := map[string]BinanceTicker{}
		tickerMap["ATOMUSDT"] = BinanceTicker{
			Symbol:    "ATOMUSDT",
			LastPrice: lastPriceAtom,
			Volume:    volume,
		}

		tickerMap["LUNAUSDT"] = BinanceTicker{
			Symbol:    "LUNAUSDT",
			LastPrice: lastPriceLuna,
			Volume:    volume,
		}

		for _, ticker := range tickerMap {
			p.setTickerPair(ticker, ticker.Symbol)
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

func TestBinanceCurrencyPairToBinancePair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	binanceSymbol := currencyPairToBinanceTickerPair(cp)
	require.Equal(t, binanceSymbol, "atomusdt@ticker")
}

func TestBinanceProvider_getSubscriptionMsgs(t *testing.T) {
	provider := &BinanceProvider{
		priceStore: newPriceStore(zerolog.Nop()),
	}
	cps := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
	}

	subMsgs := provider.getSubscriptionMsgs(cps...)

	msg, _ := json.Marshal(subMsgs[0])
	require.Equal(t, "{\"method\":\"SUBSCRIBE\",\"params\":[\"atomusdt@ticker\"],\"id\":1}", string(msg))

	msg, _ = json.Marshal(subMsgs[1])
	require.Equal(t, "{\"method\":\"SUBSCRIBE\",\"params\":[\"atomusdt@kline_1m\"],\"id\":1}", string(msg))
}
