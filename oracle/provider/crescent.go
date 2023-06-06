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
)

const (
	crescentV2WSHost   = "api.cresc-api.prod.ojo.network"
	crescentV2WSPath   = "ws"
	crescentV2RestHost = "https://api.cresc-api.prod.ojo.network"
	crescentV2RestPath = "/assetpairs"
)

var _ Provider = (*CrescentProvider)(nil)

type (
	// CrescentProvider defines an Oracle provider implemented by OJO's
	// Crescent API.
	CrescentProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	CrescentTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	CrescentCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// CrescentPairsSummary defines the response structure for an Crescent pairs
	// summary.
	CrescentPairsSummary struct {
		Data []CrescentPairData `json:"data"`
	}

	// CrescentPairData defines the data response structure for an Crescent pair.
	CrescentPairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewCrescentProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*CrescentProvider, error) {
	if endpoints.Name != ProviderCrescent {
		endpoints = Endpoint{
			Name:      ProviderCrescent,
			Rest:      crescentV2RestHost,
			Websocket: crescentV2WSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   crescentV2WSPath,
	}

	crescentV2Logger := logger.With().Str("provider", "crescent").Logger()

	provider := &CrescentProvider{
		wsURL:      wsURL,
		logger:     crescentV2Logger,
		endpoints:  endpoints,
		priceStore: newPriceStore(crescentV2Logger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToCrescentPair)

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
		crescentV2Logger,
	)

	return provider, nil
}

func (p *CrescentProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *CrescentProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *CrescentProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == "ack" {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  CrescentTicker
		tickerErr   error
		candleResp  []CrescentCandle
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
		crescentPair := currencyPairToCrescentPair(pair)
		if msg, ok := messageResp[crescentPair]; ok {
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
					crescentPair,
				)
				telemetryWebsocketMessage(ProviderCrescent, MessageTypeTicker)
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
						crescentPair,
					)
				}
				telemetryWebsocketMessage(ProviderCrescent, MessageTypeCandle)
				continue
			}
		}
	}
}

func (ct CrescentTicker) toTickerPrice() (types.TickerPrice, error) {
	return types.NewTickerPrice(
		ct.Price,
		ct.Volume,
	)
}

func (cc CrescentCandle) toCandlePrice() (types.CandlePrice, error) {
	return types.NewCandlePrice(
		cc.Close,
		cc.Volume,
		cc.EndTime,
	)
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *CrescentProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *CrescentProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + crescentV2RestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []CrescentPairData
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

// currencyPairToCrescentPair receives a currency pair and return crescent
// ticker symbol atomusdt@ticker.
func currencyPairToCrescentPair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
