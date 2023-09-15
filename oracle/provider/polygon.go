package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

const (
	polygonWSHost          = "socket.polygon.io"
	polygonWSPath          = "/forex"
	polygonRestHost        = "https://api.polygon.io"
	polygonRestPath        = "/v3/reference/tickers?market=fx&active=true&apikey="
	polygonLimitOne        = "&limit=1000"
	polygonLimitTwo        = "&limit=360"
	polygonOrderOne        = "&order=asc"
	polygonOrderTwo        = "&order=desc"
	polygonStatusEvent     = "status"
	polygonAggregatesEvent = "CA"
)

var _ Provider = (*PolygonProvider)(nil)

type (
	// PolygonProvider defines an Oracle provider implemented by the polygon.io
	// API.
	//
	// REF: https://polygon.io/docs/forex/getting-started
	PolygonProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	// Status response send back when connecting and authenticating with polygon's
	// websocket API.
	PolygonStatusResponse struct {
		EV      string `json:"ev"`      // Event type
		Message string `json:"message"` // ex.: "Connected Successfully"
	}

	// Real-time per-minute forex aggregates for a given forex pair.
	PolygonAggregatesResponse struct {
		EV        string  `json:"ev"`   // Event type
		Pair      string  `json:"pair"` // ex.: USD/EUR
		Close     float64 `json:"c"`    // Rate at close
		Volume    float64 `json:"v"`    // Volume during 1 minute interval
		Timestamp int64   `json:"e"`    // Endtime of candle (Unix milliseconds)
	}

	PolygonSubscriptionMsg struct {
		Action string `json:"action"` // ex.: subscribe
		Params string `json:"params"` // ex.: CA.EUR/USD,CA.JPY/USD
	}

	// Response returns all tickers available to be subsribed to.
	PolygonTickersResponse struct {
		Result []PolygonTicker `json:"results"`
	}
	PolygonTicker struct {
		Ticker string `json:"ticker"` // ex: C.EURUSD
	}
)

func NewPolygonProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*PolygonProvider, error) {
	if endpoints.Name != ProviderPolygon {
		endpoints = Endpoint{
			Name:      ProviderPolygon,
			Rest:      polygonRestHost,
			Websocket: polygonWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   polygonWSPath,
	}

	polygonLogger := logger.With().Str("provider", "polygon").Logger()

	provider := &PolygonProvider{
		logger:     polygonLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(polygonLogger),
	}
	provider.priceStore.setCurrencyPairToTickerAndCandlePair(currencyPairToPolygonPair)

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
		polygonLogger,
	)

	return provider, nil
}

func (p *PolygonProvider) StartConnections() {
	p.wsc.StartConnections()
}

func (p *PolygonProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(cps)*2+1)

	// Send authorization request first
	authMsg := PolygonSubscriptionMsg{
		Action: "auth",
		Params: p.endpoints.APIKey,
	}
	subscriptionMsgs = append(subscriptionMsgs, authMsg)

	msg := newPolygonSubscriptionMsg(cps)
	subscriptionMsgs = append(subscriptionMsgs, msg)
	msg = newPolygonSubscriptionMsg(cps)
	subscriptionMsgs = append(subscriptionMsgs, msg)

	return subscriptionMsgs
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *PolygonProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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
		defaultPingDuration,
		websocket.PingMessage,
	)
	p.setSubscribedPairs(confirmedPairs...)
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p *PolygonProvider) GetAvailablePairs() (map[string]struct{}, error) {
	// request for first 1000 tickers (request limit)
	resp, err := http.Get(p.endpoints.Rest + polygonRestPath + p.endpoints.APIKey + polygonOrderOne + polygonLimitOne)
	if err != nil {
		return nil, err
	}
	var tickers PolygonTickersResponse
	if err := json.NewDecoder(resp.Body).Decode(&tickers); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// request for rest of the tickers
	resp, err = http.Get(p.endpoints.Rest + polygonRestPath + p.endpoints.APIKey + polygonOrderTwo + polygonLimitTwo)
	if err != nil {
		return nil, err
	}
	var tickersLeftover PolygonTickersResponse
	if err := json.NewDecoder(resp.Body).Decode(&tickersLeftover); err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	tickers.Result = append(tickers.Result, tickersLeftover.Result...)

	availablePairs := make(map[string]struct{}, len(tickers.Result))
	for _, pair := range tickers.Result {
		if len(pair.Ticker) != 8 {
			continue
		}

		cp := types.CurrencyPair{
			Base:  pair.Ticker[2:5],
			Quote: pair.Ticker[5:8],
		}

		availablePairs[strings.ToUpper(cp.String())] = struct{}{}
	}

	return availablePairs, nil
}

func (p *PolygonProvider) messageReceived(messageType int, _ *WebsocketConnection, bz []byte) {
	if messageType != websocket.TextMessage {
		return
	}

	var (
		statusResp     []PolygonStatusResponse
		statusErr      error
		aggregatesResp []PolygonAggregatesResponse
		aggregatesErr  error
	)

	statusErr = json.Unmarshal(bz, &statusResp)
	if statusResp[0].EV == polygonStatusEvent {
		p.logger.Debug().Str("status msg received: ", statusResp[0].Message)
		return
	}

	aggregatesErr = json.Unmarshal(bz, &aggregatesResp)
	if aggregatesResp[0].EV == polygonAggregatesEvent {
		p.setTickerPair(aggregatesResp[0], aggregatesResp[0].Pair)
		p.setCandlePair(aggregatesResp[0], aggregatesResp[0].Pair)
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("status", statusErr).
		AnErr("aggregates", aggregatesErr).
		Msg("Error on receive message")
}

func (par PolygonAggregatesResponse) toTickerPrice() (types.TickerPrice, error) {
	return types.NewTickerPrice(
		fmt.Sprintf("%f", par.Close),
		fmt.Sprintf("%f", par.Volume),
	)
}

func (par PolygonAggregatesResponse) toCandlePrice() (types.CandlePrice, error) {
	return types.NewCandlePrice(
		fmt.Sprintf("%f", par.Close),
		fmt.Sprintf("%f", par.Volume),
		par.Timestamp,
	)
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *PolygonProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// currencyPairToPolygonPair receives a currency pair and returns a polygon
// ticker symbol i.e: EUR/USD
func currencyPairToPolygonPair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + "/" + cp.Quote)
}

// currencyPairsToPolygonPairs receives a list of currency pairs and returns
// the polygon multi-ticker symbol for subscribing to multiple pairs.
// i.e: "CA.EUR/USD,CA.JPY/USD"
func currencyPairsToPolygonPairs(cps []types.CurrencyPair) (pairs string) {
	for i, cp := range cps {
		pair := strings.ToUpper(polygonAggregatesEvent + "." + cp.Base + "/" + cp.Quote)
		if i != len(cps)-1 {
			pair += ","
		}
		pairs += pair
	}
	return pairs
}

// newPolygonSubscriptionMsg returns a new subscription Msg.
func newPolygonSubscriptionMsg(cps []types.CurrencyPair) PolygonSubscriptionMsg {
	return PolygonSubscriptionMsg{
		Action: "subscribe",
		Params: currencyPairsToPolygonPairs(cps),
	}
}
