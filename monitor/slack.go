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
	config      *config.MonitorConfig

	ongoingIncidents map[string]PriceError
}

func NewSlackClient(cfg *config.Config) *SlackClient {
	api := slack.New(cfg.MonitorConfig.SlackToken, slack.OptionDebug(true))
	return &SlackClient{
		client:           api,
		config:           &cfg.MonitorConfig,
		ongoingIncidents: make(map[string]PriceError),
	}
}

func (sc *SlackClient) Notify(priceErrors []PriceError) {
	messages := []string{}

	if sc.lastFullLog.Add(fullLogInterval).Before(time.Now()) {
		sc.lastFullLog = time.Now()
		messages = append(messages, "Daily Full Log:")
		for _, priceError := range priceErrors {
			messages = append(messages, priceError.Message)
		}
		sc.SendMessages(messages)
		return
	}

	// create a map of only the critical errors
	currentErrors := make(map[string]PriceError)
	for _, priceError := range priceErrors {
		if _, ok := criticalErrorTypes[priceError.ErrorType]; ok {
			currentErrors[priceError.Key()] = priceError
		}
	}

	// remove and resolve incidents in the ongoing list that are not in the current list
	for key, priceError := range sc.ongoingIncidents {
		if _, ok := currentErrors[key]; !ok {
			delete(sc.ongoingIncidents, key)
			messages = append(messages, fmt.Sprintf("RESOLVED: %s", priceError.Message))
		}
	}

	for key, priceError := range currentErrors {
		if _, ok := sc.ongoingIncidents[key]; !ok {
			messages = append(messages, fmt.Sprintf("ONGOING: %s", priceError.Message))
			sc.ongoingIncidents[key] = priceError
		}
	}

	if len(messages) == 0 {
		fmt.Println("no new errors to report")
		return
	}

	sc.SendMessages(messages)
}

func (sc *SlackClient) SendMessages(messages []string) {
	message := strings.Join(messages, "\n")
	fmt.Println(message)
	sc.client.PostMessage(sc.config.SlackChannel, slack.MsgOptionText(message, false))
}
