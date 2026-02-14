package contracts

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var LiquidityAddedEventSig = crypto.Keccak256Hash([]byte("LiquidityAdded(address,uint256,address,uint256)"))

var TokenManager = common.HexToAddress("0x5c952063c7fc8610FFDB798152D69F0B9550762b")

const TokenTypeTax = 5

const TokenInfoABI = `[{"inputs":[{"internalType":"address","name":"","type":"address"}],"name":"_tokenInfos","outputs":[{"internalType":"uint256","name":"template","type":"uint256"}],"stateMutability":"view","type":"function"}]`

type LiquidityAddedEvent struct {
	Base   common.Address
	Offers *big.Int
	Quote  common.Address
	Funds  *big.Int
}

func ParseLiquidityAddedEvent(data []byte, topics []common.Hash) (*LiquidityAddedEvent, error) {
	if len(data) < 128 {
		return nil, fmt.Errorf("invalid data length: %d", len(data))
	}

	event := &LiquidityAddedEvent{
		Base:   common.BytesToAddress(data[12:32]),
		Offers: new(big.Int).SetBytes(data[32:64]),
		Quote:  common.BytesToAddress(data[76:96]),
		Funds:  new(big.Int).SetBytes(data[96:128]),
	}

	return event, nil
}

func IsTaxToken(client *ethclient.Client, tokenAddress common.Address) (bool, error) {
	parsedABI, err := abi.JSON(strings.NewReader(TokenInfoABI))
	if err != nil {
		return false, fmt.Errorf("failed to parse ABI: %w", err)
	}

	data, err := parsedABI.Pack("_tokenInfos", tokenAddress)
	if err != nil {
		return false, fmt.Errorf("failed to pack _tokenInfos: %w", err)
	}

	result, err := client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &TokenManager,
		Data: data,
	}, nil)
	if err != nil {
		return false, fmt.Errorf("failed to call _tokenInfos: %w", err)
	}

	outputs, err := parsedABI.Unpack("_tokenInfos", result)
	if err != nil {
		return false, fmt.Errorf("failed to unpack _tokenInfos: %w", err)
	}

	template := outputs[0].(*big.Int)
	creatorType := new(big.Int).Rsh(template, 10)
	creatorType.And(creatorType, big.NewInt(0x3F))

	return creatorType.Int64() == TokenTypeTax, nil
}
