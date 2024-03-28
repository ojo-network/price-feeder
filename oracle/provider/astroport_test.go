package provider

// import (
// 	"context"
// 	"os"
// 	"testing"
// 	"time"

// 	"github.com/ojo-network/price-feeder/oracle/types"
// 	"github.com/rs/zerolog"
// 	"github.com/stretchr/testify/require"
// )

// // TestAstroportProvider_GetTickers tests the polling process.
// // TODO: Make this more comprehensive.
// //
// // Ref: https://github.com/ojo-network/price-feeder/issues/317
// func TestAstroportProvider_GetTickers(t *testing.T) {
// 	ctx := context.Background()
// 	pairs := []types.CurrencyPair{{
// 		Base:  "STINJ",
// 		Quote: "INJ",
// 	}}
// 	p, err := NewAstroportProvider(
// 		ctx,
// 		zerolog.New(os.Stdout).With().Timestamp().Logger(),
// 		Endpoint{},
// 		pairs...,
// 	)
// 	require.NoError(t, err)
// 	availPairs, err := p.GetAvailablePairs()
// 	require.NoError(t, err)
// 	require.NotEmpty(t, availPairs)

// 	p.StartConnections()
// 	time.Sleep(10 * time.Second)

// 	res, err := p.GetTickerPrices(pairs...)
// 	require.NoError(t, err)
// 	require.NotEmpty(t, res)
// }
