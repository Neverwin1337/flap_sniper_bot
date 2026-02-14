# Flap Sniper Bot

BSC 鏈上自動狙擊買入機器人，監聯內盤代幣上線事件並自動買入。

## 收益展示

**7 日已實現盈虧：+$2.17K (+23.54%)**
<img width="1909" height="764" alt="image" src="https://github.com/user-attachments/assets/a72f9171-f911-4603-8ad7-b598ea9556fe" />
查看實時收益：[Binance Web3 Portfolio](https://web3.binance.com/zh-CN/portfolio/bsc/0x18edc15024df2881d261310f972a75eddbe65a35)

## 功能

- **事件監聽**：監聽 `LiquidityAdded` 事件，偵測新代幣上線
- **TaxToken 過濾**：只買入 TaxToken 類型的代幣
- **多錢包支援**：支援多個錢包同時狙擊
- **止損**：當價格下跌超過設定百分比時自動賣出
- **止盈**：當單個代幣價格達到 0.0002 USDT 時自動賣出 70%

## 配置

在 `.env` 文件中設定：

```env
BSC_RPC_URL=wss://your-websocket-rpc
BSC_RPC_HTTP=https://your-http-rpc
CONTRACT_ADDRESS=0x5c952063c7fc8610FFDB798152D69F0B9550762b
PRIVATE_KEYS=key1,key2
BUY_AMOUNTS_BNB=0.1,0.1
SLIPPAGE=10
GAS_LIMIT=300000
GAS_PRICE_GWEI=5
ENABLE_STOP_LOSS=true
STOP_LOSS_PERCENT=20
```

## 運行

```bash
go build -o flap.exe
./flap.exe
```

## 注意事項

- 請確保錢包有足夠的 BNB 用於買入和 Gas 費用
- 私鑰請妥善保管，不要外洩
