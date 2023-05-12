package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	gql "github.com/hasura/go-graphql-client"
	"github.com/ojo-network/ojo/util/decmath"

	"github.com/ojo-network/price-feeder/oracle/types"
)

// var _ Provider = (*UniswapProvider)(nil)
var (
	uniswapURL = "https://api.studio.thegraph.com/query/46403/unidexer/test20"
)

type (

	// BundleQuery eth price query has fixed id of 1
	BundleQuery struct {
		Bundle struct {
			EthPriceUSD float64 `graphql:"ethPriceUSD"`
		} `graphql:"bundle(id: 1)"`
	}

	Token struct {
		Name   string `graphql:"name"`
		Symbol string `graphql:"symbol"`
	}

	// TokenMinuteDataQuery currently minute data supports usd mapping only
	TokenMinuteDataQuery struct {
		TokenMinuteDatas []struct {
			PriceUSD        float64 `graphql:"priceUSD"`
			Open            float64 `graphql:"open"`
			Close           float64 `graphql:"close"`
			High            float64 `graphql:"high"`
			Low             float64 `graphql:"low"`
			PeriodStartUnix int     `graphql:"periodStartUnix"`
			Token           struct {
				Name string `graphql:"name"`
			} `graphql:"token"`
		} `graphql:"tokenMinuteDatas(first: 2, orderBy: periodStartUnix, orderDirection: desc, where: {token_in: $ids, periodStartUnix_gt: $start})"`
	}

	PoolMinuteDataQuery struct {
		PoolMinuteDatas []struct {
			ID              string `graphql:"id"`
			PoolID          string `graphql:"poolID"`
			PeriodStartUnix int    `graphql:"periodStartUnix"`
			Token0Price     string `graphql:"token0Price"`
			Token1Price     string `graphql:"token1Price"`
			Open            string `graphql:"open"`
			High            string `graphql:"high"`
			Low             string `graphql:"low"`
			Close           string `graphql:"close"`
		} `graphql:"poolMinuteDatas(first: 1, orderBy: periodStartUnix, orderDirection: desc, where: {poolID_in: $poolIDs})"`
	}

	Pools struct {
		Pools []struct {
			ID          string `graphql:"id"`
			Token0      Token  `graphql:"token0"`
			Token1      Token  `graphql:"token1"`
			Token0Price string `graphql:"token0Price"`
			Token1Price string `graphql:"token1Price"`
		} `graphql:"pools(where: {ID_in: $poolIDs})"`
	}

	PoolDayDataQuery struct {
		PoolDayDatas []struct {
			ID                 string  `graphql:"id"`
			PoolID             string  `graphql:"poolID"`
			PeriodStartUnix    int     `graphql:"periodStartUnix"`
			Token0             Token   `graphql:"token0"`
			Token1             Token   `graphql:"token1"`
			VolumeUSDTracked   string `graphql:"volumeUSDTracked"`
			VolumeUSDUntracked string `graphql:"volumeUSDUntracked"`
		} `graphql:"poolDayDatas(first: 1, orderBy: periodStartUnix, orderDirection: desc, where: {poolID_in: $ids})"`
	}

	// UniswapProvider defines an Oracle provider implemented by the Uniswap public
	// API.
	//
	// REF: https://api-Uniswap.imperator.co/swagger/
	UniswapProvider struct {
		baseURL string
		//client  *http.Client
		client         *gql.Client
		poolAddressMap map[string]string
	}

	// UniswapTokenResponse defines the response structure for an Uniswap token
	// request.
	UniswapTokenResponse struct {
		Price  float64 `json:"price"`
		Symbol string  `json:"symbol"`
		Volume float64 `json:"volume_24h"`
	}

	// UniswapCandleResponse defines the response structure for an Uniswap candle
	// request.
	UniswapCandleResponse struct {
		Time   int64   `json:"time"`
		Close  float64 `json:"close"`
		Volume float64 `json:"volume"`
	}

	// UniswapPairsSummary defines the response structure for an Uniswap pairs
	// summary.
	UniswapPairsSummary struct {
		Data []UniswapPairData `json:"data"`
	}

	// UniswapPairData defines the data response structure for an Uniswap pair.
	UniswapPairData struct {
		Base  string `json:"base_symbol"`
		Quote string `json:"quote_symbol"`
	}
)

func NewUniswapProvider(endpoint Endpoint, poolAddressMap map[string]string) *UniswapProvider {
	if endpoint.Name == ProviderUniswap {
		return &UniswapProvider{
			baseURL:        endpoint.Rest,
			client:         gql.NewClient(endpoint.Rest, nil),
			poolAddressMap: poolAddressMap,
		}
	}
	return &UniswapProvider{
		baseURL: uniswapURL,
		client:  gql.NewClient(uniswapURL, nil),
	}
}

func (p *UniswapProvider) StartConnections() {
	// no-op Uniswap v1 does not use websockets
}

// SubscribeCurrencyPairs performs a no-op since Uniswap does not use websockets
func (p UniswapProvider) SubscribeCurrencyPairs(...types.CurrencyPair) {}

func (p UniswapProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]types.TickerPrice, error) {
	// create token ids
	var poolIDS []string
	var poolIDtoPool map[string]string{}
	for _, pair := range pairs {
		if _, found := p.poolAddressMap[pair.String()]; !found {
			return nil, fmt.Errorf("pool id for %s not found", pair.String())
		}

		pairID:= p.poolAddressMap[pair.String()]
		poolIDtoPool[pairID] = pair.String()
		poolIDS = append(poolIDS, pairID)
	}

	idMap := map[string]interface{}{
		"poolIDS": poolIDS,
	}

	var poolsData Pools
	err := p.client.Query(context.Background(), &poolsData, idMap)

	if err != nil {
		return nil, err
	}

	// query volume from day data
	var poolVolume PoolDayDataQuery
	err = p.client.Query(context.Background(), &poolVolume, idMap)
	if err != nil {
		return nil, err
	}

	// TODO: length and token order validation

	baseDenomIdx := make(map[string]types.CurrencyPair)
	for _, cp := range pairs {
		baseDenomIdx[strings.ToUpper(cp.Base)] = cp
	}

	tickerPrices := make(map[string]types.TickerPrice, len(pairs))
	for _, poolData := range poolsData.Pools {
		symbol := strings.ToUpper(poolData.Token0.Symbol) // symbol == base in a currency pair

		cp, ok := baseDenomIdx[symbol]
		if !ok {
			// skip tokens that are not requested
			continue
		}

		if _, ok := tickerPrices[symbol]; ok {
			return nil, fmt.Errorf("duplicate token found in uniswap response: %s", symbol)
		}

		token0Price, err := strconv.ParseFloat(poolData.Token0Price, 64)
		token1Price, err := strconv.ParseFloat(poolData.Token1Price, 64)

		price, err := decmath.NewDecFromFloat(token0Price / token1Price)
		if err != nil {
			return nil, fmt.Errorf("failed to read Uniswap price (%f) for %s", price, symbol)
		}

		tickerPrices[cp.String()] = types.TickerPrice{Price: price}
	}

	for _, poolDayData := range poolVolume.PoolDayDatas {
		tickerPrice:= tickerPrices[poolIDtoPool[poolDayData.PoolID]]
		tickerPrice.Volume=sdk.MustNewDecFromStr(poolDayData.VolumeUSDTracked)
		tickerPrices[poolIDtoPool[poolDayData.PoolID]]=tickerPrice
	}

	return tickerPrices,nil
}

func (p UniswapProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]types.CandlePrice, error) {



	return nil,nil
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p UniswapProvider) GetAvailablePairs() (map[string]struct{}, error) {
	return nil, nil
}
