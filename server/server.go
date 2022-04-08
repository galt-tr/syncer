package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/BuxOrg/bux-agent/client"

	"github.com/BuxOrg/bux/chainstate"

	"github.com/BuxOrg/bux-agent/node"

	_ "net/http/pprof"

	"github.com/centrifugal/centrifuge"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type BlockSubcriber interface {
	Subscribe() error
	Publish() error
	GetTransactions() ([]string, error)
}

type BlockFilter struct {
	Channel       string   `json:"channel"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Transactions  []string `json:"transactions"`
	Block         *node.Block
	Synced        bool `json:"synced"`
	ProcessorNode *ProcessorNode
}

type ProcessorNode struct {
	Processor chainstate.MonitorProcessor
	Node      *centrifuge.Node
}

func (b *BlockFilter) Publish(data []byte) error {
	log.Printf("id: %v", b.ProcessorNode.Node.ID())
	_, err := b.ProcessorNode.Node.Publish(
		b.Channel,
		data,
		centrifuge.WithHistory(300, time.Minute),
	)
	if err != nil {
		log.Printf("ERROR PUBLISHING: %v", err)
	}
	log.Printf("published!!")
	return err

}

func (b *BlockFilter) launchBlockSync() error {
	c := node.NewClient()
	if c == nil {
		return errors.New("error launching block sync")
	}
	block, err := c.GetBlock(b.From)
	if err != nil {
		return err
	}
	if block == nil {
		return errors.New("block is nil")
	}
	for _, tx := range block.Txs {
		hex, err := b.ProcessorNode.Processor.FilterMempoolTx(tx.Hex)
		if err != nil {
			log.Printf("error with processor: %v", err)
			continue
		}
		if hex == "" {
			continue
		}

		out, err := json.Marshal(tx)
		if err != nil {
			return err
		}
		err = b.Publish(out)
		if err != nil {
			return err
		}
	}
	log.Printf("Processed block %v", b.From)
	return nil
}

func (b *BlockFilter) Subscribe() error {
	log.Printf("client subscribing to %s", b.From)
	err := b.launchBlockSync()
	if err != nil {
		log.Printf("error: %v", err)
	}
	return err
}

func (b *BlockFilter) GetTransactions() ([]string, error) {
	return b.Transactions, nil
}

type addFilterMessage struct {
	Timestamp int64  `json:"timestamp"`
	Filter    string `json:"filter"`
}

type streamBlockMessage struct {
	Timestamp int64  `json:"timestamp"`
	Block     string `json:"block"`
}

type subscribeReply struct {
	Message string `json:"message"`
}

type blockReply struct {
	Block string `json:"block"`
}

func handleLog(e centrifuge.LogEntry) {
	log.Printf("%s: %v", e.Message, e.Fields)
}

func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		newCtx := centrifuge.SetCredentials(ctx, &centrifuge.Credentials{
			UserID:   "42",
			ExpireAt: time.Now().Unix() + 60,
			Info:     []byte(`{"name": "Alexander"}`),
		})
		r = r.WithContext(newCtx)
		h.ServeHTTP(w, r)
	})
}

func waitExitSignal(n *centrifuge.Node) {
	sigCh := make(chan os.Signal, 1)
	done := make(chan bool, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		_ = n.Shutdown(context.Background())
		done <- true
	}()
	<-done
}

const blockFilterChannel = "block:filter:"
const blockSyncChannel = "block:sync"

// Check whether channel is allowed for subscribing.
func channelSubscribeAllowed(channel string) bool {
	return strings.HasPrefix(channel, blockFilterChannel)
}

func Start() {
	centrifugeNode, _ := centrifuge.New(centrifuge.Config{
		LogLevel:   centrifuge.LogLevelInfo,
		LogHandler: handleLog,
	})
	p := chainstate.NewBloomProcessor(uint(1000), 0.001)
	pNode := ProcessorNode{
		Node:      centrifugeNode,
		Processor: p,
	}

	// Override default broker which does not use HistoryMetaTTL.
	broker, _ := centrifuge.NewMemoryBroker(centrifugeNode, centrifuge.MemoryBrokerConfig{
		HistoryMetaTTL: 120 * time.Second,
	})
	centrifugeNode.SetBroker(broker)

	centrifugeNode.OnConnecting(func(ctx context.Context, e centrifuge.ConnectEvent) (centrifuge.ConnectReply, error) {
		cred, _ := centrifuge.GetCredentials(ctx)
		return centrifuge.ConnectReply{
			Data: []byte(`{}`),
			// Subscribe to personal several server-side channel.
			Subscriptions: map[string]centrifuge.SubscribeOptions{
				"#" + cred.UserID: {Recover: true, Presence: true, JoinLeave: true},
			},
		}, nil
	})

	centrifugeNode.OnConnect(func(client *centrifuge.Client) {
		transport := client.Transport()
		log.Printf("user %s connected via %s with protocol: %s", client.UserID(), transport.Name(), transport.Protocol())

		// Event handler should not block, so start separate goroutine to
		// periodically send messages to client.
		go func() {
			for {
				select {
				case <-client.Context().Done():
					return
				case <-time.After(5 * time.Second):
					err := client.Send([]byte(`{"time": "` + strconv.FormatInt(time.Now().Unix(), 10) + `"}`))
					if err != nil {
						if err == io.EOF {
							return
						}
						log.Printf("error sending message: %s", err)
					}
				}
			}
		}()

		client.OnAlive(func() {
			log.Printf("user %s connection is still active", client.UserID())
		})

		client.OnRefresh(func(e centrifuge.RefreshEvent, cb centrifuge.RefreshCallback) {
			log.Printf("user %s connection is going to expire, refreshing", client.UserID())
			cb(centrifuge.RefreshReply{
				ExpireAt: time.Now().Unix() + 60,
			}, nil)
		})

		client.OnSubscribe(func(e centrifuge.SubscribeEvent, cb centrifuge.SubscribeCallback) {
			log.Printf("user %s subscribes on %s", client.UserID(), e.Channel)
			if !channelSubscribeAllowed(e.Channel) {
				cb(centrifuge.SubscribeReply{}, centrifuge.ErrorPermissionDenied)
				return
			}
			split := strings.Split(e.Channel, "block:filter:")
			opts := parseSubOptions(split[1])
			for _, f := range opts.Filters {
				pNode.Processor.Add(opts.FilterRegex, f)
			}
			sub := BlockFilter{
				Channel:       e.Channel,
				From:          opts.From,
				To:            opts.To,
				ProcessorNode: &pNode,
			}

			msg := fmt.Sprintf("subscribed to channel from %s to %s", sub.From, sub.To)

			reply := subscribeReply{
				Message: msg,
			}
			data, err := json.Marshal(reply)
			if err != nil {
				return
			}

			go sub.Subscribe()
			/*if err != nil {
				log.Printf("error subscribing to the channel %v", err)
				cb(centrifuge.SubscribeReply{}, centrifuge.ErrorInternal)
				return
			}*/
			cb(centrifuge.SubscribeReply{
				Options: centrifuge.SubscribeOptions{
					Recover:   true,
					Presence:  true,
					JoinLeave: true,
					Data:      data,
				},
			}, nil)
		})

		client.OnUnsubscribe(func(e centrifuge.UnsubscribeEvent) {
			log.Printf("user %s unsubscribed from %s", client.UserID(), e.Channel)
		})

		client.OnPublish(func(e centrifuge.PublishEvent, cb centrifuge.PublishCallback) {
			log.Printf("user %s publishes into channel %s: %s", client.UserID(), e.Channel, string(e.Data))
			if !client.IsSubscribed(e.Channel) {
				cb(centrifuge.PublishReply{}, centrifuge.ErrorPermissionDenied)
				return
			}

			var msg addFilterMessage
			err := json.Unmarshal(e.Data, &msg)
			if err != nil {
				cb(centrifuge.PublishReply{}, centrifuge.ErrorBadRequest)
				return
			}
			msg.Timestamp = time.Now().Unix()
			data, _ := json.Marshal(msg)

			result, err := centrifugeNode.Publish(
				e.Channel, data,
				centrifuge.WithHistory(300, time.Minute),
				centrifuge.WithClientInfo(e.ClientInfo),
			)

			cb(centrifuge.PublishReply{Result: &result}, err)
		})

		client.OnRPC(func(e centrifuge.RPCEvent, cb centrifuge.RPCCallback) {
			log.Printf("RPC from user: %s, data: %s, method: %s", client.UserID(), string(e.Data), e.Method)
			cb(centrifuge.RPCReply{
				Data: []byte(`{"year": "2022"}`),
			}, nil)
		})

		client.OnPresence(func(e centrifuge.PresenceEvent, cb centrifuge.PresenceCallback) {
			log.Printf("user %s calls presence on %s", client.UserID(), e.Channel)
			if !client.IsSubscribed(e.Channel) {
				cb(centrifuge.PresenceReply{}, centrifuge.ErrorPermissionDenied)
				return
			}
			cb(centrifuge.PresenceReply{}, nil)
		})

		client.OnMessage(func(e centrifuge.MessageEvent) {
			log.Printf("message from user: %s, data: %s", client.UserID(), string(e.Data))
		})

		client.OnDisconnect(func(e centrifuge.DisconnectEvent) {
			log.Printf("user %s disconnected, disconnect: %s", client.UserID(), e.Disconnect)
		})
	})

	if err := centrifugeNode.Run(); err != nil {
		log.Fatal(err)
	}

	go func() {
		// Publish personal notifications for user 42 periodically.
		i := 1
		for {
			_, err := centrifugeNode.Publish(
				"blocks",
				[]byte(`{"test": "`+strconv.Itoa(i)+`"}`),
				centrifuge.WithHistory(300, time.Minute),
			)
			if err != nil {
				log.Printf("error publishing to personal channel: %s", err)
			}
			i++
			time.Sleep(5000 * time.Millisecond)
		}
	}()

	go func() {
		// Publish to channel periodically.
		i := 1
		for {
			_, err := centrifugeNode.Publish(
				"chat:index",
				[]byte(`{"input": "Publish from server `+strconv.Itoa(i)+`"}`),
				centrifuge.WithHistory(300, time.Minute),
			)
			if err != nil {
				log.Printf("error publishing to channel: %s", err)
			}
			i++
			time.Sleep(10000 * time.Millisecond)
		}
	}()

	websocketHandler := centrifuge.NewWebsocketHandler(centrifugeNode, centrifuge.WebsocketConfig{
		ReadBufferSize:     1024,
		UseWriteBufferPool: true,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	})
	http.Handle("/connection/websocket", authMiddleware(websocketHandler))

	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/", http.FileServer(http.Dir("./")))

	go func() {
		if err := http.ListenAndServe(":8000", nil); err != nil {
			log.Fatal(err)
		}
	}()

	waitExitSignal(centrifugeNode)
	log.Println("bye!")
}

func parseSubOptions(opts string) *client.SubscriptionOptions {
	splitString := strings.Split(opts, ":")
	if len(splitString) != 3 {
		return nil
	}
	from := splitString[0]
	regex := splitString[1]
	filtersAll := splitString[2]
	filters := strings.Split(filtersAll, "&")
	return &client.SubscriptionOptions{
		From:        from,
		FilterRegex: regex,
		Filters:     filters,
	}

}
