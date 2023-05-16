package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hasura/go-graphql-client"
	"github.com/stretchr/testify/require"

	"github.com/ojo-network/price-feeder/oracle/types"
)

type Bundle struct {
	EthPriceUSD string `json:"ethPriceUSD"`
	ID          string `json:"id"`
}
type BundleResponse struct {
	Data struct {
		Bundle Bundle `json:"bundle"`
	} `json:"data"`
}

type TokenData struct {
	PriceUSD        float64 `json:"priceUSD"`
	Open            float64 `json:"open"`
	Close           float64 `json:"close"`
	High            float64 `json:"high"`
	Low             float64 `json:"low"`
	PeriodStartUnix int     `json:"periodStartUnix"`
	Token           Token   `json:"token"`
}

type PoolDayData struct {
	ID                 string  `json:"id"`
	PoolID             string  `json:"poolID"`
	PeriodStartUnix    int     `json:"periodStartUnix"`
	Token0             Token   `json:"token0"`
	Token1             Token   `json:"token1"`
	VolumeUSDTracked   float64 `json:"volumeUSDTracked"`
	VolumeUSDUntracked string  `json:"volumeUSDUntracked"`
}

type PoolMinuteData struct {
	ID               string  `json:"id"`
	PoolID           string  `json:"poolID"`
	PeriodStartUnix  int     `json:"periodStartUnix"`
	Token0           Token   `json:"token0"`
	Token1           Token   `json:"token1"`
	Token0Price      string  `json:"token0Price"`
	Token1Price      string  `json:"token1Price"`
	Timestamp        int64   `json:"timestamp"`
	VolumeUSDTracked float64 `json:"volumeUSDTracked"`
	Open             string  `json:"open"`
	High             string  `json:"high"`
	Low              string  `json:"low"`
	Close            string  `json:"close"`
}

type PoolDayDataResponse struct {
	PoolDayDatas []PoolDayData `json:"poolDayDatas"`
}

type TokenMinuteDataResponse struct {
	TokenMinuteDatas []TokenData `json:"tokenMinuteDatas"`
}

type PoolMinuteDataResponse struct {
	PoolMinuteDatas []PoolMinuteData `json:"poolMinuteDatas"`
}

func setupMockServer() *httptest.Server {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-Type", "application/json")

		var jsonResponse []byte
		var response interface{}
		switch req.URL.Path {
		case "/bundle":
			response := BundleResponse{
				Data: struct {
					Bundle Bundle `json:"bundle"`
				}{
					Bundle: Bundle{
						EthPriceUSD: "1234",
						ID:          "1",
					},
				},
			}

			jsonResponse, _ = json.Marshal(response)
		case "/tokenMinuteData":
			response = TokenMinuteDataResponse{
				TokenMinuteDatas: []TokenData{
					{
						PriceUSD:        1.1234,
						Open:            1.0,
						Close:           1.2,
						High:            1.3,
						Low:             0.9,
						PeriodStartUnix: 1623792017,
						Token: Token{
							Name:   "name",
							Symbol: "test",
						},
					},
				},
			}

		case "/poolDayData":
			response = PoolDayDataResponse{
				PoolDayDatas: []PoolDayData{
					{
						ID:                 "1",
						PoolID:             "Pool1",
						PeriodStartUnix:    1623792017,
						Token0:             Token{ID: "Token0ID"},
						Token1:             Token{ID: "Token1ID"},
						VolumeUSDTracked:   1234.56,
						VolumeUSDUntracked: "1234.56",
					},
				},
			}

		case "/poolMinuteData":
			response = PoolMinuteDataResponse{
				PoolMinuteDatas: []PoolMinuteData{
					{
						ID:               "1",
						PoolID:           "Pool1",
						PeriodStartUnix:  1623792017,
						Token0:           Token{ID: "Token0ID"},
						Token1:           Token{ID: "Token1ID"},
						Token0Price:      "1.1234",
						Token1Price:      "1.2345",
						Timestamp:        1623792017,
						VolumeUSDTracked: 1234.56,
						Open:             "1.1",
						High:             "1.2",
						Low:              "1.0",
						Close:            "1.2",
					},
				},
			}

			jsonResponse, _ = json.Marshal(response)
		default:
			http.NotFound(res, req)
			return
		}

		res.Write(jsonResponse)
	}))

	return server
}

func TestUniswapProvider_GetBundle(t *testing.T) {
	// Setup the mock server
	server := setupMockServer()
	defer server.Close()

	// Create a GraphQL client
	provider := NewUniswapProvider(Endpoint{}, []types.AddressPair{})
	provider.client = graphql.NewClient(fmt.Sprintf(server.URL+"/bundle"), server.Client())

	ethPrice, err := provider.GetBundle()
	require.NoError(t, err)

	t.Log(ethPrice)

}

func TestUniswapProvider_GetTickerPrices(t *testing.T) {
	// Setup the mock server
	server := setupMockServer()
	defer server.Close()

	provider := NewUniswapProvider(Endpoint{}, []types.AddressPair{})
	provider.client = graphql.NewClient(fmt.Sprintf(server.URL+"/tokenMinuteData"), server.Client())

	data, err := provider.GetTickerPrices()
	t.Log(data, err)
}

func TestCoinbaseProvider_GetCandlePrices(t *testing.T) {
	// Setup the mock server
	server := setupMockServer()
	defer server.Close()

	provider := NewUniswapProvider(Endpoint{}, []types.AddressPair{})
	provider.client = graphql.NewClient(fmt.Sprintf(server.URL+"/poolMinuteData"), server.Client())

	data, err := provider.GetTickerPrices()
	t.Log(data, err)

}
