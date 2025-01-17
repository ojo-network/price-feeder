package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

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
	pairs ...types.CurrencyPair,
) (map[uint64]types.ExternalLiquidity, error) {
	externalLiquidity := make(map[uint64]types.ExternalLiquidity, len(pairs))

	for _, pair := range pairs {
		if pair.PoolId == 0 {
			continue
		}
		//https://api.gateio.ws/api/v4/spot/order_book?currency_pair=BTC_USDT&limit=5000
		route := p.endpoints.Rest + gateRestOrderBook + "?currency_pair=" + pair.Base + "_" + pair.Quote + "&limit=5000"
		fmt.Println("external_liquidity_route", route)
		resp, err := http.Get(route)
		if err != nil {
			p.logger.Err(err).
				Str("provider", gateProvider).
				Interface("pair", pair).
				Msg("ERROR_GETTING_ORDER_BOOK")
			return nil, err
		}
		defer resp.Body.Close()

		var gateResponseSpotOrderBook GateResponseSpotOrderBook
		if err := json.NewDecoder(resp.Body).Decode(&gateResponseSpotOrderBook); err != nil {
			p.logger.Err(err).
				Str("provider", gateProvider).
				Interface("pair", pair).
				Msg("ERROR_DECODING_RESPONSE_SPOT_ORDER_BOOK")
			return nil, err
		}
		asks := [][2]float64{}
		bids := [][2]float64{}

		for _, ask := range gateResponseSpotOrderBook.Asks {
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

		for _, bid := range gateResponseSpotOrderBook.Bids {
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
		poolAssetInfo, err := queries.QueryExtLiqPoolAssetInfo(ammPools, accountedPools, pair.PoolId)
		if err != nil {
			assetFound = false
			p.logger.Err(err).
				Str("provider", gateProvider).
				Interface("pair", pair).
				Msg("ERROR_QUERYING_POOL_ASSET_INFO")
		}

		price, _ := p.GetTickerPrice(pair)
		externalLiquidityEntity, err := usecase.CalculateExternalLiquidityUseCase(depthDataEntity, poolAssetInfo, assetFound, price)
		if err != nil {
			p.logger.Err(err).
				Str("provider", gateProvider).
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
			pair.PoolId,
			baseAsset,
			quoteAsset,
			fmt.Sprintf("%f", externalLiquidityEntity.BaseAmount),
			fmt.Sprintf("%f", externalLiquidityEntity.QuoteAmount),
			fmt.Sprintf("%f", externalLiquidityEntity.BaseDepth),
			fmt.Sprintf("%f", externalLiquidityEntity.QuoteDepth),
		)
		if err != nil {
			p.logger.Err(err).
				Str("provider", gateProvider).
				Interface("pair", pair).
				Msg("ERROR_CREATING_NEW_EXTERNAL_LIQUIDITY")
			return externalLiquidity, err
		}

		p.logger.Info().
			Str("provider", gateProvider).
			Interface("pair", pair).Msg("EXTERNAL_LIQUIDITY_WAS_CREATED")
		externalLiquidity[pair.PoolId] = liq

	}

	return externalLiquidity, nil
}
