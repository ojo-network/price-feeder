package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ojo-network/price-feeder/oracle/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// TestGateLiveWebsocket connects to Gate's live websocket and listens for real data
func TestGateLiveWebsocket(t *testing.T) {
	// Skip in CI/CD environments to avoid rate limiting
	if testing.Short() {
		t.Skip("Skipping live websocket test in short mode")
	}

	// Test configuration
	testPairs := []types.CurrencyPair{
		{Base: "BTC", Quote: "USDT"},
		{Base: "ETH", Quote: "USDT"},
		{Base: "ATOM", Quote: "USDT"},
	}

	// Create logger
	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	// Create Gate provider
	provider, err := NewGateProvider(
		context.Background(),
		logger,
		Endpoint{},
		testPairs...,
	)
	require.NoError(t, err)

	// Start connections
	provider.StartConnections()

	// Wait for initial connection
	time.Sleep(2 * time.Second)

	// Test data collection for 30 seconds
	testDuration := 30 * time.Second
	startTime := time.Now()
	tickerCount := 0
	candleCount := 0

	t.Logf("Starting live websocket test for %v", testDuration)
	t.Logf("Subscribing to pairs: %v", testPairs)

	// Collect data for the test duration
	for time.Since(startTime) < testDuration {
		// Get ticker prices
		tickers, err := provider.GetTickerPrices(testPairs...)
		if err == nil && len(tickers) > 0 {
			tickerCount++
			for pair, ticker := range tickers {
				t.Logf("Ticker [%d] %s: Price=%s, Volume=%s",
					tickerCount, pair.String(), ticker.Price, ticker.Volume)
			}
		}

		// Get candle prices
		candles, err := provider.GetCandlePrices(testPairs...)
		if err == nil && len(candles) > 0 {
			candleCount++
			for pair, candleList := range candles {
				if len(candleList) > 0 {
					latestCandle := candleList[len(candleList)-1]
					t.Logf("Candle [%d] %s: Price=%s, Volume=%s, Time=%d",
						candleCount, pair.String(), latestCandle.Price, latestCandle.Volume, latestCandle.TimeStamp)
				}
			}
		}

		time.Sleep(5 * time.Second)
	}

	t.Logf("Test completed. Received %d ticker updates and %d candle updates", tickerCount, candleCount)
	require.Greater(t, tickerCount, 0, "Should receive at least some ticker updates")
	require.Greater(t, candleCount, 0, "Should receive at least some candle updates")
}

// TestGateDirectWebsocket tests direct websocket connection without the provider abstraction
func TestGateDirectWebsocket(t *testing.T) {
	// Skip in CI/CD environments
	if testing.Short() {
		t.Skip("Skipping direct websocket test in short mode")
	}

	// Connect directly to Gate websocket
	wsURL := url.URL{
		Scheme: "wss",
		Host:   "ws.gate.io",
		Path:   "/v4",
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL.String(), nil)
	require.NoError(t, err)
	defer conn.Close()

	// Subscribe to BTC/USDT ticker and candle data
	subscriptionMsgs := []interface{}{
		GateTickerSubscriptionMsg{
			Method: "ticker.subscribe",
			Params: []string{"BTC_USDT"},
			ID:     1,
		},
		GateCandleSubscriptionMsg{
			Method: "kline.subscribe",
			Params: []interface{}{"BTC_USDT", 60},
			ID:     2,
		},
	}

	// Send subscription messages
	for _, msg := range subscriptionMsgs {
		err := conn.WriteJSON(msg)
		require.NoError(t, err)
		t.Logf("Sent subscription: %+v", msg)
	}

	// Listen for messages
	testDuration := 20 * time.Second
	startTime := time.Now()
	messageCount := 0

	t.Logf("Listening for messages for %v", testDuration)

	for time.Since(startTime) < testDuration {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err) {
				t.Logf("Websocket closed: %v", err)
				break
			}
			continue
		}

		messageCount++
		t.Logf("Message [%d]: %s", messageCount, string(message))

		// Try to parse as different message types
		if err := parseGateMessage(message); err != nil {
			t.Logf("Failed to parse message: %v", err)
		}
	}

	t.Logf("Received %d messages", messageCount)
	require.Greater(t, messageCount, 0, "Should receive at least some messages")
}

// parseGateMessage attempts to parse Gate websocket messages
func parseGateMessage(message []byte) error {
	// Try to parse as subscription event
	var event GateEvent
	if err := json.Unmarshal(message, &event); err == nil {
		if event.Result.Status == "success" {
			fmt.Printf("Subscription successful: ID=%d\n", event.ID)
			return nil
		}
	}

	// Try to parse as ticker update
	var tickerResp GateTickerResponse
	if err := json.Unmarshal(message, &tickerResp); err == nil {
		if tickerResp.Method == "ticker.update" && len(tickerResp.Params) >= 2 {
			symbol, _ := tickerResp.Params[0].(string)
			tickerData, _ := json.Marshal(tickerResp.Params[1])

			var ticker GateTicker
			if err := json.Unmarshal(tickerData, &ticker); err == nil {
				ticker.Symbol = symbol
				fmt.Printf("Ticker Update: %s - Price: %s, Volume: %s\n",
					ticker.Symbol, ticker.Last, ticker.Vol)
				return nil
			}
		}
	}

	// Try to parse as candle update
	var candleResp GateCandleResponse
	if err := json.Unmarshal(message, &candleResp); err == nil {
		if candleResp.Method == "kline.update" && len(candleResp.Params) > 0 {
			var candle GateCandle
			if err := candle.UnmarshalParams(candleResp.Params); err == nil {
				fmt.Printf("Candle Update: %s - Close: %s, Volume: %s, Time: %d\n",
					candle.Symbol, candle.Close, candle.Volume, candle.TimeStamp)
				return nil
			}
		}
	}

	return fmt.Errorf("unknown message format")
}

// TestGateAvailablePairs tests fetching available pairs from Gate API
func TestGateAvailablePairs(t *testing.T) {
	// Skip in CI/CD environments
	if testing.Short() {
		t.Skip("Skipping available pairs test in short mode")
	}

	provider := &GateProvider{
		endpoints: Endpoint{},
	}

	pairs, err := provider.GetAvailablePairs()
	require.NoError(t, err)
	require.NotEmpty(t, pairs)

	// Check for common pairs
	commonPairs := []string{"BTCUSDT", "ETHUSDT", "ATOMUSDT"}
	for _, pair := range commonPairs {
		if _, exists := pairs[pair]; exists {
			t.Logf("Found common pair: %s", pair)
		}
	}

	t.Logf("Total available pairs: %d", len(pairs))
}

// TestGateProviderIntegration tests the full provider integration
func TestGateProviderIntegration(t *testing.T) {
	// Skip in CI/CD environments
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()

	// Test with multiple pairs
	testPairs := []types.CurrencyPair{
		{Base: "BTC", Quote: "USDT"},
		{Base: "ETH", Quote: "USDT"},
	}

	provider, err := NewGateProvider(
		context.Background(),
		logger,
		Endpoint{},
		testPairs...,
	)
	require.NoError(t, err)

	// Start connections
	provider.StartConnections()

	// Wait for initial data
	time.Sleep(5 * time.Second)

	// Test ticker prices
	tickers, err := provider.GetTickerPrices(testPairs...)
	require.NoError(t, err)

	for pair, ticker := range tickers {
		t.Logf("Ticker for %s: Price=%s, Volume=%s",
			pair.String(), ticker.Price, ticker.Volume)
		require.NotEmpty(t, ticker.Price, "Price should not be empty")
		require.NotEmpty(t, ticker.Volume, "Volume should not be empty")
	}

	// Test candle prices
	candles, err := provider.GetCandlePrices(testPairs...)
	require.NoError(t, err)

	for pair, candleList := range candles {
		if len(candleList) > 0 {
			latestCandle := candleList[len(candleList)-1]
			t.Logf("Latest candle for %s: Price=%s, Volume=%s, Time=%d",
				pair.String(), latestCandle.Price, latestCandle.Volume, latestCandle.TimeStamp)
			require.NotEmpty(t, latestCandle.Price, "Price should not be empty")
			require.NotEmpty(t, latestCandle.Volume, "Volume should not be empty")
		}
	}

	// Test subscribing to additional pairs
	additionalPairs := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
	}

	provider.SubscribeCurrencyPairs(additionalPairs...)
	time.Sleep(3 * time.Second)

	// Test getting prices for all pairs
	allPairs := append(testPairs, additionalPairs...)
	allTickers, err := provider.GetTickerPrices(allPairs...)
	require.NoError(t, err)

	t.Logf("Total tickers available: %d", len(allTickers))
	require.GreaterOrEqual(t, len(allTickers), len(testPairs),
		"Should have at least the original pairs")
}

// TestGateAllGatePairsIntegration tests all pairs with 'gate' as a provider
func TestGateAllGatePairsIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping all-gate-pairs integration test in short mode")
	}

	// List of all pairs with 'gate' as a provider (from user input)
	gatePairs := []types.CurrencyPair{
		{Base: "ATOM", Quote: "USDT"},
		{Base: "AKT", Quote: "USDT"},
		{Base: "TIA", Quote: "USDT"},
		{Base: "KAVA", Quote: "USDT"},
		{Base: "SAGA", Quote: "USDT"},
		{Base: "XION", Quote: "USDT"},
		{Base: "SCRT", Quote: "USDT"},
		{Base: "OSMO", Quote: "USDT"},
		{Base: "NTRN", Quote: "USDT"},
		{Base: "OM", Quote: "USDT"},
		{Base: "BLD", Quote: "USDT"},
		{Base: "BTC", Quote: "USDT"},
		{Base: "ETH", Quote: "USDT"},
		{Base: "PAXG", Quote: "USDT"},
		{Base: "BABY", Quote: "USDT"},
		{Base: "FET", Quote: "USDT"},
		{Base: "INJ", Quote: "USDT"},
		{Base: "XRP", Quote: "USDT"},
		{Base: "LINK", Quote: "USDT"},
		{Base: "ONDO", Quote: "USDT"},
	}

	logger := zerolog.New(zerolog.NewConsoleWriter()).With().Timestamp().Logger()
	provider, err := NewGateProvider(
		context.Background(),
		logger,
		Endpoint{},
		gatePairs...,
	)
	require.NoError(t, err)

	provider.StartConnections()
	// Wait for initial data
	time.Sleep(5 * time.Second)

	// Check available pairs on Gate
	available, err := provider.GetAvailablePairs()
	require.NoError(t, err)

	missing := 0
	for _, cp := range gatePairs {
		gateKey := cp.Base + cp.Quote
		if _, ok := available[gateKey]; !ok {
			t.Logf("[WARN] Pair not available on Gate: %s/%s", cp.Base, cp.Quote)
			missing++
		}
	}
	if missing > 0 {
		t.Logf("%d pairs not available on Gate, continuing with available pairs", missing)
	}

	// Only test pairs that are available
	var testPairs []types.CurrencyPair
	for _, cp := range gatePairs {
		gateKey := cp.Base + cp.Quote
		if _, ok := available[gateKey]; ok {
			testPairs = append(testPairs, cp)
		}
	}

	t.Logf("Testing %d pairs with Gate", len(testPairs))
	require.NotEmpty(t, testPairs, "No gate pairs available for testing")

	// Wait a bit more for data to accumulate
	time.Sleep(10 * time.Second)

	// Test ticker prices
	tickers, err := provider.GetTickerPrices(testPairs...)
	require.NoError(t, err)
	for _, cp := range testPairs {
		ticker, ok := tickers[cp]
		if !ok {
			t.Errorf("No ticker for %s/%s", cp.Base, cp.Quote)
			continue
		}
		t.Logf("Ticker for %s/%s: Price=%s, Volume=%s", cp.Base, cp.Quote, ticker.Price, ticker.Volume)
		require.NotEmpty(t, ticker.Price, "Price should not be empty for %s/%s", cp.Base, cp.Quote)
		require.NotEmpty(t, ticker.Volume, "Volume should not be empty for %s/%s", cp.Base, cp.Quote)
	}

	// Test candle prices
	candles, err := provider.GetCandlePrices(testPairs...)
	require.NoError(t, err)
	for _, cp := range testPairs {
		candleList, ok := candles[cp]
		if !ok || len(candleList) == 0 {
			t.Errorf("No candle for %s/%s", cp.Base, cp.Quote)
			continue
		}
		latest := candleList[len(candleList)-1]
		t.Logf("Candle for %s/%s: Price=%s, Volume=%s, Time=%d", cp.Base, cp.Quote, latest.Price, latest.Volume, latest.TimeStamp)
		require.NotEmpty(t, latest.Price, "Candle price should not be empty for %s/%s", cp.Base, cp.Quote)
		require.NotEmpty(t, latest.Volume, "Candle volume should not be empty for %s/%s", cp.Base, cp.Quote)
	}
}
