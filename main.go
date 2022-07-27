package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	badger "github.com/dgraph-io/badger/v3"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	tmPrefix                = "tendermint"
	ethPrefix               = "ethereum"
	maxRequestContentLength = 1024 * 512
	defaultErrorCode        = -32000
)

type rpcBlock struct {
	EthHash common.Hash `json:"hash"`
	TmHash  common.Hash `json:"tm_hash"`
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

func tmHashLookup(db *badger.DB, hash common.Hash) ([]byte, error) {
	txn := db.NewTransaction(false)
	defer txn.Discard()
	item, err := txn.Get(append([]byte(tmPrefix), hash.Bytes()...))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return hash.Bytes(), err
	}
	if err != nil {
		return nil, err
	}
	return item.ValueCopy(nil)
}

func ethHashLookup(db *badger.DB, hash common.Hash) ([]byte, error) {
	txn := db.NewTransaction(false)
	defer txn.Discard()
	item, err := txn.Get(append([]byte(ethPrefix), hash.Bytes()...))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return hash.Bytes(), err
	}
	if err != nil {
		return nil, err
	}
	return item.ValueCopy(nil)
}

func (s *EthService) GetBlockByNumber(number string, full bool) (types.Header, error) {
	ctx := context.Background()
	n := new(big.Int)
	n.SetString(number, 16)
	header, err := s.ethClient.HeaderByNumber(ctx, n)
	if err != nil {
		return types.Header{}, err
	}
	// swap the tm parent hash for the eth equivalent
	parentHash, err := tmHashLookup(s.db, header.ParentHash)
	if err != nil {
		return types.Header{}, err
	}
	header.ParentHash = common.BytesToHash(parentHash)

	return *header, nil
}

func (s *EthService) GetBlockByHash(hash string, full bool) (types.Header, error) {
	ctx := context.Background()
	// swap the given Eth hash for the tm hash before retrieving the header
	dbHash, err := ethHashLookup(s.db, common.HexToHash(hash))
	if errors.Is(err, badger.ErrKeyNotFound) {
	} else if err != nil {
		return types.Header{}, err
	}
	header, err := s.ethClient.HeaderByHash(ctx, common.BytesToHash(dbHash))
	if err != nil {
		return types.Header{}, err
	}
	// swap the tm parent hash for the eth equivalent
	parentHash, err := tmHashLookup(s.db, header.ParentHash)
	if err != nil {
		return types.Header{}, err
	}
	header.ParentHash = common.BytesToHash(parentHash)

	return *header, nil
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
		err = txn.Set(append([]byte(tmPrefix), b.TmHash.Bytes()...), b.EthHash.Bytes())
		if err != nil {
			return 0, err
		}
		// eth to tm
		err = txn.Set(append([]byte(ethPrefix), b.EthHash.Bytes()...), b.TmHash.Bytes())
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

func onExit(shutdownCh chan os.Signal, db *badger.DB) {
	<-shutdownCh
	db.Close()
}

func main() {
	// Open the Badger database located in the /badger directory.
	// It will be created if it doesn't exist.
	// Create channel to listen to OS interrupt signals
	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, syscall.SIGINT, syscall.SIGTERM)
	db, err := badger.Open(badger.DefaultOptions("/badger"))
	if err != nil {
		panic(err)
	}
	defer db.Close()
	go onExit(shutdownCh, db)

	client, err := ethclient.Dial("http://ethermint0:8545")
	if err != nil {
		panic(err)
	}
	rawClient, err := rpc.DialHTTP("http://ethermint0:8545")
	if err != nil {
		panic(err)
	}

	// Start the server
	errChan := make(chan error)
	go server(errChan, db, client)

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
		if err.Error() != "Key not found" {
			panic(err)
		}
	}

	// Walk the Ethermint chain starting from block 0
	// Retrieve each block and parse out the "result.hash" and "result.eth_hash"
	head, err := walkChain(*rawClient, *client, uint64(height), db)
	if err != nil {
		panic(err)
	}

	// Tick every 4 seconds
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()

	for {
		select {
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
			err = txn.Set(append([]byte(tmPrefix), b.TmHash.Bytes()...), b.EthHash.Bytes())
			if err != nil {
				panic(err)
			}
			// eth to tm
			err = txn.Set(append([]byte(ethPrefix), b.EthHash.Bytes()...), b.TmHash.Bytes())
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
}
