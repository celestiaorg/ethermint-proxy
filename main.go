package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"time"

	badger "github.com/dgraph-io/badger/v3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	maxRequestContentLength = 1024 * 512
	defaultErrorCode        = -32000
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

type EthService struct {
	db        *badger.DB
	ethClient *ethclient.Client
}

func newEthService(db *badger.DB, ethClient *ethclient.Client) *EthService {
	return &EthService{
		db,
		ethClient,
	}
}

func dbHashLookup(db *badger.DB, hash string) (string, error) {
	// Lookup any values using the given hash as a key
	// If there's a match that means the given hash is an Ethereum hash
	// Transparently swap the given ethereum hash for the matching tm hash
	// Make the eth_getBlockByHash(hash) call using the tm hash
	var dbHash string
	err := db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(hash))
		if err != nil {
			return err
		}
		err = item.Value(func(val []byte) error {
			// This func with val would only be called if item.Value encounters no error.
			valCopy := append([]byte{}, val...)
			dbHash = string(valCopy)
			return nil
		})
		return nil
	})
	if err != nil {
		if err.Error() != "key not found" {
			return "", err
		}
	}

	return dbHash, nil
}

func (s *EthService) GetBlockByHash(hash string, full bool) (*ethtypes.Block, error) {
	ctx := context.Background()
	fmt.Println("GetBlockByHash input: ", hash)
	dbHash, err := dbHashLookup(s.db, hash)
	if err != nil {
		return &ethtypes.Block{}, err
	}
	fmt.Println("GetBlockByHash dbHash: ", common.HexToHash(dbHash))
	block, err := s.ethClient.BlockByHash(ctx, common.HexToHash(dbHash))
	if err != nil {
		return &ethtypes.Block{}, err
	}
	return block, nil
}

func server(errChan chan error, db *badger.DB, ethClient *ethclient.Client) {
	eth := newEthService(db, ethClient)
	server := rpc.NewServer()
	server.RegisterName("eth", eth)
	http.HandleFunc("/", server.ServeHTTP)
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		errChan <- err
	}
}

// walk the chain from height to head
func walkChain(rawClient rpc.Client, client ethclient.Client, height uint64, db *badger.DB) (uint64, error) {
	fmt.Println("walking chain from height: ", height)
	head, err := client.BlockNumber(context.Background())
	if err != nil {
		panic(err)
	}
	var i uint64
	for i = height; i < head; i++ {
		b, err := getBlockHashesByNum(&rawClient, toBlockNumArg(big.NewInt(int64(i))), true)
		if err != nil {
			return 0, err
		}
		// Create a two kv pairs for each block, one from eth->tm and one from tm->eth

		// Add to the DB
		txn := db.NewTransaction(true)
		defer txn.Discard()

		// tm to eth
		// err = txn.Set(b.TmHash.Bytes(), b.EthHash.Bytes())
		// if err != nil {
		// 	return 0, err
		// }
		// eth to tm
		err = txn.Set(b.EthHash.Bytes(), b.TmHash.Bytes())
		if err != nil {
			return 0, err
		}
		// block height
		err = txn.Set([]byte("height"), []byte(strconv.Itoa(int(i))))
		if err != nil {
			return 0, err
		}
		// Commit the transaction and check for error.
		if err := txn.Commit(); err != nil {
			return 0, err
		}
		fmt.Printf("height: %d\ttmHash: %v\tethHash: %v\n", i, b.TmHash, b.EthHash)
	}
	return i, nil
}

func main() {
	// Open the Badger database located in the /badger directory.
	// It will be created if it doesn't exist.
	db, err := badger.Open(badger.DefaultOptions("/badger"))
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
	// wsClient, err := ethclient.Dial("ws://ethermint0:8546")
	// if err != nil {
	// 	panic(err)
	// }

	// Start from chain genesis
	height := 0

	// Set the starting height to the stored height
	err = db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("height"))
		if err != nil {
			return err
		}
		err = item.Value(func(val []byte) error {
			// This func with val would only be called if item.Value encounters no error.
			valCopy := append([]byte{}, val...)
			height, err = strconv.Atoi(string(valCopy))
			if err != nil {
				return err
			}
			return nil
		})
		return nil
	})
	if err != nil {
		if err.Error() != "key not found" {
			panic(err)
		}
	}

	// Walk the Ethermint chain starting from block 0
	// Retrieve each block and parse out the "result.hash" and "result.eth_hash"
	head, err := walkChain(*rawClient, *client, uint64(height), db)
	if err != nil {
		panic(err)
	}

	errChan := make(chan error)
	go server(errChan, db, client)

	// Tick every 4 seconds
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
		// case <-done:
		// 	fmt.Println("Done!")
		// 	return nil
		case _ = <-ticker.C:
			b, err := getBlockHashesByNum(rawClient, toBlockNumArg(big.NewInt(int64(head))), true)
			if err != nil {
				if err.Error() != "key not found" {
					panic(err)
				}
				continue
			}
			// Create a two kv pairs for each block, one from eth->tm and one from tm->eth

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
			// block height
			err = txn.Set([]byte("height"), []byte(strconv.Itoa(int(head+1))))
			if err != nil {
				panic(err)
			}
			// Commit the transaction and check for error.
			if err := txn.Commit(); err != nil {
				panic(err)
			}
			fmt.Printf("height: %d\ttmHash: %v\tethHash: %v\n", head, b.TmHash, b.EthHash)
			head++
		case err := <-errChan:
			fmt.Println(err)
		}
	}

	// Since subscriptions don't work in Optimint right now we'll just poll every X seconds
	/*
		headers := make(chan *ethtypes.Header)
		sub, err := wsClient.SubscribeNewHead(context.Background(), headers)
		if err != nil {
			panic("can't sub: " + err.Error())
		}

		for {
			select {
			case err := <-sub.Err():
				panic(err)
			case header := <-headers:
				fmt.Println(header.Hash().Hex()) // 0xbc10defa8dda384c96a17640d84de5578804945d347072e091b4e5f390ddea7f
			}
		}
	*/
}
