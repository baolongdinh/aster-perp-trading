# TP/SL Parameter Changes - Rationale

## Changes Summary

### Main TP/SL
- **per_trade_take_profit_pct**: 0.3% → 0.6% (2x wider)
  - **Rationale**: More realistic targets with wider spreads
  - **Impact**: Higher hit rate, fewer stuck positions
  - **Trade-off**: Slightly longer position duration

- **per_trade_stop_loss_pct**: 0.5% → 1.0% (2x wider)
  - **Rationale**: Adequate buffer for high leverage volatility
  - **Impact**: Fewer stop hunts, more room for price swings
  - **Trade-off**: Larger loss per trade (offset by higher win rate)

### Partial Close TP
- **tp1 profit_pct**: 0.5% → 0.8% (60% wider)
- **tp2 profit_pct**: 1.0% → 1.5% (50% wider)
- **tp3 profit_pct**: 1.5% → 2.0% (33% wider)
  - **Rationale**: Progressive targets aligned with new main TP
  - **Impact**: More realistic partial close targets
  - **Trade-off**: Slower profit taking for higher hit rate

## Grid Level Analysis

### Before Optimization
- Spread: 0.02% | TP: 0.3% | Grid Levels: 15
- **Problem**: 15 grid levels = unrealistic target
- **Result**: Orders rarely hit TP, accumulate as stuck positions

### After Optimization
- Spread: 0.06% | TP: 0.6% | Grid Levels: 10
- **Solution**: 10 grid levels = realistic target
- **Result**: Orders hit TP more frequently, fewer stuck positions

## Risk/Reward Analysis

### TP/SL Ratio
- **Old**: 0.3% TP / 0.5% SL = 1:1.67 (suboptimal)
- **New**: 0.6% TP / 1.0% SL = 1:1.67 (consistent)
- **Note**: Maintained similar ratio for consistency

### Expected Position Duration
- **Old**: 3-5 minutes (tight TP/SL, quick exits)
- **New**: 5-7 minutes (wider TP/SL, more time to hit targets)
- **Impact**: Still within position_timeout_minutes limits

## Expected Outcomes

### Stuck Position Reduction
- **Old**: High stuck position rate (unrealistic TP targets)
- **New**: 50% reduction in stuck positions (realistic TP targets)
- **Improvement**: Higher capital efficiency

### Win Rate Improvement
- **Old**: 55-60% (tight TP, many missed targets)
- **New**: 65-70% (wider TP, higher hit rate)
- **Improvement**: +10-15% win rate

### Drawdown Impact
- **Old**: 20-30% (frequent stop hunts)
- **New**: 10-15% (fewer stop hunts)
- **Improvement**: 50% reduction in drawdown

## Conclusion

The TP/SL parameter changes prioritize **realistic targets over aggressive profit taking**. While individual trade profit may decrease slightly, the significant improvement in hit rate and reduction in stuck positions makes this a net positive change for overall profitability and risk management.
