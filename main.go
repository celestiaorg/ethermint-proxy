package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"

	badger "github.com/dgraph-io/badger/v3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

type rpcBlock struct {
	TmHash  common.Hash `json:"hash"`
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
func walkChain(rawClient rpc.Client, client ethclient.Client, height uint64, db *badger.DB) {
	head, err := client.BlockNumber(context.Background())
	if err != nil {
		panic(err)
	}
	for i := height; i < head; i++ {
		b, err := getBlockHashesByNum(&rawClient, toBlockNumArg(big.NewInt(int64(i))), true)
		if err != nil {
			panic(err)
		}
		// Add to the DB
		txn := db.NewTransaction(true)
		defer txn.Discard()

		// tm to eth
		err = txn.Set(b.TmHash.Bytes(), b.EthHash.Bytes())
		if err != nil {
			panic(err)
		}
		// eth to tm
		err = txn.Set(b.EthHash.Bytes(), b.TmHash.Bytes())
		if err != nil {
			panic(err)
		}
		// Commit the transaction and check for error.
		if err := txn.Commit(); err != nil {
			panic(err)
		}
		fmt.Printf("height: %d\ttmHash: %v\tethHash: %v\n", i, b.TmHash, b.EthHash)
	}
}

func main() {
	// Open the Badger database located in the /tmp/badger directory.
	// It will be created if it doesn't exist.
	db, err := badger.Open(badger.DefaultOptions("/tmp/badger"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	client, err := ethclient.Dial("http://ethermint0:8545")
	if err != nil {
		panic(err)
	}
	rawClient, err := rpc.DialHTTP("http://ethermint0:8545")
	if err != nil {
		panic(err)
	}

	// Walk the Ethermint chain starting from block 0
	// Retrieve each block and parse out the "result.hash" and "result.eth_hash"
	walkChain(*rawClient, *client, 0, db)

	// Create a two kv pairs for each block, one from eth->tm and one from tm->eth
	// Increment a block height counter

	// start server
	// proxy := goproxy.NewProxyHttpServer()
	// proxy.Verbose = true
	// log.Fatal(http.ListenAndServe(":8080", proxy))
}
