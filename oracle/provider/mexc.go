package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"

	"github.com/ojo-network/ojo/util/decmath"
)

const (
	mexcWSHost   = "wbs.mexc.com"
	mexcWSPath   = "/raw/ws"
	mexcRestHost = "https://www.mexc.com"
	mexcRestPath = "/open/api/v2/market/ticker"
)

var _ Provider = (*MexcProvider)(nil)

type (
	// MexcProvider defines an Oracle provider implemented by the Mexc public
	// API.
	//
	// REF: https://mxcdevelop.github.io/apidocs/spot_v2_en/#ticker-information
	// REF: https://mxcdevelop.github.io/apidocs/spot_v2_en/#k-line
	// REF: https://mxcdevelop.github.io/apidocs/spot_v2_en/#overview
	MexcProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	// MexcTickerResponse is the ticker price response object.
	MexcTickerResponse struct {
		Symbol map[string]MexcTicker `json:"data"` // e.x. ATOM_USDT
	}
	MexcTicker struct {
		LastPrice float64 `json:"p"` // Last price ex.: 0.0025
		Volume    float64 `json:"v"` // Total traded base asset volume ex.: 1000
	}

	// MexcCandle is the candle websocket response object.
	MexcCandleResponse struct {
		Symbol   string     `json:"symbol"` // Symbol ex.: ATOM_USDT
		Metadata MexcCandle `json:"data"`   // Metadata for candle
	}
	MexcCandle struct {
		Close     float64 `json:"c"` // Price at close
		TimeStamp int64   `json:"t"` // Close time in unix epoch ex.: 1645756200000
		Volume    float64 `json:"v"` // Volume during period
	}

	// MexcCandleSubscription Msg to subscribe all the candle channels.
	MexcCandleSubscription struct {
		OP       string `json:"op"`       // kline
		Symbol   string `json:"symbol"`   // streams to subscribe ex.: atom_usdt
		Interval string `json:"interval"` // Min1、Min5、Min15、Min30
	}

	// MexcTickerSubscription Msg to subscribe all the ticker channels.
	MexcTickerSubscription struct {
		OP string `json:"op"` // kline
	}

	// MexcPairSummary defines the response structure for a Mexc pair
	// summary.
	MexcPairSummary struct {
		Data []MexcPairData `json:"data"`
	}

	// MexcPairData defines the data response structure for an Mexc pair.
	MexcPairData struct {
		Symbol string `json:"symbol"`
	}
)

func NewMexcProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*MexcProvider, error) {
	if (endpoints.Name) != ProviderMexc {
		endpoints = Endpoint{
			Name:      ProviderMexc,
			Rest:      mexcRestHost,
			Websocket: mexcWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   mexcWSPath,
	}

	mexcLogger := logger.With().Str("provider", "mexc").Logger()

	provider := &MexcProvider{
		logger:     mexcLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(mexcLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToMexcPair)

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
		defaultPingDuration,
		websocket.PingMessage,
		mexcLogger,
	)

	return provider, nil
}

func (p *MexcProvider) StartConnections() {
	p.wsc.StartConnections()
}

func (p *MexcProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(cps)+1)
	for _, cp := range cps {
		mexcPair := currencyPairToMexcPair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newMexcCandleSubscriptionMsg(mexcPair))
	}
	subscriptionMsgs = append(subscriptionMsgs, newMexcTickerSubscriptionMsg())
	return subscriptionMsgs
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *MexcProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *MexcProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	var (
		tickerResp MexcTickerResponse
		tickerErr  error
		candleResp MexcCandleResponse
		candleErr  error
	)

	tickerErr = json.Unmarshal(bz, &tickerResp)
	for _, cp := range p.subscribedPairs {
		mexcPair := currencyPairToMexcPair(cp)
		if tickerResp.Symbol[mexcPair].LastPrice != 0 {
			p.setTickerPair(
				tickerResp.Symbol[mexcPair],
				mexcPair,
			)
			telemetryWebsocketMessage(ProviderMexc, MessageTypeTicker)
			return
		}
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if candleResp.Metadata.Close != 0 {
		p.setCandlePair(candleResp.Metadata, candleResp.Symbol)
		telemetryWebsocketMessage(ProviderMexc, MessageTypeCandle)
		return
	}

	if tickerErr != nil || candleErr != nil {
		p.logger.Error().
			Int("length", len(bz)).
			AnErr("ticker", tickerErr).
			AnErr("candle", candleErr).
			Msg("mexc: Error on receive message")
	}
}

func (mt MexcTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := decmath.NewDecFromFloat(mt.LastPrice)
	if err != nil {
		return types.TickerPrice{}, err
	}
	volume, err := decmath.NewDecFromFloat(mt.Volume)
	if err != nil {
		return types.TickerPrice{}, err
	}

	ticker := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return ticker, nil
}

func (mc MexcCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := decmath.NewDecFromFloat(mc.Close)
	if err != nil {
		return types.CandlePrice{}, err
	}
	volume, err := decmath.NewDecFromFloat(mc.Volume)
	if err != nil {
		return types.CandlePrice{}, err
	}
	candle := types.CandlePrice{
		Price:  close,
		Volume: volume,
		// convert seconds -> milli
		TimeStamp: SecondsToMilli(mc.TimeStamp),
	}
	return candle, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *MexcProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *MexcProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + mexcRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary MexcPairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary.Data))
	for _, pairName := range pairsSummary.Data {
		availablePairs[strings.ToUpper(strings.ReplaceAll(pairName.Symbol, "_", ""))] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToMexcPair receives a currency pair and return mexc
// ticker symbol atomusdt@ticker.
func currencyPairToMexcPair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + "_" + cp.Quote)
}

// newMexcCandleSubscriptionMsg returns a new candle subscription Msg.
func newMexcCandleSubscriptionMsg(param string) MexcCandleSubscription {
	return MexcCandleSubscription{
		OP:       "sub.kline",
		Symbol:   param,
		Interval: "Min1",
	}
}

// newMexcTickerSubscriptionMsg returns a new ticker subscription Msg.
func newMexcTickerSubscriptionMsg() MexcTickerSubscription {
	return MexcTickerSubscription{
		OP: "sub.overview",
	}
}
