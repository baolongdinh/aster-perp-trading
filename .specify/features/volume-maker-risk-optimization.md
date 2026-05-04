# Feature Specification: Volume Maker Risk Optimization & Fill Rate Maximization

## Overview

### Feature Description
Tối ưu hóa chiến lược Volume Farm Maker để farm volume + micro profit với vốn nhỏ + leverage cao (150x), đồng thời ngăn ngừa liquidation trong mọi điều kiện thị trường. Đặc biệt tập trung vào việc tăng FILL RATE (số lượng orders được fill) thay vì chỉ tăng số lượng orders.

**Nguyên tắc cốt lõi:** Với 150x leverage, liquidation xảy ra khi giá đi ngược 0.67%. Giải pháp: KHÔNG BAO GIỜ giữ position đủ lâu để giá di chuyển 0.67%. Target profit 0.05-0.1%, hold 10-30 giây.

**Nguyên tắc tăng fill:** Không phải nhiều orders hơn, mà là nhiều orders ĐƯỢC FILL hơn. Orders xa market KHÔNG BAO GIỜ fill - chỉ tốn margin.

### Business Value
- Ngăn ngừa liquidation hoàn toàn (bảo toàn vốn)
- Tăng fill rate gấp 2-3 lần (orders gần market)
- Farm volume liên tục với risk thấp nhất
- Tận dụng tối đa high leverage (150x) mà không bị liquidation
- Nhiều fills = nhiều maker rebate = nhiều profit hơn (compounding)
- Có chiến lược tiếp cận trong mọi điều kiện thị trường

## User Scenarios & Testing

### Scenario 1: Grid Trading với Active Zone (Tăng Fill)
**Given** Bot đang chạy với 150x leverage, spread thấp
**When** Giá dao động trong grid range
**Then** 
- Chỉ 5-10 orders gần market nhất được đặt (trong 0.1% range)
- Orders được fill liên tục vì gần market
- Micro profit được ghi nhận (0.05-0.1% = 7.5-15% margin ROI)
- Position không bị liquidation

### Scenario 2: Smaller Size, More Orders
**Given** Balance $10, leverage 150x
**When** Đặt orders
**Then** 
- Thay vì 1 order $100 → 5 orders $20
- 5 cơ hội fill thay vì 1
- Nhiều volume hơn, nhiều maker rebate hơn

### Scenario 3: Position Timeout (Ngăn Liquidation)
**Given** Position đang lỗ 0.5% (gần liquidation với 150x)
**When** Position timeout trigger (60 giây)
**Then** 
- Position được force close
- Không bao giờ chạm liquidation (0.67%)
- Bot regrid với grid mới

### Scenario 4: Trailing Stop (Khóa Profit)
**Given** Position đang lãi 0.1%
**When** Trailing stop activate
**Then** 
- Trail by 0.03% (sell nếu profit giảm từ peak)
- Không bao giờ give back > 0.03%
- Profit được khóa an toàn

### Scenario 5: Spread quá rộng
**Given** Spread > 0.1%
**When** Bot kiểm tra spread trước khi đặt order
**Then** 
- Tạm dừng đặt order mới
- Tiếp tục khi spread trở lại bình thường

### Scenario 6: Zone-Based Sizing (EMA Zones)
**Given** Giá cách EMA bao xa
**When** Xác định zone
**Then** 
- Above EMA: 0.5x size (đắt, mua ít)
- Normal dip (0% to -1%): 1.0x size
- Strong dip (-1% to -2%): 1.5x size
- Hard dip (<-2%): 0x size (crash protection - KHÔNG MUA)

### Scenario 7: Daily Reset
**Given** Hết ngày giao dịch
**When** Đến giờ close
**Then** 
- Close ALL positions
- Không overnight exposure
- Start fresh ngày mai

### Scenario 8: Bot restart
**Given** Bot bị restart hoặc crash
**When** Bot khởi động lại
**Then** 
- Verify tất cả positions từ exchange
- Verify tất cả open orders từ exchange
- Tiếp tục trading chỉ khi state hợp lệ

### Scenario 9: Emergency trigger
**Given** Position đạt 80% max limit
**When** Risk check chạy
**Then** 
- Block new orders
- Giữ nguyên existing positions
- Alert user

## Functional Requirements

### FR1: Active Zone Grid (Tăng Fill Rate)
**Acceptance Criteria:**
- Chỉ đặt orders trong 5-10 levels gần market nhất (trong 0.1% range từ giá hiện tại)
- Orders xa market (không có khả năng fill) KHÔNG được đặt
- Khi price di chuyển → regrid để orders mới vẫn gần market
- Grid spacing: 0.05-0.06% (gần với liquidation buffer 0.067% nhưng vẫn an toàn)

### FR2: Smaller Order Size, More Orders
**Acceptance Criteria:**
- Thay vì 1 order lớn → split thành nhiều orders nhỏ hơn
- Order size tối thiểu: $1-2 (đủ để có ý nghĩa với balance)
- Order size tối đa: $10 (để đảm bảo nhiều fill opportunities)
- Số lượng orders tăng theo balance (balance-based order count)

### FR3: Post-Only + Maker Rebate
**Acceptance Criteria:**
- Sử dụng post-only để tránh taker fees
- Nhận maker rebate trên mỗi fill
- Với 150x: 0.01% spread = 1.5% margin ROI per fill
- Tối ưu hóa cho maker rebate > taker fees

### FR4: Position Timeout (Ngăn Liquidation)
**Acceptance Criteria:**
- Mỗi position phải có timestamp khi được tạo
- Check timeout mỗi cycle (5s)
- Force close khi position tồn tại > 60 giây
- Log reason khi force close
- NEVER hold long enough for 0.67% adverse move

### FR5: Trailing Stop
**Acceptance Criteria:**
- Activate trailing tại +0.1% profit
- Trail by 0.03% (sell nếu profit giảm 0.03% từ peak)
- Never give back > 0.03% profit
- Configurable trailing parameters

### FR6: Max Position Limits + FIFO
**Acceptance Criteria:**
- Max 5 positions mỗi side (long/short)
- Khi exceed: close OLDEST position first (FIFO)
- Ngăn tích lũy quá nhiều exposure
- Prevents one-sided accumulation

### FR7: Zone-Based Sizing (EMA Zones)
**Acceptance Criteria:**
- Xác định zone dựa trên khoảng cách từ EMA
- Above EMA: 0.5x multiplier (đắt, mua ít)
- Normal dip (0% to -1%): 1.0x multiplier
- Strong dip (-1% to -2%): 1.5x multiplier (mua nhiều)
- Hard dip (<-2%): 0x multiplier (crash protection - KHÔNG MUA)

### FR8: Daily Reset
**Acceptance Criteria:**
- Close ALL positions trước khi hết ngày
- Không overnight exposure
- Configurable close time (default: 23:00 UTC)
- Start fresh mỗi ngày

### FR9: Spread Protection
**Acceptance Criteria:**
- Check spread trước khi đặt order mới
- Pause khi spread > 0.1%
- Auto resume khi spread < 0.1%
- Configurable spread threshold

### FR10: Startup Reconciliation
**Acceptance Criteria:**
- On startup: fetch all positions từ exchange
- On startup: fetch all open orders từ exchange
- Verify local state vs exchange state
- Block trading nếu state không khớp
- Log all discrepancies

### FR11: Emergency Trigger at 80%
**Acceptance Criteria:**
- Trigger emergency khi position >= 80% max limit
- Block new order placement
- Allow existing positions to close naturally
- Alert user qua log

### FR12: Regrid Cooldown
**Acceptance Criteria:**
- Sau khi regrid, cooldown 60 giây
- Khi order FILL → immediately place new order (không đợi full cycle)
- Continuous placement = continuous fills

### FR13: 10-Level Risk Management
**Acceptance Criteria:**
- Level 1: IM Rate >= 90% → Block workflow
- Level 2: High IM + profit → Close position
- Level 3: Trailing stop → Lock profit
- Level 4: Position > 40% limit → Block new orders
- Level 5: Position timeout 60s → Force close
- Level 6: Both sides profitable → Close both
- Level 7: TP safety check → Pause TP if insufficient margin
- Level 8: TP safety pre-check → Block protective orders
- Level 9: Rate limit → Max regrid per 48h
- Level 10: Emergency stop → Full stop, manual reset

### FR14: Margin Buffer
**Acceptance Criteria:**
- Chỉ sử dụng 50-75% equity cho grid
- Giữ 25-50% làm buffer
- Configurable equity ratio

### FR15: Balance-Based Order Count
**Acceptance Criteria:**
- Khi balance tăng → tăng số lượng orders, KHÔNG tăng order size
- $10 → 5 orders × $2
- $15 → 7 orders × $2
- Compounding effect: more fills → more profit → more orders → even more fills

## Success Criteria

1. **Zero Liquidation**: Không có position nào bị liquidation trong 30 ngày
2. **Fill Rate gấp 2-3**: > 2000-3000 fills/ngày (so với ~1000 trước đây)
3. **Micro Profit**: Mỗi fill đạt profit >= 0.05% (sau fees, = 7.5% margin ROI với 150x)
4. **Risk Control**: Emergency trigger không quá 3 lần/tuần
5. **Position Timeout**: 100% positions được close trước 60 giây
6. **Spread Protection**: 0 orders được đặt khi spread > 0.1%
7. **Startup Safety**: 0 lần trading với state không khớp sau restart
8. **Maker Rebate**: Tối đa hóa maker rebate từ nhiều fills
9. **Daily Reset**: 100% positions closed end of day

## Key Entities

- **Position**: Entry price, size, side, open time, unrealized PnL, trailing peak
- **Order**: Order ID, price, size, side, status, placed time, isPostOnly
- **GridConfig**: Grid levels (5-10), spacing (0.05-0.06%), active zone range (0.1%)
- **RiskState**: IM rate, position size, daily PnL, emergency status, position count
- **MarketState**: Spread, volatility, last price, volume, EMA distance, zone
- **TrailingState**: Peak profit, activation level, callback level

## Assumptions & Dependencies

- Exchange API cung cấp position và order data chính xác
- WebSocket connection ổn định cho real-time updates
- Leverage tối đa 150x được hỗ trợ
- Post-only order type được hỗ trợ bởi exchange
- Maker rebate available trên exchange

## Out of Scope

- Multi-symbol trading (chỉ single symbol)
- Complex market regime detection (dùng simple volatility + EMA zones)
- Kelly Criterion sizing
- TWAP/VWAP execution
- Telegram/Discord alerts
- Giảm leverage (vẫn giữ 150x)
