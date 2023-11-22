package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ojo-network/ojo/util/decmath"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
)

var _ Provider = (*AstroportProvider)(nil)

const (
	ProviderAstroport = "astroport"
	restURL           = "https://markets-api.astroport.fi"
	tickersURL        = "/markets/cg/tickers"
	assetsURL         = "/markets/cmc/v1/assets"
	pollInterval      = 3 * time.Second
)

type (
	AstroportProvider struct {
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		client *http.Client
		priceStore
	}

	// AstroportAssetResponse is the response from the Astroport assets endpoint.
	AstroportAssetResponse struct {
		BaseID      string  `json:"base_id"`
		BaseName    string  `json:"base_name"`
		BaseSymbol  string  `json:"base_symbol"`
		QuoteID     string  `json:"quote_id"`
		QuoteName   string  `json:"quote_name"`
		QuoteSymbol string  `json:"quote_symbol"`
		LastPrice   float64 `json:"last_price"`
		BaseVolume  float64 `json:"base_volume"`
		QuoteVolume float64 `json:"quote_volume"`
		USDVolume   float64 `json:"USD_volume"`
	}
	// AstroportTickersResponse is the response from the Astroport tickers endpoint.
	AstroportTickersResponse struct {
		TickerID       string  `json:"ticker_id"`
		BaseCurrency   string  `json:"base_currency"`
		TargetCurrency string  `json:"target_currency"`
		LastPrice      float64 `json:"last_price"`
		LiquidityInUSD float64 `json:"liquidity_in_usd"`
		BaseVolume     float64 `json:"base_volume"`
		TargetVolume   float64 `json:"target_volume"`
		PoolID         string  `json:"pool_id"`
	}
)

// NewAstroportProvider returns a new AstroportProvider.
// It also starts a go routine to poll for new data.
func NewAstroportProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*AstroportProvider, error) {
	if (endpoints.Name) != ProviderAstroport {
		endpoints = Endpoint{
			Name: ProviderAstroport,
			Rest: restURL,
		}
	}

	astroLogger := logger.With().Str("provider", string(ProviderAstroport)).Logger()

	provider := &AstroportProvider{
		logger:     astroLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(astroLogger),
		client:     &http.Client{},
	}

	confirmedPairs, err := ConfirmPairAvailability(
		provider,
		provider.endpoints.Name,
		provider.logger,
		pairs...,
	)
	if err != nil {
		return nil, err
	}

	go func() {
		logger.Debug().Msg("starting ftx polling...")
		err := provider.pollCache(ctx, pairs...)
		if err != nil {
			logger.Err(err).Msg("astroport provider unable to poll new data")
		}
	}()

	provider.setSubscribedPairs(confirmedPairs...)

	return provider, nil
}

// GetAvailablePairs return all available pair symbols.
func (p AstroportProvider) GetAvailablePairs() (map[string]struct{}, error) {
	availablePairs, _, err := p.getTickerMaps()
	if err != nil {
		return nil, err
	}

	availableSymbols := map[string]struct{}{}
	for _, pair := range availablePairs {
		availableSymbols[pair.String()] = struct{}{}
	}

	return availableSymbols, nil
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array.
func (p *AstroportProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
	p.mtx.Lock()
	defer p.mtx.Unlock()

	newPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if _, ok := p.subscribedPairs[cp.String()]; !ok {
			newPairs = append(newPairs, cp)
		}
	}

	confirmedPairs, err := ConfirmPairAvailability(
		p,
		p.endpoints.Name,
		p.logger,
		newPairs...,
	)
	if err != nil {
		return
	}

	p.setSubscribedPairs(confirmedPairs...)
}

// StartConnections starts the websocket connections.
// This function is a no-op for the astroport provider.
func (p AstroportProvider) StartConnections() {}

// AstroportTickerPairs is a struct to hold the AstroportTickersResponse and the
// corresponding pair. It satisfies the TickerPrice interface.
type AstroportTickerPairs struct {
	ticker AstroportTickersResponse
	pair   types.CurrencyPair
}

// toTickerPrice converts the AstroportTickerPairs to a TickerPrice.
// It satisfies the TickerPrice interface.
func (atr AstroportTickersResponse) toTickerPrice() (types.TickerPrice, error) {
	lp, err := decmath.NewDecFromFloat(atr.LastPrice)
	if err != nil {
		return types.TickerPrice{}, err
	}
	volume, err := decmath.NewDecFromFloat(atr.BaseVolume)
	if err != nil {
		return types.TickerPrice{}, err
	}
	return types.TickerPrice{
		Price:  lp,
		Volume: volume,
	}, nil
}

// setTickers queries the Astroport API for the latest tickers and updates the
// priceStore.
func (p AstroportProvider) setTickers() error {
	tickers, err := p.queryTickers()
	if err != nil {
		return err
	}
	for _, v := range tickers {
		p.setTickerPair(v.ticker, v.pair.String())
	}
	return nil
}

// findTickersForPairs returns a map of ticker IDs -> pairs, but filters out
// pairs that we are not subscribed to.
func (p AstroportProvider) findTickersForPairs() (map[string]types.CurrencyPair, error) {
	queryingPairs := p.subscribedPairs
	_, pairToTickerIDMap, err := p.getTickerMaps()
	if err != nil {
		return nil, err
	}

	// map of ticker IDs -> pairs
	tickerIDs := make(map[string]types.CurrencyPair, len(queryingPairs))
	for _, pair := range queryingPairs {
		if tickerID, ok := pairToTickerIDMap[pair.String()]; ok {
			tickerIDs[tickerID] = pair
		}
	}
	return tickerIDs, nil
}

// getTickerMaps returns all available assets from the api.
// It returns a map of ticker IDs -> pairs and a map of pairs -> ticker IDs.
func (p AstroportProvider) getTickerMaps() (map[string]types.CurrencyPair, map[string]string, error) {
	res, err := p.client.Get(p.endpoints.Rest + assetsURL)
	if err != nil {
		return nil, nil, err
	}
	defer res.Body.Close()

	bz, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to read response: %w", err)
	}

	astroportAssets := []map[string]AstroportAssetResponse{}
	if err := json.Unmarshal(bz, &astroportAssets); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	availablePairs := map[string]types.CurrencyPair{}
	for _, assetMap := range astroportAssets {
		for tickerID, asset := range assetMap {
			availablePairs[tickerID] = types.CurrencyPair{
				Base:  strings.ToUpper(asset.BaseSymbol),
				Quote: strings.ToUpper(asset.QuoteSymbol),
			}
		}
	}

	pairToTickerID := map[string]string{}
	for tickerID, pair := range availablePairs {
		pairToTickerID[pair.String()] = tickerID
	}

	return availablePairs, pairToTickerID, nil
}

// queryTickers returns the AstroportTickerPairs available from the API.
func (p AstroportProvider) queryTickers() ([]AstroportTickerPairs, error) {
	res, err := p.client.Get(p.endpoints.Rest + tickersURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bz, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	astroportTickers := []AstroportTickersResponse{}
	if err := json.Unmarshal(bz, &astroportTickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	tickerMap, err := p.findTickersForPairs()
	if err != nil {
		return nil, err
	}
	// filter out tickers that we are not subscribed to
	tickers := []AstroportTickerPairs{}
	for tickerID, v := range tickerMap {
		for _, ticker := range astroportTickers {
			if ticker.TickerID == tickerID {
				tickers = append(tickers, AstroportTickerPairs{
					ticker: ticker,
					pair:   v,
				})
			}
		}
	}

	return tickers, nil
}

// This function periodically calls setTickers to update the priceStore.
func (p AstroportProvider) pollCache(ctx context.Context, pairs ...types.CurrencyPair) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		default:
			p.logger.Debug().Msg("querying astroport api")

			err := p.setTickers()
			if err != nil {
				return err
			}

			time.Sleep(pollInterval)
		}
	}
}
