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
	restURL           = "https://api.astroport.fi"
	tickersURL        = "/api/markets/cg/tickers"
	assetsURL         = "/api/markets/cmc/v1/assets"
	pollInterval      = 3 * time.Second
)

type (
	AstroportProvider struct {
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		client *http.Client
		priceStore
		ctx context.Context
	}

	// AstroportAssetResponse is the response from the Astroport assets endpoint.
	AstroportAssetResponse struct {
		BaseID      string      `json:"base_id"`
		BaseName    string      `json:"base_name"`
		BaseSymbol  string      `json:"base_symbol"`
		QuoteID     string      `json:"quote_id"`
		QuoteName   string      `json:"quote_name"`
		QuoteSymbol interface{} `json:"quote_symbol"`
		LastPrice   float64     `json:"last_price"`
		BaseVolume  float64     `json:"base_volume"`
		QuoteVolume float64     `json:"quote_volume"`
		USDVolume   float64     `json:"USD_volume"`
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
		ctx:        ctx,
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

	provider.setSubscribedPairs(confirmedPairs...)

	return provider, nil
}

// GetAvailablePairs return all available pair symbols.
func (p *AstroportProvider) GetAvailablePairs() (map[string]struct{}, error) {
	availablePairs, err := p.getAvailableAssets()
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

// StartConnections begins the polling process for
// the astroport provider.
func (p *AstroportProvider) StartConnections() {
	go func() {
		p.logger.Debug().Msg("starting astroport polling...")
		err := p.poll()
		if err != nil {
			p.logger.Err(err).Msg("astroport provider unable to poll new data")
		}
	}()
}

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
func (p *AstroportProvider) setTickers() error {
	tickers, err := p.queryTickers()
	if err != nil {
		return err
	}
	for _, v := range tickers {
		p.setTickerPair(v.ticker, v.pair.String())
	}
	return nil
}

// getAvailableAssets returns all available assets from the api.
// It returns a map of ticker IDs -> pairs.
func (p *AstroportProvider) getAvailableAssets() (map[string]types.CurrencyPair, error) {
	res, err := p.client.Get(p.endpoints.Rest + assetsURL)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	bz, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	astroportAssets := []map[string]AstroportAssetResponse{}
	if err := json.Unmarshal(bz, &astroportAssets); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body: %w", err)
	}

	// convert the astroport assets to a map of ticker IDs -> pairs
	availablePairs := map[string]types.CurrencyPair{}
	for _, assetMap := range astroportAssets {
		for tickerID, asset := range assetMap {
			// Some responses can return a 0 number value for Quote Symbol which
			// needs to be handled here.
			var quoteSymbol string
			switch v := asset.QuoteSymbol.(type) {
			case string:
				quoteSymbol = strings.ToUpper(v)
			default:
				quoteSymbol = ""
			}

			availablePairs[tickerID] = types.CurrencyPair{
				Base:  strings.ToUpper(asset.BaseSymbol),
				Quote: quoteSymbol,
			}
		}
	}
	return availablePairs, nil
}

// queryTickers returns the AstroportTickerPairs available from the API.
func (p *AstroportProvider) queryTickers() ([]AstroportTickerPairs, error) {
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

	availableAssets, err := p.getAvailableAssets()
	if err != nil {
		return nil, err
	}

	// filter out tickers that we are not subscribed to
	tickers := []AstroportTickerPairs{}
	for tickerID, v := range availableAssets {
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
func (p *AstroportProvider) poll() error {
	for {
		select {
		case <-p.ctx.Done():
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
