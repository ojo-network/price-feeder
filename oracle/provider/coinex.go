package provider

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	"github.com/rs/zerolog"

	"github.com/ojo-network/price-feeder/oracle/provider/coinex/model"
	"github.com/ojo-network/price-feeder/oracle/queries"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/usecase"
	"github.com/ojo-network/price-feeder/usecase/entity"
)

const (
	coinExWSHost         = "socket.coinex.com"
	coinExWSPath         = "/v2/spot"
	coinExRestHost       = "https://api.coinex.com"
	coinExRestPath       = "/v2/spot/ticker"
	coinExRestDepthPath  = "/v2/spot/depth"
	coinExRestCandlePath = "/v2/spot/kline"
)

var _ Provider = (*CoinExProvider)(nil)

type (
	CoinExProvider struct {
		wsc       *WebsocketController
		logger    zerolog.Logger
		mtx       sync.RWMutex
		endpoints Endpoint

		priceStore
	}

	// CoinExTicker ticker price response. https://pkg.go.dev/encoding/json#Unmarshal
	// Unmarshal matches incoming object keys to the keys used by Marshal (either the
	// struct field name or its tag), preferring an exact match but also accepting a
	// case-insensitive match. C field which is Statistics close time is not used, but
	// it avoids to implement specific UnmarshalJSON.
	CoinExTicker struct {
		Symbol    string `json:"s"` // Symbol ex.: BTCUSDT
		LastPrice string `json:"c"` // Last price ex.: 0.0025
		Volume    string `json:"v"` // Total traded base asset volume ex.: 1000
		C         uint64 `json:"C"` // Statistics close time
	}

	// CoinExCandleMetadata candle metadata used to compute tvwap price.
	CoinExCandleMetadata struct {
		Close     string `json:"c"` // Price at close
		TimeStamp int64  `json:"T"` // Close time in unix epoch ex.: 1645756200000
		Volume    string `json:"v"` // Volume during period
	}

	// CoinExCandle candle CoinEx websocket channel "kline_1m" response.
	CoinExCandle struct {
		Symbol   string               `json:"s"` // Symbol ex.: BTCUSDT
		Metadata CoinExCandleMetadata `json:"k"` // Metadata for candle
	}

	// CoinExSubscribeMsg Msg to subscribe all the tickers channels.
	CoinExSubscriptionMsg struct {
		Method string                  `json:"method"`
		Params CoinExSubscriptioParams `json:"params"`
		ID     int                     `json:"id"`
	}

	CoinExSubscriptioParams struct {
		MarketList []string `json:"market_list"` // Channels to be subscribed ex. ticker.ATOM_USDT
	}

	CoinExSubscriptionMarketList []string
	// CoinExSubscriptionResp the response structure for a CoinEx subscription response
	CoinExSubscriptionResp struct {
		Result string `json:"result"`
		ID     uint16 `json:"id"`
	}
)

func NewCoinExProvider(
	ctx context.Context,
	logger zerolog.Logger,
	endpoints Endpoint,
	pairs ...types.CurrencyPair,
) (*CoinExProvider, error) {
	if (endpoints.Name) != ProviderCoinEx {
		endpoints = Endpoint{
			Name:      ProviderCoinEx,
			Rest:      coinExRestHost,
			Websocket: coinExWSHost,
		}
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   endpoints.Websocket,
		Path:   coinExWSPath,
	}

	CoinExLogger := logger.With().Str("provider", string(ProviderCoinEx)).Logger()

	provider := &CoinExProvider{
		logger:     CoinExLogger,
		endpoints:  endpoints,
		priceStore: newPriceStore(CoinExLogger),
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
		CoinExLogger,
	)

	return provider, nil
}

func (p *CoinExProvider) StartConnections() {
	p.wsc.StartConnections()
}

func (p *CoinExProvider) getSubscriptionMsgs(cps ...types.CurrencyPair) []interface{} {
	subscriptionMsgs := make([]interface{}, 0, len(p.subscribedPairs)*2)
	for _, cp := range cps {
		CoinExTickerPair := currencyPairToCoinExTickerPair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newCoinExSubscriptionMsg(CoinExTickerPair))

		CoinExCandlePair := currencyPairToCoinExCandlePair(cp)
		subscriptionMsgs = append(subscriptionMsgs, newCoinExSubscriptionMsg(CoinExCandlePair))
	}
	return subscriptionMsgs
}

// SubscribeCurrencyPairs sends the new subscription messages to the websocket
// and adds them to the providers subscribedPairs array
func (p *CoinExProvider) SubscribeCurrencyPairs(cps ...types.CurrencyPair) {
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

func (p *CoinExProvider) messageReceived(_ int, _ *WebsocketConnection, bz []byte) {
	var (
		tickerErr        error
		candleErr        error
		subscribeRespErr error
	)

	reader, err := gzip.NewReader(bytes.NewReader(bz))
	if err != nil {
		p.logger.Error().
			Err(err).
			Int("length", len(bz)).
			AnErr("ticker", tickerErr).
			Str("provider", coinExProvider).
			Str("event", "gzip").
			Msg("ERROR_CREATING_NEW_READER")
		return
	}
	defer reader.Close()

	if err != nil {

		p.logger.Error().
			Err(err).
			Int("length", len(bz)).
			AnErr("ticker", tickerErr).
			Str("provider", coinExProvider).
			Str("event", "gzip").
			Msg("ERROR_RECEIVING_MESSAGE")
		return
	}

	decompressed, err := io.ReadAll(reader)
	if err != nil {
		p.logger.Error().
			Err(err).
			Int("length", len(bz)).
			AnErr("ticker", tickerErr).
			Str("provider", coinExProvider).
			Str("event", "io.ReadAll").
			Msg("ERROR_RECEIVING_MESSAGE")
	}

	spotTickerResponse := model.StateUpdateResponse{}
	err = json.Unmarshal(decompressed, &spotTickerResponse)

	if err != nil {
		p.logger.Error().
			Err(err).
			Int("length", len(bz)).
			AnErr("ticker", tickerErr).
			Str("provider", coinExProvider).
			Str("event", "json.unmarshal").
			Str("message", string(decompressed)).
			Msg("ERROR_RECEIVING_MESSAGE")
		return
	}

	if spotTickerResponse.Method == "state.update" {

		for _, state := range spotTickerResponse.Data.StateList {
			coinExTicker := CoinExTicker{
				Symbol:    state.Market,
				LastPrice: state.Last,
				Volume:    state.Volume,
			}

			p.setTickerPair(coinExTicker, coinExTicker.Symbol)
			telemetryWebsocketMessage(ProviderCoinEx, MessageTypeTicker)

			// go p.GetAndSetCandle(state.Market)
		}
		return
	}

	p.logger.Error().
		Int("length", len(bz)).
		AnErr("ticker", tickerErr).
		AnErr("candle", candleErr).
		AnErr("subscribeResp", subscribeRespErr).
		Msg("Error on receive message")
}

func (ticker CoinExTicker) toTickerPrice() (types.TickerPrice, error) {
	return types.NewTickerPrice(ticker.LastPrice, ticker.Volume)
}

func (candle CoinExCandle) toCandlePrice() (types.CandlePrice, error) {
	return types.NewCandlePrice(candle.Metadata.Close, candle.Metadata.Volume, candle.Metadata.TimeStamp)
}

// GetAvailablePairs returns all pairs to which the provider can subscribe.
// ex.: map["ATOMUSDT" => {}, "OJOUSDC" => {}].
func (p *CoinExProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.endpoints.Rest + coinExRestPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Err(err).
			Str("provider", coinExProvider).
			Msg("ERROR_GETTING_AVAILABLE_PAIRS")
		return nil, err
	}

	var spotTickerResponse model.SpotTickerResponse
	if err := json.Unmarshal(b, &spotTickerResponse); err != nil {
		p.logger.Err(err).
			Str("provider", coinExProvider).
			Interface("response", resp).
			Msg("ERROR_GETTING_AVAILABLE_PAIRS")

		return nil, err
	}

	pairsSummary := spotTickerResponse.Data

	availablePairs := make(map[string]struct{}, len(pairsSummary))
	for _, pairName := range pairsSummary {
		availablePairs[strings.ToUpper(pairName.Market)] = struct{}{}
	}

	return availablePairs, nil
}

const coinExProvider = "coinex"

func (p *CoinExProvider) GetAndSetCandle(pair string) {
	//https://docs.coinex.com/api/v2/spot/market/http/list-market-kline
	route := p.endpoints.Rest + coinExRestCandlePath + "?market=" + pair + "&limit=1&period=1min"
	resp, err := http.Get(route)
	if err != nil {
		p.logger.Err(err).
			Str("route", route).
			Str("provider", coinExProvider).
			Interface("pair", pair).
			Msg("ERROR_GETTING_CANDLE_INFO")
		return
	}
	defer resp.Body.Close()

	candleResponse := model.CandleResponse{}

	if err := json.NewDecoder(resp.Body).Decode(&candleResponse); err != nil {
		p.logger.Err(err).
			Str("provider", gateProvider).
			Interface("pair", pair).
			Msg("ERROR_DECODING_RESPONSE_SPOT_ORDER_BOOK")
		return
	}

	if len(candleResponse.Data) == 0 {

		errCandleResponseData := errors.New("error there is not data in coinEx candle response")

		p.logger.Err(errCandleResponseData).
			Str("route", route).
			Str("provider", gateProvider).
			Interface("pair", pair).
			Msg("ERROR_THERE_IS_NOT_DATA_IN_COINEX_CANDLE_RESPONSE")

		return
	}

	candle := CoinExCandle{}

	for _, data := range candleResponse.Data {
		candle.Symbol = pair
		candle.Metadata.Close = data.Close
		candle.Metadata.TimeStamp = data.CreatedAt
		candle.Metadata.Volume = data.Volume
		break
	}

	p.setCandlePair(candle, pair)
}

// GetExternalLiquidity returns external liquidity info on the provided pairs
func (p *CoinExProvider) GetExternalLiquidity(
	ammPools map[uint64]oracletypes.Pool,
	accountedPools map[uint64]oracletypes.AccountedPool,
	pairs ...types.CurrencyPair,
) (map[uint64]types.ExternalLiquidity, error) {
	externalLiquidity := make(map[uint64]types.ExternalLiquidity, len(pairs))
	for _, pair := range pairs {
		if pair.PoolID == 0 {
			continue
		}
		// https://api.coinex.com/v2/spot/depth?market=STARSUSDT&limit=50&interval=0.001
		route := p.endpoints.Rest + coinExRestDepthPath + "?market=" + pair.Base + pair.Quote + "&limit=50&interval=0.001"
		resp, err := http.Get(route)
		if err != nil {
			p.logger.Err(err).
				Str("route", route).
				Str("provider", coinExProvider).
				Interface("pair", pair).
				Msg("ERROR_GETTING_ORDER_BOOK")
			return nil, err
		}
		defer resp.Body.Close()

		depthData := model.DepthResponse{}
		if err := json.NewDecoder(resp.Body).Decode(&depthData); err != nil {
			p.logger.Err(err).
				Str("provider", gateProvider).
				Interface("pair", pair).
				Msg("ERROR_DECODING_RESPONSE_SPOT_ORDER_BOOK")
			return nil, err
		}

		asks := [][2]float64{}
		bids := [][2]float64{}

		for _, ask := range depthData.Data.Depth.Asks {
			price, err := strconv.ParseFloat(ask[0], 64)

			if err != nil {
				continue
			}

			quantity, err := strconv.ParseFloat(ask[1], 64)

			if err != nil {
				continue
			}

			asks = append(asks, [2]float64{price, quantity})
		}

		for _, bid := range depthData.Data.Depth.Bids {
			price, err := strconv.ParseFloat(bid[0], 64)

			if err != nil {
				continue
			}

			quantity, err := strconv.ParseFloat(bid[1], 64)

			if err != nil {
				continue
			}

			bids = append(bids, [2]float64{price, quantity})
		}

		depthDataEntity := entity.DepthData{
			Asks: asks,
			Bids: bids,
		}

		assetFound := true
		poolAssetInfo, err := queries.QueryExtLiqPoolAssetInfo(ammPools, accountedPools, pair.PoolID)
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
				Str("provider", coinExProvider).
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
			fmt.Sprintf("%f", externalLiquidityEntity.BaseAmount),
			fmt.Sprintf("%f", externalLiquidityEntity.QuoteAmount),
			fmt.Sprintf("%f", externalLiquidityEntity.BaseDepth),
			fmt.Sprintf("%f", externalLiquidityEntity.QuoteDepth),
		)
		if err != nil {
			p.logger.Err(err).
				Str("provider", coinExProvider).
				Interface("pair", pair).
				Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
			return externalLiquidity, err
		}
		p.logger.Info().
			Str("provider", coinExProvider).
			Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")

		externalLiquidity[pair.PoolID] = liq
	}

	return externalLiquidity, nil
}

// currencyPairToCoinExTickerPair receives a currency pair and return CoinEx
func currencyPairToCoinExTickerPair(cp types.CurrencyPair) string {
	return strings.ToUpper(cp.String())
}

// currencyPairToCoinExCandlePair receives a currency pair and return CoinEx
func currencyPairToCoinExCandlePair(cp types.CurrencyPair) string {
	return strings.ToLower(cp.String() + "@kline_1m")
}

// newCoinExSubscriptionMsg returns a new subscription Msg.
func newCoinExSubscriptionMsg(params ...string) CoinExSubscriptionMsg {

	return CoinExSubscriptionMsg{
		Method: "state.subscribe",
		Params: CoinExSubscriptioParams{
			MarketList: params,
		},
		ID: 1,
	}
}
