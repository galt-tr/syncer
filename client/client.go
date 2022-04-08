package client

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/BuxOrg/bux/chainstate"

	"github.com/mrz1836/go-whatsonchain"

	_ "net/http/pprof"

	"github.com/centrifugal/centrifuge-go"
)

// In real life clients should never know secret key. This is only for example
// purposes to quickly generate JWT for connection.
/*const exampleTokenHmacSecret = "secret"

func connToken(user string, exp int64) string {
	// NOTE that JWT must be generated on backend side of your application!
	// Here we are generating it on client side only for example simplicity.
	claims := jwt.MapClaims{"sub": user}
	if exp > 0 {
		claims["exp"] = exp
	}
	t, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(exampleTokenHmacSecret))
	if err != nil {
		panic(err)
	}
	return t
}*/

type EventHandler struct{}

func (h *EventHandler) SetMonitor(_ *chainstate.Monitor) {
	return
}

func (h *EventHandler) RecordTransaction(_ context.Context, _, _, _ string) error {
	return nil
}

func (h *EventHandler) GetWhatsOnChain() whatsonchain.ClientInterface {
	return nil
}

func (h *EventHandler) OnConnect(_ *centrifuge.Client, e centrifuge.ConnectEvent) {
	log.Printf("Connected to chat with ID %s", e.ClientID)
}

func (h *EventHandler) OnError(_ *centrifuge.Client, e centrifuge.ErrorEvent) {
	log.Printf("Error: %s", e.Message)
}

func (h *EventHandler) OnMessage(_ *centrifuge.Client, e centrifuge.MessageEvent) {
	log.Printf("Message from server: %s", string(e.Data))
}

func (h *EventHandler) OnDisconnect(_ *centrifuge.Client, e centrifuge.DisconnectEvent) {
	log.Printf("Disconnected from chat: %s", e.Reason)
	os.Exit(0)
}

func (h *EventHandler) OnServerSubscribe(_ *centrifuge.Client, e centrifuge.ServerSubscribeEvent) {
	log.Printf("Subscribe to server-side channel %s: (resubscribe: %t, recovered: %t)", e.Channel, e.Resubscribed, e.Recovered)
}

func (h *EventHandler) OnServerUnsubscribe(_ *centrifuge.Client, e centrifuge.ServerUnsubscribeEvent) {
	log.Printf("Unsubscribe from server-side channel %s", e.Channel)
}

func (h *EventHandler) OnServerJoin(_ *centrifuge.Client, e centrifuge.ServerJoinEvent) {
	log.Printf("Server-side join to channel %s: %s (%s)", e.Channel, e.User, e.Client)
}

func (h *EventHandler) OnServerLeave(_ *centrifuge.Client, e centrifuge.ServerLeaveEvent) {
	log.Printf("Server-side leave from channel %s: %s (%s)", e.Channel, e.User, e.Client)
}

func (h *EventHandler) OnServerPublish(_ *centrifuge.Client, e centrifuge.ServerPublishEvent) {
	log.Printf("Publication from server-side channel %s: %s", e.Channel, e.Data)
}

func (h *EventHandler) OnPublish(sub *centrifuge.Subscription, e centrifuge.PublishEvent) {
	publishMessage := whatsonchain.TxInfo{}
	err := json.Unmarshal(e.Data, &publishMessage)
	if err != nil {
		return
	}
	log.Printf("channel [%s]: %#v", sub.Channel(), publishMessage)
}

func (h *EventHandler) OnJoin(sub *centrifuge.Subscription, e centrifuge.JoinEvent) {
	log.Printf("Someone joined %s: user id %s, client id %s", sub.Channel(), e.User, e.Client)
}

func (h *EventHandler) OnLeave(sub *centrifuge.Subscription, e centrifuge.LeaveEvent) {
	log.Printf("Someone left %s: user id %s, client id %s", sub.Channel(), e.User, e.Client)
}

func (h *EventHandler) OnSubscribeSuccess(sub *centrifuge.Subscription, e centrifuge.SubscribeSuccessEvent) {
	log.Printf("Subscribed on channel %s, resubscribed: %v, recovered: %v", sub.Channel(), e.Resubscribed, e.Recovered)
}

func (h *EventHandler) OnSubscribeError(sub *centrifuge.Subscription, e centrifuge.SubscribeErrorEvent) {
	log.Printf("Subscribed on channel %s failed, error: %s", sub.Channel(), e.Error)
}

func (h *EventHandler) OnUnsubscribe(sub *centrifuge.Subscription, _ centrifuge.UnsubscribeEvent) {
	log.Printf("Unsubscribed from channel %s", sub.Channel())
}

func NewClient(handler chainstate.MonitorHandler) *centrifuge.Client {
	wsURL := "ws://mapi.gorillapool.io:8000/connection/websocket"
	c := centrifuge.NewJsonClient(wsURL, centrifuge.DefaultConfig())

	// Uncomment to make it work with Centrifugo and its JWT auth.
	//c.SetToken(connToken("49", 0))

	c.OnConnect(handler)
	c.OnDisconnect(handler)
	c.OnMessage(handler)
	c.OnError(handler)

	c.OnServerPublish(handler)
	c.OnServerSubscribe(handler)
	c.OnServerUnsubscribe(handler)
	c.OnServerJoin(handler)
	c.OnServerLeave(handler)

	return c
}

type SubscriptionOptions struct {
	From        string   `json:"from"`
	To          string   `json:"to"`
	Filters     []string `json:"filters"`
	FilterRegex string   `json:"filter_regex"`
	Handler     chainstate.MonitorHandler
}

func (s *SubscriptionOptions) FilterParams() string {
	return fmt.Sprintf("%s:%s", s.FilterRegex, strings.Join(s.Filters, "&"))
}

func SubscribeNewClient(opts *SubscriptionOptions) {
	c := NewClient(opts.Handler)
	defer func() { _ = c.Close() }()

	sub, err := c.NewSubscription(fmt.Sprintf("block:filter:%s:%s", opts.From, opts.FilterParams()))
	if err != nil {
		log.Fatalln(err)
	}

	sub.OnPublish(opts.Handler)
	sub.OnJoin(opts.Handler)
	sub.OnLeave(opts.Handler)
	sub.OnSubscribeSuccess(opts.Handler)
	sub.OnSubscribeError(opts.Handler)
	sub.OnUnsubscribe(opts.Handler)

	err = sub.Subscribe()
	if err != nil {
		log.Fatalln(err)
	}

	err = c.Connect()
	if err != nil {
		log.Fatalln(err)
	}

	// Run until CTRL+C.
	select {}
}
