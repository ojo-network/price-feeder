package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	"github.com/ojo-network/price-feeder/oracle/queries"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/usecase"
	"github.com/ojo-network/price-feeder/usecase/entity"
)

type OKxOrderBookResponse struct {
	Code string            `json:"code"`
	Msg  string            `json:"msg"`
	Data []OKOrderBookData `json:"data"`
}

type OKOrderBookData struct {
	Asks [][]string `json:"asks"`
	Bids [][]string `json:"bids"`
	Ts   string     `json:"ts"`
}

const okxProvider = "okx"

func (p *OkxProvider) GetExternalLiquidity(
	ammPools map[uint64]oracletypes.Pool,
	accountedPools map[uint64]oracletypes.AccountedPool,
	usdcDenom string,
	pairs ...types.CurrencyPair,
) (map[uint64]types.ExternalLiquidity, error) {
	externalLiquidity := make(map[uint64]types.ExternalLiquidity, len(pairs))

	client := http.Client{
		Timeout: 1000 * time.Millisecond,
	}

	var wg sync.WaitGroup
	var mutex sync.Mutex

	for _, pair := range pairs {

		if pair.PoolID == 0 {
			continue
		}

		wg.Add(1)

		go func(pair types.CurrencyPair) {
			defer wg.Done()

			if err := p.rateLimiter.Wait(context.Background()); err != nil {
				p.logger.Err(err).
					Str("provider", okxProvider).
					Interface("pair", pair).
					Msg("RATE_LIMIT_WAIT_ERROR")
				return
			}

			route := p.endpoints.Rest + "/api/v5/market/books-full?instId=" + pair.Base + "-" + pair.Quote + "&sz=5000"
			resp, err := client.Get(route)
			if err != nil {
				p.logger.Err(err).
					Str("provider", okxProvider).
					Interface("pair", pair).
					Msg("ERROR_GETTING_ORDER_BOOK_ON_OKX")
				return
			}
			defer resp.Body.Close()

			var okxOrderBookResponse OKxOrderBookResponse
			if err := json.NewDecoder(resp.Body).Decode(&okxOrderBookResponse); err != nil {
				p.logger.Err(err).
					Str("provider", okxProvider).
					Interface("pair", pair).
					Msg("ERROR_DECODING_RESPONSE_OKX")
				return
			}

			depthDataEntity, err := oKXBookResponseToEntityDepthData(okxOrderBookResponse)

			if err != nil {
				p.logger.Err(err).
					Str("provider", okxProvider).
					Interface("pair", pair).
					Msg("ERROR_MAPPING_RESPONSE_OKX")

				return
			}

			assetFound := true
			poolAssetInfo, err := queries.QueryExtLiqPoolAssetInfo(ammPools, accountedPools, pair.PoolID, usdcDenom)
			if err != nil {
				assetFound = false
				p.logger.Err(err).
					Str("provider", okxProvider).
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
					Str("provider", okxProvider).
					Interface("pair", pair).
					Msg("ERROR_CALCULATING_EXTERNAL_LIQUIDITY")
				return
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
					Str("provider", okxProvider).
					Interface("pair", pair).
					Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
				return
			}

			p.logger.Info().
				Str("provider", okxProvider).
				Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")
			mutex.Lock()
			externalLiquidity[pair.PoolID] = liq
			mutex.Unlock()
		}(pair)

	}

	wg.Wait()
	return externalLiquidity, nil
}

func oKXBookResponseToEntityDepthData(book OKxOrderBookResponse) (entity.DepthData, error) {
	asks := [][2]float64{}
	bids := [][2]float64{}

	if len(book.Data) == 0 {
		return entity.DepthData{}, errors.New("okx order book response data is empty")
	}

	for _, ask := range book.Data[0].Asks {

		if len(ask) < 2 {
			continue
		}

		price, err := strconv.ParseFloat(ask[0], 64)

		if err != nil {
			return entity.DepthData{}, err
		}

		quantity, err := strconv.ParseFloat(ask[1], 64)

		if err != nil {
			return entity.DepthData{}, err
		}

		asks = append(asks, [2]float64{price, quantity})
	}

	for _, bid := range book.Data[0].Bids {

		if len(bid) < 2 {
			continue
		}

		price, err := strconv.ParseFloat(bid[0], 64)

		if err != nil {
			return entity.DepthData{}, err
		}

		quantity, err := strconv.ParseFloat(bid[1], 64)

		if err != nil {
			return entity.DepthData{}, err
		}

		bids = append(bids, [2]float64{price, quantity})
	}

	return entity.DepthData{
		Asks: asks,
		Bids: bids,
	}, nil

}
