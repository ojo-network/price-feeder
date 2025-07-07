package provider

import (
	"context"
	"testing"

	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestDPSNProvider_New tests the creation of a new DPSN provider.
func TestDPSNProvider_New(t *testing.T) {
	ctx := context.Background()
	pairs := []types.CurrencyPair{{
		Base:  "WHITE",
		Quote: "USDC",
		DPSNTopics: &types.DPSNTopics{
			TopicID: "0xcaa7f02362c7a2f1d5d7a1b40c46b4c92acf20eb84231adda6a1a6903adbf7cb",
			AssetID: "0x6Ec94F50cAdcc79984463688dE42A0Ca696EC2db",
		},
	}}

	p, err := NewDPSNProvider(
		ctx,
		zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger(),
		Endpoint{
			Name:   ProviderDPSN,
			Rest:   dpsnRestURL,
			APIKey: "test-api-key",
		},
		pairs...,
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	require.Equal(t, ProviderDPSN, p.endpoints.Name)
}

// TestDPSNProvider_New_WithoutTopics tests that pairs without DPSN topics are ignored.
func TestDPSNProvider_New_WithoutTopics(t *testing.T) {
	ctx := context.Background()
	pairs := []types.CurrencyPair{{
		Base:  "WHITE",
		Quote: "USDC",
		// No DPSNTopics configured
	}}

	p, err := NewDPSNProvider(
		ctx,
		zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger(),
		Endpoint{
			Name:   ProviderDPSN,
			Rest:   dpsnRestURL,
			APIKey: "test-api-key",
		},
		pairs...,
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	// The pair should be ignored since it doesn't have DPSN topics configured
	require.Empty(t, p.subscribedPairs)
}

// TestDPSNProvider_GetAvailablePairs tests that GetAvailablePairs returns an empty map
// since DPSN doesn't provide an endpoint to get all available pairs.
func TestDPSNProvider_GetAvailablePairs(t *testing.T) {
	ctx := context.Background()
	p, err := NewDPSNProvider(
		ctx,
		zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger(),
		Endpoint{
			Name:   ProviderDPSN,
			Rest:   dpsnRestURL,
			APIKey: "test-api-key",
		},
	)
	require.NoError(t, err)

	availablePairs, err := p.GetAvailablePairs()
	require.NoError(t, err)
	require.Empty(t, availablePairs)
}

// TestDPSNProvider_SubscribeCurrencyPairs tests the subscription functionality.
func TestDPSNProvider_SubscribeCurrencyPairs(t *testing.T) {
	ctx := context.Background()
	p, err := NewDPSNProvider(
		ctx,
		zerolog.New(zerolog.NewTestWriter(t)).With().Timestamp().Logger(),
		Endpoint{
			Name:   ProviderDPSN,
			Rest:   dpsnRestURL,
			APIKey: "test-api-key",
		},
	)
	require.NoError(t, err)

	newPairs := []types.CurrencyPair{{
		Base:  "WHITE",
		Quote: "USDC",
		DPSNTopics: &types.DPSNTopics{
			TopicID: "0xcaa7f02362c7a2f1d5d7a1b40c46b4c92acf20eb84231adda6a1a6903adbf7cb",
			AssetID: "0x6Ec94F50cAdcc79984463688dE42A0Ca696EC2db",
		},
	}}

	p.SubscribeCurrencyPairs(newPairs...)
	// This should not panic and should add the pairs to the subscribed pairs
}

// TestDPSNOHLCData_toCandlePrice tests the conversion of OHLC data to CandlePrice.
func TestDPSNOHLCData_toCandlePrice(t *testing.T) {
	ohlcData := DPSNOHLCData{
		Timestamp: 1751651209,
		Pair:      "0x6Ec94F50cAdcc79984463688dE42A0Ca696EC2db",
		Open:      0.00040433134302706765,
		High:      0.00040433134302706765,
		Low:       0.00040433134302706765,
		Close:     0.00040433134302706765,
		Volume0:   "1394060.3188960943",
		Volume1:   "563.662281",
		Token0:    "WHITE",
		Token1:    "USDC",
		Count:     3,
	}

	candlePrice, err := ohlcData.toCandlePrice()
	require.NoError(t, err)
	require.NotNil(t, candlePrice)
	require.Equal(t, int64(1751651209), candlePrice.TimeStamp)
	require.Equal(t, "0.00040433134302706765", candlePrice.Price.String())
	require.Equal(t, "1394060.3188960943", candlePrice.Volume.String())
}
