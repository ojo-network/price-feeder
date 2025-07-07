package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"cosmossdk.io/math"
	"github.com/ojo-network/ojo/util/decmath"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

var _ Provider = (*DPSNProvider)(nil)

const (
	dpsnRestURL = "https://data.streams.dpsn.org"
	tickerPath  = "/api/stream/%s/%s/%s/ticker/latest"
	ohlcPath    = "/api/stream/%s/%s/%s/ohlc/latest"

	dpsnPollInterval = 3 * time.Second
)

type (
	DPSNProvider struct {
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		client *http.Client
		priceStore
		ctx context.Context
	}

	// DPSNTickerResponse is the response from the DPSN ticker endpoint.
	DPSNTickerResponse struct {
		Data string `json:"data"`
	}

	// DPSNTickerData is the parsed data from the DPSN ticker response.
	DPSNTickerData struct {
		Price     float64 `json:"price"`
		Volume24h string  `json:"volume24h"`
		Timestamp int64   `json:"timestamp"`
		Pair      string  `json:"pair"`
	}

	// DPSNOHLCResponse is the response from the DPSN OHLC endpoint.
	DPSNOHLCResponse struct {
		Data string `json:"data"`
	}

	// DPSNOHLCData is the parsed data from the DPSN OHLC response.
	DPSNOHLCData struct {
		Timestamp int64   `json:"timestamp"`
		Pair      string  `json:"pair"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Close     float64 `json:"close"`
		Volume0   string  `json:"volume0"`
		Volume1   string  `json:"volume1"`
		Token0    string  `json:"token0"`
		Token1    string  `json:"token1"`
		Count     int     `json:"count"`
	}
)

// NewDPSNProvider returns a new DPSNProvider.
// It also starts a go routine to poll for new data.
func NewDPSNProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*DPSNProvider, error) {
	if (endpoints.Name) != ProviderDPSN {
		endpoints = Endpoint{
			Name: ProviderDPSN,
			Rest: dpsnRestURL,
		}
	}

	dpsnLogger := logger.With().Str("provider", string(ProviderDPSN)).Logger()

	provider := &DPSNProvider{
		logger:     dpsnLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(dpsnLogger),
		client:     &http.Client{},
		ctx:        ctx,
	}

	// For DPSN, we skip pair availability confirmation since DPSN doesn't provide
	// an endpoint to get all available pairs. Instead, we validate that DPSN topics
	// are configured for each pair.
	confirmedPairs := []types.CurrencyPair{}
	for _, pair := range pairs {
		if pair.DPSNTopics == nil {
			dpsnLogger.Error().Msg(fmt.Sprintf(
				"DPSN topics not configured for pair %s, ignoring pair",
				pair.String(),
			))
			continue
		}
		confirmedPairs = append(confirmedPairs, pair)
	}

	provider.setSubscribedPairs(confirmedPairs...)

	return provider, nil
}

// GetAvailablePairs return all available pair symbols.
// For DPSN, we'll return an empty map since we don't have a way to get all available pairs.
func (p *DPSNProvider) GetAvailablePairs() (map[string]struct{}, error) {
	// DPSN doesn't provide an endpoint to get all available pairs
	// We'll return an empty map and rely on the pairs being provided during initialization
	return map[string]struct{}{}, nil
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array.
func (p *DPSNProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	newPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if _, ok := p.subscribedPairs[cp.String()]; !ok {
			newPairs = append(newPairs, cp)
		}
	}

	// For DPSN, we skip pair availability confirmation since DPSN doesn't provide
	// an endpoint to get all available pairs. Instead, we validate that DPSN topics
	// are configured for each pair.
	confirmedPairs := []types.CurrencyPair{}
	for _, pair := range newPairs {
		if pair.DPSNTopics == nil {
			p.logger.Error().Msg(fmt.Sprintf(
				"DPSN topics not configured for pair %s, ignoring pair",
				pair.String(),
			))
			continue
		}
		confirmedPairs = append(confirmedPairs, pair)
	}

	p.setSubscribedPairs(confirmedPairs...)
}

// StartConnections begins the polling process for
// the dpsn provider.
func (p *DPSNProvider) StartConnections() {
	go func() {
		p.logger.Debug().Msg("starting dpsn polling...")
		err := p.poll()
		if err != nil {
			p.logger.Err(err).Msg("dpsn provider unable to poll new data")
		}
	}()
}

// toTickerPrice converts the DPSNTickerData to a TickerPrice.
// It satisfies the TickerPrice interface.
func (dtd DPSNTickerData) toTickerPrice() (types.TickerPrice, error) {
	lp, err := decmath.NewDecFromFloat(dtd.Price)
	if err != nil {
		return types.TickerPrice{}, err
	}
	volume, err := math.LegacyNewDecFromStr(dtd.Volume24h)
	if err != nil {
		return types.TickerPrice{}, err
	}
	return types.TickerPrice{
		Price:  lp,
		Volume: volume,
	}, nil
}

// toCandlePrice converts the DPSNOHLCData to a CandlePrice.
// It satisfies the CandlePrice interface.
func (dod DPSNOHLCData) toCandlePrice() (types.CandlePrice, error) {
	closePrice, err := decmath.NewDecFromFloat(dod.Close)
	if err != nil {
		return types.CandlePrice{}, err
	}
	volume, err := math.LegacyNewDecFromStr(dod.Volume0)
	if err != nil {
		return types.CandlePrice{}, err
	}
	return types.CandlePrice{
		Price:     closePrice,
		Volume:    volume,
		TimeStamp: dod.Timestamp,
	}, nil
}

// setTickers queries the DPSN API for the latest tickers and updates the
// priceStore.
func (p *DPSNProvider) setTickers() error {
	p.subscribedPairsMtx.RLock()
	subscribedPairs := make([]types.CurrencyPair, 0, len(p.subscribedPairs))
	for _, pair := range p.subscribedPairs {
		subscribedPairs = append(subscribedPairs, pair)
	}
	p.subscribedPairsMtx.RUnlock()

	for _, pair := range subscribedPairs {
		ticker, err := p.queryTicker(pair)
		if err != nil {
			p.logger.Error().Err(err).Str("pair", pair.String()).Msg("failed to query ticker")
			continue
		}
		p.setTickerPair(ticker, pair.String())
	}
	return nil
}

// setCandles queries the DPSN API for the latest candles and updates the
// priceStore.
func (p *DPSNProvider) setCandles() error {
	p.subscribedPairsMtx.RLock()
	subscribedPairs := make([]types.CurrencyPair, 0, len(p.subscribedPairs))
	for _, pair := range p.subscribedPairs {
		subscribedPairs = append(subscribedPairs, pair)
	}
	p.subscribedPairsMtx.RUnlock()

	for _, pair := range subscribedPairs {
		candle, err := p.queryOHLC(pair)
		if err != nil {
			p.logger.Error().Err(err).Str("pair", pair.String()).Msg("failed to query OHLC")
			continue
		}
		p.setCandlePair(candle, pair.String())
	}
	return nil
}

// queryTicker queries the DPSN API for a specific currency pair.
func (p *DPSNProvider) queryTicker(pair types.CurrencyPair) (DPSNTickerData, error) {
	// Check if DPSN topics are configured for this pair
	if pair.DPSNTopics == nil {
		return DPSNTickerData{}, fmt.Errorf("DPSN topics not configured for pair %s", pair.String())
	}

	// Construct the URL using the configurable TopicID and AssetID
	url := fmt.Sprintf("%s%s", p.endpoints.Rest, fmt.Sprintf(tickerPath, pair.DPSNTopics.TopicID, pair.String(), pair.DPSNTopics.AssetID))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return DPSNTickerData{}, err
	}

	// Add API key if provided
	if p.endpoints.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.endpoints.APIKey)
	}

	res, err := p.client.Do(req)
	if err != nil {
		return DPSNTickerData{}, err
	}
	defer res.Body.Close()

	bz, err := io.ReadAll(res.Body)
	if err != nil {
		return DPSNTickerData{}, fmt.Errorf("failed to read response: %w", err)
	}

	var dpsnResponse DPSNTickerResponse
	if err := json.Unmarshal(bz, &dpsnResponse); err != nil {
		return DPSNTickerData{}, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	var tickerData DPSNTickerData
	if err := json.Unmarshal([]byte(dpsnResponse.Data), &tickerData); err != nil {
		return DPSNTickerData{}, fmt.Errorf("failed to unmarshal ticker data: %w", err)
	}

	return tickerData, nil
}

// queryOHLC queries the DPSN API for OHLC data for a specific currency pair.
func (p *DPSNProvider) queryOHLC(pair types.CurrencyPair) (DPSNOHLCData, error) {
	// Check if DPSN topics are configured for this pair
	if pair.DPSNTopics == nil {
		return DPSNOHLCData{}, fmt.Errorf("DPSN topics not configured for pair %s", pair.String())
	}

	// Construct the URL using the configurable TopicID and AssetID
	url := fmt.Sprintf("%s%s", p.endpoints.Rest, fmt.Sprintf(ohlcPath, pair.DPSNTopics.TopicID, pair.String(), pair.DPSNTopics.AssetID))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return DPSNOHLCData{}, err
	}

	// Add API key if provided
	if p.endpoints.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.endpoints.APIKey)
	}

	res, err := p.client.Do(req)
	if err != nil {
		return DPSNOHLCData{}, err
	}
	defer res.Body.Close()

	bz, err := io.ReadAll(res.Body)
	if err != nil {
		return DPSNOHLCData{}, fmt.Errorf("failed to read response: %w", err)
	}

	var dpsnResponse DPSNOHLCResponse
	if err := json.Unmarshal(bz, &dpsnResponse); err != nil {
		return DPSNOHLCData{}, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	var ohlcData DPSNOHLCData
	if err := json.Unmarshal([]byte(dpsnResponse.Data), &ohlcData); err != nil {
		return DPSNOHLCData{}, fmt.Errorf("failed to unmarshal OHLC data: %w", err)
	}

	return ohlcData, nil
}

// This function periodically calls setTickers to update the priceStore.
func (p *DPSNProvider) poll() error {
	for {
		select {
		case <-p.ctx.Done():
			return nil

		default:
			p.logger.Debug().Msg("querying dpsn api")

			err := p.setTickers()
			if err != nil {
				return err
			}

			err = p.setCandles()
			if err != nil {
				return err
			}

			time.Sleep(dpsnPollInterval)
		}
	}
}
