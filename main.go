package main

import (
	"context"
	"flap/config"
	"flap/contracts"
	"flap/listener"
	"flap/stoploss"
	"log"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/ethereum/go-ethereum/ethclient"
)

func main() {
	cfg := config.Load()

	if len(cfg.Wallets) == 0 {
		log.Fatal("PRIVATE_KEYS is required")
	}
	if cfg.ContractAddress == "" {
		log.Fatal("CONTRACT_ADDRESS is required")
	}

	log.Println("Connecting to BSC...")

	httpClient, err := ethclient.Dial(cfg.BSCRPCHttp)
	if err != nil {
		log.Fatalf("Failed to connect to BSC HTTP: %v", err)
	}
	defer httpClient.Close()

	var wallets []listener.WalletInfo
	for i, w := range cfg.Wallets {
		swapper, err := contracts.NewPancakeSwapper(
			httpClient,
			w.PrivateKey,
			cfg.GasLimit,
			cfg.GasPriceGwei,
			cfg.Slippage,
		)
		if err != nil {
			log.Fatalf("Failed to create swapper for wallet %d: %v", i+1, err)
		}

		buyAmountWei := new(big.Int)
		w.BuyAmountBNB.Mul(w.BuyAmountBNB, big.NewFloat(1e18)).Int(buyAmountWei)

		log.Printf("Wallet %d: %s (Buy: %s BNB)", i+1, swapper.GetAddress().Hex(), w.BuyAmountBNB.String())
		wallets = append(wallets, listener.WalletInfo{
			Swapper:      swapper,
			BuyAmountWei: buyAmountWei,
		})
	}

	var stopLossMonitor *stoploss.StopLossMonitor
	if cfg.EnableStopLoss {
		stopLossMonitor = stoploss.NewStopLossMonitor(cfg.StopLossPercent)
		go stopLossMonitor.Start()
		log.Printf("Stop-loss enabled: %d%% threshold", cfg.StopLossPercent)
	}

	eventListener, err := listener.NewEventListener(
		cfg.BSCRPCURL,
		cfg.ContractAddress,
		wallets,
		stopLossMonitor,
		httpClient,
	)
	if err != nil {
		log.Fatalf("Failed to create event listener: %v", err)
	}
	defer eventListener.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Println("Shutting down...")
		if stopLossMonitor != nil {
			stopLossMonitor.Stop()
		}
		cancel()
	}()

	log.Println("Starting event listener...")
	if err := eventListener.Start(ctx); err != nil {
		log.Printf("Event listener stopped: %v", err)
	}

	log.Println("Goodbye!")
}
