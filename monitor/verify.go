package monitor

import (
	"fmt"

	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/ojo-network/price-feeder/util"
)

type ErrorType int

const (
	PRICE_MATCH             = iota
	ORACLE_MISSING_PRICE    = iota
	ORACLE_DEVIATED_PRICE   = iota
	PROVIDER_MISSING_PRICE  = iota
	PROVIDER_DEVIATED_PRICE = iota
	API_MISSING_PRICE       = iota
	API_BAD_PRICE           = iota
	API_DOWN                = iota
)

type PriceError struct {
	ErrorType ErrorType
	Message   string
}

const (
	maxCoeficientOfVariation = 0.75
)

var (
	KnownIncorrectAPIPrices = map[string]struct{}{
		"stATOM":  {},
		"stkATOM": {},
	}
)

func VerifyPrices(
	cfg *config.Config,
	oracle *oracle.Oracle,
) []PriceError {
	var priceErrors []PriceError
	expectedSymbols := cfg.ExpectedSymbols()

	apiPrices, err := GetCoinMarketCapPrices(expectedSymbols)
	if err != nil {
		apiPrices = make(map[string]float64)
		priceErrors = append(priceErrors, PriceError{
			ErrorType: API_DOWN,
			Message:   err.Error(),
		})
	}

	oraclePrices := oracle.GetPrices()

	for _, denom := range expectedSymbols {
		cp := types.CurrencyPair{Base: denom, Quote: "USD"}

		if _, ok := oraclePrices[cp]; !ok {
			priceErrors = append(priceErrors, PriceError{
				ErrorType: ORACLE_MISSING_PRICE,
				Message:   fmt.Sprintf("FAIL %s oracle price not found", cp),
			})
			continue
		}
		oraclePrice := oraclePrices[cp].MustFloat64()

		if _, ok := apiPrices[denom]; !ok {
			priceErrors = append(priceErrors, PriceError{
				ErrorType: API_MISSING_PRICE,
				Message:   fmt.Sprintf("SKIP %s oracle price: %f, API price: not available at coinmarketcap", denom, oraclePrice),
			})
			continue
		}

		apiPrice := apiPrices[denom]
		cv := util.CalcCoeficientOfVariation([]float64{oraclePrice, apiPrice})

		if _, ok := KnownIncorrectAPIPrices[denom]; ok {
			priceErrors = append(priceErrors, PriceError{
				ErrorType: API_BAD_PRICE,
				Message:   fmt.Sprintf("SKIP %s oracle price: %f, API price: %f (incorrect)", denom, oraclePrice, apiPrice),
			})
			continue
		}

		if cv > maxCoeficientOfVariation {
			priceErrors = append(priceErrors, PriceError{
				ErrorType: ORACLE_DEVIATED_PRICE,
				Message: fmt.Sprintf(
					"FAIL %s deviated oracle price: %f, API price: %f, Variation: %f > %f",
					cp, oraclePrice, apiPrice, cv, maxCoeficientOfVariation,
				),
			})
			continue
		}
		priceErrors = append(priceErrors, PriceError{
			ErrorType: PRICE_MATCH,
			Message: fmt.Sprintf(
				"PASS %s matched oracle price: %f, API price: %f, Variation: %f < %f",
				cp, oraclePrice, apiPrice, cv, maxCoeficientOfVariation,
			),
		})
	}
	return priceErrors
}
