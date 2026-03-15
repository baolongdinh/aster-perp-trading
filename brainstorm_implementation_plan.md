# Aster Perp Trading Bot — Brainstorm & Implementation Plan

## Context Summary

| Item | Detail |
|------|--------|
| Exchange | Aster Finance (Futures perp) |
| API Version | **V3** (EIP-712 wallet signing, on-chain traceability, Noop cancel) |
| Auth | `user` (main wallet) + `signer` (API wallet) + `nonce` (µs) + ECDSA sig |
| Base URLs | REST `https://fapi.asterdex.com`, WS `wss://fstream.asterdex.com` |
| Skills available | auth-v3, trading-v3, account-v3, market-data-v3, websocket-v3, errors-v3 |
| Language | **Go** (backend bot) + **Next.js / React** (frontend dashboard) |

---

## 1. Brainstorm — Core Ideas

### 1.1 What the bot needs to do (from requirements + API skills)

1. **Authenticate** every request using EIP-712 signing (V3): build nonce from µs clock, sort params, Keccak-hash, sign with signer private key.
2. **Consume real-time market data** via WebSocket: `aggTrade`, `markPrice`, `kline_*`, `bookTicker`, `depth` streams.
3. **Maintain user data stream** (listenKey keepalive every 30 min) to receive `ORDER_TRADE_UPDATE`, `ACCOUNT_UPDATE`, `MARGIN_CALL`.
4. **Execute trading strategies** (pluggable): decide entry/exit, size, leverage, SL/TP.
5. **Manage position lifecycle**: open, manage, close, handle errors (rate-limit 429, signature errors -1022).
6. **Risk management**: per-symbol max position, drawdown limits, daily loss cutoff.
7. **Log & observability**: structured logs, trade history, P&L tracking.
8. **Control surface**: REST API (backend) + WebSocket push → used by Frontend dashboard.
9. **Config at runtime**: strategies, symbols, leverage, sizes — without restarting.

### 1.2 Strategy Ideas

| Strategy | Description | Signals |
|----------|-------------|---------|
| **Trend Following (EMA cross)** | Fast EMA crosses slow EMA → enter in trend direction | Kline WS + EMA calc |
| **Funding Rate Arb** | Long/Short based on extreme funding rates (funding fee income) | `GET /fapi/v3/fundingInfo` + `premiumIndex` |
| **Grid Trading (perp)** | Place buy/sell grid around current price, profit from oscillation | Depth + markPrice |
| **Breakout (Bollinger)** | Enter when price breaks Bollinger Band, SL inside band | Kline OHLCV |
| **Liquidation Hunting** | Watch `forceOrder` stream for large liquidations as momentum signal | WS `!forceOrder@arr` |

### 1.3 Key Architecture Decisions

- **Go backend** — goroutine-per-symbol strategies, channels for market data fan-out, graceful shutdown, testable.
- **Next.js frontend** — dashboard, config management, live P&L chart.
- **SQLite (or Postgres)** — store trades, orders, snapshots, config.
- **WebSocket bridge** — backend exposes WS endpoint that pushes live events to FE.
- **Config-driven strategies** — YAML/JSON config, hot-reloadable.
- **V3 API only** — stronger security, replay protection, future on-chain traceability.

---

## 2. Project Folder Structure

```
aster-bot-perp-trading-2/
├── backend/                    ← Go trading bot + REST/WS API
│   ├── cmd/
│   │   └── bot/
│   │       └── main.go         ← entrypoint
│   ├── internal/
│   │   ├── auth/               ← EIP-712 signer module
│   │   │   ├── signer.go       ← nonce, param sort, Keccak, ECDSA sign
│   │   │   └── signer_test.go
│   │   ├── client/             ← Aster REST API client (rate-limited)
│   │   │   ├── futures.go      ← typed wrappers (PlaceOrder, CancelOrder, etc.)
│   │   │   └── market.go       ← public endpoints (klines, depth, ticker)
│   │   ├── stream/             ← WebSocket client
│   │   │   ├── market_stream.go   ← market data streams (aggTrade, kline, depth)
│   │   │   └── user_stream.go     ← listenKey, keepalive, user events
│   │   ├── strategy/           ← pluggable strategies
│   │   │   ├── interface.go    ← Strategy interface
│   │   │   ├── ema_cross.go    ← EMA crossover impl
│   │   │   ├── funding_arb.go  ← funding rate arb impl
│   │   │   └── grid.go         ← grid trading impl
│   │   ├── engine/             ← bot core
│   │   │   ├── engine.go       ← orchestrator: loads strategies, manages lifecycle
│   │   │   └── position_manager.go ← track open positions
│   │   ├── risk/               ← risk management
│   │   │   └── risk_manager.go ← max drawdown, daily loss, position size limits
│   │   ├── ordermanager/       ← order state machine
│   │   │   └── order_manager.go  ← local order book, reconcile with exchange
│   │   ├── store/              ← persistence (SQLite or Postgres)
│   │   │   ├── db.go
│   │   │   ├── trade_repo.go
│   │   │   └── config_repo.go
│   │   ├── api/                ← HTTP REST + WS server for FE
│   │   │   ├── server.go
│   │   │   ├── routes.go
│   │   │   ├── handlers/
│   │   │   │   ├── status.go
│   │   │   │   ├── strategy.go
│   │   │   │   ├── trades.go
│   │   │   │   └── config.go
│   │   │   └── ws_hub.go       ← push live events to FE WebSocket clients
│   │   └── config/             ← config loader
│   │       ├── config.go       ← struct + YAML/env loader
│   │       └── validator.go
│   ├── go.mod
│   ├── go.sum
│   └── .env.example            ← ASTER_USER, ASTER_SIGNER, ASTER_SIGNER_KEY, etc.
│
├── frontend/                   ← Next.js dashboard
│   ├── app/
│   │   ├── page.tsx            ← Dashboard home (P&L, account overview)
│   │   ├── strategies/
│   │   │   └── page.tsx        ← Strategy list, enable/disable, params
│   │   ├── trades/
│   │   │   └── page.tsx        ← Trade history table
│   │   ├── config/
│   │   │   └── page.tsx        ← Bot global config (symbols, risk limits)
│   │   └── layout.tsx
│   ├── components/
│   │   ├── PnLChart.tsx        ← Real-time P&L chart (recharts)
│   │   ├── PositionTable.tsx   ← Live positions with unrealized PnL
│   │   ├── OrderBook.tsx       ← Optional: live order book visual
│   │   ├── StrategyCard.tsx    ← Per-strategy config card
│   │   └── LiveFeed.tsx        ← WebSocket event feed
│   ├── lib/
│   │   ├── api.ts              ← REST API calls to backend
│   │   └── ws.ts               ← WebSocket hook
│   ├── package.json
│   └── .env.local.example      ← NEXT_PUBLIC_API_URL
│
├── api-docs/                   ← Existing Aster API docs (keep as-is)
├── skills/                     ← Existing MCP skills (keep as-is)
├── docker-compose.yml          ← bot + FE + db orchestration
└── README.md
```

---

## 3. Backend Modules — Detailed Plan

### 3.1 Auth Engine (`internal/auth`)

The most critical module. Implements V3 EIP-712 signing:

```
nonce = time.UnixMicro()
params = all business params + user + signer + nonce
sorted = sort by ASCII key
paramStr = "key=value&key=value&..."
domain = { name: "AsterSignTransaction", version: "1", chainId: 1666, verifyingContract: "0x000...000" }
msg = EIP-712 typed data hash(domain, { Message: { msg: paramStr } })
sig = ECDSA.sign(msg, signerPrivKey)
```

Uses `go-ethereum` (`crypto` package) for Keccak256 + ECDSA.

### 3.2 REST Client (`internal/client`)

- Rate-limit aware (429 detection → backoff, 418 → full ban handling per errors-v3 skill)
- All signed endpoints auto-inject auth params
- Typed request/response structs per endpoint
- Retry with exponential backoff

### 3.3 WebSocket Stream Manager (`internal/stream`)

- **Market Stream**: subscribes to configured symbols × streams; fan-out via channels to strategies
- **User Data Stream**: manages `listenKey` lifecycle (create on start, PUT every 30 min, reconnect on `listenKeyExpired`)
- Reconnect on disconnect with exponential backoff

### 3.4 Strategy Engine (`internal/strategy`)

Interface:
```go
type Strategy interface {
    Name() string
    OnKline(symbol string, k Kline)
    OnMarkPrice(symbol string, mp MarkPrice)
    OnOrderUpdate(update OrderUpdate)
    OnAccountUpdate(update AccountUpdate)
    ShouldEnter(symbol string) *Signal    // returns nil if no signal
    ShouldExit(symbol string, pos Position) *Signal
}
```

Each strategy runs in its own goroutine, receives events from market stream dispatcher.

### 3.5 Risk Manager (`internal/risk`)

- Max position size per symbol (USDT notional)
- Max open positions count
- Daily loss limit (auto-pause on breach)
- Per-trade max loss %
- Position mode: One-Way vs Hedge (config-driven, uses `POST /fapi/v3/positionSide/dual`)

### 3.6 Order Manager (`internal/ordermanager`)

- Local shadow of open orders (reconciled with `GET /fapi/v3/openOrders` on start)
- Handles `ORDER_TRADE_UPDATE` events to keep state current
- SL/TP placement after entry fill
- Noop cancel support (V3 feature — faster cancel)

### 3.7 Internal API for Frontend (`internal/api`)

REST endpoints:
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/status` | Bot health, uptime, account summary |
| GET | `/api/v1/positions` | Current open positions |
| GET | `/api/v1/orders` | Open orders |
| GET | `/api/v1/trades` | Trade history (paginated) |
| GET | `/api/v1/strategies` | List strategies + status |
| POST | `/api/v1/strategies/:name/enable` | Enable strategy |
| POST | `/api/v1/strategies/:name/disable` | Disable strategy |
| GET | `/api/v1/config` | Current config |
| PUT | `/api/v1/config` | Update config (hot reload) |
| GET | `/api/v1/metrics` | P&L, win rate, drawdown |

WebSocket: `ws://localhost:8080/ws` — push events:
- `position_update`, `order_update`, `trade_filled`, `pnl_snapshot`, `risk_alert`

---

## 4. Frontend Dashboard — Detailed Plan

### Pages

| Page | Key Features |
|------|-------------|
| **Dashboard** | Account equity, unrealized P&L card, daily P&L chart, live positions table, recent fills |
| **Strategies** | Cards per strategy: enabled toggle, param editor (leverage, size, symbols), last signal |
| **Trades** | Paginated trade history, filters by symbol/strategy/date, export CSV |
| **Config** | Global settings: risk limits, symbols list, API connectivity test, bot start/stop |

### Tech Stack

- **Next.js 14** (App Router)
- **Recharts** or **TradingView Lightweight Charts** for P&L chart
- **Tailwind CSS** for styling (or Vanilla CSS)
- **SWR** for REST polling + native WebSocket for live push
- **shadcn/ui** components

---

## 5. Config Schema (YAML)

```yaml
bot:
  dry_run: false           # true = simulate, don't send real orders
  log_level: info

exchange:
  user_wallet: "0x..."    # from env: ASTER_USER
  signer_wallet: "0x..."  # from env: ASTER_SIGNER
  # signer_private_key: never in config file, only env: ASTER_SIGNER_KEY

risk:
  max_position_usdt: 500
  max_open_positions: 3
  daily_loss_limit_usdt: 100
  per_trade_stop_loss_pct: 2.0
  position_mode: one_way   # or hedge

strategies:
  - name: ema_cross
    enabled: true
    symbols: [BTCUSDT, ETHUSDT]
    params:
      fast_period: 9
      slow_period: 21
      leverage: 5
      order_size_usdt: 50
      timeframe: 5m

  - name: funding_arb
    enabled: false
    symbols: [BTCUSDT]
    params:
      threshold_pct: 0.05   # enter if funding > 0.05%
      leverage: 3
      order_size_usdt: 100
```

---

## 6. Phased Delivery Roadmap

### Phase 1 — Foundation (Week 1-2)
- [ ] `backend/` Go module scaffold
- [ ] Auth engine (EIP-712 V3 signer) + unit tests
- [ ] REST client (market data + signed endpoints) + integration test (testnet)
- [ ] Config loader (YAML + env)

### Phase 2 — Market Connectivity (Week 2-3)
- [ ] WebSocket market stream manager (kline, markPrice, depth)
- [ ] User data stream (listenKey lifecycle, ORDER_TRADE_UPDATE, ACCOUNT_UPDATE)
- [ ] SQLite store for trades + config

### Phase 3 — Core Bot Logic (Week 3-4)
- [ ] Strategy interface + EMA cross strategy (first impl)
- [ ] Order manager (place, cancel, SL/TP, reconcile)
- [ ] Risk manager
- [ ] Engine orchestrator (strategy runner goroutines)

### Phase 4 — Internal API + Frontend (Week 4-5)
- [ ] Backend REST + WS API for FE (`internal/api`)
- [ ] Next.js project scaffold
- [ ] Dashboard page (P&L, positions)
- [ ] Strategies config page
- [ ] Trades history page

### Phase 5 — Strategies + Polish (Week 5-6)
- [ ] Funding rate arb strategy
- [ ] Grid strategy
- [ ] Dry-run mode (simulate orders)
- [ ] Docker Compose for local dev
- [ ] Testnet end-to-end test

---

## 7. Key Technical Decisions

| Decision | Choice | Reason |
|----------|--------|--------|
| API Version | V3 | On-chain traceability, Noop cancel, replay protection |
| Language | Go | Goroutines for concurrency, fast, strong stdlib, good for trading bots |
| Frontend | Next.js | Full-stack capability, good ecosystem, easy API integration |
| DB | SQLite first, Postgres later | Simple local dev, easy migration |
| Private key handling | Env var only, zeroed after sign | Match security model from skills audit |
| Dry-run | Built-in flag | Safe development without real orders |
| Position mode | Configurable (one_way/hedge) | V3 supports both, hedge gives more control |

## 8. Security Notes (from existing audit)

- **Signer private key**: env var only (`ASTER_SIGNER_KEY`), zeroed from memory after each signing operation, never logged.
- **chainId 1666**: EIP-712 signing domain only (not Harmony mainnet), per SEC-08.
- **Rate limits**: 429 → backoff, 418 → full ban, per errors-v3 skill. Never hammer.
- **Dry-run mode**: default `true` initially to prevent accidental real orders during dev.
- **API key registration**: follow [aster-api-key-registration.md](file:///c:/CODE/GOLANG/TRADE/aster-bot-perp-trading-2/api-docs/aster-api-key-registration.md) workflow to create signer wallet.
