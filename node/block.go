package node

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
)

type Txs []Tx

type Tx struct {
	Hex  string `json:"hex"`
	Hash string `json:"hash"`
}

type Block struct {
	Txs  Txs    `json:"tx"`
	Hash string `json:"hash"`
}

type BlockWrapper struct {
	Txs  json.RawMessage `json:"tx"`
	Hash string          `json:"hash"`
}

func (c *Client) GetBlock(hash string) (*Block, error) {
	res, err := c.Client.Get(fmt.Sprintf("%s/rest/block/%s.json", c.Host, hash))
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	blockWrapper := BlockWrapper{}
	err = json.Unmarshal(body, &blockWrapper)

	txs := Txs{}
	err = json.Unmarshal(blockWrapper.Txs, &txs)
	return &Block{Txs: txs, Hash: blockWrapper.Hash}, nil
}
