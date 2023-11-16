package monitor

import (
	"fmt"
	"strings"
	"time"

	"github.com/ojo-network/price-feeder/config"
	"github.com/slack-go/slack"
)

const fullLogInterval = 24 * time.Hour

type SlackClient struct {
	lastFullLog time.Time
	client      *slack.Client
	config      *config.SlackConfig
}

func NewSlackClient(cfg *config.Config) *SlackClient {
	api := slack.New(cfg.SlackConfig.SlackToken, slack.OptionDebug(true))
	return &SlackClient{
		client: api,
		config: &cfg.SlackConfig,
	}
}

func (sc *SlackClient) Notify(priceErrors []PriceError) {
	fullLog := false
	messages := []string{}
	if sc.lastFullLog.Add(fullLogInterval).Before(time.Now()) {
		sc.lastFullLog = time.Now()
		fullLog = true
	}

	for _, priceError := range priceErrors {
		if fullLog || priceError.ErrorType == ORACLE_MISSING_PRICE {
			messages = append(messages, priceError.Message)
		}
	}

	if len(messages) == 0 {
		return
	}

	message := strings.Join(messages, "\n")

	fmt.Println(message)

	// sc.client.PostMessage(sc.config.SlackChannel, slack.MsgOptionText("test", false))
}
