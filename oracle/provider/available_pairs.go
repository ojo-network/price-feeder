package provider

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog"

	"github.com/ojo-network/price-feeder/oracle/types"
)

// ConfirmPairAvailability takes a list of pairs that are meant to be subscribed
// to, and uses the given provider's GetAvailablePairs method to check that the
// given pairs can be subscribed to. It will return an updated list of pairs that
// can be subsribed to, and send a warning log about any pairs passed in that
// cannot be subsribed to.
func ConfirmPairAvailability(
	p Provider,
	providerName Name,
	logger zerolog.Logger,
	cps ...types.CurrencyPair,
) ([]types.CurrencyPair, error) {
	availablePairs, err := p.GetAvailablePairs()
	if err != nil {
		return nil, err
	}

	// confirm pairs can be subscribed to
	confirmedPairs := []types.CurrencyPair{}
	for _, cp := range cps {
		if _, ok := availablePairs[strings.ToUpper(cp.String())]; !ok {
			logger.Warn().Msg(fmt.Sprintf(
				"%s not an available pair to be subscribed to in %v, %v ignoring pair",
				cp.String(),
				providerName,
				providerName,
			))
			continue
		}
		confirmedPairs = append(confirmedPairs, cp)
	}

	return confirmedPairs, nil
}
