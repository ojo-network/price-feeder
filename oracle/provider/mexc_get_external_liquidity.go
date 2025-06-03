package provider

import (
	"context"
	"encoding/json"
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

const mexcProvider = "mexc"

type MexcOrderBookResponse struct {
	LastUpdateID int64      `json:"lastUpdateId"`
	Bids         [][]string `json:"bids"`
	Asks         [][]string `json:"asks"`
	Timestamp    int64      `json:"timestamp"`
}

// GetExternalLiquidity returns external liquidity info on the provided pairs
func (p *MexcProvider) GetExternalLiquidity(
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
					Str("provider", mexcProvider).
					Interface("pair", pair).
					Msg("RATE_LIMIT_WAIT_ERROR")
				return
			}

			route := mexcRestBase + "/api/v3/depth?symbol=" + pair.Base + pair.Quote + "&limit=5000"
			resp, err := client.Get(route)
			if err != nil {
				p.logger.Err(err).
					Str("provider", mexcProvider).
					Interface("pair", pair).
					Msg("ERROR_GETTING_ORDER_BOOK_ON_MEXC")
				return
			}
			defer resp.Body.Close()

			var MexcOrderBookResponse MexcOrderBookResponse
			if err := json.NewDecoder(resp.Body).Decode(&MexcOrderBookResponse); err != nil {
				p.logger.Err(err).
					Str("provider", mexcProvider).
					Interface("pair", pair).
					Msg("ERROR_DECODING_RESPONSE_MEXC")
				return
			}

			depthDataEntity, err := mexcBookResponseToEntityDepthData(MexcOrderBookResponse)
			if err != nil {
				p.logger.Err(err).
					Str("provider", mexcProvider).
					Interface("pair", pair).
					Msg("ERROR_MAPPING_RESPONSE_MEXC")
				return
			}

			assetFound := true
			poolAssetInfo, err := queries.QueryExtLiqPoolAssetInfo(ammPools, accountedPools, pair.PoolID, usdcDenom)
			if err != nil {
				assetFound = false
				p.logger.Err(err).
					Str("provider", mexcProvider).
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
					Str("provider", mexcProvider).
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
					Str("provider", mexcProvider).
					Interface("pair", pair).
					Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
				return
			}

			p.logger.Info().
				Str("provider", mexcProvider).
				Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")
			mutex.Lock()
			externalLiquidity[pair.PoolID] = liq
			mutex.Unlock()

		}(pair)
	}

	wg.Wait()

	return externalLiquidity, nil
}

func mexcBookResponseToEntityDepthData(book MexcOrderBookResponse) (entity.DepthData, error) {

	asks := [][2]float64{}
	bids := [][2]float64{}

	for _, ask := range book.Asks {

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

	for _, bid := range book.Bids {

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
