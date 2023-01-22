package types

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const ModuleName = "price-feeder"

// Price feeder errors
var (
	ErrProviderConnection  = sdkerrors.Register(ModuleName, 1, "provider connection")
	ErrMissingExchangeRate = sdkerrors.Register(ModuleName, 2, "missing exchange rate for %s")
	ErrTickerNotFound      = sdkerrors.Register(ModuleName, 3, "%s failed to get ticker price for %s")
	ErrCandleNotFound      = sdkerrors.Register(ModuleName, 4, "%s failed to get candle price for %s")
	ErrNoTickers           = sdkerrors.Register(ModuleName, 5, "%s has no ticker data for requested pairs: %v")
	ErrNoCandles           = sdkerrors.Register(ModuleName, 6, "%s has no candle data for requested pairs: %v")

	ErrWebsocketDial  = sdkerrors.Register(ModuleName, 7, "error connecting to %s websocket: %w")
	ErrWebsocketClose = sdkerrors.Register(ModuleName, 8, "error closing %s websocket: %w")
	ErrWebsocketSend  = sdkerrors.Register(ModuleName, 9, "error sending to %s websocket: %w")
	ErrWebsocketRead  = sdkerrors.Register(ModuleName, 10, "error reading from %s websocket: %w")
)
