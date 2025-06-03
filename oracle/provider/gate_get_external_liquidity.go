package provider

import (
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

type GateResponseSpotOrderBook struct {
	Asks [][2]string `json:"asks"`
	Bids [][2]string `json:"bids"`
}

const gateProvider = "gate"

// GetExternalLiquidity returns external liquidity info on the provided pairs
func (p *GateProvider) GetExternalLiquidity(
	ammPools map[uint64]oracletypes.Pool,
	accountedPools map[uint64]oracletypes.AccountedPool,
	usdcDenom string,
	pairs ...types.CurrencyPair,
) (map[uint64]types.ExternalLiquidity, error) {
	externalLiquidity := make(map[uint64]types.ExternalLiquidity, len(pairs))

	client := http.Client{
		Timeout: 2000 * time.Millisecond,
	}

	var wg sync.WaitGroup
	var mutex sync.Mutex

	for _, pair := range pairs {
		if pair.PoolID == 0 {
			continue
		}
		wg.Add(1)

		go func() {
			defer wg.Done()
			//https://api.gateio.ws/api/v4/spot/order_book?currency_pair=BTC_USDT&limit=5000
			route := p.endpoints.Rest + gateRestOrderBook + "?currency_pair=" + pair.Base + "_" + pair.Quote + "&limit=5000"
			resp, err := client.Get(route)
			if err != nil {
				p.logger.Err(err).
					Str("provider", gateProvider).
					Interface("pair", pair).
					Msg("ERROR_GETTING_ORDER_BOOK_ON_GATE")
				return
			}
			defer resp.Body.Close()

			var gateResponseSpotOrderBook GateResponseSpotOrderBook
			if err := json.NewDecoder(resp.Body).Decode(&gateResponseSpotOrderBook); err != nil {
				p.logger.Err(err).
					Str("provider", gateProvider).
					Interface("pair", pair).
					Msg("ERROR_DECODING_RESPONSE_SPOT_ORDER_BOOK")
				return
			}
			asks := [][2]float64{}
			bids := [][2]float64{}

			for _, ask := range gateResponseSpotOrderBook.Asks {

				if len(ask) < 2 {
					p.logger.Error().
						Str("provider", gateProvider).
						Interface("ask", ask).
						Msg("INVALID_ASK_DATA_FORMAT")
					continue
				}

				price, err := strconv.ParseFloat(ask[0], 64)

				if err != nil {
					return
				}

				quantity, err := strconv.ParseFloat(ask[1], 64)

				if err != nil {
					return
				}

				asks = append(asks, [2]float64{price, quantity})
			}

			for _, bid := range gateResponseSpotOrderBook.Bids {

				if len(bid) < 2 {
					p.logger.Error().
						Str("provider", gateProvider).
						Interface("bid", bid).
						Msg("INVALID_BID_DATA_FORMAT")
					continue
				}

				price, err := strconv.ParseFloat(bid[0], 64)

				if err != nil {
					return
				}

				quantity, err := strconv.ParseFloat(bid[1], 64)

				if err != nil {
					return
				}

				bids = append(bids, [2]float64{price, quantity})
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
					Str("provider", gateProvider).
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
					Str("provider", gateProvider).
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
					Str("provider", gateProvider).
					Interface("pair", pair).
					Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
				return
			}

			p.logger.Info().
				Str("provider", gateProvider).
				Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")
			mutex.Lock()
			externalLiquidity[pair.PoolID] = liq
			mutex.Unlock()
		}()

	}
	wg.Wait()
	return externalLiquidity, nil
}
