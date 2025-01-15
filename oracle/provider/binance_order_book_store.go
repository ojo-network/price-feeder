package provider

import (
	"errors"
	"strconv"
	"sync"

	"github.com/rs/zerolog/log"
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

func (obs *BinanceProviderBookStore) GetOrderBook(symbol string) (*BinanceOrderBook, error) {
	obs.mutex.RLock()
	defer obs.mutex.RUnlock()

	orderBook, ok := obs.orderBooks[symbol]

	if !ok {
		return nil, errors.New("order book not found")
	}

	return orderBook, nil
}

func (obs *BinanceProviderBookStore) SetOrderBook(orderBook *BinanceOrderBook, sync bool) error {

	gotOrderBook, err := binanceProviderBookStore.GetOrderBook(orderBook.Symbol)
	obs.mutex.Lock()
	defer obs.mutex.Unlock()
	if err != nil {
		binanceProviderBookStore.orderBooks[orderBook.Symbol] = orderBook
		return nil
	}

	if sync {
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

	gotOrderBook, err := binanceProviderBookStore.GetOrderBook(symbol)

	if err != nil {
		snapshot, err := binanceProvider.GetSnapshotOrderBook(symbol)

		if err != nil {
			return err
		}

		orderBook := binanceDepthDataResponseToBinanceOrderBook(snapshot)
		orderBook.Symbol = symbol
		binanceProviderBookStore.SetOrderBook(&orderBook, false)

		return nil
	}

	last_update_id := gotOrderBook.lastU

	if binanceDepth.U0 <= int64(last_update_id) {
		log.Info().Str("pair", symbol).Uint64("last_update_id", uint64(last_update_id)).Msg("BINANCE_ORDER_BOOK_STORE")
		return nil
	}

	if binanceDepth.U <= last_update_id+1 && binanceDepth.U0 >= last_update_id+1 {
		orderBook := binanceDepthToBinanceOrderBook(binanceDepth)
		binanceProviderBookStore.SetOrderBook(&orderBook, false)
		log.Debug().Str("event", "ok").Str("pair", symbol).Msg("BINANCE_ORDER_BOOK_STORE")
		return nil
	}

	log.Info().Str("event", "sync").Str("pair", symbol).Uint64("last_update_id", uint64(last_update_id)).Msg("BINANCE_ORDER_BOOK_STORE")
	snapshot, err := binanceProvider.GetSnapshotOrderBook(symbol)

	if err != nil {
		return err
	}
	orderBook := binanceDepthDataResponseToBinanceOrderBook(snapshot)
	orderBook.Symbol = symbol
	binanceProviderBookStore.SetOrderBook(&orderBook, true)
	return nil
}

// MAPPERS
func binanceDepthDataResponseToBinanceOrderBook(bddr BinanceDepthDataResponse) BinanceOrderBook {
	binanceOrderBook := BinanceOrderBook{
		Bids:  map[float64]float64{},
		Asks:  map[float64]float64{},
		lastU: bddr.LastUpdateId,
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
