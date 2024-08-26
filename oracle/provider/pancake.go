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
	pancakeWSHost   = "api.eth-api.prod.ojo.network"
	pancakeWSPath   = "/pancake/ws"
	pancakeWSScheme = "wss"
	pancakeRestHost = "https://api.eth-api.prod.ojo.network"
	pancakeRestPath = "/pancake/assetpairs"
	pancakeAckMsg   = "ack"
)

var _ Provider = (*PancakeProvider)(nil)

type (
	// PancakeProvider defines an Oracle provider implemented by OJO's
	// Pancake API.
	//
	// REF: https://github.com/ojo-network/ehereum-api
	PancakeProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	PancakeTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	PancakeCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// PancakePairsSummary defines the response structure for an Pancake pairs
	// summary.
	PancakePairsSummary struct {
		Data []PancakePairData `json:"data"`
	}

	// PancakePairData defines the data response structure for an Pancake pair.
	PancakePairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewPancakeProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*PancakeProvider, error) {
	if endpoints.Name != ProviderEthPancake {
		endpoints = Endpoint{
			Name:      ProviderEthPancake,
			Rest:      pancakeRestHost,
			Websocket: pancakeWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: pancakeWSScheme,
		Host:   endpoints.Websocket,
		Path:   pancakeWSPath,
	}

	pancakeLogger := logger.With().Str("provider", "pancake").Logger()

	provider := &PancakeProvider{
		wsURL:      wsURL,
		logger:     pancakeLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(pancakeLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToPancakePair)

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
		pancakeLogger,
	)

	return provider, nil
}

func (p *PancakeProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *PancakeProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *PancakeProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == pancakeAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  PancakeTicker
		tickerErr   error
		candleResp  []PancakeCandle
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
		pancakePair := currencyPairToPancakePair(pair)
		if msg, ok := messageResp[pancakePair]; ok {
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
					pancakePair,
				)
				telemetryWebsocketMessage(ProviderEthPancake, MessageTypeTicker)
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
						pancakePair,
					)
				}
				telemetryWebsocketMessage(ProviderEthPancake, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o PancakeTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := math.LegacyNewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("pancake: failed to parse ticker price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("pancake: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o PancakeCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := math.LegacyNewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("pancake: failed to parse candle price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("pancake: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *PancakeProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *PancakeProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + pancakeRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []PancakePairData
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

// currencyPairToPancakePair receives a currency pair and return pancake
// ticker symbol atomusdt@ticker.
func currencyPairToPancakePair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
