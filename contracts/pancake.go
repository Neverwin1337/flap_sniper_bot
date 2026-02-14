package contracts

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	PancakeRouterV2 = common.HexToAddress("0x10ED43C718714eb63d5aA57B78B54704E256024E")
	WBNB            = common.HexToAddress("0xbb4CdB9CBd36B01bD1cBaEBF2De08d9173bc095c")
	USDT            = common.HexToAddress("0x55d398326f99059fF775485246999027B3197955")
)

const SwapExactETHForTokensABI = `[{"inputs":[{"internalType":"uint256","name":"amountOutMin","type":"uint256"},{"internalType":"address[]","name":"path","type":"address[]"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"deadline","type":"uint256"}],"name":"swapExactETHForTokensSupportingFeeOnTransferTokens","outputs":[],"stateMutability":"payable","type":"function"}]`

const SwapExactTokensForETHABI = `[{"inputs":[{"internalType":"uint256","name":"amountIn","type":"uint256"},{"internalType":"uint256","name":"amountOutMin","type":"uint256"},{"internalType":"address[]","name":"path","type":"address[]"},{"internalType":"address","name":"to","type":"address"},{"internalType":"uint256","name":"deadline","type":"uint256"}],"name":"swapExactTokensForETHSupportingFeeOnTransferTokens","outputs":[],"stateMutability":"nonpayable","type":"function"}]`

const GetAmountsOutABI = `[{"inputs":[{"internalType":"uint256","name":"amountIn","type":"uint256"},{"internalType":"address[]","name":"path","type":"address[]"}],"name":"getAmountsOut","outputs":[{"internalType":"uint256[]","name":"amounts","type":"uint256[]"}],"stateMutability":"view","type":"function"}]`

const ERC20ABI = `[{"inputs":[{"internalType":"address","name":"spender","type":"address"},{"internalType":"uint256","name":"amount","type":"uint256"}],"name":"approve","outputs":[{"internalType":"bool","name":"","type":"bool"}],"stateMutability":"nonpayable","type":"function"},{"inputs":[{"internalType":"address","name":"account","type":"address"}],"name":"balanceOf","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`

type PancakeSwapper struct {
	client     *ethclient.Client
	privateKey *ecdsa.PrivateKey
	address    common.Address
	chainID    *big.Int
	gasLimit   uint64
	gasPrice   *big.Int
	slippage   int
}

func NewPancakeSwapper(client *ethclient.Client, privateKeyHex string, gasLimit uint64, gasPriceGwei int64, slippage int) (*PancakeSwapper, error) {
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}

	publicKey := privateKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("cannot assert type: publicKey is not of type *ecdsa.PublicKey")
	}

	address := crypto.PubkeyToAddress(*publicKeyECDSA)

	chainID, err := client.NetworkID(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get chain ID: %w", err)
	}

	gasPrice := new(big.Int).Mul(big.NewInt(gasPriceGwei), big.NewInt(1e9))

	return &PancakeSwapper{
		client:     client,
		privateKey: privateKey,
		address:    address,
		chainID:    chainID,
		gasLimit:   gasLimit,
		gasPrice:   gasPrice,
		slippage:   slippage,
	}, nil
}

func (p *PancakeSwapper) BuyToken(tokenAddress common.Address, amountBNB *big.Int) (string, error) {
	parsedABI, err := abi.JSON(strings.NewReader(SwapExactETHForTokensABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %w", err)
	}

	path := []common.Address{WBNB, tokenAddress}
	deadline := big.NewInt(time.Now().Unix() + 300)
	amountOutMin := big.NewInt(0)

	data, err := parsedABI.Pack("swapExactETHForTokensSupportingFeeOnTransferTokens", amountOutMin, path, p.address, deadline)
	if err != nil {
		return "", fmt.Errorf("failed to pack data: %w", err)
	}

	nonce, err := p.client.PendingNonceAt(context.Background(), p.address)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	tx := types.NewTransaction(nonce, PancakeRouterV2, amountBNB, p.gasLimit, p.gasPrice, data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(p.chainID), p.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	err = p.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}

func (p *PancakeSwapper) GetAddress() common.Address {
	return p.address
}

func (p *PancakeSwapper) GetTokenBalance(tokenAddress common.Address) (*big.Int, error) {
	parsedABI, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC20 ABI: %w", err)
	}

	data, err := parsedABI.Pack("balanceOf", p.address)
	if err != nil {
		return nil, fmt.Errorf("failed to pack balanceOf: %w", err)
	}

	result, err := p.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &tokenAddress,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call balanceOf: %w", err)
	}

	outputs, err := parsedABI.Unpack("balanceOf", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack balanceOf: %w", err)
	}

	return outputs[0].(*big.Int), nil
}

func (p *PancakeSwapper) GetTokenPrice(tokenAddress common.Address, amount *big.Int) (*big.Int, error) {
	parsedABI, err := abi.JSON(strings.NewReader(GetAmountsOutABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	path := []common.Address{tokenAddress, WBNB}
	data, err := parsedABI.Pack("getAmountsOut", amount, path)
	if err != nil {
		return nil, fmt.Errorf("failed to pack getAmountsOut: %w", err)
	}

	result, err := p.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &PancakeRouterV2,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call getAmountsOut: %w", err)
	}

	outputs, err := parsedABI.Unpack("getAmountsOut", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getAmountsOut: %w", err)
	}

	amounts := outputs[0].([]*big.Int)
	if len(amounts) < 2 {
		return nil, fmt.Errorf("invalid amounts length")
	}

	return amounts[1], nil
}

func (p *PancakeSwapper) GetTokenPriceInUSDT(tokenAddress common.Address, amount *big.Int) (*big.Int, error) {
	parsedABI, err := abi.JSON(strings.NewReader(GetAmountsOutABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ABI: %w", err)
	}

	path := []common.Address{tokenAddress, WBNB, USDT}
	data, err := parsedABI.Pack("getAmountsOut", amount, path)
	if err != nil {
		return nil, fmt.Errorf("failed to pack getAmountsOut: %w", err)
	}

	result, err := p.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &PancakeRouterV2,
		Data: data,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to call getAmountsOut: %w", err)
	}

	outputs, err := parsedABI.Unpack("getAmountsOut", result)
	if err != nil {
		return nil, fmt.Errorf("failed to unpack getAmountsOut: %w", err)
	}

	amounts := outputs[0].([]*big.Int)
	if len(amounts) < 3 {
		return nil, fmt.Errorf("invalid amounts length")
	}

	return amounts[2], nil
}

func (p *PancakeSwapper) ApproveToken(tokenAddress common.Address, amount *big.Int) (string, error) {
	parsedABI, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ERC20 ABI: %w", err)
	}

	data, err := parsedABI.Pack("approve", PancakeRouterV2, amount)
	if err != nil {
		return "", fmt.Errorf("failed to pack approve: %w", err)
	}

	nonce, err := p.client.PendingNonceAt(context.Background(), p.address)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	tx := types.NewTransaction(nonce, tokenAddress, big.NewInt(0), p.gasLimit, p.gasPrice, data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(p.chainID), p.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	err = p.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}

func (p *PancakeSwapper) SellToken(tokenAddress common.Address, amount *big.Int) (string, error) {
	parsedABI, err := abi.JSON(strings.NewReader(SwapExactTokensForETHABI))
	if err != nil {
		return "", fmt.Errorf("failed to parse ABI: %w", err)
	}

	path := []common.Address{tokenAddress, WBNB}
	deadline := big.NewInt(time.Now().Unix() + 300)
	amountOutMin := big.NewInt(0)

	data, err := parsedABI.Pack("swapExactTokensForETHSupportingFeeOnTransferTokens", amount, amountOutMin, path, p.address, deadline)
	if err != nil {
		return "", fmt.Errorf("failed to pack data: %w", err)
	}

	nonce, err := p.client.PendingNonceAt(context.Background(), p.address)
	if err != nil {
		return "", fmt.Errorf("failed to get nonce: %w", err)
	}

	tx := types.NewTransaction(nonce, PancakeRouterV2, big.NewInt(0), p.gasLimit, p.gasPrice, data)

	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(p.chainID), p.privateKey)
	if err != nil {
		return "", fmt.Errorf("failed to sign transaction: %w", err)
	}

	err = p.client.SendTransaction(context.Background(), signedTx)
	if err != nil {
		return "", fmt.Errorf("failed to send transaction: %w", err)
	}

	return signedTx.Hash().Hex(), nil
}
