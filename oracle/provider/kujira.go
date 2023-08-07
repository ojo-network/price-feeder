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
	kujiraWSHost   = "api.kujira-api.prod.ojo.network"
	kujiraWSPath   = "wss"
	kujiraRestHost = "https://api.kujira-api.prod.ojo.network"
	kujiraRestPath = "/assetpairs"
	kujiraAckMsg   = "ack"
)

var _ Provider = (*KujiraProvider)(nil)

type (
	// KujiraProvider defines an Oracle provider implemented by OJO's
	// Kujira API.
	//
	// REF: https://github.com/ojo-network/kujira-api
	KujiraProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	KujiraTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	KujiraCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// KujiraPairsSummary defines the response structure for an Kujira pairs
	// summary.
	KujiraPairsSummary struct {
		Data []KujiraPairData `json:"data"`
	}

	// KujiraPairData defines the data response structure for an Kujira pair.
	KujiraPairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewKujiraProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*KujiraProvider, error) {
	if endpoints.Name != ProviderKujira {
		endpoints = Endpoint{
			Name:      ProviderKujira,
			Rest:      kujiraRestHost,
			Websocket: kujiraWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "ws",
		Host:   endpoints.Websocket,
		Path:   kujiraWSPath,
	}

	kujiraLogger := logger.With().Str("provider", "kujira").Logger()

	provider := &KujiraProvider{
		wsURL:      wsURL,
		logger:     kujiraLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(kujiraLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToKujiraPair)

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
		kujiraLogger,
	)

	return provider, nil
}

func (p *KujiraProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *KujiraProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *KujiraProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == kujiraAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  KujiraTicker
		tickerErr   error
		candleResp  []KujiraCandle
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
		kujiraPair := currencyPairToKujiraPair(pair)
		if msg, ok := messageResp[kujiraPair]; ok {
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
					kujiraPair,
				)
				telemetryWebsocketMessage(ProviderKujira, MessageTypeTicker)
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
						kujiraPair,
					)
				}
				telemetryWebsocketMessage(ProviderKujira, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o KujiraTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := sdk.NewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("kujira: failed to parse ticker price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("kujira: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o KujiraCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := sdk.NewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("kujira: failed to parse candle price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("kujira: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *KujiraProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *KujiraProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + kujiraRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []KujiraPairData
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

// currencyPairToKujiraPair receives a currency pair and return kujira
// ticker symbol atomusdt@ticker.
func currencyPairToKujiraPair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
