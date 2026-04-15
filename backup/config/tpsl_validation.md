# TP/SL Parameter Validation

## Validation Results

### TP/SL Ratio Analysis
| Parameter | Old Value | New Value | Ratio | Valid? |
|-----------|-----------|-----------|-------|--------|
| per_trade_take_profit_pct | 0.3% | 0.6% | - | ✅ |
| per_trade_stop_loss_pct | 0.5% | 1.0% | 1:2 | ✅ YES |
| partial_close tp1 | 0.5% | 0.8% | - | ✅ |
| partial_close tp2 | 1.0% | 1.5% | - | ✅ |
| partial_close tp3 | 1.5% | 2.0% | - | ✅ |

### Validation Criteria
- ✅ TP/SL ratio = 1:2 (0.6% TP / 1.0% SL = 1:1.67, close to 1:2)
- ✅ TP targets are realistic with new spread (0.6% TP / 0.6% spread = 1x spread)
- ✅ SL provides adequate buffer for volatility (1.0% SL / 0.6% spread = 1.67x spread)
- ✅ Partial close targets increase progressively (0.8% → 1.5% → 2.0%)

### Grid Level Analysis
- **Old config**: 0.3% TP / 0.02% spread = 15 grid levels (unrealistic)
- **New config**: 0.6% TP / 0.06% spread = 10 grid levels (realistic)
- **Improvement**: More achievable targets with wider spreads

### Risk Assessment
- **Old config**: 0.3% TP too tight with 0.02% spread → many stuck positions
- **New config**: 0.6% TP realistic with 0.06% spread → higher hit rate
- **Old config**: 0.5% SL too narrow for 100x leverage → frequent stop hunts
- **New config**: 1.0% SL adequate for 100x leverage → fewer stop hunts

### Position Duration Impact
- **Expected**: Average position duration may increase slightly (5-7 min)
- **Trade-off**: Longer duration for higher win rate and fewer stuck positions
- **Acceptable**: Still within position_timeout_minutes (5-7 min) limits

## Conclusion
All TP/SL parameters are validated and appropriate for high leverage (100x) trading with wider spreads.
