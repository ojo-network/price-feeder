package queries

import (
	"fmt"
	"os"

	"github.com/cosmos/cosmos-sdk/types"
	oracletypes "github.com/ojo-network/ojo/x/oracle/types"
)

// TESTNET USDC DENOM
// var USDC_DENOM = "ibc/2180E84E20F5679FCC760D8C165B60F42065DEF7F46A72B447CFF1B7DC6C0A65"

// DEVNET USDC DENOM
// var USDC_DENOM = "uusdc"

type FinaliseAssetInfo struct {
	TokenA types.Coin
	TokenB types.Coin // Assuming TokenB will always be USDC
}

// Finalize and set AssetB to USDC
func QueryExtLiqPoolAssetInfo(
	ammPools map[uint64]oracletypes.Pool,
	accountedPools map[uint64]oracletypes.AccountedPool,
	poolID uint64,
) (FinaliseAssetInfo, error) {
	finaliseAssetInfo := FinaliseAssetInfo{}

	ammPool, existsAmm := ammPools[poolID]
	if !existsAmm {
		return FinaliseAssetInfo{}, fmt.Errorf("ammPool not found for poolId: %d", poolID)
	}

	accountedPool, existsAccountPool := accountedPools[poolID]

	if len(ammPool.PoolAssets) != 2 {
		return FinaliseAssetInfo{}, fmt.Errorf("more than 2 assets found in the ammPool")
	}

	finaliseAssetInfo.TokenA = ammPool.PoolAssets[0].Token
	finaliseAssetInfo.TokenB = ammPool.PoolAssets[1].Token

	if existsAccountPool && len(ammPool.PoolAssets) == 2 {
		if accountedPool.TotalTokens[0].Denom == finaliseAssetInfo.TokenA.Denom &&
			accountedPool.TotalTokens[1].Denom == finaliseAssetInfo.TokenB.Denom {

			finaliseAssetInfo.TokenA = accountedPool.TotalTokens[0]
			finaliseAssetInfo.TokenB = accountedPool.TotalTokens[1]
		} else if accountedPool.TotalTokens[1].Denom == finaliseAssetInfo.TokenA.Denom &&
			accountedPool.TotalTokens[0].Denom == finaliseAssetInfo.TokenB.Denom {

			finaliseAssetInfo.TokenA = accountedPool.TotalTokens[1]
			finaliseAssetInfo.TokenB = accountedPool.TotalTokens[0]
		}
		// Else we have found different pair in accounted pool, leave it use amm-pool (Should Not Happen)
	}

	usdcDENOM, _ := os.LookupEnv("USDC_DENOM")

	if finaliseAssetInfo.TokenA.Denom != usdcDENOM && finaliseAssetInfo.TokenB.Denom != usdcDENOM {
		return FinaliseAssetInfo{}, fmt.Errorf("pool asset info does not contain USDC")
	}

	// Make AssetB -> USDC
	if finaliseAssetInfo.TokenA.Denom == usdcDENOM {
		finaliseAssetInfo.TokenA, finaliseAssetInfo.TokenB = finaliseAssetInfo.TokenB, finaliseAssetInfo.TokenA
	}

	return finaliseAssetInfo, nil
}
