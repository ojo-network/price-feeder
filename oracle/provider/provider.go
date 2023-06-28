package provider

import (
	"time"

	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	defaultTimeout = 10 * time.Second

<<<<<<< HEAD
	ProviderKraken    types.ProviderName = "kraken"
	ProviderBinance   types.ProviderName = "binance"
	ProviderBinanceUS types.ProviderName = "binanceus"
	ProviderOsmosisV2 types.ProviderName = "osmosisv2"
	ProviderHuobi     types.ProviderName = "huobi"
	ProviderOkx       types.ProviderName = "okx"
	ProviderGate      types.ProviderName = "gate"
	ProviderCoinbase  types.ProviderName = "coinbase"
	ProviderBitget    types.ProviderName = "bitget"
	ProviderMexc      types.ProviderName = "mexc"
	ProviderCrypto    types.ProviderName = "crypto"
	ProviderPolygon   types.ProviderName = "polygon"
	ProviderCrescent  types.ProviderName = "crescent"
	ProviderMock      types.ProviderName = "mock"
=======
	ProviderKraken     types.ProviderName = "kraken"
	ProviderBinance    types.ProviderName = "binance"
	ProviderBinanceUS  types.ProviderName = "binanceus"
	ProviderOsmosisV2  types.ProviderName = "osmosisv2"
	ProviderHuobi      types.ProviderName = "huobi"
	ProviderOkx        types.ProviderName = "okx"
	ProviderGate       types.ProviderName = "gate"
	ProviderCoinbase   types.ProviderName = "coinbase"
	ProviderBitget     types.ProviderName = "bitget"
	ProviderMexc       types.ProviderName = "mexc"
	ProviderCrypto     types.ProviderName = "crypto"
	ProviderPolygon    types.ProviderName = "polygon"
	ProviderCrescent   types.ProviderName = "crescent"
	ProviderEthUniswap types.ProviderName = "eth-uniswap"
	ProviderKujira     types.ProviderName = "kujira"
	ProviderMock       types.ProviderName = "mock"
>>>>>>> b6aa047 (feat: add kujira api provider (#157))
)

var (
	ping = []byte("ping")
)

type (
	// Provider defines an interface an exchange price provider must implement.
	Provider interface {
		// GetTickerPrices returns the tickerPrices based on the provided pairs.
		GetTickerPrices(...types.CurrencyPair) (types.CurrencyPairTickers, error)

		// GetCandlePrices returns the candlePrices based on the provided pairs.
		GetCandlePrices(...types.CurrencyPair) (types.CurrencyPairCandles, error)

		// GetAvailablePairs return all available pairs symbol to subscribe.
		GetAvailablePairs() (map[string]struct{}, error)

		// SubscribeCurrencyPairs sends subscription messages for the new currency
		// pairs and adds them to the providers subscribed pairs
		SubscribeCurrencyPairs(...types.CurrencyPair)

		// StartConnections starts the websocket connections.
		StartConnections()
	}

	// Endpoint defines an override setting in our config for the
	// hardcoded rest and websocket api endpoints.
	Endpoint struct {
		// Name of the provider, ex. "binance"
		Name types.ProviderName `toml:"name"`

		// Rest endpoint for the provider, ex. "https://api1.binance.com"
		Rest string `toml:"rest"`

		// Websocket endpoint for the provider, ex. "stream.binance.com:9443"
		Websocket string `toml:"websocket"`

		// APIKey for API Key protected endpoints
		APIKey string `toml:"apikey"`
	}
)

// PastUnixTime returns a millisecond timestamp that represents the unix time
// minus t.
func PastUnixTime(t time.Duration) int64 {
	return time.Now().Add(t*-1).Unix() * int64(time.Second/time.Millisecond)
}

// SecondsToMilli converts seconds to milliseconds for our unix timestamps.
func SecondsToMilli(t int64) int64 {
	return t * int64(time.Second/time.Millisecond)
}
