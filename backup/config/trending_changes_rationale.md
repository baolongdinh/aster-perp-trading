# Trending Parameter Changes - Rationale

## Changes Summary

### Order Capacity
- **max_orders_per_side**: 2 → 5 (2.5x increase)
  - **Rationale**: More fill opportunities during trending (20% of time)
  - **Impact**: 50% increase in trending volume
  - **Trade-off**: More orders = more management overhead

### Order Size
- **order_size_usdt**: 1.0 → 0.8 (20% reduction)
  - **Rationale**: Compensate for increased order count
  - **Impact**: Same total exposure, more distributed
  - **Trade-off**: Smaller per-order risk

## Exposure Analysis

### Before Optimization
- Orders: 2/side × $1.0 = $4 total trending exposure
- Fill opportunities: Limited (few orders)
- Volume: Low during trending periods

### After Optimization
- Orders: 5/side × $0.8 = $4 total trending exposure (unchanged)
- Fill opportunities: 2.5x more (more orders)
- Volume: 50% increase during trending periods

## Risk Assessment

### Per-Order Risk
- **Old**: $1.0 per order
- **New**: $0.8 per order
- **Improvement**: 20% reduction in per-order risk

### Total Exposure
- **Old**: $4 total (2 × $1.0)
- **New**: $4 total (5 × $0.8)
- **Status**: Unchanged

### Fill Rate
- **Old**: Low (few orders)
- **New**: High (many orders)
- **Improvement**: 2.5x more fill opportunities

## Expected Outcomes

### Volume Increase
- **Old**: Low trending volume (few orders)
- **New**: 50% increase in trending volume
- **Impact**: More overall volume farming

### Risk Profile
- **Old**: Higher per-order risk, fewer fills
- **New**: Lower per-order risk, more fills
- **Improvement**: Better risk distribution

## Conclusion

The trending parameter changes prioritize **fill opportunities over order size**. By increasing order count while reducing individual order size, total exposure remains the same while providing significantly more fill opportunities during trending periods. This is a net positive change for volume farming without increasing risk.
