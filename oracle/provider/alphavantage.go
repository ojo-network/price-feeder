package provider

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/util/alphavantage"
	"github.com/rs/zerolog"
)

const (
	alphaVantageRestURL        = "https://www.alphavantage.co/query?"
	alphaVantageCandleEndpoint = "function=FX_INTRADAY&interval=15min&datatypesize=csv&output=compact"

	cacheInterval = 500 * time.Millisecond
)

var _ Provider = (*AlphaVantageProvider)(nil)

type (
	// AlphaVantageProvider defines an Oracle provider implemented by the AlphaVantage
	// API.
	//
	// REF: https://www.alphavantage.co/documentation/
	AlphaVantageProvider struct {
		baseURL string
		apiKey  string
		client  *http.Client

		logger zerolog.Logger
		mtx    sync.RWMutex

		// candleCache is the cache of candle prices for assets.
		candleCache map[string][]types.CandlePrice
	}
)

func NewAlphaVantageProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) *AlphaVantageProvider {
	if endpoints.Name != ProviderAlphaVantage {
		endpoints = Endpoint{
			Name: ProviderAlphaVantage,
			Rest: alphaVantageRestURL,
		}
	}

	alphavantage := AlphaVantageProvider{
		baseURL:     endpoints.Rest,
		apiKey:      endpoints.APIKey,
		client:      newDefaultHTTPClient(),
		logger:      logger,
		candleCache: nil,
	}

	go func() {
		logger.Debug().Msg("starting alphavantage polling...")
		err := alphavantage.pollCache(ctx, pairs...)
		if err != nil {
			logger.Err(err).Msg("alphavantage provider unable to poll new data")
		}
	}()

	return &alphavantage
}

// SubscribeCurrencyPairs performs a no-op since alphavantage does not use websockets
func (p *AlphaVantageProvider) SubscribeCurrencyPairs(...types.CurrencyPair) error {
	return nil
}

// GetTickerPrices uses the cached candlePrices to return ticker prices based on the provided pairs.
func (p *AlphaVantageProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]types.TickerPrice, error) {
	candleCache := p.getCandleCache()
	if len(candleCache) < 1 {
		return nil, fmt.Errorf("candles have not been cached")
	}

	tickerPrices := make(map[string]types.TickerPrice, len(pairs))
	for _, pair := range pairs {
		if _, ok := candleCache[pair.String()]; !ok {
			return nil, fmt.Errorf("alphavantage failed to get ticker price for %s", pair.String())
		}
		// construct ticker with most recent candle
		latestCandle := candleCache[pair.String()][len(candleCache[pair.String()])-1]
		tickerPrices[pair.String()] = types.TickerPrice{
			Price:  latestCandle.Price,
			Volume: latestCandle.Volume,
		}
	}

	return tickerPrices, nil
}

// GetCandlePrices returns the cached candlePrices based on provided pairs.
func (p *AlphaVantageProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]types.CandlePrice, error) {
	candleCache := p.getCandleCache()
	if len(candleCache) < 1 {
		return nil, fmt.Errorf("candles have not been cached")
	}

	candlePrices := make(map[string][]types.CandlePrice, len(pairs))
	for _, pair := range pairs {
		if _, ok := candleCache[pair.String()]; !ok {
			return nil, fmt.Errorf("alphavantage failed to get candle price for %s", pair.String())
		}
		candlePrices[pair.String()] = candleCache[pair.String()]
	}

	return candlePrices, nil
}

// pollCache polls the c andles endpoint and updates the alphavantage cache.
func (p *AlphaVantageProvider) pollCache(ctx context.Context, pairs ...types.CurrencyPair) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		default:
			p.logger.Debug().Msg("querying alphavantage api")

			err := p.pollCandles(pairs...)
			if err != nil {
				return err
			}

			time.Sleep(cacheInterval)
		}
	}
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p *AlphaVantageProvider) GetAvailablePairs() (map[string]struct{}, error) {
	return nil, nil
}

// pollCandles retrieves the candles response from the alphavantage api
// and places it in p.candleCache.
func (p *AlphaVantageProvider) pollCandles(pairs ...types.CurrencyPair) error {
	candles := make(map[string][]types.CandlePrice)

	for _, pair := range pairs {
		if _, ok := candles[pair.Base]; !ok {
			candles[pair.String()] = []types.CandlePrice{}
		}

		path := fmt.Sprintf("%s%s&from_symbol=%s&to_symbol=%s&apikey=%s",
			p.baseURL,
			alphaVantageCandleEndpoint,
			pair.Base,
			pair.Quote,
			p.apiKey,
		)

		resp, err := p.client.Get(path)
		if err != nil {
			return fmt.Errorf("failed to make AlphaVantage candle request: %w", err)
		}
		err = checkHTTPStatus(resp)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		timeSeriesData, err := alphavantage.ParseTimeSeriesData(resp.Body)
		if err != nil {
			return err
		}

		candlePrices := []types.CandlePrice{}
		for _, timeSeries := range timeSeriesData {
			candlePrice, err := types.NewCandlePrice(
				string(ProviderAlphaVantage),
				pair.String(),
				strconv.FormatFloat(timeSeries.Close, 'f', -1, 64),
				strconv.FormatFloat(timeSeries.Volume, 'f', -1, 64),
				timeSeries.Time.Unix(),
			)
			if err != nil {
				return err
			}
			candlePrices = append(candlePrices, candlePrice)
		}
		candles[pair.String()] = candlePrices
	}

	p.setCandleCache(candles)
	return nil
}

func (p *AlphaVantageProvider) setCandleCache(c map[string][]types.CandlePrice) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.candleCache = c
}

func (p *AlphaVantageProvider) getCandleCache() map[string][]types.CandlePrice {
	p.mtx.RLock()
	defer p.mtx.RUnlock()
	return p.candleCache
}
