package provider

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestBitgetProvider_GetTickerPrices(t *testing.T) {
	p, err := NewBitgetProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{Base: "BTC", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := "34.69000000"
		volume := "2396974.02000000"
		instId := "ATOMUSDT"

		bitgetTicker := BitgetTicker{
			Arg: BitgetSubscriptionArg{
				Channel: "tickers",
				InstID:  instId,
			},
			Data: []BitgetTickerData{
				{
					InstID: instId,
					Price:  lastPrice,
					Volume: volume,
				},
			},
		}
		p.setTickerPair(bitgetTicker, instId)

		prices, err := p.GetTickerPrices(ATOMUSDT)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr(lastPrice), prices[ATOMUSDT].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices[ATOMUSDT].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		atomInstID := "ATOMUSDT"
		atomLastPrice := "34.69000000"
		lunaInstID := "LUNAUSDT"
		lunaLastPrice := "41.35000000"
		volume := "2396974.02000000"

		tickerMap := map[string]BitgetTicker{}
		tickerMap[atomInstID] = BitgetTicker{
			Arg: BitgetSubscriptionArg{
				Channel: "tickers",
				InstID:  atomInstID,
			},
			Data: []BitgetTickerData{
				{
					InstID: atomInstID,
					Price:  atomLastPrice,
					Volume: volume,
				},
			},
		}
		tickerMap[lunaInstID] = BitgetTicker{
			Arg: BitgetSubscriptionArg{
				Channel: "tickers",
				InstID:  lunaInstID,
			},
			Data: []BitgetTickerData{
				{
					InstID: lunaInstID,
					Price:  lunaLastPrice,
					Volume: volume,
				},
			},
		}

		for _, bitgetTicker := range tickerMap {
			p.setTickerPair(bitgetTicker, bitgetTicker.Arg.InstID)
		}

		prices, err := p.GetTickerPrices(
			types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
			types.CurrencyPair{Base: "LUNA", Quote: "USDT"},
		)

		require.NoError(t, err)
		require.Len(t, prices, 2)
		require.Equal(t, sdk.MustNewDecFromStr(atomLastPrice), prices[ATOMUSDT].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices[ATOMUSDT].Volume)
		require.Equal(t, sdk.MustNewDecFromStr(lunaLastPrice), prices[LUNAUSDT].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices[LUNAUSDT].Volume)
	})

	t.Run("invalid_request_invalid_ticker", func(t *testing.T) {
		prices, err := p.GetTickerPrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.EqualError(t, err, "bitget has no ticker data for requested pairs: [FOOBAR]")
		require.Nil(t, prices)
	})
}

func TestBitgetProvider_GetCandlePrices(t *testing.T) {
	p, err := NewBitgetProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{Base: "ATOM", Quote: "USDT"},
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.689998626708984000"
		volume := "2396974.000000000000000000"
		timeStamp := int64(1000000)

		bitgetCandle := BitgetCandle{
			TimeStamp: timeStamp,
			Close:     price,
			Volume:    volume,
			Arg: BitgetSubscriptionArg{
				Channel: "candle15m",
				InstID:  "ATOMUSDT",
			},
		}
		p.setCandlePair(bitgetCandle, bitgetCandle.Arg.InstID)

		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "ATOM", Quote: "USDT"})
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, sdk.MustNewDecFromStr(price), prices[ATOMUSDT][0].Price)
		require.Equal(t, sdk.MustNewDecFromStr(volume), prices[ATOMUSDT][0].Volume)
		require.Equal(t, timeStamp, prices[ATOMUSDT][0].TimeStamp)
	})

	t.Run("invalid_request_invalid_candle", func(t *testing.T) {
		prices, err := p.GetCandlePrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.EqualError(t, err, "bitget has no candle data for requested pairs: [FOOBAR]")
		require.Nil(t, prices)
	})
}

func TestBitgetProvider_AvailablePairs(t *testing.T) {
	p, err := NewBitgetProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		types.CurrencyPair{},
	)
	require.NoError(t, err)

	pairs, err := p.GetAvailablePairs()
	require.NoError(t, err)

	require.NotEmpty(t, pairs)
}

func TestBitgetProvider_NewSubscriptionMsg(t *testing.T) {
	cps := []types.CurrencyPair{
		{
			Base: "ATOM", Quote: "USDT",
		},
		{
			Base: "FOO", Quote: "BAR",
		},
	}
	sub := newBitgetTickerSubscriptionMsg(cps)

	require.Equal(t, len(sub.Args), 2*len(cps))
	require.Equal(t, sub.Args[0].InstID, "ATOMUSDT")
	require.Equal(t, sub.Args[0].Channel, "ticker")
	require.Equal(t, sub.Args[1].InstID, "ATOMUSDT")
	require.Equal(t, sub.Args[1].Channel, "candle5m")
	require.Equal(t, sub.Args[2].InstID, "FOOBAR")
	require.Equal(t, sub.Args[2].Channel, "ticker")
	require.Equal(t, sub.Args[3].InstID, "FOOBAR")
	require.Equal(t, sub.Args[3].Channel, "candle5m")
}
