package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/gorilla/websocket"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

const (
	cryptoCompareWSHost   = "data-streamer.cryptocompare.com"
	cryptoCompareWSPath   = ""
	cryptoCompareRestHost = ""
	cryptoCompareRestPath = ""
)

var _ Provider = (*CryptoCompareProvider)(nil)

type (
	// CryptoCompareProvider defines an Oracle provider implemented by the CryptoCompare public
	// API.

	CryptoCompareProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	CryptoCompareSubscriptionMsg struct {
		Action      string   `json:"action"`
		Type        string   `json:"type"`
		Groups      []string `json:"groups"`
		Market      string   `json:"market"`
		Instruments []string `json:"instruments"`
	}

	CryptoCompareTickerResponse struct {
		Type             string  `json:"TYPE"`
		Instrument       string  `json:"INSTRUMENT"`
		Market           string  `json:"MARKET"`
		Value            float64 `json:"VALUE"`
		CurrentDayVolume float64 `json:"CURRENT_DAY_VOLUME"`
	}

	CryptoCompareTicker struct {
		Last   float64 // Last traded price ex.: 43508.9
		Vol    float64 // Trading volume ex.: 11159.87127845
		Symbol string  // Symbol ex.: ATOM_UDST
	}
)

func NewCryptoCompareProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*CryptoCompareProvider, error) {
	if (endpoints.Name) != ProviderCryptoCompare {
		endpoints = Endpoint{
			Name:      ProviderCryptoCompare,
			Rest:      cryptoCompareRestHost,
			Websocket: cryptoCompareWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   "?api_key=" + endpoints.APIKey,
	}

	cryptoCompareLogger := logger.With().Str("provider", "cryptocompare").Logger()

	provider := &CryptoCompareProvider{
		logger:     cryptoCompareLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(cryptoCompareLogger),
	}
	//Modificar aqui
	provider.setCurrencyPairToTickerAndCandlePair(currencyPairToCryptoComparePairPriceStore)

	/*confirmedPairs, err := ConfirmPairAvailability(
		provider,
		provider.endpoints.Name,
		provider.logger,
		pairs...,
	)
	if err != nil {
		return nil, err
	}*/

	provider.setSubscribedPairs(pairs...)

	provider.wsc = NewWebsocketController(
		ctx,
		endpoints.Name,
		wsURL,
		provider.getSubscriptionMsgs(pairs...),
		provider.messageReceived,
		defaultPingDuration,
		websocket.PingMessage,
		cryptoCompareLogger,
	)

	return provider, nil
}

func (p *CryptoCompareProvider) StartConnections() {
	p.wsc.StartConnections()
}

func (p *CryptoCompareProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(cps)+1)
	cryptoComparePair := []string{}
	for _, cp := range cps {
		cryptoComparePair = append(cryptoComparePair, currencyPairToCryptoComparePair(cp))
	}
	subscriptionMsgs = append(subscriptionMsgs, newCryptoCompareTickerSubscriptionMsg(cryptoComparePair))

	return subscriptionMsgs
}

func (p *CryptoCompareProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	newPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if _, ok := p.subscribedPairs[cp.String()]; !ok {
			newPairs = append(newPairs, cp)
		}
	}

	/*confirmedPairs, err := ConfirmPairAvailability(
		p,
		p.endpoints.Name,
		p.logger,
		newPairs...,
	)
	if err != nil {
		return
	}*/

	newSubscriptionMsgs := p.getSubscriptionMsgs(newPairs...)
	p.wsc.AddWebsocketConnection(
		newSubscriptionMsgs,
		p.messageReceived,
		defaultPingDuration,
		websocket.PingMessage,
	)
	p.setSubscribedPairs(newPairs...)
}

func (p *CryptoCompareProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + mexcRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary MexcPairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(strings.ReplaceAll(pairName.Symbol, "_", ""))] = struct{}{}
	}

	return availablePairs, nil
}

func (p *CryptoCompareProvider) setSubscribedPairs(cps ...types.CurrencyPair) {
	for _, cp := range cps {
		p.subscribedPairs[cp.String()] = cp
	}
}

func (p *CryptoCompareProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	var (
		tickerResp CryptoCompareTickerResponse
		tickerErr  error
	)

	tickerErr = json.Unmarshal(bz, &tickerResp)

	if tickerErr != nil {
		return
	}

	//REVISAR ESTO POR QUE EL TIPO PUEDE SER DIFERENTE APARENTEMENTE HAY 4
	//https://developers.cryptocompare.com/documentation/data-streamer/index_cc_v1_latest_tick_adaptive_inclusion_methodology
	if tickerResp.Type == "985" || tickerResp.Type == "266" || tickerResp.Type == "987" || tickerResp.Type == "1101" {

		pair := tickerResp.GetInstrument()

		if _, ok := p.subscribedPairs[pair]; ok {
			cryptoCompareTicket := CryptoCompareTicker{
				Last:   tickerResp.Value,
				Vol:    tickerResp.CurrentDayVolume,
				Symbol: pair,
			}

			p.setTickerPair(cryptoCompareTicket, pair)
			return
		}

		return

	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		Msg("Error on receive message")

}

func currencyPairToCryptoComparePairPriceStore(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + cp.Quote)
}

func currencyPairToCryptoComparePair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.Base + "-" + cp.Quote)
}

// newCryptoCompareTickerSubscriptionMsg returns a new ticker subscription Msg.
func newCryptoCompareTickerSubscriptionMsg(instruments []string) CryptoCompareSubscriptionMsg {

	return CryptoCompareSubscriptionMsg{
		Action: "SUBSCRIBE",
		Type:   "index_cc_v1_latest_tick",
		Groups: []string{
			"VALUE",
			"CURRENT_DAY",
		},
		Market:      "cadli",
		Instruments: instruments,
	}
}

func (p *CryptoCompareProvider) GetExternalLiquidity(ctx client.Context, pairs ...types.CurrencyPair) (map[uint64]types.ExternalLiquidity, error) {
	externalLiquidity := make(map[uint64]types.ExternalLiquidity, len(pairs))

	return externalLiquidity, nil
}

func (p CryptoCompareTickerResponse) GetInstrument() string {
	instrument := strings.ReplaceAll(p.Instrument, "-", "")
	return instrument
}

func (ticker CryptoCompareTicker) toTickerPrice() (types.TickerPrice, error) {

	var last string
	var vol string

	last = strconv.FormatFloat(ticker.Last, 'f', -1, 64)
	vol = strconv.FormatFloat(ticker.Vol, 'f', -1, 64)

	return types.NewTickerPrice(last, vol)
}
