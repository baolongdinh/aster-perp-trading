# Volume Farming Bot Specification

## Feature Overview

Bot chuyên biệt để farm volume trading trên Aster Exchange với mục tiêu tối đa hóa điểm tích lũy (points) thông qua fee contribution và maker liquidity contribution, thay vì kiếm lợi nhuận từ price movements.

## User Stories

### As a volume farmer, I want to:
- **US1**: Tự động đặt lệnh maker-only quanh giá mid-price để tối đa hóa số lần filled
- **US2**: Quản lý rủi ro loss từ fee trading với các ngưỡng an toàn configurable
- **US3**: Farm volume trên các cặp có quote currency linh hoạt (USDT, USD1, hoặc all) với spread thấp nhất
- **US4**: Chạy bot farming volume riêng biệt để không ảnh hưởng trading bot chính
- **US5**: Monitor real-time volume farming performance và points accumulation

## Functional Requirements

### FR1: Volume Farming Strategy Engine
- **FR1.1**: Implement tight grid maker-only strategy với configurable spread
- **FR1.2**: Auto-replace filled orders immediately on opposite side at new mid-price
- **FR1.3**: Support multiple symbols simultaneously với independent grid management
- **FR1.4**: Real-time mid-price calculation từ orderbook data

### FR2: Symbol Selection & Management
- **FR2.1**: Auto-discover symbols với flexible quote currency (USDT, USD1, hoặc all symbols) và lowest spread
- **FR2.2**: Configurable symbol whitelist/blacklist và quote currency filters
- **FR2.3**: Dynamic symbol addition/removal based on spread thresholds
- **FR2.4**: Prioritize boosted symbols cho higher point accumulation
- **FR2.5**: Advanced filtering algorithm để chọn optimal symbols per quote currency
- **FR2.6**: Real-time spread monitoring và volatility analysis
- **FR2.7**: Liquidity scoring system để balance volume vs spread optimization

### FR3: Risk Management System
- **FR3.1**: Maximum daily loss threshold (configurable per symbol)
- **FR3.2**: Maximum total unrealized PnL drawdown limit
- **FR3.3**: Auto-stop functionality khi vượt rủi ro cho phép
- **FR3.4**: Real-time fee tracking và cost analysis

### FR4: Points & Performance Tracking
- **FR4.1**: Track fee contribution points (taker: 1 nguồn, maker: 2 nguồn)
- **FR4.2**: Calculate points per dollar spent efficiency metrics
- **FR4.3**: Real-time dashboard showing farming performance
- **FR4.4**: Historical performance reports và optimization suggestions

### FR5: Separate Service Architecture
- **FR5.1**: Independent command/process from main trading bot
- **FR5.2**: Separate configuration file và database
- **FR5.3**: Independent API endpoints cho farming management
- **FR5.4**: Isolated risk management từ main bot

## Configuration Requirements

### Core Farming Settings
```yaml
volume_farming:
  enabled: true
  max_daily_loss_usdt: 50  # Stop loss nếu thua > $50/ngày
  max_total_drawdown_pct: 5.0  # Stop nếu drawdown > 5%
  order_size_usdt: 100  # Size mỗi lệnh
  grid_spread_pct: 0.05  # 0.05% từ mid-price
  max_orders_per_side: 2  # Max 2 buy + 2 sell per symbol
```

### Symbol Management
```yaml
symbols:
  auto_discover: true
  quote_currency_mode: "flexible"  # "USDT" | "USD1" | "flexible" | "all"
  min_volume_24h: 10000000  # Minimum $10M daily volume
  max_spread_pct: 0.1  # Maximum spread 0.1%
  boosted_only: false  # Prioritize boosted symbols
  whitelist: ["BTCUSDT", "ETHUSDT"]  # Optional whitelist
  quote_currencies: ["USDT", "USD1"]  # Available quote currencies
  allow_mixed_quotes: true  # Allow mixing USDT and USD1 pairs
  
  # Advanced filtering for optimal pairs
  max_symbols_per_quote: 10  # Max 10 symbols per quote currency
  spread_ranking: true  # Rank by lowest spread first
  volume_weighting: 0.7  # 70% weight to volume, 30% to spread
  min_liquidity_score: 0.5  # Minimum liquidity score (0-1)
  
  # Spread optimization
  optimal_spread_range: [0.01, 0.05]  # Prefer 0.01% - 0.05% spread
  spread_volatility_threshold: 0.02  # Max spread change 0.02% in 5min
  exclude_high_fee_symbols: true  # Exclude symbols with unusual fee structures
```

### Risk Controls
```yaml
risk:
  max_position_usdt_per_symbol: 500
  max_total_positions_usdt: 2000
  fee_loss_threshold_pct: 0.1  # Stop nếu fee > 0.1% volume
  position_timeout_minutes: 30  # Close positions sau 30 phút
```

## Success Criteria

### Performance Metrics
- **SC1**: Achieve minimum $100,000 daily trading volume per bot instance
- **SC2**: Maintain average order fill rate > 85% within 5 minutes
- **SC3**: Keep fee costs below 0.05% of total volume traded
- **SC4**: Generate minimum 1000 points per $1000 volume traded

### Reliability & Safety
- **SC5**: Zero unauthorized position exposure beyond configured limits
- **SC6**: 99.9% uptime during market hours with automatic recovery
- **SC7**: Response time < 100ms for order placement/modification
- **SC8**: Complete audit trail of all farming activities

### Operational Efficiency
- **SC9**: Manual intervention required < 1 time per week
- **SC10**: Configuration changes applied without restart < 10 seconds
- **SC11**: Real-time performance dashboard updates < 1 second latency
- **SC12**: Complete state recovery within 30 seconds after restart

## Key Entities

### FarmingBot
- bot_id, status, start_time, total_volume, total_points
- daily_loss, max_drawdown, risk_limits
- configuration, performance_metrics

### SymbolConfig
- symbol, enabled, quote_currency, current_spread
- min_volume, max_spread, is_boosted
- grid_settings, risk_limits
- quote_currency_group (USDT/USD1/mixed)
- liquidity_score, spread_volatility, efficiency_ranking
- last_updated, selection_priority

### FarmingOrder
- order_id, symbol, side, size, price
- order_type (LIMIT), time_in_force (GTC)
- fill_time, fee_paid, points_earned

### PerformanceMetrics
- timestamp, symbol, volume_24h, fill_rate
- points_per_dollar, efficiency_score
- fee_costs, net_pnl, risk_metrics

## Acceptance Criteria

### AC1: Strategy Implementation
- [ ] Tight grid places orders at exactly mid_price ± configured_spread
- [ ] Filled orders replaced immediately on opposite side within 1 second
- [ ] Grid maintains configured maximum orders per side
- [ ] Orders cancelled automatically if price moves > 2x spread

### AC2: Symbol Management
- [ ] Auto-discovery scans exchange every 5 minutes for new symbols
- [ ] Symbols filtered by volume, spread, and flexible quote currency criteria
- [ ] Multiple quote currency support (USDT, USD1, or all)
- [ ] Mixed quote currency pairs allowed when configured
- [ ] Advanced ranking algorithm selects top N symbols per quote currency
- [ ] Real-time spread volatility monitoring and symbol exclusion
- [ ] Liquidity scoring balances volume vs spread optimization
- [ ] Boosted symbols prioritized in allocation decisions
- [ ] Manual symbol overrides respected in configuration

### AC3: Risk Controls
- [ ] Daily loss tracked accurately including all fees
- [ ] Bot stops immediately when any risk limit breached
- [ ] Position timeout enforced with automatic closure
- [ ] Unrealized PnL calculated in real-time

### AC4: Performance Tracking
- [ ] Points calculated correctly (maker: 2x, taker: 1x)
- [ ] Efficiency metrics updated every trade
- [ ] Dashboard displays real-time performance data
- [ ] Historical reports generated accurately

### AC5: Service Isolation
- [ ] Volume farming bot runs as separate process
- [ ] Independent configuration from main trading bot
- [ ] Separate database schema for farming data
- [ ] Isolated API endpoints for farming management

## Assumptions

1. **Exchange Infrastructure**: Aster Exchange provides real-time orderbook and trade data via WebSocket
2. **Fee Structure**: Maker orders receive 2x points compared to taker orders
3. **Point System**: Points are calculated based on fee contribution and liquidity provision
4. **Market Conditions**: Sufficient liquidity exists in selected symbols for consistent fills
5. **API Limits**: Rate limits allow for high-frequency order modifications
6. **Quote Currency Flexibility**: Exchange supports multiple quote currencies (USDT, USD1, etc.)

## Dependencies

### External Dependencies
- **Aster Exchange API**: Real-time market data and order management
- **WebSocket Streams**: Orderbook and trade data feeds
- **Database**: PostgreSQL for persistent storage
- **Configuration Service**: YAML-based configuration management

### Internal Dependencies
- **Authentication Module**: Shared EIP-712 signing infrastructure
- **Risk Management Library**: Common risk calculation utilities
- **Logging Framework**: Structured logging for monitoring
- **Metrics Collection**: Performance and health monitoring

## Constraints

### Technical Constraints
- Must use existing EIP-712 V3 authentication infrastructure
- Cannot interfere with main trading bot operations
- Must maintain sub-100ms response times for order operations
- Limited to maker-only orders (no market orders)

### Business Constraints
- Maximum daily loss cannot exceed configured thresholds
- Must comply with exchange rate limits and API restrictions
- Points calculation based on official exchange formulas
- No leverage usage for volume farming strategies

### Operational Constraints
- Requires 24/7 operation during market hours
- Manual override capabilities for emergency situations
- Configuration changes without service restart
- Complete audit trail for compliance purposes

## Testing Requirements

### Unit Tests
- Strategy logic and grid placement algorithms
- Risk management calculations and limit enforcement
- Points calculation and efficiency metrics
- Configuration validation and parsing

### Integration Tests
- Exchange API connectivity and order management
- WebSocket data feed processing
- Database operations and persistence
- Risk limit enforcement across components

### Performance Tests
- Load testing with maximum configured symbols
- Stress testing under high-volume conditions
- Latency measurement for order operations
- Memory and resource usage monitoring

### End-to-End Tests
- Complete farming workflow simulation
- Risk limit breach scenarios
- Configuration change propagation
- Recovery and restart procedures

## Implementation Notes

This specification focuses on volume farming as a separate concern from profit-oriented trading. The bot prioritizes point accumulation through consistent market making activity while maintaining strict risk controls to prevent significant losses from fee costs and adverse price movements.

The tight grid strategy ensures maximum order fill rates while the maker-only approach maximizes point efficiency. Separate service architecture prevents interference with existing trading strategies while allowing independent optimization and risk management.
