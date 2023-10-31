package oracle

import (
	"context"
	"sync"

	tmrpcclient "github.com/cometbft/cometbft/rpc/client"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	tmctypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	"github.com/rs/zerolog"
)

const (
	// paramsCacheInterval represents the amount of blocks
	// during which we will cache the oracle params.
	paramsCacheInterval = int64(200)
)

var (
	queryEventParamUpdate = "tm.event='NewBlock' AND param_update.notify_price_feeder=1"
)

// ParamCache is used to cache oracle param data for
// an amount of blocks, defined by paramsCacheInterval.
type ParamCache struct {
	Logger zerolog.Logger

	mtx              sync.RWMutex
	errGetParams     error
	params           *oracletypes.Params
	lastUpdatedBlock int64
	paramUpdateEvent bool
}

// Initialize initializes a ParamCache struct that
// starts a new goroutine subscribed to param update events.
// It is also updated every paramsCacheInterval incase
// param update events events are missed.
func (paramCache *ParamCache) Initialize(
	ctx context.Context,
	client client.TendermintRPC,
	logger zerolog.Logger,
) error {
	rpcClient := client.(*rpchttp.HTTP)

	if !rpcClient.IsRunning() {
		if err := rpcClient.Start(); err != nil {
			return err
		}
	}

	newOracleParamsSubscription, err := rpcClient.Subscribe(ctx, "", queryEventParamUpdate)
	if err != nil {
		return err
	}

	paramCache.Logger = logger.With().Str("oracle_client", "oracle_params").Logger()

	go paramCache.subscribe(ctx, rpcClient, newOracleParamsSubscription)

	return nil
}

// Update retrieves the most recent oracle params and
// updates the instance.
func (paramCache *ParamCache) UpdateParamCache(currentBlockHeight int64, params oracletypes.Params, err error) {
	paramCache.mtx.Lock()
	defer paramCache.mtx.Unlock()

	paramCache.lastUpdatedBlock = currentBlockHeight
	paramCache.params = &params
	paramCache.errGetParams = err
	paramCache.paramUpdateEvent = false
}

// IsOutdated checks whether or not the current
// param data was fetched in the last 200 blocks.
func (paramCache *ParamCache) IsOutdated(currentBlockHeight int64) bool {
	if paramCache.params == nil {
		return true
	}

	if currentBlockHeight < paramsCacheInterval {
		return false
	}

	// This is an edge case, which should never happen.
	// The current blockchain height is lower
	// than the last updated block, to fix we should
	// just update the cached params again.
	if currentBlockHeight < paramCache.lastUpdatedBlock {
		return true
	}

	return (currentBlockHeight - paramCache.lastUpdatedBlock) > paramsCacheInterval
}

// subscribe listens to param update events.
func (paramCache *ParamCache) subscribe(
	ctx context.Context,
	eventsClient tmrpcclient.EventsClient,
	newOracleParamsSubscription <-chan tmctypes.ResultEvent,
) {
	for {
		select {
		case <-ctx.Done():
			err := eventsClient.Unsubscribe(ctx, "", queryEventParamUpdate)
			if err != nil {
				paramCache.Logger.Err(err)
			}
			paramCache.Logger.Info().Msg("closing the param update event subscription")
			return

		case <-newOracleParamsSubscription:
			paramCache.Logger.Debug().Msg("Got param update event")
			paramCache.paramUpdateEvent = true
		}
	}
}
