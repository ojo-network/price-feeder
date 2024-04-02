package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"cosmossdk.io/math"

	"github.com/gorilla/websocket"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

const (
	mexcWSHost   = "wbs.mexc.com"
	mexcWSPath   = "/ws"
	mexcRestHost = "https://api.mexc.com/"
	mexcRestPath = "/api/v3/ticker/price"
)

var _ Provider = (*MexcProvider)(nil)

type (
	// MexcProvider defines an Oracle provider implemented by the Mexc public
	// API.
	//
	// REF: https://mexcdevelop.github.io/apidocs/spot_v3_en/
	MexcProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	// MexcTickerResponse is the ticker price response object.
	MexcTickerResponse struct {
		Symbol   string     `json:"s"` // e.x. ATOMUSDT
		Metadata MexcTicker `json:"d"` // Metadata for ticker
	}
	MexcTicker struct {
		LastPrice string `json:"b"` // Best bid price ex.: 0.0025
		Volume    string `json:"B"` // Best bid qty ex.: 1000
	}

	// MexcCandle is the candle websocket response object.
	MexcCandleResponse struct {
		Symbol   string     `json:"s"` // Symbol ex.: ATOMUSDT
		Metadata MexcCandle `json:"d"` // Metadata for candle
	}
	MexcCandle struct {
		Data MexcCandleData `json:"k"`
	}
	MexcCandleData struct {
		Close     *big.Float `json:"c"` // Price at close
		TimeStamp int64      `json:"T"` // Close time in unix epoch ex.: 1645756200
		Volume    *big.Float `json:"v"` // Volume during period
	}

	// MexcCandleSubscription Msg to subscribe all the candle channels.
	MexcCandleSubscription struct {
		Method string   `json:"method"` // ex.: SUBSCRIPTION
		Params []string `json:"params"` // ex.: [spot@public.kline.v3.api@<symbol>@<interval>]
	}

	// MexcTickerSubscription Msg to subscribe all the ticker channels.
	MexcTickerSubscription struct {
		Method string   `json:"method"` // ex.: SUBSCRIPTION
		Params []string `json:"params"` // ex.: [spot@public.bookTicker.v3.api@<symbol>]
	}

	// MexcPairSummary defines the response structure for a Mexc pair
	// summary.
	MexcPairSummary []MexcPairData

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
	subscriptionMsgs := make([]interface{}, 0, len(cps)*2)
	mexcPairs := make([]string, 0, len(cps))
	for _, cp := range cps {
		mexcPairs = append(mexcPairs, currencyPairToMexcPair(cp))
	}
	subscriptionMsgs = append(subscriptionMsgs, newMexcCandleSubscriptionMsg(mexcPairs))
	subscriptionMsgs = append(subscriptionMsgs, newMexcTickerSubscriptionMsg(mexcPairs))
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
	if tickerResp.Metadata.LastPrice != "" {
		p.setTickerPair(tickerResp.Metadata, tickerResp.Symbol)
		telemetryWebsocketMessage(ProviderMexc, MessageTypeTicker)
		return
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if candleResp.Metadata.Data.Close != nil {
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
	price, err := math.LegacyNewDecFromStr(mt.LastPrice)
	if err != nil {
		return types.TickerPrice{}, err
	}
	volume, err := math.LegacyNewDecFromStr(mt.Volume)
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
	close, err := math.LegacyNewDecFromStr(mc.Data.Close.String())
	if err != nil {
		return types.CandlePrice{}, err
	}
	volume, err := math.LegacyNewDecFromStr(mc.Data.Volume.String())
	if err != nil {
		return types.CandlePrice{}, err
	}

	candle := types.CandlePrice{
		Price:  close,
		Volume: volume,
		// convert seconds -> milli
		TimeStamp: SecondsToMilli(mc.Data.TimeStamp),
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

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(pairName.Symbol)] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToMexcPair receives a currency pair and return mexc
// ticker symbol atomusdt@ticker.
func currencyPairToMexcPair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + cp.Quote)
}

// newMexcCandleSubscriptionMsg returns a new candle subscription Msg.
func newMexcCandleSubscriptionMsg(symbols []string) MexcCandleSubscription {
	params := make([]string, len(symbols))
	for i, symbol := range symbols {
		params[i] = fmt.Sprintf("spot@public.kline.v3.api@%s@Min1", symbol)
	}
	return MexcCandleSubscription{
		Method: "SUBSCRIPTION",
		Params: params,
	}
}

// newMexcTickerSubscriptionMsg returns a new ticker subscription Msg.
func newMexcTickerSubscriptionMsg(symbols []string) MexcTickerSubscription {
	params := make([]string, len(symbols))
	for i, symbol := range symbols {
		params[i] = fmt.Sprintf("spot@public.bookTicker.v3.api@%s", symbol)
	}
	return MexcTickerSubscription{
		Method: "SUBSCRIPTION",
		Params: params,
	}
}
