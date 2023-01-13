package provider

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/ojo/v3/util/coin"
)

const (
	alphaVantageRestUrl        = "https://www.alphavantage.co/query?"
	alphaVantageCandleEndpoint = "function=FX_INTRADAY&interval=15min"
	alphaVantageAPIKey         = "E3DX2T5QYNZ0ERVM"

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
		client  *http.Client

		logger zerolog.Logger
		mtx    sync.RWMutex

		// candleCache is the cache of candle prices for assets.
		candleCache map[string][]types.CandlePrice
	}

	AlphaVantageTimeSeriesData struct {
		Time   time.Time
		Open   float64
		High   float64
		Low    float64
		Close  float64
		Volume float64
	}
)

func NewAlphaVantageProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoint Endpoint,
	pairs ...types.CurrencyPair,
) *AlphaVantageProvider {
	restURL := alphaVantageRestUrl

	if endpoint.Name == ProviderAlphaVantage {
		restURL = endpoint.Rest
	}

	alphavantage := AlphaVantageProvider{
		baseURL:      restURL,
		client:       newDefaultHTTPClient(),
		logger:       logger,
		candleCache:  nil,
	}

	go func() {
		logger.Debug().Msg("starting alphavantage polling...")
		err := alphavantage.pollCache(ctx, pairs...)
		if err != nil {
			logger.Err(err).Msg("ftx provider unable to poll new data")
		}
	}()

	return &alphavantage
}

// SubscribeCurrencyPairs performs a no-op since alphavantage does not use websockets
func (p *AlphaVantageProvider) SubscribeCurrencyPairs(pairs ...types.CurrencyPair) error {
	return nil
}

func (p *AlphaVantageProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]types.TickerPrice, error) {

}

// pollCache polls the markets and candles endpoints,
// and updates the alphavantage cache.
func (p *AlphaVantageProvider) pollCache(ctx context.Context, pairs ...types.CurrencyPair) error {
	for {
		select {
		case <-ctx.Done():
			return nil

		default:
			p.logger.Debug().Msg("querying alphavantage api")

			err := p.pollMarkets()
			if err != nil {
				return err
			}
			err = p.pollCandles(pairs...)
			if err != nil {
				return err
			}

			time.Sleep(cacheInterval)
		}
	}
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
			return nil, fmt.Errorf("missing candles for %s", pair.String())
		}
		candlePrices[pair.String()] = candleCache[pair.String()]
	}

	return candlePrices, nil
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p *AlphaVantageProvider) GetAvailablePairs() (map[string]struct{}, error) {
	markets := p.getMarketsCache()
	availablePairs := make(map[string]struct{}, len(markets))
	for _, pair := range markets {
		cp := types.CurrencyPair{
			Base:  strings.ToUpper(pair.Base),
			Quote: strings.ToUpper(pair.Quote),
		}
		availablePairs[cp.String()] = struct{}{}
	}

	return availablePairs, nil
}

// pollMarkets retrieves the candles response from the ftx api and
// places it in p.candleCache.
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
			alphaVantageAPIKey,
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

		bz, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read AlphaVantage candle response body: %w", err)
		}

		var candlesResp AlphaVantageCandleResponse
		if err := json.Unmarshal(bz, &candlesResp); err != nil {
			return fmt.Errorf("failed to unmarshal AlphaVantage response body: %w", err)
		}

		candlePrices := []types.CandlePrice{}
		for _, responseCandle := range candlesResp.Candle {
			// the ftx api does not provide the endtime for these candles,
			// so we have to calculate it
			candleStart, err := responseCandle.parseTime()
			if err != nil {
				return err
			}
			candleEnd := candleStart.Add(candleWindowLength).Unix() * int64(time.Second/time.Millisecond)

			candlePrices = append(candlePrices, types.CandlePrice{
				Price:     coin.MustNewDecFromFloat(responseCandle.Price),
				Volume:    coin.MustNewDecFromFloat(responseCandle.Volume),
				TimeStamp: candleEnd,
			})
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
