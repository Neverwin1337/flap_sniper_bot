package stoploss

import (
	"context"
	"log"
	"math/big"
	"sync"
	"time"

	"flap/contracts"

	"github.com/ethereum/go-ethereum/common"
)

const (
	TakeProfitPriceUSDT   = 0.0002
	TakeProfitSellPercent = 70
)

type Position struct {
	TokenAddress       common.Address
	BuyPriceWei        *big.Int
	TokenAmount        *big.Int
	InitialTokenAmount *big.Int
	WalletIndex        int
	Swapper            *contracts.PancakeSwapper
	Sold               bool
	Approved           bool
	TakeProfitDone     bool
}

type StopLossMonitor struct {
	positions       map[string]*Position
	stopLossPercent int
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
}

func NewStopLossMonitor(stopLossPercent int) *StopLossMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	return &StopLossMonitor{
		positions:       make(map[string]*Position),
		stopLossPercent: stopLossPercent,
		ctx:             ctx,
		cancel:          cancel,
	}
}

func (m *StopLossMonitor) AddPosition(walletIndex int, swapper *contracts.PancakeSwapper, tokenAddress common.Address, buyAmountWei *big.Int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	balance, err := swapper.GetTokenBalance(tokenAddress)
	if err != nil {
		log.Printf("[Wallet %d] Failed to get token balance: %v", walletIndex+1, err)
		return
	}

	if balance.Cmp(big.NewInt(0)) <= 0 {
		log.Printf("[Wallet %d] No token balance found", walletIndex+1)
		return
	}

	currentPrice, err := swapper.GetTokenPrice(tokenAddress, balance)
	if err != nil {
		log.Printf("[Wallet %d] Failed to get initial price: %v", walletIndex+1, err)
		currentPrice = buyAmountWei
	}

	key := positionKey(walletIndex, tokenAddress)
	m.positions[key] = &Position{
		TokenAddress:       tokenAddress,
		BuyPriceWei:        currentPrice,
		TokenAmount:        balance,
		InitialTokenAmount: new(big.Int).Set(balance),
		WalletIndex:        walletIndex,
		Swapper:            swapper,
		Sold:               false,
		Approved:           false,
		TakeProfitDone:     false,
	}

	log.Printf("[Wallet %d] Stop-loss monitoring started for %s", walletIndex+1, tokenAddress.Hex())
	log.Printf("[Wallet %d] Token balance: %s, Initial value: %s wei", walletIndex+1, balance.String(), currentPrice.String())
}

func (m *StopLossMonitor) Start() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	log.Printf("Stop-loss monitor started (threshold: %d%%)", m.stopLossPercent)

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-ticker.C:
			m.checkPositions()
		}
	}
}

func (m *StopLossMonitor) checkPositions() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for key, pos := range m.positions {
		if pos.Sold {
			continue
		}

		balance, err := pos.Swapper.GetTokenBalance(pos.TokenAddress)
		if err != nil || balance.Cmp(big.NewInt(0)) <= 0 {
			continue
		}
		pos.TokenAmount = balance

		if !pos.TakeProfitDone {
			m.checkTakeProfit(pos)
		}

		currentPrice, err := pos.Swapper.GetTokenPrice(pos.TokenAddress, pos.TokenAmount)
		if err != nil {
			continue
		}

		dropPercent := m.calculateDropPercent(pos.BuyPriceWei, currentPrice)

		if dropPercent >= m.stopLossPercent {
			log.Printf("[Wallet %d] STOP-LOSS TRIGGERED! Token: %s, Drop: %d%%", pos.WalletIndex+1, pos.TokenAddress.Hex(), dropPercent)
			m.executeSell(pos, pos.TokenAmount)
			delete(m.positions, key)
		}
	}
}

func (m *StopLossMonitor) checkTakeProfit(pos *Position) {
	oneToken := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	if pos.TokenAmount.Cmp(oneToken) < 0 {
		oneToken = pos.TokenAmount
	}

	usdtAmount, err := pos.Swapper.GetTokenPriceInUSDT(pos.TokenAddress, oneToken)
	if err != nil {
		return
	}

	usdtFloat := new(big.Float).SetInt(usdtAmount)
	divisor := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	priceUSDT, _ := new(big.Float).Quo(usdtFloat, divisor).Float64()

	if priceUSDT >= TakeProfitPriceUSDT {
		log.Printf("[Wallet %d] TAKE-PROFIT TRIGGERED! Price: %.8f USDT >= %.8f USDT",
			pos.WalletIndex+1, priceUSDT, TakeProfitPriceUSDT)

		sellAmount := new(big.Int).Mul(pos.InitialTokenAmount, big.NewInt(TakeProfitSellPercent))
		sellAmount.Div(sellAmount, big.NewInt(100))

		if sellAmount.Cmp(pos.TokenAmount) > 0 {
			sellAmount = pos.TokenAmount
		}

		if sellAmount.Cmp(big.NewInt(0)) > 0 {
			m.executeSell(pos, sellAmount)
			log.Printf("[Wallet %d] Sold %d%% at %.8f USDT", pos.WalletIndex+1, TakeProfitSellPercent, priceUSDT)
		}

		pos.TakeProfitDone = true
	}
}

func (m *StopLossMonitor) calculateDropPercent(buyPrice, currentPrice *big.Int) int {
	if buyPrice.Cmp(big.NewInt(0)) == 0 {
		return 0
	}

	diff := new(big.Int).Sub(buyPrice, currentPrice)
	if diff.Cmp(big.NewInt(0)) <= 0 {
		return 0
	}

	percent := new(big.Int).Mul(diff, big.NewInt(100))
	percent.Div(percent, buyPrice)

	return int(percent.Int64())
}

func (m *StopLossMonitor) executeSell(pos *Position, amount *big.Int) {
	if !pos.Approved {
		log.Printf("[Wallet %d] Approving token for sale...", pos.WalletIndex+1)

		maxApprove := new(big.Int)
		maxApprove.SetString("115792089237316195423570985008687907853269984665640564039457584007913129639935", 10)

		approveTx, err := pos.Swapper.ApproveToken(pos.TokenAddress, maxApprove)
		if err != nil {
			log.Printf("[Wallet %d] Failed to approve: %v", pos.WalletIndex+1, err)
			return
		}
		log.Printf("[Wallet %d] Approve TX: %s", pos.WalletIndex+1, approveTx)
		pos.Approved = true

		time.Sleep(3 * time.Second)
	}

	log.Printf("[Wallet %d] Selling %s tokens...", pos.WalletIndex+1, amount.String())
	sellTx, err := pos.Swapper.SellToken(pos.TokenAddress, amount)
	if err != nil {
		log.Printf("[Wallet %d] Failed to sell: %v", pos.WalletIndex+1, err)
		return
	}

	log.Printf("[Wallet %d] SOLD! TX: %s", pos.WalletIndex+1, sellTx)
	log.Printf("[Wallet %d] BSCScan: https://bscscan.com/tx/%s", pos.WalletIndex+1, sellTx)
}

func (m *StopLossMonitor) Stop() {
	m.cancel()
}

func positionKey(walletIndex int, tokenAddress common.Address) string {
	return tokenAddress.Hex() + "-" + string(rune(walletIndex))
}
