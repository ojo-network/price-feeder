package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	sdk "github.com/cosmos/cosmos-sdk/types"
	gql "github.com/hasura/go-graphql-client"
	"github.com/rs/zerolog"

	"github.com/ojo-network/price-feeder/oracle/types"
)

var _ Provider = (*UniswapProvider)(nil)

const USDC = "USDC"

type (

	// BundleQuery eth price query has fixed id of 1
	BundleQuery struct {
		Bundle struct {
			EthPriceUSD string `graphql:"ethPriceUSD"`
			ID          string `graphql:"id"`
		} `graphql:"bundle(id: \"1\")"`
	}

	Token struct {
		Name   string `graphql:"name"`
		Symbol string `graphql:"symbol"`
	}

	PoolMinuteDataCandleQuery struct {
		PoolMinuteDatas []struct {
			ID               string  `graphql:"id"`
			PoolID           string  `graphql:"poolID"`
			PeriodStartUnix  float64 `graphql:"periodStartUnix"`
			Timestamp        float64 `graphql:"timestamp"`
			Token0           Token   `graphql:"token0"`
			Token1           Token   `graphql:"token1"`
			Token0Price      string  `graphql:"token0Price"`
			Token0PriceUSD   string  `graphql:"token0PriceUSD"`
			Token1PriceUSD   string  `graphql:"token1PriceUSD"`
			Token1Price      string  `graphql:"token1Price"`
			VolumeUSDTracked string  `graphql:"volumeUSDTracked"`
		} `graphql:"poolMinuteDatas(first:$first, after:$after, orderBy: periodStartUnix, orderDirection: asc, where: {poolID_in: $poolIDS, periodStartUnix_gte: $start,periodStartUnix_lte:$stop})"` //nolint:lll
	}

	PoolHourDataQuery struct {
		PoolHourDatas []struct {
			ID                 string  `graphql:"id"`
			PoolID             string  `graphql:"poolID"`
			PeriodStartUnix    float64 `graphql:"periodStartUnix"`
			Timestamp          float64 `graphql:"timestamp"`
			Token0             Token   `graphql:"token0"`
			Token1             Token   `graphql:"token1"`
			Token0Price        string  `graphql:"token0Price"`
			Token1Price        string  `graphql:"token1Price"`
			Token0PriceUSD     string  `graphql:"token0PriceUSD"`
			Token1PriceUSD     string  `graphql:"token1PriceUSD"`
			VolumeUSDTracked   string  `graphql:"volumeUSDTracked"`
			VolumeUSDUntracked string  `graphql:"volumeUSDUntracked"`
		} `graphql:"poolHourDatas(first: $first,after: $after, orderBy: periodStartUnix, orderDirection: desc, where: {poolID_in: $poolIDS, periodStartUnix_gte:$start,periodStartUnix_lte:$stop})"` //nolint:lll
	}

	// UniswapProvider defines an Oracle provider implemented to consume data from Uniswap graphql
	UniswapProvider struct {
		logger  zerolog.Logger
		baseURL string
		client  *gql.Client
		mut     sync.Mutex

		poolIDS          []string
		pairs            []types.CurrencyPair
		denomToAddress   map[string]string
		addressToPair    map[string]types.CurrencyPair
		poolsHoursDatas  PoolHourDataQuery
		poolsMinuteDatas PoolMinuteDataCandleQuery
	}
)

func NewUniswapProvider(
	ctx context.Context,
	logger zerolog.Logger,
	providerName string,
	endpoint Endpoint,
	currencyPairs ...types.CurrencyPair,
) *UniswapProvider {
	// create pair name to address map
	denomToAddress := make(map[string]string)
	addressToPair := make(map[string]types.CurrencyPair)
	for _, pair := range currencyPairs {
		// graph supports all lower case id's
		// currently supports only 1 fee tier pool per currency pair
		address := strings.ToLower(pair.Address)
		denomToAddress[pair.String()] = address
		addressToPair[address] = pair
	}

	// default provider to eth uniswap
	uniswapLogger := logger.With().Str("provider", providerName).Logger()
	provider := &UniswapProvider{
		baseURL:        endpoint.Rest,
		client:         gql.NewClient(endpoint.Rest, nil),
		denomToAddress: denomToAddress,
		addressToPair:  addressToPair,
		logger:         uniswapLogger,
		pairs:          currencyPairs,
		mut:            sync.Mutex{},
	}

	go provider.startPooling(ctx)

	return provider
}

func (p *UniswapProvider) startPooling(ctx context.Context) {
	tick := 0
	err := p.setPoolIDS()
	if err != nil {
		p.logger.Err(err).Msg("error generating pool ids")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		default:
			if err := p.getHourAndMinuteData(ctx); err != nil {
				p.logger.Err(err).Msgf("failed to get hour and minute data")
			}

			tick++
			p.logger.Log().Int("uniswap tick", tick)

			// slightly larger than a second (time for new candle data to be populated)
			time.Sleep(time.Millisecond * 1100)
		}
	}
}
func (p *UniswapProvider) StartConnections() {
	// no-op Uniswap v1 does not use websockets
}

func (p *UniswapProvider) getHourAndMinuteData(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	// ticker prices
	g.Go(func() error {
		idMap := map[string]interface{}{
			"poolIDS": p.poolIDS,
			"start":   time.Now().Unix() - 86400,
			"stop":    time.Now().Unix(),
		}

		var lastID string
		var firstID string
		var poolsHourDatas PoolHourDataQuery
		for {
			// limit by graph
			idMap["first"] = 1000
			idMap["after"] = lastID

			// query volume from day data
			var poolsHourData PoolHourDataQuery
			err := p.client.Query(ctx, &poolsHourData, idMap)

			if err != nil {
				return err
			}

			// check if no new id or repeated id
			if len(poolsHourData.PoolHourDatas) == 0 || firstID == poolsHourData.PoolHourDatas[0].ID {
				break
			}

			firstID = poolsHourData.PoolHourDatas[0].ID
			lastID = poolsHourData.PoolHourDatas[len(poolsHourData.PoolHourDatas)-1].ID

			// append poolsHourDatas
			poolsHourDatas.PoolHourDatas = append(poolsHourDatas.PoolHourDatas, poolsHourData.PoolHourDatas...)
		}

		p.mut.Lock()
		p.poolsHoursDatas = poolsHourDatas
		p.mut.Unlock()

		return nil
	})

	// candle prices
	g.Go(func() error {

		idMap := map[string]interface{}{
			"poolIDS": p.poolIDS,
			"start":   time.Now().Unix() - int64((10 * time.Minute).Seconds()),
			"stop":    time.Now().Unix(),
		}

		var lastID string
		var firstID string
		var poolsMinuteDatas PoolMinuteDataCandleQuery
		for {
			// limit by	graph
			idMap["first"] = 1000
			idMap["after"] = lastID

			// query volume from day data
			var poolsMinuteData PoolMinuteDataCandleQuery
			err := p.client.Query(ctx, &poolsMinuteData, idMap)

			if err != nil {
				return err
			}

			// check if no new id or repeated id
			if len(poolsMinuteData.PoolMinuteDatas) == 0 || firstID == poolsMinuteData.PoolMinuteDatas[0].ID {
				break
			}

			firstID = poolsMinuteData.PoolMinuteDatas[0].ID
			lastID = poolsMinuteData.PoolMinuteDatas[len(poolsMinuteData.PoolMinuteDatas)-1].ID

			poolsMinuteDatas.PoolMinuteDatas = append(poolsMinuteDatas.PoolMinuteDatas, poolsMinuteData.PoolMinuteDatas...)
		}

		p.mut.Lock()
		p.poolsMinuteDatas = poolsMinuteDatas
		p.mut.Unlock()

		return nil
	})

	return g.Wait()
}

// SubscribeCurrencyPairs performs a no-op since Uniswap does not use websockets
func (p *UniswapProvider) SubscribeCurrencyPairs(...types.CurrencyPair) {}

func (p *UniswapProvider) GetTickerPrices(pairs ...types.CurrencyPair) (map[string]types.TickerPrice, error) {
	tickerPrices := make(map[string]types.TickerPrice, len(pairs))
	latestTimestamp := make(map[string]float64)

	p.mut.Lock()
	defer p.mut.Unlock()
	for _, poolData := range p.poolsHoursDatas.PoolHourDatas {
		symbol0 := strings.ToUpper(poolData.Token0.Symbol) // symbol == base in a currency pair
		symbol1 := strings.ToUpper(poolData.Token1.Symbol) // symbol == quote in a currency pair

		// check if this pair is request
		requestedPair, found := p.addressToPair[strings.ToLower(poolData.PoolID)]
		if !found {
			continue
		}

		base := requestedPair.Base
		quote := requestedPair.Quote
		name := requestedPair.String()
		var tokenPrice string
		switch {
		case base == symbol0 && quote == symbol1:
			// pricing
			tokenPrice = poolData.Token1Price

		case base == symbol0 && (quote == USDC && symbol1 != USDC):
			// consider USDC BASED PRICING
			tokenPrice = poolData.Token0PriceUSD

		case base == symbol1 && quote == symbol0:
			// flip prices
			tokenPrice = poolData.Token0Price

		case base == symbol1 && (quote == USDC && symbol0 != USDC):
			// consider USDC BASED PRICING
			tokenPrice = poolData.Token1PriceUSD
		}

		price, err := toSdkDec(tokenPrice)
		if err != nil {
			return nil, err
		}

		timestamp := poolData.PeriodStartUnix
		vol, err := toSdkDec(poolData.VolumeUSDTracked)
		if err != nil {
			return nil, err
		}

		// update price according to latest timestamp
		if timestamp > latestTimestamp[name] {
			latestTimestamp[name] = timestamp
			if _, found := tickerPrices[name]; !found {
				tickerPrices[name] = types.TickerPrice{Price: price, Volume: sdk.ZeroDec()}
			} else {
				tickerPrices[name].Price.Set(price)
			}
		}

		tickerPrices[name].Volume.Set(tickerPrices[name].Volume.Add(vol))
	}

	return tickerPrices, nil
}

func (p *UniswapProvider) GetCandlePrices(pairs ...types.CurrencyPair) (map[string][]types.CandlePrice, error) {
	p.mut.Lock()
	defer p.mut.Unlock()
	candlePrices := make(map[string][]types.CandlePrice, len(pairs))
	for _, poolData := range p.poolsMinuteDatas.PoolMinuteDatas {
		symbol0 := strings.ToUpper(poolData.Token0.Symbol) // symbol == base in a currency pair
		symbol1 := strings.ToUpper(poolData.Token1.Symbol) // symbol == quote in a currency pai// r

		// check if this pair is request
		requestedPair, found := p.addressToPair[strings.ToLower(poolData.PoolID)]
		if !found {
			continue
		}

		base := requestedPair.Base
		quote := requestedPair.Quote
		name := requestedPair.String()
		var tokenPrice string
		switch {
		case base == symbol0 && quote == symbol1:
			// pricing
			tokenPrice = poolData.Token1Price
		case base == symbol0 && (quote == USDC && symbol1 != USDC):
			// consider USDC BASED PRICING
			tokenPrice = poolData.Token0PriceUSD
		case base == symbol1 && quote == symbol0:
			// flip prices here
			tokenPrice = poolData.Token0Price
		case base == symbol1 && (quote == USDC && symbol0 != USDC):
			// consider USDC BASED PRICING
			tokenPrice = poolData.Token1PriceUSD
		}

		price, err := toSdkDec(tokenPrice)
		if err != nil {
			return nil, err
		}

		vol, err := toSdkDec(poolData.VolumeUSDTracked)
		if err != nil {
			return nil, err
		}

		// second to millisecond for filtering
		candlePrices[name] = append(candlePrices[name], types.CandlePrice{
			Price:     price,
			Volume:    vol,
			TimeStamp: int64(poolData.Timestamp * 1000),
		})
	}

	return candlePrices, nil
}

// GetBundle returns eth price
func (p *UniswapProvider) GetBundle() (float64, error) {
	var bundle BundleQuery
	err := p.client.Query(context.Background(), &bundle, nil)
	if err != nil {
		return 0, err
	}

	return strconv.ParseFloat(bundle.Bundle.EthPriceUSD, 64)
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p *UniswapProvider) GetAvailablePairs() (map[string]struct{}, error) {
	availablePairs := make(map[string]struct{})

	// return denoms that is tracked at provider init
	for denom := range p.denomToAddress {
		availablePairs[denom] = struct{}{} //nolint:structcheck
	}

	return availablePairs, nil
}

func toSdkDec(value string) (sdk.Dec, error) {
	valueFloat, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return sdk.ZeroDec(), err
	}

	return sdk.NewDecFromStr(fmt.Sprintf("%.18f", valueFloat))
}

func (p *UniswapProvider) setPoolIDS() error {
	poolIDS := make([]string, len(p.pairs))
	for i, pair := range p.pairs {
		if _, found := p.denomToAddress[pair.String()]; !found {
			return fmt.Errorf("pool id for %s not found", pair.String())
		}

		poolID := p.denomToAddress[pair.String()]
		poolIDS[i] = poolID
	}
	p.poolIDS = poolIDS

	return nil
}