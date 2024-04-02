package provider

import (
	"context"
	"testing"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestCryptoProvider_GetTickerPrices(t *testing.T) {
	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		ATOMUSDT,
	)
	require.NoError(t, err)

	t.Run("valid_request_single_ticker", func(t *testing.T) {
		lastPrice := math.LegacyMustNewDecFromStr("34.69000000")
		volume := math.LegacyMustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]types.TickerPrice{}
		tickerMap["ATOM_USDT"] = types.TickerPrice{
			Price:  lastPrice,
			Volume: volume,
		}

		p.tickers = tickerMap

		prices, err := p.GetTickerPrices(ATOMUSDT)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		require.Equal(t, lastPrice, prices[ATOMUSDT].Price)
		require.Equal(t, volume, prices[ATOMUSDT].Volume)
	})

	t.Run("valid_request_multi_ticker", func(t *testing.T) {
		lastPriceAtom := math.LegacyMustNewDecFromStr("34.69000000")
		lastPriceLuna := math.LegacyMustNewDecFromStr("41.35000000")
		volume := math.LegacyMustNewDecFromStr("2396974.02000000")

		tickerMap := map[string]types.TickerPrice{}
		tickerMap["ATOM_USDT"] = types.TickerPrice{
			Price:  lastPriceAtom,
			Volume: volume,
		}

		tickerMap["LUNA_USDT"] = types.TickerPrice{
			Price:  lastPriceLuna,
			Volume: volume,
		}

		p.tickers = tickerMap
		prices, err := p.GetTickerPrices(
			ATOMUSDT,
			LUNAUSDT,
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

func TestCryptoProvider_GetCandlePrices(t *testing.T) {
	p, err := NewCryptoProvider(
		context.TODO(),
		zerolog.Nop(),
		Endpoint{},
		ATOMUSDT,
	)
	require.NoError(t, err)

	t.Run("valid_request_single_candle", func(t *testing.T) {
		price := "34.689998626708984000"
		volume := "2396974.000000000000000000"
		timeStamp := int64(1000000)

		candle := CryptoCandle{
			Volume:    volume,
			Close:     price,
			Timestamp: timeStamp,
		}

		p.setCandlePair(candle, "ATOM_USDT")

		prices, err := p.GetCandlePrices(ATOMUSDT)
		require.NoError(t, err)
		require.Len(t, prices, 1)
		priceDec, _ := math.LegacyNewDecFromStr(price)
		volumeDec, _ := math.LegacyNewDecFromStr(volume)

		require.Equal(t, priceDec, prices[ATOMUSDT][0].Price)
		require.Equal(t, volumeDec, prices[ATOMUSDT][0].Volume)
		require.Equal(t, timeStamp, prices[ATOMUSDT][0].TimeStamp)
	})

	t.Run("invalid_request_invalid_candle", func(t *testing.T) {
		prices, _ := p.GetCandlePrices(types.CurrencyPair{Base: "FOO", Quote: "BAR"})
		require.Empty(t, prices)
	})
}

func TestCryptoCurrencyPairToCryptoPair(t *testing.T) {
	cp := ATOMUSDT
	cryptoSymbol := currencyPairToCryptoPair(cp)
	require.Equal(t, cryptoSymbol, "ATOM_USDT")
}
