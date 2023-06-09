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
	osmosisV2WSHost   = "api.osmo-api.prod.ojo.network"
	osmosisV2WSPath   = "ws"
	osmosisV2RestHost = "https://api.osmo-api.prod.ojo.network"
	osmosisV2RestPath = "/assetpairs"
)

var _ Provider = (*OsmosisV2Provider)(nil)

type (
	// OsmosisV2Provider defines an Oracle provider implemented by OJO's
	// Osmosis API.
	//
	// REF: https://github.com/ojo-network/osmosis-api
	OsmosisV2Provider struct {
		wsc       *WebsocketController
		wsURL     url.URL
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	OsmosisV2Ticker struct {
		Price  string `json:"Price"`
		Volume string `json:"Volume"`
	}

	OsmosisV2Candle struct {
		Close   string `json:"Close"`
		Volume  string `json:"Volume"`
		EndTime int64  `json:"EndTime"`
	}

	// OsmosisV2PairsSummary defines the response structure for an Osmosis pairs
	// summary.
	OsmosisV2PairsSummary struct {
		Data []OsmosisPairData `json:"data"`
	}

	// OsmosisPairData defines the data response structure for an Osmosis pair.
	OsmosisPairData struct {
		Base  string `json:"base_symbol"`
		Quote string `json:"quote_symbol"`
	}

	// OsmosisV2PairData defines the data response structure for an Osmosis pair.
	OsmosisV2PairData struct {
		Base  string `json:"base"`
		Quote string `json:"quote"`
	}
)

func NewOsmosisV2Provider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*OsmosisV2Provider, error) {
	if endpoints.Name != ProviderOsmosisV2 {
		endpoints = Endpoint{
			Name:      ProviderOsmosisV2,
			Rest:      osmosisV2RestHost,
			Websocket: osmosisV2WSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   osmosisV2WSPath,
	}

	osmosisV2Logger := logger.With().Str("provider", "osmosisv2").Logger()

	provider := &OsmosisV2Provider{
		wsURL:      wsURL,
		logger:     osmosisV2Logger,
		endpoints:  endpoints,
		priceStore: newPriceStore(osmosisV2Logger),
	}
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToOsmosisV2Pair)

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
		osmosisV2Logger,
	)

	return provider, nil
}

func (p *OsmosisV2Provider) StartConnections() {
	p.wsc.StartConnections()
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *OsmosisV2Provider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *OsmosisV2Provider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	// check if message is an ack
	if string(bz) == "ack" {
		return
	}

	var (
		messageResp map[string]interface{}
		messageErr  error
		tickerResp  OsmosisV2Ticker
		tickerErr   error
		candleResp  []OsmosisV2Candle
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
		osmosisV2Pair := currencyPairToOsmosisV2Pair(pair)
		if msg, ok := messageResp[osmosisV2Pair]; ok {
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
					osmosisV2Pair,
				)
				telemetryWebsocketMessage(ProviderOsmosisV2, MessageTypeTicker)
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
						osmosisV2Pair,
					)
				}
				telemetryWebsocketMessage(ProviderOsmosisV2, MessageTypeCandle)
				continue
			}
		}
	}
}

func (o OsmosisV2Ticker) toTickerPrice() (types.TickerPrice, error) {
	price, err := sdk.NewDecFromStr(o.Price)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("osmosisv2: failed to parse ticker price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.TickerPrice{}, fmt.Errorf("osmosisv2: failed to parse ticker volume: %w", err)
	}

	tickerPrice := types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
	return tickerPrice, nil
}

func (o OsmosisV2Candle) toCandlePrice() (types.CandlePrice, error) {
	close, err := sdk.NewDecFromStr(o.Close)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("osmosisv2: failed to parse candle price: %w", err)
	}
	volume, err := sdk.NewDecFromStr(o.Volume)
	if err != nil {
		return types.CandlePrice{}, fmt.Errorf("osmosisv2: failed to parse candle volume: %w", err)
	}
	candlePrice := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: o.EndTime,
	}
	return candlePrice, nil
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (p *OsmosisV2Provider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *OsmosisV2Provider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + osmosisV2RestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []OsmosisV2PairData
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

// currencyPairToOsmosisV2Pair receives a currency pair and return osmosisv2
// ticker symbol atomusdt@ticker.
func currencyPairToOsmosisV2Pair(cp types.CurrencyPair) string {
	return cp.Base + "/" + cp.Quote
}
