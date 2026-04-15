# Quick Start Guide: Micro Profit + Volume Farming

## Overview

The micro profit feature adds automatic take profit order placement to the grid trading bot. When a grid order is filled and a position is opened, the bot immediately places a take profit order at a small spread (default 0.005%) to capture micro profit. Once the take profit order is filled, the bot automatically rebalances the grid to continue volume farming.

## Prerequisites

- Bot version 2.0 or higher
- Exchange API with ReduceOnly order support
- Existing grid trading configuration

## Installation

### 1. Enable Feature

Edit `config/micro_profit.yaml`:

```yaml
micro_profit:
  enabled: true
  spread_pct: 0.005
  timeout_seconds: 30
  min_profit_usdt: 0.01
```

### 2. Restart Bot

```bash
# Stop bot
pkill -f bot

# Start bot
./bot
```

Or if using systemd:

```bash
sudo systemctl restart bot
```

### 3. Verify Feature Enabled

Check logs for:

```
Micro profit feature enabled
Take profit manager initialized
```

Check dashboard for micro profit metrics section.

## Configuration

### Basic Configuration

Edit `config/micro_profit.yaml`:

```yaml
micro_profit:
  enabled: true                    # Enable/disable feature
  spread_pct: 0.005                # Take profit spread (0.005%)
  timeout_seconds: 30              # Timeout duration (30 seconds)
  min_profit_usdt: 0.01            # Minimum profit ($0.01)
```

### Symbol-Specific Configuration

```yaml
micro_profit:
  enabled: true
  spread_pct: 0.005
  timeout_seconds: 30
  min_profit_usdt: 0.01
  symbols:
    BTCUSD1:
      enabled: true
      spread_pct: 0.006
      timeout_seconds: 45
    ETHUSD1:
      enabled: false
```

### Configuration Parameters

| Parameter | Type | Range | Default | Description |
|-----------|------|-------|---------|-------------|
| enabled | boolean | true/false | false | Enable/disable feature |
| spread_pct | float | 0.001-0.01 | 0.005 | Take profit spread percentage |
| timeout_seconds | integer | 10-300 | 30 | Timeout in seconds |
| min_profit_usdt | float | 0.001-1.0 | 0.01 | Minimum profit threshold |

## Testing

### 1. Manual Test

Place a test grid order and verify:

1. Grid order filled
2. Take profit order placed (check logs)
3. Take profit order filled (check logs)
4. Profit recorded (check dashboard)
5. Grid rebalanced (check logs)

### 2. Automated Test

Run integration test:

```bash
go test ./internal/farming/adaptive_grid -run TestMicroProfitIntegration
```

### 3. Simulation Test

Run simulation test with historical data:

```bash
go test ./internal/farming/adaptive_grid -run TestMicroProfitSimulation
```

## Monitoring

### Dashboard Metrics

Check dashboard for:

- **Total Micro Profit**: Cumulative profit from take profit orders
- **Take Profit Success Rate**: Percentage of take profit orders filled
- **Average Position Holding Time**: Average time position held before take profit
- **Timeout Rate**: Percentage of take profit orders that timed out

### Log Monitoring

Key log messages:

```
Take profit order placed: symbol=BTCUSD1, side=SELL, price=1000.05
Take profit order filled: symbol=BTCUSD1, profit=0.05
Take profit order timeout: symbol=BTCUSD1, closing position
```

### Alert Thresholds

Configure alerts for:

- Take profit success rate < 50%
- Timeout rate > 30%
- Average holding time > 60 seconds

## Troubleshooting

### Issue: Take Profit Orders Not Placed

**Symptoms:**
- Grid orders filled but no take profit orders in logs
- Dashboard shows 0 take profit orders

**Solutions:**
1. Check if feature is enabled in config
2. Check if config file exists and is valid
3. Check logs for configuration errors
4. Verify exchange API supports ReduceOnly orders
5. Check if min_profit_usdt threshold is too high

### Issue: High Timeout Rate

**Symptoms:**
- Timeout rate > 30%
- Many positions closed by market order

**Solutions:**
1. Increase timeout_seconds (e.g., 30 → 60)
2. Increase spread_pct (e.g., 0.005 → 0.008)
3. Check if market conditions are volatile
4. Consider disabling feature during high volatility

### Issue: Low Success Rate

**Symptoms:**
- Take profit success rate < 50%
- Many take profit orders not filled

**Solutions:**
1. Increase spread_pct (e.g., 0.005 → 0.008)
2. Decrease timeout_seconds (e.g., 30 → 15)
3. Check if spread is too tight for market conditions
4. Verify take profit orders are being placed correctly

### Issue: Configuration Not Hot-Reloading

**Symptoms:**
- Changed config but bot not using new values
- Old configuration still in effect

**Solutions:**
1. Verify file watcher is running
2. Check if config file has syntax errors
3. Check logs for reload errors
4. Restart bot if hot-reload fails

### Issue: Take Profit Order Placement Fails

**Symptoms:**
- Error logs when placing take profit orders
- Take profit placement skipped

**Solutions:**
1. Check exchange API rate limits
2. Verify sufficient balance for take profit order
3. Check if ReduceOnly flag is supported
4. Verify order size meets exchange minimums

## Best Practices

### 1. Start Conservative

Begin with conservative settings:
```yaml
spread_pct: 0.008
timeout_seconds: 60
min_profit_usdt: 0.02
```

Monitor for 1 week, then adjust based on results.

### 2. Monitor Metrics Closely

Check dashboard daily for:
- Take profit success rate
- Timeout rate
- Average holding time
- Total micro profit

### 3. Adjust for Market Conditions

- Low volatility: Tighter spread (0.003-0.005)
- High volatility: Wider spread (0.006-0.010)
- Ranging market: Shorter timeout (15-30s)
- Trending market: Longer timeout (45-60s)

### 4. Use Symbol-Specific Config

Different symbols may need different settings:
- High volume symbols: Tighter spread
- Low volume symbols: Wider spread
- Volatile symbols: Wider spread, longer timeout

### 5. Feature Flag for Safety

Keep feature disabled by default. Enable gradually:
1. Test in staging environment
2. Enable for 1 symbol in production
3. Monitor for 1 week
4. Enable for all symbols if successful

## Performance Optimization

### 1. Reduce Latency

Ensure take profit orders are placed quickly:
- Use low-latency exchange API
- Optimize order placement code
- Monitor placement time (< 100ms target)

### 2. Efficient Tracking

Use efficient data structures:
- In-memory maps for fast lookup
- Cleanup old entries periodically
- Avoid blocking operations

### 3. Batch Operations

Process multiple take profit orders in batches:
- Batch fill status checks
- Batch timeout checks
- Batch profit recording

## Security Considerations

### 1. Validate Configuration

Always validate config values:
- Check ranges
- Check types
- Reject invalid values

### 2. Limit Exposure

Ensure take profit orders respect risk limits:
- Check position limits
- Check CircuitBreaker status
- Check exposure limits

### 3. Error Handling

Handle errors gracefully:
- Continue without take profit if placement fails
- Log all errors
- Alert on critical errors

## Support

### Documentation

- [Feature Specification](../spec.md)
- [Implementation Plan](../implementation.md)
- [Data Model](../tech-specs/data-model.md)

### Contact

For issues or questions:
- Check logs first
- Review troubleshooting section
- Contact development team

### Known Limitations

- Take profit orders not persisted across restarts
- Partial fills not supported (assumes full fill or timeout)
- No dynamic spread adjustment (fixed spread only)
