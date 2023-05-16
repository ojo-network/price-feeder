package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	gql "github.com/hasura/go-graphql-client"
	"github.com/ojo-network/ojo/util/decmath"

	"github.com/ojo-network/price-feeder/oracle/types"
)

var _ Provider = (*UniswapProvider)(nil)

var (
	UNISWAP_URL = "https://api.studio.thegraph.com/query/46403/unidexer/test20"
)

type (

	// BundleQuery eth price query has fixed id of 1
	BundleQuery struct {
		Bundle struct {
			EthPriceUSD string `graphql:"ethPriceUSD"`
			Id          string `graphql:"id"`
		} `graphql:"bundle(id: \"1\")"`
	}

	Token struct {
		Name   string `graphql:"name"`
		Symbol string `graphql:"symbol"`
		ID     string `graphql:"id"`
	}

	PoolMinuteDataCandleQuery struct {
		PoolMinuteDatas []struct {
			ID               string  `graphql:"id"`
			PoolID           string  `graphql:"poolID"`
			PeriodStartUnix  string  `graphql:"periodStartUnix"`
			Token0           Token   `graphql:"token0"`
			Token1           Token   `graphql:"token1"`
			Token0Price      string  `graphql:"token0Price"`
			Token1Price      string  `graphql:"token1Price"`
			Timestamp        int64   `graphql:"timestamp"`
			VolumeUSDTracked float64 `graphql:"volumeUSDTracked"`
			Open             string  `graphql:"open"`
			High             string  `graphql:"high"`
			Low              string  `graphql:"low"`
			Close            string  `graphql:"close"`
		} `graphql:"poolMinuteDatas(first:1, orderBy: periodStartUnix, orderDirection: desc, where: {poolID_in: $poolIDs, periodStartUnix_gte: $start})"`
	}

	PoolHourDataQuery struct {
		PoolHourDatas []struct {
			ID                 string `graphql:"id"`
			PoolID             string `graphql:"poolID"`
			PeriodStartUnix    string `graphql:"periodStartUnix"`
			Token0             Token  `graphql:"token0"`
			Token1             Token  `graphql:"token1"`
			Token0Price        string `graphql:"token0price"`
			Token1Price        string `graphql:"token1price"`
			VolumeUSDTracked   string `graphql:"volumeUSDTracked"`
			VolumeUSDUntracked string `graphql:"volumeUSDUntracked"`
		} `graphql:"poolHourDatas(first: $first, after: $after, orderBy: periodStartUnix, orderDirection: desc, where: {poolID_in: $ids ,periodStartUnix_gte: $oneDayAgo, periodStartUnix_lte: $currentTimestamp})"`
	}

	// UniswapProvider defines an Oracle provider implemented to consume data from Uniswap graphql
	UniswapProvider struct {
		baseURL        string
		client         *gql.Client
		addressToDenom map[string]string
		denomToAddress map[string]string
	}
)

func NewUniswapProvider(endpoint Endpoint, addressPairs []types.AddressPair) *UniswapProvider {
	// create address denom and reverse map
	addressToDenom := make(map[string]string)
	denomToAddress := make(map[string]string)
	for _, pair := range addressPairs {
		pairName := pair.String()
		addressToDenom[pair.Address] = pairName
		denomToAddress[pairName] = pair.Address
	}

	if endpoint.Name == ProviderUniswap {
		return &UniswapProvider{
			baseURL:        endpoint.Rest,
			client:         gql.NewClient(endpoint.Rest, nil),
			addressToDenom: addressToDenom,
			denomToAddress: denomToAddress,
		}
	}
	return &UniswapProvider{
		baseURL:        UNISWAP_URL,
		client:         gql.NewClient(UNISWAP_URL, nil),
		addressToDenom: addressToDenom,
		denomToAddress: denomToAddress,
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
	var poolIDtoPool map[string]string
	for _, pair := range pairs {
		if _, found := p.addressToDenom[pair.String()]; !found {
			return nil, fmt.Errorf("pool id for %s not found", pair.String())
		}

		pairID := p.denomToAddress[pair.String()]
		poolIDtoPool[pairID] = pair.String()
		poolIDS = append(poolIDS, pairID)
	}

	idMap := map[string]interface{}{
		"poolIDS":          poolIDS,
		"oneDayAgo":        PastUnixTime(24 * time.Hour),
		"currentTimestamp": time.Now().Unix(),
	}

	// TODO: length verification
	baseDenomIdx := make(map[string]types.CurrencyPair)
	for _, cp := range pairs {
		baseDenomIdx[strings.ToUpper(cp.Base)] = cp
	}

	tickerPrices := make(map[string]types.TickerPrice, len(pairs))
	latestTimestamp := make(map[string]float64)
	var lastID string

	var poolsHourDatas PoolHourDataQuery
	for {
		idMap["first"] = 24
		idMap["after"] = lastID

		// query volume from day data
		var poolsHourData PoolHourDataQuery
		err := p.client.Query(context.Background(), &poolsHourData, idMap)
		if err != nil {
			return nil, err
		}

		if len(poolsHourData.PoolHourDatas) == 0 {
			break
		}

		lastID = poolsHourData.PoolHourDatas[len(poolsHourData.PoolHourDatas)-1].ID
		// append pools
		poolsHourDatas.PoolHourDatas = append(poolsHourDatas.PoolHourDatas, poolsHourData.PoolHourDatas...)
	}

	for _, poolData := range poolsHourDatas.PoolHourDatas {
		symbol := strings.ToUpper(poolData.Token0.Symbol) // symbol == base in a currency pair

		cp, ok := baseDenomIdx[symbol]
		if !ok {
			// skip tokens that are not requested
			continue
		}

		if _, ok := tickerPrices[symbol]; ok {
			return nil, fmt.Errorf("duplicate token found in uniswap response: %s", symbol)
		}

		price, err := formatTokenPriceData(poolData.Token0Price, poolData.Token1Price)
		if err != nil {
			return nil, err
		}

		timestamp, err := strconv.ParseFloat(poolData.PeriodStartUnix, 64)
		if err != nil {
			return nil, err
		}

		vol, err := toSdkDec(poolData.VolumeUSDTracked)
		if err != nil {
			// add error
			return nil, err
		}

		if _, found := tickerPrices[cp.String()]; !found {
			latestTimestamp[cp.String()] = timestamp
			tickerPrices[cp.String()] = types.TickerPrice{Price: price, Volume: sdk.ZeroDec()}
		} else {
			if timestamp > latestTimestamp[cp.String()] {

				// update to most latest price recorded
				latestTimestamp[cp.String()] = timestamp
				prevVolume := tickerPrices[cp.String()].Volume
				tickerPrices[cp.String()] = types.TickerPrice{
					Price:  price,
					Volume: prevVolume,
				}
			}
		}

		tickerPrices[cp.String()].Volume.Add(vol)
	}

	return tickerPrices, nil
}

func (p UniswapProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]types.CandlePrice, error) {
	// create token ids
	var poolIDS []string
	var poolIDtoPool map[string]string
	for _, pair := range pairs {
		if _, found := p.addressToDenom[pair.String()]; !found {
			return nil, fmt.Errorf("pool id for %s not found", pair.String())
		}

		pairID := p.denomToAddress[pair.String()]
		poolIDtoPool[pairID] = pair.String()
		poolIDS = append(poolIDS, pairID)
	}

	idMap := map[string]interface{}{
		"poolIDS": poolIDS,
		"start":   PastUnixTime(providerCandlePeriod),
	}

	var lastID string
	var poolsMinuteDatas PoolMinuteDataCandleQuery
	for {
		idMap["first"] = 10
		idMap["after"] = lastID

		// query volume from day data
		var poolsMinuteData PoolMinuteDataCandleQuery
		err := p.client.Query(context.Background(), &poolsMinuteData, idMap)
		if err != nil {
			return nil, err
		}

		if len(poolsMinuteData.PoolMinuteDatas) == 0 {
			break
		}

		lastID = poolsMinuteDatas.PoolMinuteDatas[len(poolsMinuteData.PoolMinuteDatas)-1].ID
		// append data fetches
		poolsMinuteDatas.PoolMinuteDatas = append(poolsMinuteDatas.PoolMinuteDatas, poolsMinuteDatas.PoolMinuteDatas...)
	}

	// should return 10 queries at max
	var poolsData PoolMinuteDataCandleQuery
	err := p.client.Query(context.Background(), &poolsData, idMap)

	if err != nil {
		return nil, err
	}

	// TODO: length and token order validation
	baseDenomIdx := make(map[string]types.CurrencyPair)
	for _, cp := range pairs {
		baseDenomIdx[strings.ToUpper(cp.Base)] = cp
	}

	candlePrices := make(map[string][]types.CandlePrice, len(pairs))
	for _, poolData := range poolsData.PoolMinuteDatas {
		symbol := strings.ToUpper(poolData.Token0.Symbol) // symbol == base in a currency pair
		cp, ok := baseDenomIdx[symbol]
		if !ok {
			// skip tokens that are not requested
			continue
		}

		if _, ok := candlePrices[symbol]; ok {
			return nil, fmt.Errorf("duplicate token found in uniswap response: %s", symbol)
		}

		price, err := formatTokenPriceData(poolData.Token0Price, poolData.Token1Price)
		if err != nil {
			return nil, err
		}

		volume, err := decmath.NewDecFromFloat(poolData.VolumeUSDTracked)
		if err != nil {
			return nil, err
		}

		candlePrices[cp.String()] = append(candlePrices[cp.String()], types.CandlePrice{Price: price, Volume: volume, TimeStamp: poolData.Timestamp})
	}

	return candlePrices, nil
}

// GetBundle returns eth price
func (p UniswapProvider) GetBundle() (float64, error) {
	var bundle BundleQuery
	err := p.client.Query(context.Background(), &bundle, nil)
	if err != nil {
		return 0, err
	}

	return strconv.ParseFloat(bundle.Bundle.EthPriceUSD, 64)
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p UniswapProvider) GetAvailablePairs() (map[string]struct{}, error) {
	availablePairs := make(map[string]struct{})
	// return denoms that is tracked at provider init
	for denom, _ := range p.denomToAddress {
		availablePairs[denom] = struct{}{}
	}

	return availablePairs, nil
}

func formatTokenPriceData(token0Price, token1Price string) (sdk.Dec, error) {
	price0, err := strconv.ParseFloat(token0Price, 64)
	if err != nil {
		return sdk.ZeroDec(), err
	}
	price1, err := strconv.ParseFloat(token1Price, 64)
	if err != nil {
		return sdk.ZeroDec(), err
	}

	return decmath.NewDecFromFloat(price0 / price1)
}

func toSdkDec(volume string) (sdk.Dec, error) {
	vol, err := strconv.ParseFloat(volume, 64)
	if err != nil {
		return sdk.ZeroDec(), err
	}

	return decmath.NewDecFromFloat(vol)
}
