package listener

import (
	"context"
	"fmt"
	"log"
	"math/big"
	"sync"
	"time"

	"flap/contracts"
	"flap/stoploss"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	healthCheckInterval  = 30 * time.Second
	reconnectDelay       = 5 * time.Second
	maxReconnectAttempts = 10
)

type WalletInfo struct {
	Swapper      *contracts.PancakeSwapper
	BuyAmountWei *big.Int
}

type EventListener struct {
	wsURL           string
	client          *ethclient.Client
	httpClient      *ethclient.Client
	contractAddress common.Address
	wallets         []WalletInfo
	stopLossMonitor *stoploss.StopLossMonitor
	mu              sync.RWMutex
}

func NewEventListener(wsURL string, contractAddr string, wallets []WalletInfo, stopLossMonitor *stoploss.StopLossMonitor, httpClient *ethclient.Client) (*EventListener, error) {
	client, err := ethclient.Dial(wsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to BSC: %w", err)
	}

	return &EventListener{
		wsURL:           wsURL,
		client:          client,
		httpClient:      httpClient,
		contractAddress: common.HexToAddress(contractAddr),
		wallets:         wallets,
		stopLossMonitor: stopLossMonitor,
	}, nil
}

func (l *EventListener) reconnect() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.client != nil {
		l.client.Close()
	}

	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		log.Printf("Reconnecting... attempt %d/%d", attempt, maxReconnectAttempts)

		client, err := ethclient.Dial(l.wsURL)
		if err != nil {
			log.Printf("Reconnect failed: %v", err)
			time.Sleep(reconnectDelay * time.Duration(attempt))
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err = client.BlockNumber(ctx)
		cancel()

		if err != nil {
			log.Printf("Connection test failed: %v", err)
			client.Close()
			time.Sleep(reconnectDelay * time.Duration(attempt))
			continue
		}

		l.client = client
		log.Printf("Reconnected successfully")
		return nil
	}

	return fmt.Errorf("failed to reconnect after %d attempts", maxReconnectAttempts)
}

func (l *EventListener) healthCheck(ctx context.Context) bool {
	l.mu.RLock()
	client := l.client
	l.mu.RUnlock()

	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := client.BlockNumber(checkCtx)
	if err != nil {
		log.Printf("Health check failed: %v", err)
		return false
	}
	return true
}

func (l *EventListener) Start(ctx context.Context) error {
	log.Printf("Listening for LiquidityAdded events on contract: %s", l.contractAddress.Hex())
	for i, w := range l.wallets {
		log.Printf("Wallet %d: %s (Buy: %s wei)", i+1, w.Swapper.GetAddress().Hex(), w.BuyAmountWei.String())
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			err := l.subscribe(ctx)
			if err != nil {
				log.Printf("Subscription ended: %v", err)

				select {
				case <-ctx.Done():
					return nil
				default:
					if reconnErr := l.reconnect(); reconnErr != nil {
						return reconnErr
					}
				}
			}
		}
	}
}

func (l *EventListener) subscribe(ctx context.Context) error {
	l.mu.RLock()
	client := l.client
	l.mu.RUnlock()

	query := ethereum.FilterQuery{
		Addresses: []common.Address{l.contractAddress},
		Topics:    [][]common.Hash{{contracts.LiquidityAddedEventSig}},
	}

	logs := make(chan types.Log)
	sub, err := client.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}
	defer sub.Unsubscribe()

	log.Println("Subscription active, listening for events...")

	healthTicker := time.NewTicker(healthCheckInterval)
	defer healthTicker.Stop()

	for {
		select {
		case err := <-sub.Err():
			return fmt.Errorf("subscription error: %w", err)
		case vLog := <-logs:
			l.handleLog(vLog)
		case <-healthTicker.C:
			if !l.healthCheck(ctx) {
				return fmt.Errorf("health check failed")
			}

		case <-ctx.Done():
			return nil
		}
	}
}

func (l *EventListener) handleLog(vLog types.Log) {
	event, err := contracts.ParseLiquidityAddedEvent(vLog.Data, vLog.Topics)
	if err != nil {
		log.Printf("Failed to parse event: %v", err)
		return
	}

	log.Printf("=== LiquidityAdded Event Detected ===")
	log.Printf("Base (Token): %s", event.Base.Hex())
	log.Printf("Offers: %s", event.Offers.String())
	log.Printf("Quote: %s", event.Quote.Hex())
	log.Printf("Funds: %s", event.Funds.String())
	log.Printf("TX Hash: %s", vLog.TxHash.Hex())

	isTax, err := contracts.IsTaxToken(l.httpClient, event.Base)
	if err != nil {
		log.Printf("Failed to check TaxToken: %v", err)
		return
	}

	if !isTax {
		log.Printf("Token %s is NOT a TaxToken, skipping...", event.Base.Hex())
		return
	}

	log.Printf("Token %s is a TaxToken, proceeding to buy...", event.Base.Hex())

	var wg sync.WaitGroup
	for i, w := range l.wallets {
		wg.Add(1)
		go func(idx int, wallet WalletInfo) {
			defer wg.Done()
			log.Printf("[Wallet %d] Attempting to buy token %s with %s wei BNB...", idx+1, event.Base.Hex(), wallet.BuyAmountWei.String())

			txHash, err := wallet.Swapper.BuyToken(event.Base, wallet.BuyAmountWei)
			if err != nil {
				log.Printf("[Wallet %d] Failed to buy token: %v", idx+1, err)
				return
			}

			log.Printf("[Wallet %d] Buy transaction sent! TX Hash: %s", idx+1, txHash)
			log.Printf("[Wallet %d] BSCScan: https://bscscan.com/tx/%s", idx+1, txHash)

			if l.stopLossMonitor != nil {
				time.Sleep(5 * time.Second)
				l.stopLossMonitor.AddPosition(idx, wallet.Swapper, event.Base, wallet.BuyAmountWei)
			}
		}(i, w)
	}
	wg.Wait()
}

func (l *EventListener) Close() {
	l.client.Close()
}
