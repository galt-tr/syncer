package node

import (
	"net/http"
	"os"
	"time"
)

type Client struct {
	Client http.Client
	Host   string
}

func NewClient() *Client {
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	host := os.Getenv("NODE_HOST")
	if host == "" {
		return nil
	}
	return &Client{
		Client: client,
		Host:   host,
	}
}
