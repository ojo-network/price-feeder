package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func getCoinMarketCapPrices(symbols []string, apiURL string, apiKey string) (map[string]float64, error) {
	if apiKey == "$COINMARKETCAP_API_KEY" {
		return nil, fmt.Errorf("coinmarketcap.com API key not set")
	}

	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/cryptocurrency/quotes/latest", apiURL), nil)
	if err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Add("symbol", strings.Join(symbols, ","))

	req.Header.Set("Accepts", "application/json")
	req.Header.Add("X-CMC_PRO_API_KEY", apiKey)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	fmt.Println(resp.Status)
	var target map[string]interface{}
	respBody, _ := io.ReadAll(resp.Body)
	err = json.Unmarshal(respBody, &target)
	if err != nil {
		return nil, err
	}
	data := target["data"].(map[string]interface{})

	prices := make(map[string]float64, len(symbols))

	for _, symbol := range symbols {
		tokenData, ok := data[symbol].(map[string]interface{})
		if !ok {
			fmt.Printf("coinmarketcap.com token data not found for %s\n", symbol)
			continue
		}
		tokenQuote := tokenData["quote"].(map[string]interface{})
		tokenQuoteUSD := tokenQuote["USD"].(map[string]interface{})
		price := tokenQuoteUSD["price"].(float64)
		prices[symbol] = price
	}

	return prices, nil
}
