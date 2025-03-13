package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"

	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	"github.com/ojo-network/price-feeder/oracle/queries"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/usecase"
	"github.com/ojo-network/price-feeder/usecase/entity"
)

const (
	binanceWSHost        = "stream.binance.com:9443"
	binanceUSWSHost      = "stream.binance.us:9443"
	binanceWSPath        = "/ws/ojostream"
	binanceRestHost      = "https://api1.binance.com"
	binanceRestUSHost    = "https://api.binance.us"
	binanceRestPath      = "/api/v3/ticker/price"
	binanceRestDepthPath = "/api/v3/depth"
)

var _ Provider = (*BinanceProvider)(nil)

type (
	// BinanceProvider defines an Oracle provider implemented by the Binance public
	// API.
	//
	// REF: https://binance-docs.github.io/apidocs/spot/en/#individual-symbol-mini-ticker-stream
	// REF: https://binance-docs.github.io/apidocs/spot/en/#kline-candlestick-streams
	BinanceProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	// BinanceTicker ticker price response. https://pkg.go.dev/encoding/json#Unmarshal
	// Unmarshal matches incoming object keys to the keys used by Marshal (either the
	// struct field name or its tag), preferring an exact match but also accepting a
	// case-insensitive match. C field which is Statistics close time is not used, but
	// it avoids to implement specific UnmarshalJSON.
	BinanceTicker struct {
		Symbol    string `json:"s"` // Symbol ex.: BTCUSDT
		LastPrice string `json:"c"` // Last price ex.: 0.0025
		Volume    string `json:"v"` // Total traded base asset volume ex.: 1000
		C         uint64 `json:"C"` // Statistics close time
	}

	BinanceDepth struct {
		E  string     `json:"e"` // depthUpdate // Event type
		E0 int64      `json:"E"` // Event time
		S  string     `json:"s"` // Symbol ex.: BTCUSDT
		U  int64      `json:"U"` // First update ID in event
		U0 int64      `json:"u"` // Final update ID in event
		B  [][]string `json:"b"` // Bids to be updated
		A  [][]string `json:"a"` // Asks to be updated
	}

	// BinanceCandleMetadata candle metadata used to compute tvwap price.
	BinanceCandleMetadata struct {
		Close     string `json:"c"` // Price at close
		TimeStamp int64  `json:"T"` // Close time in unix epoch ex.: 1645756200000
		Volume    string `json:"v"` // Volume during period
	}

	// BinanceCandle candle binance websocket channel "kline_1m" response.
	BinanceCandle struct {
		Symbol   string                `json:"s"` // Symbol ex.: BTCUSDT
		Metadata BinanceCandleMetadata `json:"k"` // Metadata for candle
	}

	// BinanceSubscribeMsg Msg to subscribe all the tickers channels.
	BinanceSubscriptionMsg struct {
		Method string   `json:"method"` // SUBSCRIBE/UNSUBSCRIBE
		Params []string `json:"params"` // streams to subscribe ex.: usdtatom@ticker
		ID     uint16   `json:"id"`     // identify messages going back and forth
	}

	// BinanceSubscriptionResp the response structure for a binance subscription response
	BinanceSubscriptionResp struct {
		Result string `json:"result"`
		ID     uint16 `json:"id"`
	}

	// BinancePairSummary defines the response structure for a Binance pair
	// summary.
	BinancePairSummary struct {
		Symbol string `json:"symbol"`
	}

	BinanceDepthDataResponse struct {
		LastUpdateID int64       `json:"lastUpdateId"`
		Asks         [][2]string `json:"asks"`
		Bids         [][2]string `json:"bids"`
	}
)

func NewBinanceProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	binanceUS bool,
	pairs ...types.CurrencyPair,
) (*BinanceProvider, error) {
	if (endpoints.Name) != ProviderBinance {
		if !binanceUS {
			endpoints = Endpoint{
				Name:      ProviderBinance,
				Rest:      binanceRestHost,
				Websocket: binanceWSHost,
			}
		} else {
			endpoints = Endpoint{
				Name:      ProviderBinanceUS,
				Rest:      binanceRestUSHost,
				Websocket: binanceUSWSHost,
			}
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   binanceWSPath,
	}

	binanceLogger := logger.With().Str("provider", string(ProviderBinance)).Logger()

	provider := &BinanceProvider{
		logger:     binanceLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(binanceLogger),
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

	provider.wsc = NewWebsocketController(
		ctx,
		endpoints.Name,
		wsURL,
		provider.getSubscriptionMsgs(confirmedPairs...),
		provider.messageReceived,
		disabledPingDuration,
		websocket.PingMessage,
		binanceLogger,
	)

	return provider, nil
}

func (p *BinanceProvider) StartConnections() {
	p.wsc.StartConnections()
}

func (p *BinanceProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(p.subscribedPairs)*2)
	for _, cp := range cps {
		binanceTickerPair := currencyPairToBinanceTickerPair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newBinanceSubscriptionMsg(binanceTickerPair))

		binanceCandlePair := currencyPairToBinanceCandlePair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newBinanceSubscriptionMsg(binanceCandlePair))

		if cp.PoolID > 0 {
			binanceDepthPair := currencyPairToBinanceDepthPair(cp)
			subscriptionMsgs = append(subscriptionMsgs, newBinanceSubscriptionMsg(binanceDepthPair))
		}
	}
	return subscriptionMsgs
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *BinanceProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

	newSubscriptionMsgs := p.getSubscriptionMsgs(confirmedPairs...)
	p.wsc.AddWebsocketConnection(
		newSubscriptionMsgs,
		p.messageReceived,
		disabledPingDuration,
		websocket.PingMessage,
	)
	p.setSubscribedPairs(confirmedPairs...)
}

func (p *BinanceProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	var (
		tickerResp       BinanceTicker
		tickerErr        error
		candleResp       BinanceCandle
		candleErr        error
		subscribeResp    BinanceSubscriptionResp
		subscribeRespErr error
		depthResp        BinanceDepth
		depthErr         error
	)

	tickerErr = json.Unmarshal(bz, &tickerResp)
	if len(tickerResp.LastPrice) != 0 {
		p.setTickerPair(tickerResp, tickerResp.Symbol)
		telemetryWebsocketMessage(ProviderBinance, MessageTypeTicker)
		return
	}

	candleErr = json.Unmarshal(bz, &candleResp)
	if len(candleResp.Metadata.Close) != 0 {
		p.setCandlePair(candleResp, candleResp.Symbol)
		telemetryWebsocketMessage(ProviderBinance, MessageTypeCandle)
		return
	}

	subscribeRespErr = json.Unmarshal(bz, &subscribeResp)
	if subscribeResp.ID == 1 {
		return
	}

	depthErr = json.Unmarshal(bz, &depthResp)
	if depthResp.E == "depthUpdate" {
		err := ProcessBinanceOrderBook(p, depthResp)
		p.logger.Error().AnErr("Error processing binance order book", err)
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		AnErr("depth", depthErr).
		AnErr("subscribeResp", subscribeRespErr).
		Msg("Error on receive message")
}

func (ticker BinanceTicker) toTickerPrice() (types.TickerPrice, error) {
	return types.NewTickerPrice(ticker.LastPrice, ticker.Volume)
}

func (candle BinanceCandle) toCandlePrice() (types.CandlePrice, error) {
	return types.NewCandlePrice(candle.Metadata.Close, candle.Metadata.Volume, candle.Metadata.TimeStamp)
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *BinanceProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + binanceRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var pairsSummary []BinancePairSummary
	if err := json.NewDecoder(resp.Body).Decode(&pairsSummary); err != nil {
		return nil, err
	}

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(pairName.Symbol)] = struct{}{}
	}

	return availablePairs, nil
}

func (p *BinanceProvider) GetSnapshotOrderBook(symbol string) (BinanceDepthDataResponse, error) {

	route := p.endpoints.Rest + binanceRestDepthPath + "?symbol=" + symbol + "&limit=5000"

	resp, err := http.Get(route)

	if err != nil {
		return BinanceDepthDataResponse{}, err
	}
	defer resp.Body.Close()
	var binanceDepthDataResponse BinanceDepthDataResponse

	if err := json.NewDecoder(resp.Body).Decode(&binanceDepthDataResponse); err != nil {
		return BinanceDepthDataResponse{}, err
	}

	return binanceDepthDataResponse, nil
}

const binanceProvider = "binance"

// GetExternalLiquidity returns external liquidity info on the provided pairs
func (p *BinanceProvider) GetExternalLiquidity(
	ammPools map[uint64]oracletypes.Pool,
	accountedPools map[uint64]oracletypes.AccountedPool,
	usdcDenom string,
	pairs ...types.CurrencyPair,
) (map[uint64]types.ExternalLiquidity, error) {
	externalLiquidity := make(map[uint64]types.ExternalLiquidity, len(pairs))
	for _, pair := range pairs {
		if pair.PoolID == 0 {
			continue
		}

		binanceOrderBookStore := NewOrderBookStore(p)
		binanceOrderBook, err := binanceOrderBookStore.GetOrderBook(pair.String())

		if err != nil {
			p.logger.Warn().
				Str("provider", binanceProvider).
				Interface("pair", pair).
				Err(err).
				Msg("ERROR_CALCULATING_EXTERNAL_LIQUIDITY")
			continue
		}

		bob := binanceOrderBook

		type order struct {
			price  float64
			amount float64
		}

		p.logger.Debug().Interface("asks", bob.Asks).Interface("bids", bob.Bids).Msg("Order book data")
		if bob.Bids == nil {
			return externalLiquidity, errors.New("error on bob.Bids ")
		}

		// Convertir los mapas a slices de pares clave-valor
		bidsSlice := make([]order, 0, len(bob.Bids))
		for price, amount := range bob.Bids {
			bidsSlice = append(bidsSlice, order{price, amount})
		}

		if bob.Asks == nil {
			return externalLiquidity, errors.New("error on bob.Asks ")
		}
		asksSlice := make([]order, 0, len(bob.Asks))
		for price, amount := range bob.Asks {
			asksSlice = append(asksSlice, order{price, amount})
		}

		sort.Slice(bidsSlice, func(i, j int) bool {
			return bidsSlice[i].price > bidsSlice[j].price
		})
		sort.Slice(asksSlice, func(i, j int) bool {
			return asksSlice[i].price < asksSlice[j].price
		})

		asks := [][2]float64{}
		bids := [][2]float64{}

		for _, bid := range bidsSlice {
			bids = append(bids, [2]float64{bid.price, bid.amount})
		}

		for _, ask := range asksSlice {
			asks = append(asks, [2]float64{ask.price, ask.amount})
		}

		depthDataEntity := entity.DepthData{
			Asks: asks,
			Bids: bids,
		}

		assetFound := true
		poolAssetInfo, err := queries.QueryExtLiqPoolAssetInfo(ammPools, accountedPools, pair.PoolID, usdcDenom)
		if err != nil {
			assetFound = false
			p.logger.Err(err).
				Str("provider", binanceProvider).
				Interface("pair", pair).
				Msg("ERROR_QUERYING_POOL_ASSET_INFO")
		}

		price, _ := p.GetTickerPrice(pair)
		externalLiquidityEntity, err := usecase.CalculateExternalLiquidityUseCase(
			depthDataEntity,
			poolAssetInfo,
			assetFound,
			price,
		)

		if err != nil {
			p.logger.Err(err).
				Str("provider", binanceProvider).
				Interface("pair", pair).
				Msg("ERROR_CALCULATING_EXTERNAL_LIQUIDITY")
			return externalLiquidity, err
		}

		baseAsset := pair.BaseProxy
		if baseAsset == "" {
			baseAsset = pair.Base
		}

		quoteAsset := pair.QuoteProxy
		if quoteAsset == "" {
			quoteAsset = pair.Quote
		}
		liq, err := types.NewExternalLiquidity(
			pair.PoolID,
			baseAsset,
			quoteAsset,
			externalLiquidityEntity.BaseAmount,
			externalLiquidityEntity.QuoteAmount,
			externalLiquidityEntity.BaseDepth,
			externalLiquidityEntity.QuoteDepth,
		)
		if err != nil {
			p.logger.Err(err).
				Str("provider", binanceProvider).
				Interface("pair", pair).
				Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
			return externalLiquidity, err
		}
		p.logger.Info().
			Str("provider", binanceProvider).
			Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")

		externalLiquidity[pair.PoolID] = liq
	}

	return externalLiquidity, nil
}

// currencyPairToBinanceTickerPair receives a currency pair and return binance
// ticker symbol atomusdt@ticker.
func currencyPairToBinanceTickerPair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@ticker")
}

func currencyPairToBinanceDepthPair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@depth")
}

// currencyPairToBinanceCandlePair receives a currency pair and return binance
// candle symbol atomusdt@kline_1s.
func currencyPairToBinanceCandlePair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@kline_1s")
}

// newBinanceSubscriptionMsg returns a new subscription Msg.
func newBinanceSubscriptionMsg(params ...string) BinanceSubscriptionMsg {
	return BinanceSubscriptionMsg{
		Method: "SUBSCRIBE",
		Params: params,
		ID:     1,
	}
}
