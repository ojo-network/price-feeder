package provider

import (
	"sort"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	defaultCandlePeriod = 5 * time.Minute
)

// PriceStore is an embedded struct in each provider that manages the in memory
// store of subscribed currency pairs, candles prices, and ticker prices. It also
// handles thread safety and pruning of old candle prices.
type priceStore struct {
	tickers         map[string]types.TickerPrice
	candles         map[string][]types.CandlePrice
	subscribedPairs map[string]types.CurrencyPair
	candlePeriod    time.Duration

	subscribedPairsMtx sync.RWMutex
	tickerMtx          sync.RWMutex
	candleMtx          sync.RWMutex

	// currencyPairToTickerPair translates CurrencyPair the provider specific string map index
	currencyPairToTickerPair func(types.CurrencyPair) string

	// currencyPairToCandlePair translates CurrencyPair the provider specific string map index
	curencyPairToCandlePair func(types.CurrencyPair) string

	logger zerolog.Logger
}

// providerTicker is an interface that all provider tickers must implement to be
// stored in the priceStore.
type providerTicker interface {
	toTickerPrice() (types.TickerPrice, error)
}

// providerCandle is an interface that all provider candles must implement to be
// stored in the priceStore.
type providerCandle interface {
	toCandlePrice() (types.CandlePrice, error)
}

func defaultCurrencyPairTranslation(cp types.CurrencyPair) string {
	return cp.String()
}

func newPriceStore(logger zerolog.Logger) priceStore {
	return priceStore{
		tickers:                  map[string]types.TickerPrice{},
		candles:                  map[string][]types.CandlePrice{},
		subscribedPairs:          map[string]types.CurrencyPair{},
		candlePeriod:             defaultCandlePeriod,
		logger:                   logger,
		currencyPairToTickerPair: defaultCurrencyPairTranslation,
		curencyPairToCandlePair:  defaultCurrencyPairTranslation,
	}
}

func (ps *priceStore) setCurrencyPairToTickerAndCandlePair(f func(types.CurrencyPair) string) {
	ps.currencyPairToTickerPair = f
	ps.curencyPairToCandlePair = f
}

// setSubscribedPairs sets N currency pairs to the map of subscribed pairs.
func (ps *priceStore) setSubscribedPairs(cps ...types.CurrencyPair) {
	ps.subscribedPairsMtx.Lock()
	defer ps.subscribedPairsMtx.Unlock()

	for _, cp := range cps {
		ps.subscribedPairs[cp.String()] = cp
	}
}

// AddSubscribedPairs adds any unique currency pairs to the subscribed currency
// pairs map and returns the pairs added with the duplicates removed.
func (ps *priceStore) addSubscribedPairs(cps ...types.CurrencyPair) []types.CurrencyPair {
	newPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if ps.isSubscribed(cp.String()) {
			newPairs = append(newPairs, cp)
		}
	}
	ps.setSubscribedPairs(newPairs...)
	return newPairs
}

// isSubscribed returns true if the provider is subscribed to the currency pair.
func (ps *priceStore) isSubscribed(currencyPair string) bool {
	ps.subscribedPairsMtx.RLock()
	defer ps.subscribedPairsMtx.RUnlock()

	if _, ok := ps.subscribedPairs[currencyPair]; ok {
		return true
	}
	return false
}

// GetTickerPrices returns the tickerPrices based on the provided pairs. Returns an
// error if ANY of the currency pairs are not available.
func (ps *priceStore) GetTickerPrices(pairs ...types.CurrencyPair) (types.CurrencyPairTickers, error) {
	ps.tickerMtx.RLock()
	defer ps.tickerMtx.RUnlock()

	tickerPrices := make(types.CurrencyPairTickers, len(pairs))
	for _, cp := range pairs {
		key := ps.currencyPairToTickerPair(cp)
		ticker, ok := ps.tickers[key]
		if !ok {
			ps.logger.Error().Msgf("failed to get ticker price for %s", key)
			continue
		}
		tickerPrices[cp] = ticker
	}
	return tickerPrices, nil
}

// GetCandlePrices returns a copy of the the candlePrices based on the provided pairs.
// Returns an error if ANY of the currency pairs are not available
func (ps *priceStore) GetCandlePrices(pairs ...types.CurrencyPair) (types.CurrencyPairCandles, error) {
	ps.candleMtx.RLock()
	defer ps.candleMtx.RUnlock()

	candlePrices := make(types.CurrencyPairCandles, len(pairs))
	for _, cp := range pairs {
		key := ps.curencyPairToCandlePair(cp)
		candles, ok := ps.candles[key]
		if !ok {
			ps.logger.Error().Msgf("failed to get candle prices for %s", key)
			continue
		}
		candlesCopy := make([]types.CandlePrice, 0, len(candles))
		candlesCopy = append(candlesCopy, candles...)
		candlePrices[cp] = candlesCopy
	}
	return candlePrices, nil
}

// setTickerPair sets the ticker price for a currency pair string key specific to the provider.
// Logs an error and returns early if the providerTicker fails conversion to a TickerPrice.
func (ps *priceStore) setTickerPair(ticker providerTicker, currencyPair string) {
	ps.tickerMtx.Lock()
	defer ps.tickerMtx.Unlock()

	oracleTicker, err := ticker.toTickerPrice()
	if err != nil {
		ps.logger.Error().Err(err).Msg("failed to convert providerTicker to TickerPrice")
		return
	}
	ps.tickers[currencyPair] = oracleTicker
}

// setCandlePair sets the candle price for a currency pair string key specific to the provider.
// Logs an error and returns early if the providerCandle fails conversion to a CandlePrice.
func (ps *priceStore) setCandlePair(candle providerCandle, currencyPair string) {
	ps.candleMtx.Lock()
	defer ps.candleMtx.Unlock()

	oracleCandle, err := candle.toCandlePrice()
	if err != nil {
		ps.logger.Error().Err(err).Msg("failed to convert providerCandle to CandlePrice")
		return
	}

	ps.appendAndFilterCandles(oracleCandle, currencyPair)
}

// Does not acquire lock - must be called from parent function
func (ps *priceStore) appendAndFilterCandles(newCandle types.CandlePrice, currencyPair string) {
	staleTime := PastUnixTime(ps.candlePeriod)
	newCandles := []types.CandlePrice{newCandle}

	for _, c := range ps.candles[currencyPair] {
		if staleTime < c.TimeStamp {
			newCandles = append(newCandles, c)
		}
	}
	ps.candles[currencyPair] = newCandles
}

// All candles are in one min intervals where each candle starts exactly on the minute
func (ps *priceStore) addTradeToCandles(trade types.Trade, currencyPair string) {
	ps.candleMtx.Lock()
	defer ps.candleMtx.Unlock()

	tradeCandleStamp := time.Unix(trade.Time, 0).Truncate(time.Minute).Unix() + 60
	newCandle, err := types.NewCandlePrice(trade.Price, trade.Size, tradeCandleStamp)
	if err != nil {
		ps.logger.Error().Err(err).Msg("failed to parse trade values")
		return
	}

	if len(ps.candles[currencyPair]) == 0 {
		ps.candles[currencyPair] = []types.CandlePrice{newCandle}
		return
	}

	// Sort the candles specific to the currency pair by timestamp newest -> oldest
	sort.Slice(ps.candles[currencyPair], func(i, j int) bool {
		return ps.candles[currencyPair][i].TimeStamp > ps.candles[currencyPair][j].TimeStamp
	})

	// Try to find an existing candle that matches the trade
	for _, c := range ps.candles[currencyPair] {
		if c.TimeStamp == tradeCandleStamp {
			// If the timestamps are equal add the volume to the candle and set the price to the newest trade
			c.Price = newCandle.Price
			c.Volume = c.Volume.Add(newCandle.Volume)
			return
		} else if c.TimeStamp < tradeCandleStamp {
			// If we hit a candle that is older than the trade create a new candle
			ps.appendAndFilterCandles(newCandle, currencyPair)
			return
		}
	}

	// Existing candle not found - create a new candle
	ps.appendAndFilterCandles(newCandle, currencyPair)
}
