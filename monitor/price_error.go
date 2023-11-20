package monitor

import (
	"fmt"
	"time"
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

var (
	criticalErrorTypes = map[ErrorType]struct{}{
		ORACLE_MISSING_PRICE:  {},
		ORACLE_DEVIATED_PRICE: {},
	}
)

type PriceError struct {
	ErrorType    ErrorType
	CurrencyPair string
	Message      string
	occurredAt   time.Time
}

func (pe PriceError) Key() string {
	return fmt.Sprintf("%d%s", pe.ErrorType, pe.CurrencyPair)
}
