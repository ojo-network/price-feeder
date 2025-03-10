package usecase

import (
	"errors"
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

	var highestPrice = math.LegacyNewDec(0)
	var lowestPrice = math.LegacyNewDec(0)
	var baseAmount = math.LegacyNewDec(0)
	var quoteAmount = math.LegacyNewDec(0)
	var highestBid math.LegacyDec
	var lowestAsk math.LegacyDec

	if len(depthData.Bids) > 0 && len(depthData.Asks) > 0 && len(depthData.Bids[0]) > 0 && len(depthData.Asks[0]) > 0 {
		var err error
		highestBid, err = math.LegacyNewDecFromStr(fmt.Sprintf("%f", depthData.Bids[0][0]))
		if err != nil {
			return nil, err
		}
		lowestAsk, err = math.LegacyNewDecFromStr(fmt.Sprintf("%f", depthData.Asks[0][0]))
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("no bids or asks in depthData")
	}

	// Allow to take 50% on both sides, in case of equal
	highestBidAllowed := highestBid.QuoInt64(2)
	lowestAskAllowed := lowestAsk.MulInt64(3).QuoInt64(2)

	if assetFound {

		assetInfo.TokenA.Amount = math.LegacyNewDecFromInt(assetInfo.TokenA.Amount).Mul(tPrice.Price).RoundInt()
		totalTokens := assetInfo.TokenA.Amount.ToLegacyDec().Add(assetInfo.TokenB.Amount.ToLegacyDec())
		poolBaseRatio := assetInfo.TokenB.Amount.ToLegacyDec().Quo(totalTokens)
		poolQuoteRatio := assetInfo.TokenA.Amount.ToLegacyDec().Quo(totalTokens)

		// Valid string
		midRatio, err := math.LegacyNewDecFromStr("0.5")
		if err != nil {
			return nil, err
		}
		minFactor, err := math.LegacyNewDecFromStr("0.15")
		if err != nil {
			return nil, err
		}
		subBaseFactor := math.LegacyNewDec(1).Sub(poolBaseRatio)
		subQuoteFactor := math.LegacyNewDec(1).Sub(poolQuoteRatio)

		if subBaseFactor.LT(midRatio) {
			baseFactor := math.LegacyMaxDec(minFactor, subBaseFactor)
			highestBidAllowed = highestBid.Mul(math.LegacyOneDec().Sub(baseFactor))
		}

		if subQuoteFactor.LT(midRatio) {
			quoteFactor := math.LegacyMaxDec(minFactor, subQuoteFactor)
			lowestAskAllowed = lowestAsk.Mul(math.LegacyOneDec().Add(quoteFactor))
		}
	}

	price := (highestBid.Add(lowestAsk)).QuoInt64(2)

	for _, bid := range depthData.Bids {
		price, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", bid[0]))
		if err != nil {
			return nil, err
		}
		if price.LT(highestBidAllowed) {
			break
		}

		amount, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", bid[1]))
		if err != nil {
			return nil, err
		}
		quoteAmount = quoteAmount.Add(price.Mul(amount))
		lowestPrice = price
	}

	fmt.Println("highestPrice, lowestPrice, baseAmount, quoteAmount", highestPrice, lowestPrice, baseAmount, quoteAmount)
	for _, ask := range depthData.Asks {
		price, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", ask[0]))
		if err != nil {
			return nil, err
		}
		amount, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", ask[1]))
		if err != nil {
			return nil, err
		}

		if price.GT(lowestAskAllowed) {
			break
		}
		baseAmount = baseAmount.Add(amount)
		highestPrice = price
	}
	fmt.Println("highestPrice, lowestPrice, baseAmount, quoteAmount", highestPrice, lowestPrice, baseAmount, quoteAmount)
	baseDepth := (highestPrice.Quo(price)).Sub(math.LegacyOneDec())
	quoteDepth := math.LegacyOneDec().Sub(lowestPrice.Quo(price))
	fmt.Println("baseDepth", baseDepth)
	fmt.Println("quoteDepth", quoteDepth)

	// Use decimals
	externalLiquidityEntity := entity.ExternalLiquidity{
		BaseAmount:  baseAmount.MulInt64(1000_000),
		QuoteAmount: quoteAmount.MulInt64(1000_000),
		BaseDepth:   baseDepth,
		QuoteDepth:  quoteDepth,
	}

	return &externalLiquidityEntity, nil
}
