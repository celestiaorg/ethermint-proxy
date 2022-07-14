package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type rpcBlock struct {
	Hash    common.Hash `json:"hash"`
	EthHash common.Hash `json:"eth_hash"`
}

func toBlockNumArg(number *big.Int) string {
	if number == nil {
		return "latest"
	}
	pending := big.NewInt(-1)
	if number.Cmp(pending) == 0 {
		return "pending"
	}
	return hexutil.EncodeBig(number)
}

func getBlockHashesByNum(client *rpc.Client, args ...interface{}) (*rpcBlock, error) {
	var raw json.RawMessage
	err := client.CallContext(context.Background(), &raw, "eth_getBlockByNumber", args...)
	if err != nil {
		return nil, err
	} else if len(raw) == 0 {
		return nil, ethereum.NotFound
	}
	// Decode header and transactions.
	var body rpcBlock
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return &body, nil
}

// walk the chain from height to head
func walkChain(rawClient rpc.Client, client ethclient.Client, height uint64) {
	head, err := client.BlockNumber(context.Background())
	if err != nil {
		panic(err)
	}
	for i := height; i < head; i++ {
		b, err := getBlockHashesByNum(&rawClient, toBlockNumArg(big.NewInt(int64(i))), true)
		if err != nil {
			panic(err)
		}
		fmt.Println(b)
	}

}

func main() {
	client, err := ethclient.Dial("http://ethermint0:8545")
	if err != nil {
		panic(err)
	}
	rawClient, err := rpc.DialHTTP("http://ethermint0:8545")
	if err != nil {
		panic(err)
	}

	walkChain(*rawClient, *client, 0)

	// Walk the Ethermint chain starting from block 0
	// Retrieve each block and parse out the "result.hash" and "result.eth_hash"
	// Create a two kv pairs for each block, one from eth->tm and one from tm->eth
	// Increment a block height counter
	// start server

	// proxy := goproxy.NewProxyHttpServer()
	// proxy.Verbose = true
	// log.Fatal(http.ListenAndServe(":8080", proxy))
}
