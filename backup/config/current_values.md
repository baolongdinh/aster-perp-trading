# Current Config Values - Pre-Optimization

## Volume Farm Config (volume-farm-config.yaml)
- order_size_usdt: 20.5
- grid_spread_pct: 0.0015 (0.15%)
- max_orders_per_side: 10
- max_global_pending_limit_orders: 30
- position_timeout_minutes: 5
- per_trade_take_profit_pct: 0.3
- per_trade_stop_loss_pct: 0.5
- max_position_usdt_per_symbol: 300
- max_total_positions_usdt: 900

## Adaptive Config (adaptive_config.yaml)
### Ranging
- order_size_usdt: 2.0
- grid_spread_pct: 0.02 (0.02%)
- max_orders_per_side: 10

### Trending
- order_size_usdt: 1.0
- grid_spread_pct: 0.15 (0.15%)
- max_orders_per_side: 2

### Volatile
- order_size_usdt: 0.5
- grid_spread_pct: 0.1 (0.1%)
- max_orders_per_side: 4

## Trading Hours (trading_hours.yaml)
- Mode: select
- Total trading hours: 20h/day
- Asian session (07:00-12:00): 1.0x size
- European session (13:00-18:00): 0.7x size
- US session (19:00-23:00): 0.3x size
- Overnight (01:00-07:00): 0.8x size

## Backup Timestamp
Created: $(date)
