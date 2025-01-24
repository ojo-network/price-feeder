package usecase

import (
	"fmt"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle/queries"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/usecase/entity"
)

// highestBidAllowedFactor = if (( 1 - pool base ratio) >= 0.5), max ( 0.15, 1-pool base ratio) else 0.5
// lowestAskAllowedFactor = if (( 1 - pool quote ratio) >= 0.5), max ( 0.15, 1-pool quote ratio) else 0.5
// AssetB is USDC,(Base token Atom, quote token USDC) made sure in QueryExtLiqPoolAssetInfo
func CalculateExternalLiquidityUseCase(
	depthData entity.DepthData,
	assetInfo queries.FinaliseAssetInfo,
	assetFound bool,
	tPrice types.TickerPrice,
) (*entity.ExternalLiquidity, error) {

	var highestPrice = float64(0)
	var lowestPrice = float64(0)
	var baseAmount = float64(0)
	var quoteAmount = float64(0)
	var highestBid float64
	var lowestAsk float64

	highestBid = depthData.Bids[0][0]
	lowestAsk = depthData.Asks[0][0]

	// Allow to take 50% on both sides, in case of equal
	highestBidAllowed := highestBid * 0.5
	lowestAskAllowed := lowestAsk * 1.5

	if assetFound {

		assetInfo.TokenA.Amount = math.LegacyNewDecFromInt(assetInfo.TokenA.Amount).Mul(tPrice.Price).RoundInt()
		totalTokens := assetInfo.TokenA.Amount.ToLegacyDec().Add(assetInfo.TokenB.Amount.ToLegacyDec())
		poolBaseRatio := assetInfo.TokenB.Amount.ToLegacyDec().Quo(totalTokens)
		poolQuoteRatio := assetInfo.TokenA.Amount.ToLegacyDec().Quo(totalTokens)

		// Valid string
		midRatio, _ := math.LegacyNewDecFromStr("0.5")
		minFactor, _ := math.LegacyNewDecFromStr("0.15")
		subBaseFactor := math.LegacyNewDec(1).Sub(poolBaseRatio)
		subQuoteFactor := math.LegacyNewDec(1).Sub(poolQuoteRatio)

		if subBaseFactor.LT(midRatio) {
			baseFactor := math.LegacyMaxDec(minFactor, subBaseFactor)
			highestBidAllowed = highestBid * (1 - baseFactor.MustFloat64())
		}

		if subQuoteFactor.LT(midRatio) {
			quoteFactor := math.LegacyMaxDec(minFactor, subQuoteFactor)
			lowestAskAllowed = lowestAsk * (1 + quoteFactor.MustFloat64())
		}
	}

	price := (highestBid + lowestAsk) / 2

	for _, bid := range depthData.Bids {
		price := bid[0]
		if price < highestBidAllowed {
			break
		}
		amount := bid[1]
		quoteAmount += price * amount
		lowestPrice = price
	}

	fmt.Println("highestPrice, lowestPrice, baseAmount, quoteAmount", highestPrice, lowestPrice, baseAmount, quoteAmount)
	for _, ask := range depthData.Asks {
		price := ask[0]
		amount := ask[1]
		if price > lowestAskAllowed {
			break
		}
		baseAmount += amount
		highestPrice = price
	}
	fmt.Println("highestPrice, lowestPrice, baseAmount, quoteAmount", highestPrice, lowestPrice, baseAmount, quoteAmount)
	baseDepth := (highestPrice / price) - 1
	quoteDepth := 1 - (lowestPrice / price)
	fmt.Println("baseDepth", baseDepth)
	fmt.Println("quoteDepth", quoteDepth)

	// Use decimals
	externalLiquidityEntity := entity.ExternalLiquidity{
		BaseAmount:  baseAmount * 1000_000,
		QuoteAmount: quoteAmount * 1000_000,
		BaseDepth:   baseDepth,
		QuoteDepth:  quoteDepth,
	}

	return &externalLiquidityEntity, nil
}
