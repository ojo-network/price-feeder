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
	crescentV2WSHost   = "0.0.0.0:5005"
	crescentV2WSPath   = "ws"
	crescentV2RestHost = "http://0.0.0.0:5005"
	crescentV2RestPath = "/assetpairs"
)

var _ Provider = (*CrescentProvider)(nil)

type (
	// CrescentProvider defines an Oracle provider implemented by OJO's
	// Crescent API.
	//
	// REF: https://github.com/ojo-network/crescent-api
	CrescentProvider struct {
		wsc             *WebsocketController
		wsURL           url.URL
		logger          zerolog.Logger
		mtx             sync.RWMutex
		endpoints       Endpoint
		tickers         map[string]types.TickerPrice   // Symbol => TickerPrice
		candles         map[string][]types.CandlePrice // Symbol => CandlePrice
		subscribedPairs map[string]types.CurrencyPair  // Symbol => types.CurrencyPair
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
		Path:   crescentV2WSHost,
	}

	crescentV2Logger := logger.With().Str("provider", "crescent").Logger()

	provider := &CrescentProvider{
		wsURL:           wsURL,
		logger:          crescentV2Logger,
		endpoints:       endpoints,
		tickers:         map[string]types.TickerPrice{},
		candles:         map[string][]types.CandlePrice{},
		subscribedPairs: map[string]types.CurrencyPair{},
	}

	// flip CRE/BCRE to BCRE/CRE since that is the pair provided by the crescent api.
	adjustedPairs := make([]types.CurrencyPair, len(pairs))
	for i, pair := range pairs {
		if pair.String() == "CREBCRE" {
			adjustedPairs[i] = pair.Reversed()
		}
	}

	confirmedPairs, err := ConfirmPairAvailability(
		provider,
		provider.endpoints.Name,
		provider.logger,
		adjustedPairs...,
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

// GetTickerPrices returns the tickerPrices based on the saved map.
func (p *CrescentProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]types.TickerPrice, error) {
	tickerPrices := make(map[string]types.TickerPrice, len(pairs))

	tickerErrs := 0
	for _, cp := range pairs {
		key := currencyPairToCrescentPair(cp)
		// Flip CRE/BCRE to get price for BCRE/CRE
		if cp.String() == "CREBCRE" {
			key = currencyPairToCrescentPair(cp.Reversed())
		}
		price, err := p.getTickerPrice(key)
		if err != nil {
			p.logger.Warn().Err(err)
			tickerErrs++
			continue
		}
		// Convert BCRE/CRE price to CRE/BCRE price
		if cp.String() == "CREBCRE" {
			price.Price = sdk.OneDec().Quo(price.Price)
		}
		tickerPrices[cp.String()] = price
	}

	if tickerErrs == len(pairs) {
		return nil, fmt.Errorf(
			types.ErrNoTickers.Error(),
			p.endpoints.Name,
			pairs,
		)
	}
	return tickerPrices, nil
}

// GetCandlePrices returns the candlePrices based on the saved map
func (p *CrescentProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]types.CandlePrice, error) {
	candlePrices := make(map[string][]types.CandlePrice, len(pairs))

	candleErrs := 0
	for _, cp := range pairs {
		key := currencyPairToCrescentPair(cp)
		// Flip CRE/BCRE to get prices for BCRE/CRE
		if cp.String() == "CREBCRE" {
			key = currencyPairToCrescentPair(cp.Reversed())
		}
		prices, err := p.getCandlePrices(key)
		if err != nil {
			p.logger.Warn().Err(err)
			candleErrs++
			continue
		}
		// Flip BCRE/CRE back to CRE/BCRE
		if cp.String() == "CREBCRE" {
			for i := range prices {
				prices[i].Price = sdk.OneDec().Quo(prices[i].Price)
			}
		}
		candlePrices[cp.String()] = prices
	}

	if candleErrs == len(pairs) {
		return nil, fmt.Errorf(
			types.ErrNoCandles.Error(),
			p.endpoints.Name,
			pairs,
		)
	}
	return candlePrices, nil
}

func (p *CrescentProvider) getTickerPrice(key string) (types.TickerPrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	ticker, ok := p.tickers[key]
	if !ok {
		return types.TickerPrice{}, fmt.Errorf(
			types.ErrTickerNotFound.Error(),
			p.endpoints.Name,
			key,
		)
	}

	return ticker, nil
}

func (p *CrescentProvider) getCandlePrices(key string) ([]types.CandlePrice, error) {
	p.mtx.RLock()
	defer p.mtx.RUnlock()

	candles, ok := p.candles[key]
	if !ok {
		return []types.CandlePrice{}, fmt.Errorf(
			types.ErrCandleNotFound.Error(),
			p.endpoints.Name,
			key,
		)
	}

	candleList := []types.CandlePrice{}
	candleList = append(candleList, candles...)

	return candleList, nil
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
					crescentPair,
					tickerResp,
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
						crescentPair,
						singleCandle,
					)
				}
				telemetryWebsocketMessage(ProviderCrescent, MessageTypeCandle)
				continue
			}
		}
	}
}

func (p *CrescentProvider) setTickerPair(symbol string, tickerPair CrescentTicker) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	price, err := sdk.NewDecFromStr(tickerPair.Price)
	if err != nil {
		p.logger.Warn().Err(err).Msg("crescent: failed to parse ticker price")
		return
	}
	volume, err := sdk.NewDecFromStr(tickerPair.Volume)
	if err != nil {
		p.logger.Warn().Err(err).Msg("crescent: failed to parse ticker volume")
		return
	}

	p.tickers[symbol] = types.TickerPrice{
		Price:  price,
		Volume: volume,
	}
}

func (p *CrescentProvider) setCandlePair(symbol string, candlePair CrescentCandle) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	close, err := sdk.NewDecFromStr(candlePair.Close)
	if err != nil {
		p.logger.Warn().Err(err).Msg("crescent: failed to parse candle close")
		return
	}
	volume, err := sdk.NewDecFromStr(candlePair.Volume)
	if err != nil {
		p.logger.Warn().Err(err).Msg("crescent: failed to parse candle volume")
		return
	}
	candle := types.CandlePrice{
		Price:     close,
		Volume:    volume,
		TimeStamp: candlePair.EndTime,
	}

	staleTime := PastUnixTime(providerCandlePeriod)
	candleList := []types.CandlePrice{}
	candleList = append(candleList, candle)
	for _, c := range p.candles[symbol] {
		if staleTime < c.TimeStamp {
			candleList = append(candleList, c)
		}
	}

	p.candles[symbol] = candleList
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
