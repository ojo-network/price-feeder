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
	camelotWSHost   = "api.eth-api.prod.ojo.network"
	camelotWSPath   = "/camelot/ws"
	camelotWSScheme = "wss"
	camelotRestHost = "https://api.eth-api.prod.ojo.network"
	camelotRestPath = "/camelot/assetpairs"
	camelotAckMsg   = "ack"
)

var _ Provider = (*CamelotProvider)(nil)

type (
	// CamelotProvider defines an Oracle provider implemented by OJO's
	// Camelot API.
	//
	// REF: https://github.com/ojo-network/ehereum-api
	CamelotProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	CamelotTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	CamelotCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// CamelotPairsSummary defines the response structure for an Camelot pairs
	// summary.
	CamelotPairsSummary struct {
		Data []CamelotPairData `json:"data"`
	}

	// CamelotPairData defines the data response structure for an Camelot pair.
	CamelotPairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewCamelotProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*CamelotProvider, error) {
	if endpoints.Name != ProviderEthCamelot {
		endpoints = Endpoint{
			Name:      ProviderEthCamelot,
			Rest:      camelotRestHost,
			Websocket: camelotWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: camelotWSScheme,
		Host:   endpoints.Websocket,
		Path:   camelotWSPath,
	}

	camelotLogger := logger.With().Str("provider", "camelot").Logger()

	provider := &CamelotProvider{
		wsURL:      wsURL,
		logger:     camelotLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(camelotLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToCamelotPair)

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
		camelotLogger,
	)

	return provider, nil
}

func (p *CamelotProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *CamelotProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *CamelotProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == camelotAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  CamelotTicker
		tickerErr   error
		candleResp  []CamelotCandle
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
		camelotPair := currencyPairToCamelotPair(pair)
		if msg, ok := messageResp[camelotPair]; ok {
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
					camelotPair,
				)
				telemetryWebsocketMessage(ProviderEthCamelot, MessageTypeTicker)
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
						camelotPair,
					)
				}
				telemetryWebsocketMessage(ProviderEthCamelot, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o CamelotTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := math.LegacyNewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("camelot: failed to parse ticker price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("camelot: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o CamelotCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := math.LegacyNewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("camelot: failed to parse candle price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("camelot: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *CamelotProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *CamelotProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + camelotRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []CamelotPairData
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

// currencyPairToCamelotPair receives a currency pair and return camelot
// ticker symbol atomusdt@ticker.
func currencyPairToCamelotPair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
