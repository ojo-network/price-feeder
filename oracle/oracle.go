package oracle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cosmos/cosmos-sdk/telemetry"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	"github.com/ojo-network/price-feeder/oracle/client"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
	pfsync "github.com/ojo-network/price-feeder/pkg/sync"
)

// We define tickerSleep as the minimum timeout between each oracle loop. We
// define this value empirically based on enough time to collect exchange rates,
// and broadcast pre-vote and vote transactions such that they're committed in
// at least one block during each voting period.
const (
	tickerSleep = 1000 * time.Millisecond
)

// PreviousPrevote defines a structure for defining the previous prevote
// submitted on-chain.
type PreviousPrevote struct {
	ExchangeRates     string
	Salt              string
	SubmitBlockHeight int64
}

func NewPreviousPrevote() *PreviousPrevote {
	return &PreviousPrevote{
		Salt:              "",
		ExchangeRates:     "",
		SubmitBlockHeight: 0,
	}
}

// Oracle implements the core component responsible for fetching exchange rates
// for a given set of currency pairs and determining the correct exchange rates
// to submit to the on-chain price oracle adhering the oracle specification.
type Oracle struct {
	logger zerolog.Logger
	closer *pfsync.Closer

	providerTimeout    time.Duration
	providerPairs      map[types.ProviderName][]types.CurrencyPair
	previousPrevote    *PreviousPrevote
	previousVotePeriod float64
	priceProviders     map[types.ProviderName]provider.Provider
	oracleClient       client.OracleClient
	deviations         map[string]sdk.Dec
	endpoints          map[types.ProviderName]provider.Endpoint
	paramCache         ParamCache

	pricesMutex     sync.RWMutex
	lastPriceSyncTS time.Time
	prices          types.CurrencyPairDec

	tvwapsByProvider types.PricesWithMutex
	vwapsByProvider  types.PricesWithMutex
}

func New(
	logger zerolog.Logger,
	oc client.OracleClient,
	providerPairs map[types.ProviderName][]types.CurrencyPair,
	providerTimeout time.Duration,
	deviations map[string]sdk.Dec,
	endpoints map[types.ProviderName]provider.Endpoint,
) *Oracle {
	return &Oracle{
		logger:          logger.With().Str("module", "oracle").Logger(),
		closer:          pfsync.NewCloser(),
		oracleClient:    oc,
		providerPairs:   providerPairs,
		priceProviders:  make(map[types.ProviderName]provider.Provider),
		previousPrevote: nil,
		providerTimeout: providerTimeout,
		deviations:      deviations,
		paramCache:      ParamCache{},
		endpoints:       endpoints,
	}
}

// Start starts the oracle process in a blocking fashion.
func (o *Oracle) Start(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			o.closer.Close()

		default:
			o.logger.Debug().Msg("starting oracle tick")

			startTime := time.Now()

			if err := o.tick(ctx); err != nil {
				telemetry.IncrCounter(1, "failure", "tick")
				o.logger.Err(err).Msg("oracle tick failed")
			}

			o.lastPriceSyncTS = time.Now()

			telemetry.MeasureSince(startTime, "runtime", "tick")
			telemetry.IncrCounter(1, "new", "tick")

			time.Sleep(tickerSleep)
		}
	}
}

// Stop stops the oracle process and waits for it to gracefully exit.
func (o *Oracle) Stop() {
	o.closer.Close()
	<-o.closer.Done()
}

// GetLastPriceSyncTimestamp returns the latest timestamp at which prices where
// fetched from the oracle's set of exchange rate providers.
func (o *Oracle) GetLastPriceSyncTimestamp() time.Time {
	o.pricesMutex.RLock()
	defer o.pricesMutex.RUnlock()

	return o.lastPriceSyncTS
}

// GetPrices returns a copy of the current prices fetched from the oracle's
// set of exchange rate providers.
func (o *Oracle) GetPrices() types.CurrencyPairDec {
	o.pricesMutex.RLock()
	defer o.pricesMutex.RUnlock()

	// Creates a new array for the prices in the oracle
	prices := make(types.CurrencyPairDec, len(o.prices))

	for k, v := range o.prices {
		// Fills in the prices with each value in the oracle
		prices[k] = v
	}

	return prices
}

// GetTvwapPrices returns a copy of the tvwapsByProvider map
func (o *Oracle) GetTvwapPrices() types.CurrencyPairDecByProvider {
	return o.tvwapsByProvider.GetPricesClone()
}

// GetVwapPrices returns the vwapsByProvider map using a read lock
func (o *Oracle) GetVwapPrices() types.CurrencyPairDecByProvider {
	return o.vwapsByProvider.GetPricesClone()
}

// SetPrices retrieves all the prices and candles from our set of providers as
// determined in the config. If candles are available, uses TVWAP in order
// to determine prices. If candles are not available, uses the most recent prices
// with VWAP. Warns the the user of any missing prices, and filters out any faulty
// providers which do not report prices or candles within 2𝜎 of the others.
func (o *Oracle) SetPrices(ctx context.Context) error {
	g := new(errgroup.Group)
	mtx := new(sync.Mutex)
	providerPrices := make(types.AggregatedProviderPrices)
	providerCandles := make(types.AggregatedProviderCandles)
	requiredRates := make(map[types.CurrencyPair]struct{})

	for providerName, currencyPairs := range o.providerPairs {
		providerName := providerName
		currencyPairs := currencyPairs

		priceProvider, err := o.getOrSetProvider(ctx, providerName)
		if err != nil {
			return err
		}

		for _, pair := range currencyPairs {
			usdPair := types.CurrencyPair{Base: pair.Base, Quote: config.DenomUSD}
			if _, ok := requiredRates[usdPair]; !ok {
				requiredRates[usdPair] = struct{}{}
			}
		}

		g.Go(func() error {
			prices := make(types.CurrencyPairTickers, 0)
			candles := make(types.CurrencyPairCandles, 0)
			ch := make(chan struct{})
			errCh := make(chan error, 1)

			go func() {
				defer close(ch)
				prices, err = priceProvider.GetTickerPrices(currencyPairs...)
				if err != nil {
					provider.TelemetryFailure(providerName, provider.MessageTypeTicker)
					errCh <- err
				}

				candles, err = priceProvider.GetCandlePrices(currencyPairs...)
				if err != nil {
					provider.TelemetryFailure(providerName, provider.MessageTypeCandle)
					errCh <- err
				}
			}()

			select {
			case <-ch:
				break
			case err := <-errCh:
				return err
			case <-time.After(o.providerTimeout):
				telemetry.IncrCounter(1, "failure", "provider", "type", "timeout")
				return fmt.Errorf("provider timed out")
			}

			// flatten and collect prices based on the base currency per provider
			//
			// e.g.: {ProviderKraken: {"ATOM": <price, volume>, ...}}
			mtx.Lock()
			for _, pair := range currencyPairs {
				success := SetProviderTickerPricesAndCandles(providerName, providerPrices, providerCandles, prices, candles, pair)
				if !success {
					mtx.Unlock()
					return fmt.Errorf("failed to find any exchange rates in provider responses")
				}
			}

			mtx.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		o.logger.Err(err).Msg("failed to get ticker prices from provider")
	}

	computedPrices, err := o.GetComputedPrices(
		providerCandles,
		providerPrices,
	)
	if err != nil {
		return err
	}

	for cp := range requiredRates {
		if _, ok := computedPrices[cp]; !ok {
			o.logger.Warn().Str("asset", cp.String()).Msg("unable to report price for expected asset")
		}
	}

	o.pricesMutex.Lock()
	o.prices = computedPrices
	o.pricesMutex.Unlock()
	return nil
}

func (o *Oracle) RequiredRates() []types.CurrencyPair {
	requiredRatesMap := make(map[types.CurrencyPair]struct{})
	for _, currencyPairs := range o.providerPairs {
		for _, pair := range currencyPairs {
			usdPair := types.CurrencyPair{Base: pair.Base, Quote: config.DenomUSD}
			if _, ok := requiredRatesMap[usdPair]; !ok {
				requiredRatesMap[usdPair] = struct{}{}
			}
		}
	}

	rates := make([]types.CurrencyPair, 0, len(requiredRatesMap))
	for pair := range requiredRatesMap {
		rates = append(rates, pair)
	}
	return rates
}

func (o *Oracle) GetComputedPrices(
	providerCandles types.AggregatedProviderCandles,
	providerPrices types.AggregatedProviderPrices,
) (types.CurrencyPairDec, error) {

	conversionRates, err := CalcCurrencyPairRates(
		providerCandles,
		providerPrices,
		o.deviations,
		config.SupportedConversionSlice(),
		o.logger,
	)
	if err != nil {
		return nil, err
	}

	USDRates := ConvertRatesToUSD(conversionRates)

	convertedCandles := ConvertAggregatedCandles(providerCandles, USDRates)
	convertedTickers := ConvertAggregatedTickers(providerPrices, USDRates)

	prices, err := CalcCurrencyPairRates(
		convertedCandles,
		convertedTickers,
		o.deviations,
		o.RequiredRates(),
		o.logger,
	)
	if err != nil {
		return nil, err
	}

	return prices, nil
}

// SetProviderTickerPricesAndCandles flattens and collects prices for
// candles and tickers based on the base currency per provider.
// Returns true if at least one of price or candle exists.
func SetProviderTickerPricesAndCandles(
	providerName types.ProviderName,
	providerPrices types.AggregatedProviderPrices,
	providerCandles types.AggregatedProviderCandles,
	prices types.CurrencyPairTickers,
	candles types.CurrencyPairCandles,
	pair types.CurrencyPair,
) (success bool) {
	if _, ok := providerPrices[providerName]; !ok {
		providerPrices[providerName] = make(map[types.CurrencyPair]types.TickerPrice)
	}
	if _, ok := providerCandles[providerName]; !ok {
		providerCandles[providerName] = make(map[types.CurrencyPair][]types.CandlePrice)
	}

	tp, pricesOk := prices[pair]
	cp, candlesOk := candles[pair]

	if pricesOk {
		providerPrices[providerName][pair] = tp
	}
	if candlesOk {
		providerCandles[providerName][pair] = cp
	}

	return pricesOk || candlesOk
}

// GetParamCache returns the last updated parameters of the x/oracle module
// if the current ParamCache is outdated, we will query it again.
func (o *Oracle) GetParamCache(ctx context.Context, currentBlockHeigh int64) (oracletypes.Params, error) {
	if !o.paramCache.IsOutdated(currentBlockHeigh) {
		return *o.paramCache.params, nil
	}

	params, err := o.GetParams(ctx)
	if err != nil {
		return oracletypes.Params{}, err
	}

	o.checkAcceptList(params)
	o.paramCache.Update(currentBlockHeigh, params)
	return params, nil
}

// GetParams returns the current on-chain parameters of the x/oracle module.
func (o *Oracle) GetParams(ctx context.Context) (oracletypes.Params, error) {
	grpcConn, err := grpc.Dial(
		o.oracleClient.GRPCEndpoint,
		// the Cosmos SDK doesn't support any transport security mechanism
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(dialerFunc),
	)
	if err != nil {
		return oracletypes.Params{}, fmt.Errorf("failed to dial Cosmos gRPC service: %w", err)
	}

	defer grpcConn.Close()
	queryClient := oracletypes.NewQueryClient(grpcConn)

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	queryResponse, err := queryClient.Params(ctx, &oracletypes.QueryParams{})
	if err != nil {
		return oracletypes.Params{}, fmt.Errorf("failed to get x/oracle params: %w", err)
	}

	return queryResponse.Params, nil
}

func (o *Oracle) getOrSetProvider(ctx context.Context, providerName types.ProviderName) (provider.Provider, error) {
	var (
		priceProvider provider.Provider
		ok            bool
	)

	priceProvider, ok = o.priceProviders[providerName]
	if !ok {
		newProvider, err := NewProvider(
			ctx,
			providerName,
			o.logger,
			o.endpoints[providerName],
			o.providerPairs[providerName]...,
		)
		if err != nil {
			return nil, err
		}
		newProvider.StartConnections()
		priceProvider = newProvider
		o.priceProviders[providerName] = newProvider
	}

	return priceProvider, nil
}

func NewProvider(
	ctx context.Context,
	providerName types.ProviderName,
	logger zerolog.Logger,
	endpoint provider.Endpoint,
	providerPairs ...types.CurrencyPair,
) (provider.Provider, error) {
	switch providerName {
	case provider.ProviderBinance:
		return provider.NewBinanceProvider(ctx, logger, endpoint, false, providerPairs...)

	case provider.ProviderBinanceUS:
		return provider.NewBinanceProvider(ctx, logger, endpoint, true, providerPairs...)

	case provider.ProviderKraken:
		return provider.NewKrakenProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderOsmosisV2:
		return provider.NewOsmosisV2Provider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderHuobi:
		return provider.NewHuobiProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderCoinbase:
		return provider.NewCoinbaseProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderOkx:
		return provider.NewOkxProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderGate:
		return provider.NewGateProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderBitget:
		return provider.NewBitgetProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderMexc:
		return provider.NewMexcProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderCrypto:
		return provider.NewCryptoProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderPolygon:
		return provider.NewPolygonProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderCrescent:
		return provider.NewCrescentProvider(ctx, logger, endpoint, providerPairs...)

	case provider.ProviderMock:
		return provider.NewMockProvider(), nil

	default:
		// check if any version of uniswap
		name := strings.Split(providerName.String(), "-")

		// provider is not of type chain-uniswap
		if len(name) != 2 && name[1] != "uniswap" {
			break
		}

		return provider.NewUniswapProvider(ctx, logger, providerName.String(), endpoint, providerPairs...), nil
	}

	return nil, fmt.Errorf("provider %s not found", providerName)
}

func (o *Oracle) checkAcceptList(params oracletypes.Params) {
	for _, denom := range params.AcceptList {
		symbol := strings.ToUpper(denom.SymbolDenom)
		cp := types.CurrencyPair{Base: symbol, Quote: "USD"}
		if _, ok := o.prices[cp]; !ok {
			o.logger.Warn().Str("denom", symbol).Msg("price missing for required denom")
		}
	}
}

func (o *Oracle) tick(ctx context.Context) error {
	o.logger.Debug().Msg("executing oracle tick")

	blockHeight, err := o.oracleClient.ChainHeight.GetChainHeight()
	if err != nil {
		return err
	}
	if blockHeight < 1 {
		return fmt.Errorf("expected positive block height")
	}

	oracleParams, err := o.GetParamCache(ctx, blockHeight)
	if err != nil {
		return err
	}

	if err := o.SetPrices(ctx); err != nil {
		return err
	}

	// Get oracle vote period, next block height, current vote period, and index
	// in the vote period.
	oracleVotePeriod := int64(oracleParams.VotePeriod)
	nextBlockHeight := blockHeight + 1
	currentVotePeriod := math.Floor(float64(nextBlockHeight) / float64(oracleVotePeriod))
	indexInVotePeriod := nextBlockHeight % oracleVotePeriod

	// Skip until new voting period. Specifically, skip when:
	// index [0, oracleVotePeriod - 1] > oracleVotePeriod - 2 OR index is 0
	if (o.previousVotePeriod != 0 && currentVotePeriod == o.previousVotePeriod) ||
		oracleVotePeriod-indexInVotePeriod < 2 {
		o.logger.Info().
			Int64("vote_period", oracleVotePeriod).
			Float64("previous_vote_period", o.previousVotePeriod).
			Float64("current_vote_period", currentVotePeriod).
			Msg("skipping until next voting period")

		return nil
	}

	// If we're past the voting period we needed to hit, reset and submit another
	// prevote.
	if o.previousVotePeriod != 0 && currentVotePeriod-o.previousVotePeriod != 1 {
		o.logger.Info().
			Int64("vote_period", oracleVotePeriod).
			Float64("previous_vote_period", o.previousVotePeriod).
			Float64("current_vote_period", currentVotePeriod).
			Msg("missing vote during voting period")
		telemetry.IncrCounter(1, "vote", "failure", "missed")

		o.previousVotePeriod = 0
		o.previousPrevote = nil
		return nil
	}

	salt, err := GenerateSalt(32)
	if err != nil {
		return err
	}

	valAddr, err := sdk.ValAddressFromBech32(o.oracleClient.ValidatorAddrString)
	if err != nil {
		return err
	}

	exchangeRatesStr := GenerateExchangeRatesString(o.prices)
	hash := oracletypes.GetAggregateVoteHash(salt, exchangeRatesStr, valAddr)
	preVoteMsg := &oracletypes.MsgAggregateExchangeRatePrevote{
		Hash:      hash.String(), // hash of prices from the oracle
		Feeder:    o.oracleClient.OracleAddrString,
		Validator: valAddr.String(),
	}

	isPrevoteOnlyTx := o.previousPrevote == nil
	if isPrevoteOnlyTx {
		// This timeout could be as small as oracleVotePeriod-indexInVotePeriod,
		// but we give it some extra time just in case.
		//
		// Ref : https://github.com/terra-money/oracle-feeder/blob/baef2a4a02f57a2ffeaa207932b2e03d7fb0fb25/feeder/src/vote.ts#L222
		o.logger.Info().
			Str("hash", hash.String()).
			Str("validator", preVoteMsg.Validator).
			Str("feeder", preVoteMsg.Feeder).
			Msg("broadcasting pre-vote")
		if err := o.oracleClient.BroadcastTx(nextBlockHeight, oracleVotePeriod*2, preVoteMsg); err != nil {
			return err
		}

		currentHeight, err := o.oracleClient.ChainHeight.GetChainHeight()
		if err != nil {
			return err
		}

		o.previousVotePeriod = math.Floor(float64(currentHeight) / float64(oracleVotePeriod))
		o.previousPrevote = &PreviousPrevote{
			Salt:              salt,
			ExchangeRates:     exchangeRatesStr,
			SubmitBlockHeight: currentHeight,
		}
	} else {
		// otherwise, we're in the next voting period and thus we vote
		voteMsg := &oracletypes.MsgAggregateExchangeRateVote{
			Salt:          o.previousPrevote.Salt,
			ExchangeRates: o.previousPrevote.ExchangeRates,
			Feeder:        o.oracleClient.OracleAddrString,
			Validator:     valAddr.String(),
		}

		o.logger.Info().
			Str("exchange_rates", voteMsg.ExchangeRates).
			Str("validator", voteMsg.Validator).
			Str("feeder", voteMsg.Feeder).
			Msg("broadcasting vote")
		if err := o.oracleClient.BroadcastTx(
			nextBlockHeight,
			oracleVotePeriod-indexInVotePeriod,
			voteMsg,
		); err != nil {
			return err
		}

		o.previousPrevote = nil
		o.previousVotePeriod = 0
	}

	return nil
}

// GenerateSalt generates a random salt, size length/2,  as a HEX encoded string.
func GenerateSalt(length int) (string, error) {
	if length == 0 {
		return "", fmt.Errorf("failed to generate salt: zero length")
	}

	bytes := make([]byte, length)

	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}

// GenerateExchangeRatesString generates a canonical string representation of
// the aggregated exchange rates.
func GenerateExchangeRatesString(prices types.CurrencyPairDec) string {
	exchangeRates := make([]string, len(prices))
	i := 0

	// aggregate exchange rates as "<currency_pair>:<price>"
	for cp, avgPrice := range prices {
		exchangeRates[i] = fmt.Sprintf("%s:%s", cp.Base, avgPrice.String())
		i++
	}

	sort.Strings(exchangeRates)

	return strings.Join(exchangeRates, ",")
}
