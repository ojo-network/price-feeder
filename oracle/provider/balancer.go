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
	balancerWSHost   = "api.eth-api.prod.ojo.network"
	balancerWSPath   = "/balancer/ws"
	balancerWSScheme = "wss"
	balancerRestHost = "https://api.eth-api.prod.ojo.network"
	balancerRestPath = "/balancer/assetpairs"
	balancerAckMsg   = "ack"
)

var _ Provider = (*BalancerProvider)(nil)

type (
	// BalancerProvider defines an Oracle provider implemented by OJO's
	// Balancer API.
	//
	// REF: https://github.com/ojo-network/ehereum-api
	BalancerProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	BalancerTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	BalancerCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// BalancerPairsSummary defines the response structure for an Balancer pairs
	// summary.
	BalancerPairsSummary struct {
		Data []BalancerPairData `json:"data"`
	}

	// BalancerPairData defines the data response structure for an Balancer pair.
	BalancerPairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewBalancerProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*BalancerProvider, error) {
	if endpoints.Name != ProviderEthBalancer {
		endpoints = Endpoint{
			Name:      ProviderEthBalancer,
			Rest:      balancerRestHost,
			Websocket: balancerWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: balancerWSScheme,
		Host:   endpoints.Websocket,
		Path:   balancerWSPath,
	}

	balancerLogger := logger.With().Str("provider", "balancer").Logger()

	provider := &BalancerProvider{
		wsURL:      wsURL,
		logger:     balancerLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(balancerLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToBalancerPair)

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
		balancerLogger,
	)

	return provider, nil
}

func (p *BalancerProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *BalancerProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *BalancerProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == balancerAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  BalancerTicker
		tickerErr   error
		candleResp  []BalancerCandle
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
		balancerPair := currencyPairToBalancerPair(pair)
		if msg, ok := messageResp[balancerPair]; ok {
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
					balancerPair,
				)
				telemetryWebsocketMessage(ProviderEthBalancer, MessageTypeTicker)
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
						balancerPair,
					)
				}
				telemetryWebsocketMessage(ProviderEthBalancer, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o BalancerTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := math.LegacyNewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("balancer: failed to parse ticker price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("balancer: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o BalancerCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := math.LegacyNewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("balancer: failed to parse candle price: %w", err)
	}
	volume, err := math.LegacyNewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("balancer: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *BalancerProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *BalancerProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + balancerRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []BalancerPairData
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

// currencyPairToBalancerPair receives a currency pair and return balancer
// ticker symbol atomusdt@ticker.
func currencyPairToBalancerPair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
