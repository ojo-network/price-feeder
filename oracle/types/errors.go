package types

import (
	"cosmossdk.io/errors"
)

const ModuleName = "price-feeder"

// Price feeder errors
var (
	ErrProviderConnection  = errors.Register(ModuleName, 2, "provider connection")
	ErrMissingExchangeRate = errors.Register(ModuleName, 3, "missing exchange rate for %s")
	ErrTickerNotFound      = errors.Register(ModuleName, 4, "%s failed to get ticker price for %s")
	ErrCandleNotFound      = errors.Register(ModuleName, 5, "%s failed to get candle price for %s")
	ErrNoTickers           = errors.Register(ModuleName, 6, "%s has no ticker data for requested pairs: %v")
	ErrNoCandles           = errors.Register(ModuleName, 7, "%s has no candle data for requested pairs: %v")

	ErrWebsocketDial  = errors.Register(ModuleName, 8, "error connecting to %s websocket: %w")
	ErrWebsocketClose = errors.Register(ModuleName, 9, "error closing %s websocket: %w")
	ErrWebsocketSend  = errors.Register(ModuleName, 10, "error sending to %s websocket: %w")
	ErrWebsocketRead  = errors.Register(ModuleName, 11, "error reading from %s websocket: %w")
)
