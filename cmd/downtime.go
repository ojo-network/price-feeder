package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/client"
)

func getDowntimeCmd() *cobra.Command {
	downtimeCmd := &cobra.Command{
		Use:   "downtime [config-file]",
		Args:  cobra.ExactArgs(1),
		Short: "Produces a list of assets which currently have downtime",
		RunE: func(cmd *cobra.Command, args []string) error {
			logLvlStr, err := cmd.Flags().GetString(flagLogLevel)
			if err != nil {
				return err
			}

			logLvl, err := zerolog.ParseLevel(logLvlStr)
			if err != nil {
				return err
			}

			logFormatStr, err := cmd.Flags().GetString(flagLogFormat)
			if err != nil {
				return err
			}

			var logWriter io.Writer
			switch strings.ToLower(logFormatStr) {
			case logLevelJSON:
				logWriter = os.Stderr

			case logLevelText:
				logWriter = zerolog.ConsoleWriter{Out: os.Stderr}

			default:
				return fmt.Errorf("invalid logging format: %s", logFormatStr)
			}

			logger := zerolog.New(logWriter).Level(logLvl).With().Timestamp().Logger()

			cfg, err := config.LoadConfigFromFlags(args[0], "")
			if err != nil {
				return err
			}

			ctx, cancel := context.WithCancel(cmd.Context())

			// listen for and trap any OS signal to gracefully shutdown and exit
			trapSignal(cancel, logger)

			rpcTimeout, err := time.ParseDuration(cfg.RPC.RPCTimeout)
			if err != nil {
				return fmt.Errorf("failed to parse RPC timeout: %w", err)
			}

			// Gather pass via env variable || std input
			keyringPass, err := getKeyringPassword()
			if err != nil {
				return err
			}

			oracleClient, err := client.NewOracleClient(
				ctx,
				logger,
				cfg.Account.ChainID,
				cfg.Keyring.Backend,
				cfg.Keyring.Dir,
				keyringPass,
				cfg.RPC.TMRPCEndpoint,
				rpcTimeout,
				cfg.Account.Address,
				cfg.Account.Validator,
				cfg.RPC.GRPCEndpoint,
				cfg.GasAdjustment,
			)
			if err != nil {
				return err
			}

			providerTimeout, err := time.ParseDuration(cfg.ProviderTimeout)
			if err != nil {
				return fmt.Errorf("failed to parse provider timeout: %w", err)
			}
			deviations, err := cfg.DeviationsMap()
			if err != nil {
				return err
			}
			oracle := oracle.New(
				logger,
				oracleClient,
				cfg.ProviderPairs(),
				providerTimeout,
				deviations,
				cfg.ProviderEndpointsMap(),
			)

			params, err := oracle.GetParams(ctx)
			if err != nil {
				return err
			}
			rates, err := oracle.GetExchangeRates(ctx)
			if err != nil {
				return err
			}
			// Find which assets are missing in rates
			// We cannot compare length, because there may be
			// multiple oracle entries for a single exchange rate.
			existingRates := make(map[string]interface{}, len(rates))
			for _, rate := range rates {
				existingRates[strings.ToUpper(rate.Denom)] = struct{}{}
			}
			var missingRates []string
			for _, asset := range params.AcceptList {
				if _, ok := existingRates[strings.ToUpper(asset.SymbolDenom)]; !ok {
					missingRates = append(missingRates, asset.SymbolDenom)
				}
			}

			if len(missingRates) < 1 {
				fmt.Println("No downtime detected")
				return nil
			}
			fmt.Println("Missing rates for assets: ", missingRates)
			return nil
		},
	}

	return downtimeCmd
}
