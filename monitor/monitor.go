package monitor

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ojo-network/price-feeder/config"
	"github.com/ojo-network/price-feeder/oracle"
	"github.com/ojo-network/price-feeder/oracle/client"
	"github.com/rs/zerolog"
)

func Start() {
	logger := zerolog.New(os.Stderr).Level(zerolog.ErrorLevel).With().Timestamp().Logger()

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel the context on user interrupt
	userInterrupt := make(chan os.Signal, 1)
	signal.Notify(userInterrupt, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-userInterrupt
		logger.Info().Msg("user interrupt")
		cancel()
	}()

	cfg, err := config.LoadConfigFromFlags(config.SampleNodeConfigPath, "")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to load config")
	}

	providerTimeout, err := time.ParseDuration(cfg.ProviderTimeout)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse provider timeout")
	}

	deviations, err := cfg.DeviationsMap()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse deviations")
	}

	oracle := oracle.New(
		logger,
		client.OracleClient{},
		cfg.ProviderPairs(),
		providerTimeout,
		deviations,
		cfg.ProviderEndpointsMap(),
		false,
	)
	oracle.SetPrices(ctx)

	slackClient := NewSlackClient(&cfg)

	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
			oracle.SetPrices(ctx)
			priceErrors := VerifyPrices(&cfg, oracle)
			slackClient.Notify(priceErrors)
		}
	}
}
