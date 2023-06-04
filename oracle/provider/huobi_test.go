package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ojo-network/ojo/util/decmath"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestHuobiProvider_GetTickerPrices(t *testing.T) {
	p, err := NewHuobiProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		ATOMUSDT,
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := 34.69000000
		volume := 2396974.02000000

		tickerMap := map[string]HuobiTicker{}
		tickerMap["market.atomusdt.ticker"] = HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: lastPrice,
				Vol:       volume,
			},
		}

		for _, ticker := range tickerMap {
			p.setTickerPair(ticker, ticker.CH)
		}

		prices, err := p.GetTickerPrices(ATOMUSDT)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		dec, _ := decmath.NewDecFromFloat(lastPrice)
		require.Equal(t, dec, prices[ATOMUSDT].Price)
		dec, _ = decmath.NewDecFromFloat(volume)
		require.Equal(t, dec, prices[ATOMUSDT].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := 34.69000000
		lastPriceLuna := 41.35000000
		volume := 2396974.02000000

		tickerMap := map[string]HuobiTicker{}
		tickerMap["market.atomusdt.ticker"] = HuobiTicker{
			CH: "market.atomusdt.ticker",
			Tick: HuobiTick{
				LastPrice: lastPriceAtom,
				Vol:       volume,
			},
		}

		tickerMap["market.lunausdt.ticker"] = HuobiTicker{
			CH: "market.lunausdt.ticker",
			Tick: HuobiTick{
				LastPrice: lastPriceLuna,
				Vol:       volume,
			},
		}

		for _, ticker := range tickerMap {
			p.setTickerPair(ticker, ticker.CH)
		}

		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "LUNA", Quote: "USDT"},
		)
		require.NoError(t, err)
		require.Len(t, prices, 2)
		dec, _ := decmath.NewDecFromFloat(lastPriceAtom)
		require.Equal(t, dec, prices[ATOMUSDT].Price)

		dec, _ = decmath.NewDecFromFloat(volume)
		require.Equal(t, dec, prices[ATOMUSDT].Volume)
		dec, _ = decmath.NewDecFromFloat(lastPriceLuna)
		require.Equal(t, dec, prices[LUNAUSDT].Price)
		dec, _ = decmath.NewDecFromFloat(volume)
		require.Equal(t, dec, prices[LUNAUSDT].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.EqualError(t, err, "failed to get ticker price for market.foobar.ticker")
		require.Nil(t, prices)
	})
}

func TestHuobiCurrencyPairToHuobiPair(t *testing.T) {
	cp := types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	binanceSymbol := currencyPairToHuobiTickerPair(cp)
	require.Equal(t, binanceSymbol, "market.atomusdt.ticker")
}

func TestHuobiProvider_getSubscriptionMsgs(t *testing.T) {
	provider := &HuobiProvider{}
	cps := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
	}
	subMsgs := provider.getSubscriptionMsgs(cps...)

	msg, _ := json.Marshal(subMsgs[0])
	require.Equal(t, "{\"sub\":\"market.atomusdt.ticker\"}", string(msg))

	msg, _ = json.Marshal(subMsgs[1])
	require.Equal(t, "{\"sub\":\"market.atomusdt.kline.1min\"}", string(msg))
}
