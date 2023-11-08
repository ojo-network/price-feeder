package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/websocket"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

const (
	uniswapWSHost   = "api.eth-api.prod.ojo.network"
	uniswapWSPath   = "ws"
	uniswapWSScheme = "wss"
	uniswapRestHost = "https://api.eth-api.prod.ojo.network"
	uniswapRestPath = "/assetpairs"
	uniswapAckMsg   = "ack"
)

var _ Provider = (*UniswapProvider)(nil)

type (
	// UniswapProvider defines an Oracle provider implemented by OJO's
	// Uniswap API.
	//
	// REF: https://github.com/ojo-network/ehereum-api
	UniswapProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	UniswapTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	UniswapCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// UniswapPairsSummary defines the response structure for an Uniswap pairs
	// summary.
	UniswapPairsSummary struct {
		Data []UniswapPairData `json:"data"`
	}

	// UniswapPairData defines the data response structure for an Uniswap pair.
	UniswapPairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewUniswapProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*UniswapProvider, error) {
	if endpoints.Name != ProviderEthUniswap {
		endpoints = Endpoint{
			Name:      ProviderEthUniswap,
			Rest:      uniswapRestHost,
			Websocket: uniswapWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: uniswapWSScheme,
		Host:   endpoints.Websocket,
		Path:   uniswapWSPath,
	}

	uniswapLogger := logger.With().Str("provider", "uniswap").Logger()

	provider := &UniswapProvider{
		wsURL:      wsURL,
		logger:     uniswapLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(uniswapLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToUniswapPair)

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
		uniswapLogger,
	)

	return provider, nil
}

func (p *UniswapProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *UniswapProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *UniswapProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == uniswapAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  UniswapTicker
		tickerErr   error
		candleResp  []UniswapCandle
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
		uniswapPair := currencyPairToUniswapPair(pair)
		if msg, ok := messageResp[uniswapPair]; ok {
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
					uniswapPair,
				)
				telemetryWebsocketMessage(ProviderEthUniswap, MessageTypeTicker)
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
						uniswapPair,
					)
				}
				telemetryWebsocketMessage(ProviderEthUniswap, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o UniswapTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := sdk.NewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("uniswap: failed to parse ticker price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("uniswap: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o UniswapCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := sdk.NewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("uniswap: failed to parse candle price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("uniswap: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *UniswapProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *UniswapProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + uniswapRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []UniswapPairData
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

// currencyPairToUniswapPair receives a currency pair and return uniswap
// ticker symbol atomusdt@ticker.
func currencyPairToUniswapPair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
