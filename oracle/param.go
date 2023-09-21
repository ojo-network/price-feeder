package oracle

import (
	"context"
	"errors"
	"sync"

	proto "github.com/cosmos/gogoproto/proto"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
	tmrpcclient "github.com/cometbft/cometbft/rpc/client"
	tmctypes "github.com/cometbft/cometbft/rpc/core/types"
	tmtypes "github.com/cometbft/cometbft/types"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/rs/zerolog"
)

const (
	// paramsCacheInterval represents the amount of blocks
	// during which we will cache the oracle params.
	paramsCacheInterval = int64(200)
)

var (
	evtType = proto.MessageName(&oracletypes.EventParamUpdate{})

	errParseEventParamUpdate = errors.New("error parsing EventParamUpdate")
	queryEventParamUpdate    = tmtypes.QueryForEvent(evtType)
)

// ParamCache is used to cache oracle param data for
// an amount of blocks, defined by paramsCacheInterval.
type ParamCache struct {
	Logger zerolog.Logger

	mtx          	 sync.RWMutex
	errGetParams 	 error
	params           *oracletypes.Params
	lastUpdatedBlock int64
}

// NewOracleParamCache returns a new ParamCache struct that
// starts a new goroutine subscribed to EventParamUpdate.
// It is also updated every paramsCacheInterval incase
// ParamUpdate events are missed.
func NewOracleParamCache(
	ctx context.Context,
	client client.TendermintRPC,
	logger zerolog.Logger,
) (*ParamCache, error) {
	rpcClient := client.(*rpchttp.HTTP)

	if !rpcClient.IsRunning() {
		if err := rpcClient.Start(); err != nil {
			return nil, err
		}
	}

	newOracleParamsSubscription, err := rpcClient.Subscribe(
		ctx, evtType, queryEventParamUpdate.String(),
	)
	if err != nil {
		return nil, err
	}

	paramCache := &ParamCache{
		Logger:           logger.With().Str("oracle_client", "oracle_params").Logger(),
		errGetParams:     nil,
		params:           nil,
	}

	go paramCache.subscribe(ctx, rpcClient, newOracleParamsSubscription)

	return paramCache, nil
}

// Update retrieves the most recent oracle params and
// updates the instance.
func (paramCache *ParamCache) UpdateParamCache(currentBlockHeight int64, params oracletypes.Params, err error) {
	paramCache.mtx.Lock()
	defer paramCache.mtx.Unlock()

	paramCache.lastUpdatedBlock = currentBlockHeight
	paramCache.params = &params
	paramCache.errGetParams = err
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
			err := eventsClient.Unsubscribe(ctx, evtType, queryEventParamUpdate.String())
			if err != nil {
				paramCache.Logger.Err(err)
				paramCache.UpdateParamCache(paramCache.lastUpdatedBlock, *paramCache.params, err)
			}
			paramCache.Logger.Info().Msg("closing the ParamCache subscription")
			return

		case resultEvent := <-newOracleParamsSubscription:
			eventDataParamUpdate, ok := resultEvent.Data.(oracletypes.EventParamUpdate)
			if !ok {
				paramCache.Logger.Err(errParseEventParamUpdate)
				paramCache.UpdateParamCache(paramCache.lastUpdatedBlock, *paramCache.params, errParseEventParamUpdate)
				continue
			}
			paramCache.Logger.Info().Msg("Updating param cache from event")
			paramCache.UpdateParamCache(eventDataParamUpdate.Block, eventDataParamUpdate.Params, nil)
		}
	}
}
