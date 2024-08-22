package provider

import (
	"context"
	"encoding/json"
	"fmt"
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
	curveWSHost   = "api.eth-api.prod.ojo.network"
	curveWSPath   = "/curve/ws"
	curveWSScheme = "wss"
	curveRestHost = "https://api.eth-api.prod.ojo.network"
	curveRestPath = "/curve/assetpairs"
	curveAckMsg   = "ack"
)

var _ Provider = (*CurveProvider)(nil)

type (
	// CurveProvider defines an Oracle provider implemented by OJO's
	// Curve API.
	//
	// REF: https://github.com/ojo-network/ehereum-api
	CurveProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	CurveTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	CurveCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// CurvePairsSummary defines the response structure for an Curve pairs
	// summary.
	CurvePairsSummary struct {
		Data []CurvePairData `json:"data"`
	}

	// CurvePairData defines the data response structure for an Curve pair.
	CurvePairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewCurveProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*CurveProvider, error) {
	if endpoints.Name != ProviderEthCurve {
		endpoints = Endpoint{
			Name:      ProviderEthCurve,
			Rest:      curveRestHost,
			Websocket: curveWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: curveWSScheme,
		Host:   endpoints.Websocket,
		Path:   curveWSPath,
	}

	curveLogger := logger.With().Str("provider", "curve").Logger()

	provider := &CurveProvider{
		wsURL:      wsURL,
		logger:     curveLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(curveLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToCurvePair)

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
		[]interface{}{""},
		provider.messageReceived,
		defaultPingDuration,
		websocket.PingMessage,
		curveLogger,
	)

	return provider, nil
}

func (p *CurveProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *CurveProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	confirmedPairs, err := ConfirmPairAvailability(
		p,
		p.endpoints.Name,
		p.logger,
		cps...,
	)
	if err != nil {
		return
	}

	p.setSubscribedPairs(confirmedPairs...)
}

func (p *CurveProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == curveAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  CurveTicker
		tickerErr   error
		candleResp  []CurveCandle
		candleErr   error
	)

	messageErr = json.Unmarshal(bz, &messageResp)
	if messageErr != nil {
		p.logger.Error().
			Int("length", len(bz)).
			AnErr("message", messageErr).
			Msg("Error on receive message")
	}

	// Check the response for currency pairs that the provider is subscribed
	// to and determine whether it is a ticker or candle.
	for _, pair := range p.subscribedPairs {
		curvePair := currencyPairToCurvePair(pair)
		if msg, ok := messageResp[curvePair]; ok {
			switch v := msg.(type) {
			// ticker response
			case map[string]interface{}:
				tickerString, _ := json.Marshal(v)
				tickerErr = json.Unmarshal(tickerString, &tickerResp)
				if tickerErr != nil {
					p.logger.Error().
						Int("length", len(bz)).
						AnErr("ticker", tickerErr).
						Msg("Error on receive message")
					continue
				}
				p.setTickerPair(
					tickerResp,
					curvePair,
				)
				telemetryWebsocketMessage(ProviderEthCurve, MessageTypeTicker)
				continue

			// candle response
			case []interface{}:
				// use latest candlestick in list if there is one
				if len(v) == 0 {
					continue
				}
				candleString, _ := json.Marshal(v)
				candleErr = json.Unmarshal(candleString, &candleResp)
				if candleErr != nil {
					p.logger.Error().
						Int("length", len(bz)).
						AnErr("candle", candleErr).
						Msg("Error on receive message")
					continue
				}
				for _, singleCandle := range candleResp {
					p.setCandlePair(
						singleCandle,
						curvePair,
					)
				}
				telemetryWebsocketMessage(ProviderEthCurve, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o CurveTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := math.LegacyNewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("curve: failed to parse ticker price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("curve: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o CurveCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := math.LegacyNewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("curve: failed to parse candle price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("curve: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *CurveProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *CurveProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + curveRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []CurvePairData
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pair := range pairsSummary {
		cp := types.CurrencyPair{
			Base:  pair.Base,
			Quote: pair.Quote,
		}
		availablePairs[strings.ToUpper(cp.String())] = struct{}{}
	}

	return availablePairs, nil
}

// currencyPairToCurvePair receives a currency pair and return curve
// ticker symbol atomusdt@ticker.
func currencyPairToCurvePair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
