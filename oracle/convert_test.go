package oracle_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/types"
)

func TestConvertRatesToUSD(t *testing.T) {
	rates := types.CurrencyPairDec{
		types.CurrencyPair{Base: "ATOM", Quote: "USD"}:  sdk.NewDec(10),
		types.CurrencyPair{Base: "OSMO", Quote: "ATOM"}: sdk.NewDec(3),
		types.CurrencyPair{Base: "JUNO", Quote: "ATOM"}: sdk.NewDec(20),
		types.CurrencyPair{Base: "LTC", Quote: "USDT"}:  sdk.NewDec(20),
	}

	expected := types.CurrencyPairDec{
		types.CurrencyPair{Base: "ATOM", Quote: "USD"}: sdk.NewDec(10),
		types.CurrencyPair{Base: "OSMO", Quote: "USD"}: sdk.NewDec(30),
		types.CurrencyPair{Base: "JUNO", Quote: "USD"}: sdk.NewDec(200),
	}

	convertedRates := oracle.ConvertRatesToUSD(rates)

	if len(convertedRates) != len(expected) {
		t.Errorf("Unexpected length of converted rates. Expected: %d, Got: %d", len(expected), len(convertedRates))
	}

	for cp, expectedRate := range expected {
		convertedRate, ok := convertedRates[cp]
		if !ok {
			t.Errorf("Missing converted rate for currency pair: %v", cp)
		}

		if !convertedRate.Equal(expectedRate) {
			t.Errorf("Unexpected converted rate for currency pair: %v. Expected: %s, Got: %s", cp, expectedRate.String(), convertedRate.String())
		}
	}
}
