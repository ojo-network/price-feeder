package types

import "encoding/json"

// CurrencyPair defines a currency exchange pair consisting of a base and a quote.
// We primarily utilize the base for broadcasting exchange rates and use the
// pair for querying for the ticker prices.
type CurrencyPair struct {
	Base  string
	Quote string
}

// String implements the Stringer interface and defines a ticker symbol for
// querying the exchange rate.
func (cp CurrencyPair) String() string {
	return cp.Base + cp.Quote
}

// MapPairsToSlice returns the map of currency pairs as slice.
func MapPairsToSlice(mapPairs map[string]CurrencyPair) []CurrencyPair {
	currencyPairs := make([]CurrencyPair, len(mapPairs))

	iterator := 0
	for _, cp := range mapPairs {
		currencyPairs[iterator] = cp
		iterator++
	}

	return currencyPairs
}

func (cp CurrencyPair) MarshalText() (text []byte, err error) {
	type noMethod CurrencyPair
	return json.Marshal(noMethod(cp))
}

func (cp *CurrencyPair) UnmarshalText(text []byte) error {
	type noMethod CurrencyPair
	return json.Unmarshal(text, (*noMethod)(cp))
}
