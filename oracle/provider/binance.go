package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	binanceWSHost     = "stream.binance.com:9443"
	binanceUSWSHost   = "stream.binance.us:9443"
	binanceWSPath     = "/ws/ojostream"
	binanceRestHost   = "https://api1.binance.com"
	binanceRestUSHost = "https://api.binance.us"
	binanceRestPath   = "/api/v3/ticker/price"
)

var _ Provider = (*BinanceProvider)(nil)

type (
	// BinanceProvider defines an Oracle provider implemented by the Binance public
	// API.
	//
	// REF: https://binance-docs.github.io/apidocs/spot/en/#individual-symbol-mini-ticker-stream
	// REF: https://binance-docs.github.io/apidocs/spot/en/#kline-candlestick-streams
	BinanceProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	// BinanceTicker ticker price response. https://pkg.go.dev/encoding/json#Unmarshal
	// Unmarshal matches incoming object keys to the keys used by Marshal (either the
	// struct field name or its tag), preferring an exact match but also accepting a
	// case-insensitive match. C field which is Statistics close time is not used, but
	// it avoids to implement specific UnmarshalJSON.
	BinanceTicker struct {
		Symbol    string `json:"s"` // Symbol ex.: BTCUSDT
		LastPrice string `json:"c"` // Last price ex.: 0.0025
		Volume    string `json:"v"` // Total traded base asset volume ex.: 1000
		C         uint64 `json:"C"` // Statistics close time
	}

	// BinanceCandleMetadata candle metadata used to compute tvwap price.
	BinanceCandleMetadata struct {
		Close     string `json:"c"` // Price at close
		TimeStamp int64  `json:"T"` // Close time in unix epoch ex.: 1645756200000
		Volume    string `json:"v"` // Volume during period
	}

	// BinanceCandle candle binance websocket channel "kline_1m" response.
	BinanceCandle struct {
		Symbol   string                `json:"s"` // Symbol ex.: BTCUSDT
		Metadata BinanceCandleMetadata `json:"k"` // Metadata for candle
	}

	// BinanceSubscribeMsg Msg to subscribe all the tickers channels.
	BinanceSubscriptionMsg struct {
		Method string   `json:"method"` // SUBSCRIBE/UNSUBSCRIBE
		Params []string `json:"params"` // streams to subscribe ex.: usdtatom@ticker
		ID     uint16   `json:"id"`     // identify messages going back and forth
	}

	// BinanceSubscriptionResp the response structure for a binance subscription response
	BinanceSubscriptionResp struct {
		Result string `json:"result"`
		ID     uint16 `json:"id"`
	}

	// BinancePairSummary defines the response structure for a Binance pair
	// summary.
	BinancePairSummary struct {
		Symbol string `json:"symbol"`
	}
)

func NewBinanceProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	binanceUS bool,
	pairs ...types.CurrencyPair,
) (*BinanceProvider, error) {
	if (endpoints.Name) != ProviderBinance {
		if !binanceUS {
			endpoints = Endpoint{
				Name:      ProviderBinance,
				Rest:      binanceRestHost,
				Websocket: binanceWSHost,
			}
		} else {
			endpoints = Endpoint{
				Name:      ProviderBinanceUS,
				Rest:      binanceRestUSHost,
				Websocket: binanceUSWSHost,
			}
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   binanceWSPath,
	}

	binanceLogger := logger.With().Str("provider", string(ProviderBinance)).Logger()

	provider := &BinanceProvider{
		logger:     binanceLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(binanceLogger),
	}

	confirmedPairs, err := ConfirmPairAvailability(
		provider,
		provider.endpoints.Name,
		provider.logger,
		pairs...,
	)
	if err != nil {
		return nil, err
	}

	provider.setSubscribedPairs(confirmedPairs...)

	provider.wsc = NewWebsocketController(
		ctx,
		endpoints.Name,
		wsURL,
		provider.getSubscriptionMsgs(confirmedPairs...),
		provider.messageReceived,
		disabledPingDuration,
		websocket.PingMessage,
		binanceLogger,
	)

	return provider, nil
}

func (p *BinanceProvider) StartConnections() {
	p.wsc.StartConnections()
}

func (p *BinanceProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(p.subscribedPairs)*2)
	for _, cp := range cps {
		binanceTickerPair := currencyPairToBinanceTickerPair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newBinanceSubscriptionMsg(binanceTickerPair))

		binanceCandlePair := currencyPairToBinanceCandlePair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newBinanceSubscriptionMsg(binanceCandlePair))
	}
	return subscriptionMsgs
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *BinanceProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	newPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if _, ok := p.subscribedPairs[cp.String()]; !ok {
			newPairs = append(newPairs, cp)
		}
	}

	confirmedPairs, err := ConfirmPairAvailability(
		p,
		p.endpoints.Name,
		p.logger,
		newPairs...,
	)
	if err != nil {
		return
	}

	newSubscriptionMsgs := p.getSubscriptionMsgs(confirmedPairs...)
	p.wsc.AddWebsocketConnection(
		newSubscriptionMsgs,
		p.messageReceived,
		disabledPingDuration,
		websocket.PingMessage,
	)
	p.setSubscribedPairs(confirmedPairs...)
}

func (p *BinanceProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	var (
		tickerResp       BinanceTicker
		tickerErr        error
		candleResp       BinanceCandle
		candleErr        error
		subscribeResp    BinanceSubscriptionResp
		subscribeRespErr error
	)

	tickerErr = json.Unmarshal(bz, &tickerResp)
	if len(tickerResp.LastPrice) != 0 {
		p.setTickerPair(tickerResp, tickerResp.Symbol)
		telemetryWebsocketMessage(ProviderBinance, MessageTypeTicker)
		return
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if len(candleResp.Metadata.Close) != 0 {
		p.setCandlePair(candleResp, candleResp.Symbol)
		telemetryWebsocketMessage(ProviderBinance, MessageTypeCandle)
		return
	}

	subscribeRespErr = json.Unmarshal(bz, &subscribeResp)
	if subscribeResp.ID == 1 {
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		AnErr("subscribeResp", subscribeRespErr).
		Msg("Error on receive message")
}

func (ticker BinanceTicker) toTickerPrice() (types.TickerPrice, error) {
	return types.NewTickerPrice(ticker.LastPrice, ticker.Volume)
}

func (candle BinanceCandle) toCandlePrice() (types.CandlePrice, error) {
	return types.NewCandlePrice(candle.Metadata.Close, candle.Metadata.Volume, candle.Metadata.TimeStamp)
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *BinanceProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + binanceRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []BinancePairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(pairName.Symbol)] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToBinanceTickerPair receives a currency pair and return binance
// ticker symbol atomusdt@ticker.
func currencyPairToBinanceTickerPair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@ticker")
}

// currencyPairToBinanceCandlePair receives a currency pair and return binance
// candle symbol atomusdt@kline_1m.
func currencyPairToBinanceCandlePair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@kline_1m")
}

// newBinanceSubscriptionMsg returns a new subscription Msg.
func newBinanceSubscriptionMsg(params ...string) BinanceSubscriptionMsg {
	return BinanceSubscriptionMsg{
		Method: "SUBSCRIBE",
		Params: params,
		ID:     1,
	}
}
