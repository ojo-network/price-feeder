package config

import (
	"errors"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/go-playground/validator/v10"

	"github.com/ojo-network/price-feeder/oracle/provider"
	"github.com/ojo-network/price-feeder/oracle/types"
)

const (
	DenomUSD = "USD"

	defaultListenAddr      = "0.0.0.0:7171"
	defaultSrvWriteTimeout = 15 * time.Second
	defaultSrvReadTimeout  = 15 * time.Second
	defaultProviderTimeout = 100 * time.Millisecond

	SampleNodeConfigPath = "price-feeder.example.toml"
)

var (
	validate = validator.New()

	// ErrEmptyConfigPath defines a sentinel error for an empty config path.
	ErrEmptyConfigPath = errors.New("empty configuration file path")

	// maxDeviationThreshold is the maxmimum allowed amount of standard
	// deviations which validators are able to set for a given asset.
	maxDeviationThreshold = sdk.MustNewDecFromStr("3.0")
)

type (
	// Config defines all necessary price-feeder configuration parameters.
	Config struct {
		ConfigDir           string              `mapstructure:"config_dir"`
		Server              Server              `mapstructure:"server"`
		CurrencyPairs       []CurrencyPair      `mapstructure:"currency_pairs" validate:"required,gt=0,dive,required"`
		Deviations          []Deviation         `mapstructure:"deviation_thresholds"`
		Account             Account             `mapstructure:"account" validate:"required,gt=0,dive,required"`
		Keyring             Keyring             `mapstructure:"keyring" validate:"required,gt=0,dive,required"`
		RPC                 RPC                 `mapstructure:"rpc" validate:"required,gt=0,dive,required"`
		Telemetry           telemetry.Config    `mapstructure:"telemetry"`
		GasAdjustment       float64             `mapstructure:"gas_adjustment" validate:"required"`
		ProviderTimeout     string              `mapstructure:"provider_timeout"`
		ProviderMinOverride bool                `mapstructure:"provider_min_override"`
		ProviderEndpoints   []provider.Endpoint `mapstructure:"provider_endpoints" validate:"dive"`
	}

	// Server defines the API server configuration.
	Server struct {
		ListenAddr     string   `mapstructure:"listen_addr"`
		WriteTimeout   string   `mapstructure:"write_timeout"`
		ReadTimeout    string   `mapstructure:"read_timeout"`
		VerboseCORS    bool     `mapstructure:"verbose_cors"`
		AllowedOrigins []string `mapstructure:"allowed_origins"`
	}

	// CurrencyPair defines a price quote of the exchange rate for two different
	// currencies and the supported providers for getting the exchange rate.
	CurrencyPair struct {
		Base        string                `mapstructure:"base" validate:"required"`
		Quote       string                `mapstructure:"quote" validate:"required"`
		PairAddress []PairAddressProvider `mapstructure:"pair_address_providers" validate:"dive"`
		Providers   []types.ProviderName  `mapstructure:"providers" validate:"required,gt=0,dive,required"`
	}

	PairAddressProvider struct {
		Address  string             `mapstructure:"address" validate:"required"`
		Provider types.ProviderName `mapstructure:"provider" validate:"required"`
	}

	// Deviation defines a maximum amount of standard deviations that a given asset can
	// be from the median without being filtered out before voting.
	Deviation struct {
		Base      string `mapstructure:"base" validate:"required"`
		Threshold string `mapstructure:"threshold" validate:"required"`
	}

	// Account defines account related configuration that is related to the Ojo
	// network and transaction signing functionality.
	Account struct {
		ChainID   string `mapstructure:"chain_id" validate:"required"`
		Address   string `mapstructure:"address" validate:"required"`
		Validator string `mapstructure:"validator" validate:"required"`
	}

	// Keyring defines the required Ojo keyring configuration.
	Keyring struct {
		Backend string `mapstructure:"backend" validate:"required"`
		Dir     string `mapstructure:"dir" validate:"required"`
	}

	// RPC defines RPC configuration of both the Ojo gRPC and Tendermint nodes.
	RPC struct {
		TMRPCEndpoint string `mapstructure:"tmrpc_endpoint" validate:"required"`
		GRPCEndpoint  string `mapstructure:"grpc_endpoint" validate:"required"`
		RPCTimeout    string `mapstructure:"rpc_timeout" validate:"required"`
	}
)

// telemetryValidation is custom validation for the Telemetry struct.
func telemetryValidation(sl validator.StructLevel) {
	tel := sl.Current().Interface().(telemetry.Config)

	if tel.Enabled && (len(tel.GlobalLabels) == 0 || len(tel.ServiceName) == 0) {
		sl.ReportError(tel.Enabled, "enabled", "Enabled", "enabledNoOptions", "")
	}
}

// endpointValidation is custom validation for the ProviderEndpoint struct.
func endpointValidation(sl validator.StructLevel) {
	endpoint := sl.Current().Interface().(provider.Endpoint)

	if len(endpoint.Name) < 1 || len(endpoint.Rest) < 1 || len(endpoint.Websocket) < 1 {
		sl.ReportError(endpoint, "endpoint", "Endpoint", "unsupportedEndpointType", "")
	}
	if _, ok := SupportedProviders[endpoint.Name]; !ok {
		sl.ReportError(endpoint.Name, "name", "Name", "unsupportedEndpointProvider", "")
	}
}

// hasAPIKey searches through the provided endpoints to return whether or not
// a given endpoint was supplied with an API Key.
func hasAPIKey(endpointName types.ProviderName, endpoints []provider.Endpoint) bool {
	for _, endpoint := range endpoints {
		if endpoint.Name == endpointName && endpoint.APIKey != "" {
			return true
		}
	}
	return false
}

// Validate returns an error if the Config object is invalid.
func (c Config) Validate() (err error) {
	if err = c.validateCurrencyPairs(); err != nil {
		return err
	}

	if err = c.validateDeviations(); err != nil {
		return err
	}

	validate.RegisterStructValidation(telemetryValidation, telemetry.Config{})
	validate.RegisterStructValidation(endpointValidation, provider.Endpoint{})
	return validate.Struct(c)
}

func (c Config) validateDeviations() error {
	for _, deviation := range c.Deviations {
		threshold, err := sdk.NewDecFromStr(deviation.Threshold)
		if err != nil {
			return fmt.Errorf("deviation thresholds must be numeric: %w", err)
		}

		if threshold.GT(maxDeviationThreshold) {
			return fmt.Errorf("deviation thresholds must not exceed 3.0")
		}
	}
	return nil
}

func (c Config) validateCurrencyPairs() error {
OUTER:
	for _, cp := range c.CurrencyPairs {
		if cp.Base == "" {
			return fmt.Errorf("currency pair base cannot be empty")
		}
		if cp.Quote == "" {
			return fmt.Errorf("currency pair quote cannot be empty")
		}
		if cp.Base == cp.Quote {
			return fmt.Errorf("currency pair base and quote cannot be the same")
		}
		if len(cp.Providers) == 0 {
			return fmt.Errorf("currency pair must have at least one provider")
		}
		for _, prov := range cp.Providers {
			if _, ok := SupportedProviders[prov]; !ok {
				return fmt.Errorf("unsupported provider: %s", prov)
			}
			if bool(SupportedProviders[prov]) && !hasAPIKey(prov, c.ProviderEndpoints) {
				return fmt.Errorf("provider %s requires an API Key", prov)
			}
		}
		if cp.Quote == DenomUSD {
			continue
		}
		// verify a conversion pair exists for the quote currency
		for _, conversionPair := range SupportedConversionSlice() {
			if cp.Quote == conversionPair.Base {
				continue OUTER
			}
		}
		return fmt.Errorf("currency pair quote %s is not supported", cp.Quote)
	}
	return nil
}

func (c *Config) setDefaults() {
	if c.Server.ListenAddr == "" {
		c.Server.ListenAddr = defaultListenAddr
	}
	if c.Server.WriteTimeout == "" {
		c.Server.WriteTimeout = defaultSrvWriteTimeout.String()
	}
	if c.Server.ReadTimeout == "" {
		c.Server.ReadTimeout = defaultSrvReadTimeout.String()
	}
	if c.ProviderTimeout == "" {
		c.ProviderTimeout = defaultProviderTimeout.String()
	}
}

// ProviderPairs returns a map of provider.CurrencyPair where the key is the
// provider name.
func (c Config) ProviderPairs() map[types.ProviderName][]types.CurrencyPair {
	providerPairs := make(map[types.ProviderName][]types.CurrencyPair)

	for _, pair := range c.CurrencyPairs {
		for _, provider := range pair.Providers {
			if len(pair.PairAddress) > 0 {
				for _, uniPair := range pair.PairAddress {
					if (uniPair.Provider == provider) && (uniPair.Address != "") {
						providerPairs[uniPair.Provider] = append(providerPairs[uniPair.Provider], types.CurrencyPair{
							Base:    pair.Base,
							Quote:   pair.Quote,
							Address: uniPair.Address,
						})
					}
				}
			} else {
				providerPairs[provider] = append(providerPairs[provider], types.CurrencyPair{
					Base:  pair.Base,
					Quote: pair.Quote,
				})
			}
		}
	}

	return providerPairs
}

// ProviderEndpointsMap converts the provider_endpoints from the config
// file into a map of provider.Endpoint where the key is the provider name.
func (c Config) ProviderEndpointsMap() map[types.ProviderName]provider.Endpoint {
	endpoints := make(map[types.ProviderName]provider.Endpoint, len(c.ProviderEndpoints))
	for _, endpoint := range c.ProviderEndpoints {
		endpoints[endpoint.Name] = endpoint
	}
	return endpoints
}

// DeviationsMap converts the deviation_thresholds from the config file into
// a map of sdk.Dec where the key is the base asset.
func (c Config) DeviationsMap() (map[string]sdk.Dec, error) {
	deviations := make(map[string]sdk.Dec, len(c.Deviations))
	for _, deviation := range c.Deviations {
		threshold, err := sdk.NewDecFromStr(deviation.Threshold)
		if err != nil {
			return nil, err
		}
		deviations[deviation.Base] = threshold
	}
	return deviations, nil
}

// ExpectedSymbols returns a slice of all unique base symbols from the config object.
func (c Config) ExpectedSymbols() []string {
	bases := make(map[string]interface{}, len(c.CurrencyPairs))
	for _, pair := range c.CurrencyPairs {
		bases[pair.Base] = struct{}{}
	}
	expectedSymbols := make([]string, 0, len(bases))
	for b := range bases {
		expectedSymbols = append(expectedSymbols, b)
	}
	return expectedSymbols
}
