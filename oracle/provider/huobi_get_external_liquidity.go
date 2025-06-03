package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	"github.com/ojo-network/price-feeder/oracle/queries"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/usecase"
	"github.com/ojo-network/price-feeder/usecase/entity"
)

const huobiProvider = "huobi"

type HuobiOrderBookResponse struct {
	Channel   string        `json:"ch"`
	Status    string        `json:"status"`
	Timestamp int64         `json:"ts"`
	Data      HuobiBookData `json:"tick"`
}

type HuobiBookData struct {
	Bids      [][2]float64 `json:"bids"`
	Asks      [][2]float64 `json:"asks"`
	Version   int64        `json:"version"`
	Timestamp int64        `json:"ts"`
}

// GetExternalLiquidity returns external liquidity info on the provided pairs
func (p *HuobiProvider) GetExternalLiquidity(
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
					Str("provider", huobiProvider).
					Interface("pair", pair).
					Msg("RATE_LIMIT_WAIT_ERROR")
				return
			}

			base := strings.ToLower(pair.Base)
			quote := strings.ToLower(pair.Quote)

			route := huobiRestHost + "/market/fullMbp?symbol=" + base + quote
			resp, err := client.Get(route)
			if err != nil {
				p.logger.Err(err).
					Str("provider", huobiProvider).
					Interface("pair", pair).
					Msg("ERROR_GETTING_ORDER_BOOK_ON_HUOBI")
				return
			}
			defer resp.Body.Close()

			var huobiOrderBookResponse HuobiOrderBookResponse
			if err := json.NewDecoder(resp.Body).Decode(&huobiOrderBookResponse); err != nil {
				p.logger.Err(err).
					Str("provider", huobiProvider).
					Interface("pair", pair).
					Msg("ERROR_DECODING_RESPONSE_HUOBI")
				return
			}

			depthDataEntity, err := huobiBookResponseToEntityDepthData(huobiOrderBookResponse)
			if err != nil {
				p.logger.Err(err).
					Str("provider", huobiProvider).
					Interface("pair", pair).
					Msg("ERROR_MAPPING_RESPONSE_HUOBI")
				return
			}

			assetFound := true
			poolAssetInfo, err := queries.QueryExtLiqPoolAssetInfo(ammPools, accountedPools, pair.PoolID, usdcDenom)
			if err != nil {
				assetFound = false
				p.logger.Err(err).
					Str("provider", huobiProvider).
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
					Str("provider", huobiProvider).
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
					Str("provider", huobiProvider).
					Interface("pair", pair).
					Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
				return
			}

			p.logger.Info().
				Str("provider", huobiProvider).
				Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")
			mutex.Lock()
			externalLiquidity[pair.PoolID] = liq
			mutex.Unlock()

		}(pair)
	}

	wg.Wait()
	return externalLiquidity, nil
}

func huobiBookResponseToEntityDepthData(book HuobiOrderBookResponse) (entity.DepthData, error) {

	asks := [][2]float64{}
	bids := [][2]float64{}

	if book.Data.Asks == nil || book.Data.Bids == nil {
		return entity.DepthData{}, errors.New("huobi order book response data ask or bid is empty")
	}

	for _, ask := range book.Data.Asks {
		if len(ask) < 2 {
			continue
		}
		price, quantity := ask[0], ask[1]
		asks = append(asks, [2]float64{price, quantity})
	}

	for _, bid := range book.Data.Bids {
		if len(bid) < 2 {
			continue
		}
		price, quantity := bid[0], bid[1]
		bids = append(bids, [2]float64{price, quantity})
	}

	return entity.DepthData{
		Asks: asks,
		Bids: bids,
	}, nil
}
