package provider

import (
	"fmt"
	"net/http"
	"time"

	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	defaultTimeout       = 10 * time.Second
	providerCandlePeriod = 5 * time.Minute

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
)

var (
	ping = []byte("ping")

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

// preventRedirect avoid any redirect in the http.Client the request call
// will not return an error, but a valid response with redirect response code.
func preventRedirect(_ *http.Request, _ []*http.Request) error {
	return http.ErrUseLastResponse
}

func newDefaultHTTPClient() *http.Client {
	return newHTTPClientWithTimeout(defaultTimeout)
}

func newHTTPClientWithTimeout(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: preventRedirect,
	}
}

// PastUnixTime returns a millisecond timestamp that represents the unix time
// minus t.
func PastUnixTime(t time.Duration) int64 {
	return time.Now().Add(t*-1).Unix() * int64(time.Second/time.Millisecond)
}

// SecondsToMilli converts seconds to milliseconds for our unix timestamps.
func SecondsToMilli(t int64) int64 {
	return t * int64(time.Second/time.Millisecond)
}

func checkHTTPStatus(resp *http.Response) error {
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	return nil
}
