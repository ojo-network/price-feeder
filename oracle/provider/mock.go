package provider

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cosmossdk.io/math"
	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	// Google Sheets document containing mock exchange rates.
	//
	// Ref: https://docs.google.com/spreadsheets/d/1DfVh2Xwxfehcwo08h2sBgaqL-2Jem1ri_prsQ3ayFeE/edit?usp=sharing
	mockBaseURL = "https://docs.google.com/spreadsheets/d/e/2PACX-1vQRVD0IMn8ZdRgmE2XeNkwjpSGglwelx1z0-hNV2ejfstVeuL2xF8i3EISBZfrGTjVTI0EXW9Wwq4F-/pub?output=csv"
)

var _ Provider = (*MockProvider)(nil)

type (
	// MockProvider defines a mocked exchange rate provider using a published
	// Google sheets document to fetch mocked/fake exchange rates.
	MockProvider struct {
		baseURL string
		client  *http.Client
	}
)

func NewMockProvider() *MockProvider {
	return &MockProvider{
		baseURL: mockBaseURL,
		client: &http.Client{
			Timeout: defaultTimeout,
			// the mock provider is the only one which allows redirects
			// because it gets prices from a google spreadsheet, which redirects
		},
	}
}

func (p *MockProvider) StartConnections() {
	// no-op mock does not use websockets
}

// SubscribeCurrencyPairs performs a no-op since mock does not use websockets
func (p MockProvider) SubscribeCurrencyPairs(...types.CurrencyPair) {}

func (p MockProvider) GetTickerPrices(pairs ...types.CurrencyPair) (types.CurrencyPairTickers, error) {
	tickerPrices := make(types.CurrencyPairTickers, len(pairs))

	resp, err := p.client.Get(p.baseURL)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	csvReader := csv.NewReader(resp.Body)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	tickerMap := make(map[types.CurrencyPair]struct{})
	for _, cp := range pairs {
		tickerMap[cp] = struct{}{}
	}

	// Records are of the form [base, quote, price, volume] and we skip the first
	// record as that contains the header.
	for _, r := range records[1:] {
		currencyPair := types.CurrencyPair{Base: r[0], Quote: r[1]}

		if _, ok := tickerMap[currencyPair]; !ok {
			// skip records that are not requested
			continue
		}

		price, err := math.LegacyNewDecFromStr(r[2])
		if err != nil {
			return nil, fmt.Errorf("failed to read mock price (%s) for %s", r[2], currencyPair)
		}

		volume, err := math.LegacyNewDecFromStr(r[3])
		if err != nil {
			return nil, fmt.Errorf("failed to read mock volume (%s) for %s", r[3], currencyPair)
		}

		if _, ok := tickerPrices[currencyPair]; ok {
			return nil, fmt.Errorf("found duplicate ticker: %s", currencyPair)
		}

		tickerPrices[currencyPair] = types.TickerPrice{Price: price, Volume: volume}
	}

	for t := range tickerMap {
		if _, ok := tickerPrices[t]; !ok {
			return nil, fmt.Errorf(types.ErrMissingExchangeRate.Error(), t)
		}
	}

	return tickerPrices, nil
}

func (p MockProvider) GetCandlePrices(pairs ...types.CurrencyPair) (types.CurrencyPairCandles, error) {
	price, err := p.GetTickerPrices(pairs...)
	if err != nil {
		return nil, err
	}
	candles := make(types.CurrencyPairCandles)
	for pair, price := range price {
		candles[pair] = []types.CandlePrice{
			{
				Price:     price.Price,
				Volume:    price.Volume,
				TimeStamp: PastUnixTime(1 * time.Minute),
			},
		}
	}
	return candles, nil
}

// GetAvailablePairs return all available pairs symbol to susbscribe.
func (p MockProvider) GetAvailablePairs() (map[string]struct{}, error) {
	resp, err := http.Get(p.baseURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	csvReader := csv.NewReader(resp.Body)
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, err
	}

	// Records are of the form [base, quote, price, volume] and we skip the first
	// record as that contains the header.
	availablePairs := make(map[string]struct{}, len(records[1:]))
	for _, r := range records[1:] {
		if len(r) < 2 {
			continue
		}

		cp := types.CurrencyPair{
			Base:  strings.ToUpper(r[0]),
			Quote: strings.ToUpper(r[1]),
		}
		availablePairs[strings.ToUpper(cp.String())] = struct{}{}
	}

	return availablePairs, nil
}
