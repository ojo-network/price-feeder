package v1_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/gorilla/mux"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/telemetry"
	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
	v1 "github.com/ojo-network/price-feeder/router/v1"
)

var (
	_ v1.Oracle = (*mockOracle)(nil)

	ATOMUSD = types.CurrencyPair{Base: "ATOM", Quote: "USD"}
	OJOUSD  = types.CurrencyPair{Base: "OJO", Quote: "USD"}
	FOOUSD  = types.CurrencyPair{Base: "FOO", Quote: "USD"}

	mockPrices = types.CurrencyPairDec{
		ATOMUSD: math.LegacyMustNewDecFromStr("34.84"),
		OJOUSD:  math.LegacyMustNewDecFromStr("4.21"),
	}

	mockComputedPrices = types.CurrencyPairDecByProvider{
		provider.ProviderBinance: {
			ATOMUSD: math.LegacyMustNewDecFromStr("28.21000000"),
			OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
		},
		provider.ProviderKraken: {
			ATOMUSD: math.LegacyMustNewDecFromStr("28.268700"),
			OJOUSD:  math.LegacyMustNewDecFromStr("1.13000000"),
		},
	}
)

type mockOracle struct{}

func (m mockOracle) GetLastPriceSyncTimestamp() time.Time {
	return time.Now()
}

func (m mockOracle) GetPrices() types.CurrencyPairDec {
	return mockPrices
}

func (m mockOracle) GetTvwapPrices() types.CurrencyPairDecByProvider {
	return mockComputedPrices
}

func (m mockOracle) GetVwapPrices() types.CurrencyPairDecByProvider {
	return mockComputedPrices
}

type mockMetrics struct{}

func (mockMetrics) Gather(format string) (telemetry.GatherResponse, error) {
	return telemetry.GatherResponse{}, nil
}

type RouterTestSuite struct {
	suite.Suite

	mux    *mux.Router
	router *v1.Router
}

// SetupSuite executes once before the suite's tests are executed.
func (rts *RouterTestSuite) SetupSuite() {
	mux := mux.NewRouter()
	cfg := config.Config{
		Server: config.Server{
			AllowedOrigins: []string{},
			VerboseCORS:    false,
		},
	}

	r := v1.New(zerolog.Nop(), cfg, mockOracle{}, mockMetrics{})
	r.RegisterRoutes(mux, v1.APIPathPrefix)

	rts.mux = mux
	rts.router = r
}

func TestServiceTestSuite(t *testing.T) {
	suite.Run(t, new(RouterTestSuite))
}

func (rts *RouterTestSuite) executeRequest(req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	rts.mux.ServeHTTP(rr, req)

	return rr
}

func (rts *RouterTestSuite) TestHealthz() {
	req, err := http.NewRequest("GET", "/api/v1/healthz", nil)
	rts.Require().NoError(err)

	response := rts.executeRequest(req)
	rts.Require().Equal(http.StatusOK, response.Code)

	var respBody map[string]interface{}
	rts.Require().NoError(json.Unmarshal(response.Body.Bytes(), &respBody))
	rts.Require().Equal(respBody["status"], v1.StatusAvailable)
}

func (rts *RouterTestSuite) TestPrices() {
	req, err := http.NewRequest("GET", "/api/v1/prices", nil)
	rts.Require().NoError(err)

	response := rts.executeRequest(req)
	rts.Require().Equal(http.StatusOK, response.Code)

	var respBody v1.PricesResponse
	rts.Require().NoError(json.Unmarshal(response.Body.Bytes(), &respBody))
	rts.Require().Equal(respBody.Prices[ATOMUSD], mockPrices[ATOMUSD])
	rts.Require().Equal(respBody.Prices[OJOUSD], mockPrices[OJOUSD])
	rts.Require().Equal(respBody.Prices[FOOUSD], math.LegacyDec{})
}

func (rts *RouterTestSuite) TestTvwap() {
	req, err := http.NewRequest("GET", "/api/v1/prices/providers/tvwap", nil)
	rts.Require().NoError(err)
	response := rts.executeRequest(req)
	rts.Require().Equal(http.StatusOK, response.Code)

	var respBody v1.PricesPerProviderResponse
	rts.Require().NoError(json.Unmarshal(response.Body.Bytes(), &respBody))
	rts.Require().Equal(
		respBody.Prices[provider.ProviderBinance][ATOMUSD],
		mockComputedPrices[provider.ProviderBinance][ATOMUSD],
	)
}

func (rts *RouterTestSuite) TestVwap() {
	req, err := http.NewRequest("GET", "/api/v1/prices/providers/vwap", nil)
	rts.Require().NoError(err)
	response := rts.executeRequest(req)
	rts.Require().Equal(http.StatusOK, response.Code)

	var respBody v1.PricesPerProviderResponse
	rts.Require().NoError(json.Unmarshal(response.Body.Bytes(), &respBody))
	rts.Require().Equal(
		respBody.Prices[provider.ProviderBinance][ATOMUSD],
		mockComputedPrices[provider.ProviderBinance][ATOMUSD],
	)
}
