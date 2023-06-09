package v1

import (
	"time"

	"github.com/ojo-network/price-feeder/oracle/types"
)

// Oracle defines the Oracle interface contract that the v1 router depends on.
type Oracle interface {
	GetLastPriceSyncTimestamp() time.Time
	GetPrices() types.CurrencyPairDec
	GetTvwapPrices() types.CurrencyPairDecByProvider
	GetVwapPrices() types.CurrencyPairDecByProvider
}
