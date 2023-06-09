package provider

import "github.com/ojo-network/price-feeder/oracle/types"

var (
	OJOUSDT  = types.CurrencyPair{Base: "OJO", Quote: "USDT"}
	BTCUSDT  = types.CurrencyPair{Base: "BTC", Quote: "USDT"}
	ATOMUSDT = types.CurrencyPair{Base: "ATOM", Quote: "USDT"}
	LUNAUSDT = types.CurrencyPair{Base: "LUNA", Quote: "USDT"}

	ATOMUSDC = types.CurrencyPair{Base: "ATOM", Quote: "USDC"}

	OSMOATOM = types.CurrencyPair{Base: "OSMO", Quote: "ATOM"}
	BCREATOM = types.CurrencyPair{Base: "BCRE", Quote: "ATOM"}

	EURUSD = types.CurrencyPair{Base: "EUR", Quote: "USD"}
	JPYUSD = types.CurrencyPair{Base: "JPY", Quote: "USD"}
)
