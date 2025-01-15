package queries

import (
	"context"
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/types"
	accountedtypes "github.com/elys-network/elys/x/accountedpool/types"
	ammtypes "github.com/elys-network/elys/x/amm/types"
)

// TESTNET USDC DENOM
//var USDC_DENOM = "ibc/2180E84E20F5679FCC760D8C165B60F42065DEF7F46A72B447CFF1B7DC6C0A65"

// DEVNET USDC DENOM
// var USDC_DENOM = "uusdc"

type FinaliseAssetInfo struct {
	TokenA types.Coin
	TokenB types.Coin // Assuming TokenB will always be USDC
}

// Finalise and set AssetB to USDC
func QueryExtLiqPoolAssetInfo(oracleClient client.Context, poolId uint64) (FinaliseAssetInfo, error) {
	finaliseAssetInfo := FinaliseAssetInfo{}

	ammPool, err := QueryAmmPool(oracleClient, poolId)
	if err != nil {
		return FinaliseAssetInfo{}, fmt.Errorf("failed to get elys x/amm Pool: %w", err)
	}

	// TODO: Optimise here by making async queries
	accountedPool, accountedPoolerr := QueryAccountedPool(oracleClient, poolId)

	if len(ammPool.PoolAssets) != 2 {
		return FinaliseAssetInfo{}, fmt.Errorf("more than 2 assets found in the ammPool: %w", err)
	}

	finaliseAssetInfo.TokenA = ammPool.PoolAssets[0].Token
	finaliseAssetInfo.TokenB = ammPool.PoolAssets[1].Token

	if accountedPoolerr == nil && len(ammPool.PoolAssets) == 2 {
		if accountedPool.PoolAssets[0].Token.Denom == finaliseAssetInfo.TokenA.Denom &&
			accountedPool.PoolAssets[1].Token.Denom == finaliseAssetInfo.TokenB.Denom {

			finaliseAssetInfo.TokenA = accountedPool.PoolAssets[0].Token
			finaliseAssetInfo.TokenB = accountedPool.PoolAssets[1].Token
		} else if accountedPool.PoolAssets[1].Token.Denom == finaliseAssetInfo.TokenA.Denom &&
			accountedPool.PoolAssets[0].Token.Denom == finaliseAssetInfo.TokenB.Denom {

			finaliseAssetInfo.TokenA = accountedPool.PoolAssets[1].Token
			finaliseAssetInfo.TokenB = accountedPool.PoolAssets[0].Token
		}
		// Else we have found different pair in accounted pool, leave it use amm-pool (Should Not Happen)
	}

	USDC_DENOM, _ := os.LookupEnv("USDC_DENOM")

	if finaliseAssetInfo.TokenA.Denom != USDC_DENOM && finaliseAssetInfo.TokenB.Denom != USDC_DENOM {
		return FinaliseAssetInfo{}, fmt.Errorf("pool asset info does not contain USDC")
	}

	// Make AssetB -> USDC
	if finaliseAssetInfo.TokenA.Denom == USDC_DENOM {
		usdcAsset := finaliseAssetInfo.TokenA
		finaliseAssetInfo.TokenA = finaliseAssetInfo.TokenB
		finaliseAssetInfo.TokenB = usdcAsset
	}

	return finaliseAssetInfo, nil
}

func QueryAmmPool(ctx client.Context, poolId uint64) (ammtypes.Pool, error) {
	queryClient := ammtypes.NewQueryClient(ctx)

	queryResponse, err := queryClient.Pool(context.Background(), &ammtypes.QueryGetPoolRequest{PoolId: poolId})
	if err != nil {
		return ammtypes.Pool{}, fmt.Errorf("failed to get elys x/amm Pool: %w", err)
	}

	return queryResponse.Pool, nil
}

func QueryAccountedPool(ctx client.Context, poolId uint64) (accountedtypes.AccountedPool, error) {
	queryClient := accountedtypes.NewQueryClient(ctx)

	queryResponse, err := queryClient.AccountedPool(context.Background(), &accountedtypes.QueryGetAccountedPoolRequest{PoolId: poolId})
	if err != nil {
		return accountedtypes.AccountedPool{}, fmt.Errorf("failed to get elys x/accountedpool Pool: %w", err)
	}

	return queryResponse.AccountedPool, nil
}
