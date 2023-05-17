package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/hasura/go-graphql-client"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/libs/rand"

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

type Tokens struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
}

type PoolMinuteData struct {
	ID               string `json:"id"`
	PoolID           string `json:"poolID"`
	PeriodStartUnix  int    `json:"periodStartUnix"`
	Token0           Tokens `json:"token0"`
	Token1           Tokens `json:"token1"`
	Token0Price      string `json:"token0Price"`
	Token1Price      string `json:"token1Price"`
	VolumeUSDTracked string `json:"volumeUSDTracked"`
}

type PoolHourData struct {
	ID                 string  `json:"id"`
	PoolID             string  `json:"poolID"`
	PeriodStartUnix    float64 `json:"periodStartUnix"`
	Token0             Tokens  `json:"token0"`
	Token1             Tokens  `json:"token1"`
	Token0Price        string  `json:"token0Price"`
	Token1Price        string  `json:"token1Price"`
	VolumeUSDTracked   string  `json:"volumeUSDTracked"`
	VolumeUSDUntracked string  `json:"volumeUSDUntracked"`
}

type PoolMinuteDataResponse struct {
	Data struct {
		PoolMinuteDatas []PoolMinuteData `json:"poolMinuteDatas"`
	} `json:"data"`
}

type PoolHourDataResponse struct {
	Data struct {
		PoolHourDatas []PoolHourData `json:"poolHourDatas"`
	} `json:"data"`
}

// setMockData generates random data for eth price and pool minute and hour data
func (p *ProviderTestSuite) setMockData() {
	p.pairAddress = []string{"0xa4e0faA58465A2D369aa21B3e42d43374C6F9613", "0x840DEEef2f115Cf50DA625F7368C24af6fE74410"}
	p.ethPriceUSD = strconv.FormatFloat(rand.Float64()*3000, 'f', -1, 64)
	p.totalVolume = make([]float64, len(p.pairAddress))

	// generate 24 pool data for each pair
	for i, pair := range p.pairAddress {
		// generate address pair
		cPair := types.CurrencyPair{
			Base:  fmt.Sprintf("TEST0%d", i),
			Quote: fmt.Sprintf("TEST1%d", i),
		}

		p.addressPairs = append(p.addressPairs, types.AddressPair{
			CurrencyPair: cPair,
			Address:      pair,
		})

		p.currencyPairs = append(p.currencyPairs, cPair)

		for j := 0; j < 24; j++ {
			vol := rand.Float64() * 10000
			p.hourData = append(p.hourData, PoolHourData{
				ID:              fmt.Sprintf("%s-%d", pair, j),
				PoolID:          pair,
				PeriodStartUnix: float64(time.Now().Unix() - int64(24*60*60*j)),
				Token0: Tokens{
					Name:   fmt.Sprintf("TEST0%d", i),
					Symbol: fmt.Sprintf("SYBMOL0%d", i),
				},
				Token1: Tokens{
					Name:   fmt.Sprintf("TEST1%d", i),
					Symbol: fmt.Sprintf("SYBMOL1%d", i),
				},
				Token0Price:        strconv.FormatFloat(rand.Float64()*3000, 'f', -1, 64),
				Token1Price:        strconv.FormatFloat(rand.Float64()*10000, 'f', -1, 64),
				VolumeUSDTracked:   strconv.FormatFloat(vol, 'f', -1, 64),
				VolumeUSDUntracked: strconv.FormatFloat(rand.Float64()*10000, 'f', -1, 64),
			},
			)

			p.totalVolume[i] += vol
		}
	}

	// generate 10 minute pool data for each pair
	for i, pair := range p.pairAddress {
		for j := 0; j < 10; j++ {
			p.minuteData = append(p.minuteData, PoolMinuteData{
				ID:              fmt.Sprintf("%s-%d", pair, j),
				PoolID:          pair,
				PeriodStartUnix: int(time.Now().Unix() - int64(60*j)),
				Token0: Tokens{
					Name:   fmt.Sprintf("TEST0%d", i),
					Symbol: fmt.Sprintf("SYBMOL0%d", i),
				},
				Token1: Tokens{
					Name:   fmt.Sprintf("TEST1%d", i),
					Symbol: fmt.Sprintf("SYBMOL1%d", i),
				},
				Token0Price:      strconv.FormatFloat(rand.Float64()*3000, 'f', -1, 64),
				Token1Price:      strconv.FormatFloat(rand.Float64()*10000, 'f', -1, 64),
				VolumeUSDTracked: strconv.FormatFloat(rand.Float64()*10000, 'f', -1, 64),
			},
			)
		}
	}
}

func (p *ProviderTestSuite) setupMockServer() {
	server := httptest.NewServer(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		res.Header().Set("Content-Type", "application/json")

		var jsonResponse []byte
		var response interface{}
		switch req.URL.Path {
		case "/bundle":
			response = BundleResponse{
				Data: struct {
					Bundle Bundle `json:"bundle"`
				}{
					Bundle: Bundle{
						EthPriceUSD: p.ethPriceUSD,
						ID:          "1",
					},
				},
			}

			jsonResponse, _ = json.Marshal(response)

		case "/poolHourData":
			response = PoolHourDataResponse{
				Data: struct {
					PoolHourDatas []PoolHourData `json:"poolHourDatas"`
				}(struct{ PoolHourDatas []PoolHourData }{PoolHourDatas: p.hourData}),
			}

			jsonResponse, _ = json.Marshal(response)
		case "/poolMinuteData":
			response = PoolMinuteDataResponse{
				Data: struct {
					PoolMinuteDatas []PoolMinuteData `json:"poolMinuteDatas"`
				}(struct{ PoolMinuteDatas []PoolMinuteData }{PoolMinuteDatas: p.minuteData}),
			}

			jsonResponse, _ = json.Marshal(response)
		default:
			http.NotFound(res, req)
			return
		}

		res.Write(jsonResponse)
	}))

	p.server = server
}

type ProviderTestSuite struct {
	suite.Suite
	server        *httptest.Server
	provider      *UniswapProvider
	ethPriceUSD   string
	pairAddress   []string
	addressPairs  []types.AddressPair
	currencyPairs []types.CurrencyPair
	minuteData    []PoolMinuteData
	hourData      []PoolHourData
	totalVolume   []float64
}

func (p *ProviderTestSuite) SetupSuite() {
	p.setMockData()
	p.setupMockServer()
	p.provider = NewUniswapProvider(Endpoint{}, p.addressPairs)
}

func (suite *ProviderTestSuite) TeadDownSuite() {
	suite.server.Close()
}

func (p *ProviderTestSuite) TestGetBundle() {

	client := graphql.NewClient(fmt.Sprintf(p.server.URL+"/bundle"), p.server.Client())
	p.provider.client = client

	ethPrice, err := p.provider.GetBundle()
	p.NoError(err)

	price, err := strconv.ParseFloat(p.ethPriceUSD, 64)
	p.Require().NoError(err)

	p.EqualValues(price, ethPrice)
}

func (p *ProviderTestSuite) TestGetTickerPrices() {
	client := graphql.NewClient(fmt.Sprintf(p.server.URL+"/poolHourData"), p.server.Client())
	p.provider.client = client

	data, err := p.provider.GetTickerPrices(p.currencyPairs...)
	p.NoError(err)
	p.Len(data, len(p.addressPairs))

	for i, pair := range p.addressPairs {
		hourData := p.hourData[i*24]

		price, err := toSdkDec(hourData.Token1Price)
		p.NoError(err)

		ticker := data[pair.String()]
		p.EqualValues(ticker.Price.String(), price.String())

		// precision issue
		//p.EqualValues(p.totalVolume[i], ticker.Volume)
	}
}

func (p *ProviderTestSuite) TestGetCandlePrices() {
	client := graphql.NewClient(fmt.Sprintf(p.server.URL+"/poolMinuteData"), p.server.Client())
	p.provider.client = client

	data, err := p.provider.GetCandlePrices(p.currencyPairs...)
	p.NoError(err)
	p.Len(data, len(p.addressPairs))

	for i, pair := range p.addressPairs {
		candleData := data[pair.String()]
		minuteData := p.minuteData[i*10 : (i+1)*10]

		for j, candle := range candleData {
			price, err := toSdkDec(minuteData[j].Token1Price)
			p.NoError(err)

			p.EqualValues(candle.Price.String(), price.String())
		}

	}
}

func TestProviderTestSuite(t *testing.T) {
	suite.Run(t, new(ProviderTestSuite))
}
