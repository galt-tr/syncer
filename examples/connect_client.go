package main

import (
	"github.com/BuxOrg/bux-agent/client"
	"github.com/BuxOrg/bux/utils"
)

func main() {
	handler := client.EventHandler{}
	opts := client.SubscriptionOptions{
		Handler: &handler,
		From:    "00000000000000000002f130b3acfffb75bcd53d60c74f09e05ca80b0bdab5e0",
		Filters: []string{
			"76a91481c80d970b24fb03362be1c65145544892cebe5688ac",
			"76a914522cf9e7626d9bd8729e5a1398ece40dad1b6a2f88ac",
		},
		FilterRegex: utils.P2PKHRegexpString,
	}
	client.SubscribeNewClient(&opts)
}
