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
	osmosisWSHost   = "api.osmo-api.prod.ojo.network"
	osmosisWSPath   = "ws"
	osmosisRestHost = "https://api.osmo-api.prod.ojo.network"
	osmosisRestPath = "/assetpairs"
	osmosisAckMsg   = "ack"
)

var _ Provider = (*OsmosisProvider)(nil)

type (
	// OsmosisProvider defines an Oracle provider implemented by OJO's
	// Osmosis API.
	//
	// REF: https://github.com/ojo-network/osmosis-api
	OsmosisProvider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	OsmosisTicker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	OsmosisCandle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// OsmosisPairsSummary defines the response structure for an Osmosis pairs
	// summary.
	OsmosisPairsSummary struct {
		Data []OsmosisPairData `json:"data"`
	}

	// OsmosisPairData defines the data response structure for an Osmosis pair.
	OsmosisPairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewOsmosisProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*OsmosisProvider, error) {
	if endpoints.Name != ProviderOsmosis {
		endpoints = Endpoint{
			Name:      ProviderOsmosis,
			Rest:      osmosisRestHost,
			Websocket: osmosisWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   osmosisWSPath,
	}

	osmosisLogger := logger.With().Str("provider", "osmosis").Logger()

	provider := &OsmosisProvider{
		wsURL:      wsURL,
		logger:     osmosisLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(osmosisLogger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToOsmosisPair)

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
		osmosisLogger,
	)

	return provider, nil
}

func (p *OsmosisProvider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *OsmosisProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *OsmosisProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == osmosisAckMsg {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  OsmosisTicker
		tickerErr   error
		candleResp  []OsmosisCandle
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
		osmosisPair := currencyPairToOsmosisPair(pair)
		if msg, ok := messageResp[osmosisPair]; ok {
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
					osmosisPair,
				)
				telemetryWebsocketMessage(ProviderOsmosis, MessageTypeTicker)
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
						osmosisPair,
					)
				}
				telemetryWebsocketMessage(ProviderOsmosis, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o OsmosisTicker) toTickerPrice() (types.TickerPrice, error) {
	price, err := sdk.NewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("osmosis: failed to parse ticker price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("osmosis: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o OsmosisCandle) toCandlePrice() (types.CandlePrice, error) {
	close, err := sdk.NewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("osmosis: failed to parse candle price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("osmosis: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *OsmosisProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *OsmosisProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + osmosisRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []OsmosisPairData
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

// currencyPairToOsmosisPair receives a currency pair and return osmosis
// ticker symbol atomusdt@ticker.
func currencyPairToOsmosisPair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
