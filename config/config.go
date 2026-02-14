package config

import (
	"log"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type WalletConfig struct {
	PrivateKey   string
	BuyAmountBNB *big.Float
}

type Config struct {
	BSCRPCURL       string
	BSCRPCHttp      string
	Wallets         []WalletConfig
	ContractAddress string
	Slippage        int
	GasLimit        uint64
	GasPriceGwei    int64
	StopLossPercent int
	EnableStopLoss  bool
}

func Load() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Println("Warning: .env file not found, using environment variables")
	}

	slippage, _ := strconv.Atoi(getEnv("SLIPPAGE", "10"))
	gasLimit, _ := strconv.ParseUint(getEnv("GAS_LIMIT", "300000"), 10, 64)
	gasPriceGwei, _ := strconv.ParseInt(getEnv("GAS_PRICE_GWEI", "5"), 10, 64)

	privateKeysStr := getEnv("PRIVATE_KEYS", "")
	buyAmountsStr := getEnv("BUY_AMOUNTS_BNB", "")

	privateKeys := strings.Split(privateKeysStr, ",")
	buyAmounts := strings.Split(buyAmountsStr, ",")

	var wallets []WalletConfig
	for i, pk := range privateKeys {
		pk = strings.TrimSpace(pk)
		if pk == "" {
			continue
		}

		buyAmount := new(big.Float)
		if i < len(buyAmounts) {
			amtStr := strings.TrimSpace(buyAmounts[i])
			if amtStr != "" {
				buyAmount.SetString(amtStr)
			} else {
				buyAmount.SetString("0.1")
			}
		} else {
			buyAmount.SetString("0.1")
		}

		wallets = append(wallets, WalletConfig{
			PrivateKey:   pk,
			BuyAmountBNB: buyAmount,
		})
	}

	stopLossPercent, _ := strconv.Atoi(getEnv("STOP_LOSS_PERCENT", "20"))
	enableStopLoss := getEnv("ENABLE_STOP_LOSS", "true") == "true"

	return &Config{
		BSCRPCURL:       getEnv("BSC_RPC_URL", "wss://bsc-ws-node.nariox.org:443"),
		BSCRPCHttp:      getEnv("BSC_RPC_HTTP", "https://bsc-dataseed.binance.org/"),
		Wallets:         wallets,
		ContractAddress: getEnv("CONTRACT_ADDRESS", ""),
		Slippage:        slippage,
		GasLimit:        gasLimit,
		GasPriceGwei:    gasPriceGwei,
		StopLossPercent: stopLossPercent,
		EnableStopLoss:  enableStopLoss,
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
