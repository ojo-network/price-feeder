package provider

import (
	"errors"
	"strconv"
	"sync"
)

var (
	lock                     = &sync.Mutex{}
	binanceProviderBookStore *BinanceProviderBookStore
)

type BinanceProviderBookStore struct {
	orderBooks      map[string]*BinanceOrderBook
	binanceProvider *BinanceProvider
	mutex           sync.RWMutex
}

type BinanceOrderBook struct {
	Symbol string
	Bids   map[float64]float64
	Asks   map[float64]float64
	lastU  int64
}

func NewOrderBookStore(binanceProvider *BinanceProvider) *BinanceProviderBookStore {
	lock.Lock()
	defer lock.Unlock()

	if binanceProviderBookStore == nil {
		binanceProviderBookStore = &BinanceProviderBookStore{
			orderBooks:      map[string]*BinanceOrderBook{},
			binanceProvider: binanceProvider,
		}
	}
	return binanceProviderBookStore
}

func (obs *BinanceProviderBookStore) GetOrderBook(symbol string) (BinanceOrderBook, error) {
	obs.mutex.RLock()
	defer obs.mutex.RUnlock()

	orderBook, ok := obs.orderBooks[symbol]

	if !ok {
		return BinanceOrderBook{}, errors.New("order book not found")
	}

	// Copiar mapas de bids y asks
	bidsCopy := make(map[float64]float64, len(orderBook.Bids))
	asksCopy := make(map[float64]float64, len(orderBook.Asks))
	for price, amount := range orderBook.Bids {
		bidsCopy[price] = amount
	}
	for price, amount := range orderBook.Asks {
		asksCopy[price] = amount
	}
	return BinanceOrderBook{
		Symbol: orderBook.Symbol,
		Bids:   bidsCopy,
		Asks:   asksCopy,
		lastU:  orderBook.lastU,
	}, nil
}

func (obs *BinanceProviderBookStore) SetOrderBook(orderBook *BinanceOrderBook, sync bool) error {
	obs.mutex.Lock()
	defer obs.mutex.Unlock()

	gotOrderBook, ok := binanceProviderBookStore.orderBooks[orderBook.Symbol]
	if !ok || sync {
		binanceProviderBookStore.orderBooks[orderBook.Symbol] = orderBook
		return nil
	}

	gotOrderBook.lastU = orderBook.lastU

	// Process bids
	for price, quantity := range orderBook.Bids {
		if quantity == 0 {
			delete(gotOrderBook.Bids, price)
		} else {
			gotOrderBook.Bids[price] = quantity
		}
	}

	// Process asks
	for price, quantity := range orderBook.Asks {
		if quantity == 0 {
			delete(gotOrderBook.Asks, price)
		} else {
			gotOrderBook.Asks[price] = quantity
		}
	}

	return nil
}

func ProcessBinanceOrderBook(binanceProvider *BinanceProvider, binanceDepth BinanceDepth) error {
	binanceProviderBookStore := NewOrderBookStore(binanceProvider)

	symbol := binanceDepth.S

	gotOrderBook, ok := binanceProviderBookStore.orderBooks[symbol]

	if !ok {
		snapshot, err := binanceProvider.GetSnapshotOrderBook(symbol)

		if err != nil {
			return err
		}

		orderBook := binanceDepthDataResponseToBinanceOrderBook(snapshot)
		orderBook.Symbol = symbol
		err = binanceProviderBookStore.SetOrderBook(&orderBook, false)
		if err != nil {
			return err
		}

		return nil
	}

	lastUpdateID := gotOrderBook.lastU

	if binanceDepth.U0 <= lastUpdateID {
		binanceProvider.logger.Info().Str("pair", symbol).Uint64("last_update_id", uint64(lastUpdateID)).
			Msg("BINANCE_ORDER_BOOK_STORE")
		return nil
	}

	if binanceDepth.U <= lastUpdateID+1 && binanceDepth.U0 >= lastUpdateID+1 {
		orderBook := binanceDepthToBinanceOrderBook(binanceDepth)
		err := binanceProviderBookStore.SetOrderBook(&orderBook, false)
		if err != nil {
			return err
		}

		binanceProvider.logger.Debug().Str("event", "ok").Str("pair", symbol).Msg("BINANCE_ORDER_BOOK_STORE")
		return nil
	}

	binanceProvider.logger.Info().Str("event", "sync").Str("pair", symbol).Uint64("last_update_id", uint64(lastUpdateID)).
		Msg("BINANCE_ORDER_BOOK_STORE")
	snapshot, err := binanceProvider.GetSnapshotOrderBook(symbol)
	if err != nil {
		return err
	}

	orderBook := binanceDepthDataResponseToBinanceOrderBook(snapshot)
	orderBook.Symbol = symbol
	err = binanceProviderBookStore.SetOrderBook(&orderBook, true)
	if err != nil {
		return err
	}

	return nil
}

// MAPPERS
func binanceDepthDataResponseToBinanceOrderBook(bddr BinanceDepthDataResponse) BinanceOrderBook {
	binanceOrderBook := BinanceOrderBook{
		Bids:  map[float64]float64{},
		Asks:  map[float64]float64{},
		lastU: bddr.LastUpdateID,
	}

	for _, bid := range bddr.Bids {

		price, err := strconv.ParseFloat(bid[0], 64)

		if err != nil {
			continue
		}

		quantity, err := strconv.ParseFloat(bid[1], 64)

		if err != nil {
			continue
		}

		binanceOrderBook.Bids[price] = quantity
	}

	for _, ask := range bddr.Asks {

		price, err := strconv.ParseFloat(ask[0], 64)

		if err != nil {
			continue
		}

		quantity, err := strconv.ParseFloat(ask[1], 64)

		if err != nil {
			continue
		}

		binanceOrderBook.Asks[price] = quantity
	}

	return binanceOrderBook
}

func binanceDepthToBinanceOrderBook(binanceDepth BinanceDepth) BinanceOrderBook {
	binanceOrderBook := BinanceOrderBook{
		Bids:   map[float64]float64{},
		Asks:   map[float64]float64{},
		lastU:  binanceDepth.U0,
		Symbol: binanceDepth.S,
	}

	for _, bid := range binanceDepth.B {
		price, err := strconv.ParseFloat(bid[0], 64)

		if err != nil {
			continue
		}

		quantity, err := strconv.ParseFloat(bid[1], 64)

		if err != nil {
			continue
		}

		binanceOrderBook.Bids[price] = quantity
	}

	for _, ask := range binanceDepth.A {

		price, err := strconv.ParseFloat(ask[0], 64)

		if err != nil {
			continue
		}

		quantity, err := strconv.ParseFloat(ask[1], 64)

		if err != nil {
			continue
		}

		binanceOrderBook.Asks[price] = quantity
	}

	return binanceOrderBook
}

/*
func GetBinanceSnapshotOrderBook(symbol string) (BinanceOrderBook, error) {
	route := p.endpoints.Rest + binanceRestDepthPath + "?symbol=" + pair.Base + pair.Quote + "&limit=5000"
}
*/
