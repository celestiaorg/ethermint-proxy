package types

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

type RpcHeader struct {
	ParentHash  common.Hash         `json:"parentHash"       gencodec:"required"`
	UncleHash   common.Hash         `json:"sha3Uncles"       gencodec:"required"`
	Coinbase    common.Address      `json:"miner"`
	Root        common.Hash         `json:"stateRoot"        gencodec:"required"`
	TxHash      common.Hash         `json:"transactionsRoot" gencodec:"required"`
	ReceiptHash common.Hash         `json:"receiptsRoot"     gencodec:"required"`
	Bloom       ethtypes.Bloom      `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int            `json:"difficulty"       gencodec:"required"`
	Number      *big.Int            `json:"number"           gencodec:"required"`
	GasLimit    uint64              `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64              `json:"gasUsed"          gencodec:"required"`
	Time        uint64              `json:"timestamp"        gencodec:"required"`
	Extra       []byte              `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash         `json:"mixHash"`
	Nonce       ethtypes.BlockNonce `json:"nonce"`

	// BaseFee was added by EIP-1559 and is ignored in legacy headers.
	BaseFee *big.Int `json:"baseFeePerGas" rlp:"optional"`

	Hash common.Hash `json:"hash"`
}

// field type overrides for gencodec
type headerMarshaling struct {
	Difficulty *hexutil.Big
	Number     *hexutil.Big
	GasLimit   hexutil.Uint64
	GasUsed    hexutil.Uint64
	Time       hexutil.Uint64
	Extra      hexutil.Bytes
	BaseFee    *hexutil.Big
	Hash       common.Hash `json:"hash"` // adds call to Hash() in MarshalJSON
}

func EthHeaderToRpcHeader(ethHeader *ethtypes.Header) *RpcHeader {
	rpcHeader := &RpcHeader{
		BaseFee:     ethHeader.BaseFee,
		Bloom:       ethHeader.Bloom,
		Coinbase:    ethHeader.Coinbase,
		Difficulty:  ethHeader.Difficulty,
		Extra:       ethHeader.Extra,
		GasLimit:    ethHeader.GasLimit,
		GasUsed:     ethHeader.GasUsed,
		MixDigest:   ethHeader.MixDigest,
		Nonce:       ethHeader.Nonce,
		Number:      ethHeader.Number,
		Root:        ethHeader.Root,
		Time:        ethHeader.Time,
		TxHash:      ethHeader.TxHash,
		UncleHash:   ethHeader.UncleHash,
		ReceiptHash: ethHeader.ReceiptHash,
	}
	return rpcHeader
}
