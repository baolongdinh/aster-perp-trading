# Config Optimization Implementation Plan

## Tech Stack

### Existing Stack
- **Language:** Go 1.21+
- **Config Format:** YAML
- **Configuration Files:**
  - `backend/config/volume-farm-config.yaml` - Main volume farming config
  - `backend/config/adaptive_config.yaml` - Adaptive regime config
  - `backend/config/trading_hours.yaml` - Trading hours config
  - `backend/config/safeguards.yaml` - Safety mechanisms
  - `backend/config/agentic-config.yaml` - Circuit breaker config

### Libraries & Components
- **Config Loading:** `github.com/spf13/viper`
- **Logging:** `go.uber.org/zap`, `github.com/sirupsen/logrus`
- **Math:** Standard library `math` package
- **Time:** Standard library `time` package

## Project Structure

```
backend/
├── config/
│   ├── volume-farm-config.yaml      # Main config (US1, US2, US3)
│   ├── adaptive_config.yaml          # Adaptive regimes (US1, US3, US4)
│   ├── trading_hours.yaml            # Time filters (US6)
│   ├── safeguards.yaml              # Safety mechanisms (US4, US8)
│   └── agentic-config.yaml          # Circuit breaker (US4)
├── internal/farming/
│   ├── adaptive_grid/
│   │   ├── manager.go               # Adaptive grid logic (US4, US5)
│   │   ├── risk_sizing.go           # Position sizing (US5)
│   │   └── range_detector.go        # ATR calculation (US4)
│   └── grid_manager.go               # Grid logic (US7)
```

## Implementation Strategy

### Phase 1: Immediate Config Fixes (US1, US2, US3)
**Strategy:** Direct YAML file edits - no code changes required

**Tasks:**
1. Update `volume-farm-config.yaml` with new spread, TP, SL values
2. Update `adaptive_config.yaml` with new regime-specific values
3. Validate config loading and parsing
4. Test with dry-run mode

**Risk:** Low - only config changes, can be easily reverted

### Phase 2: Dynamic Adjustments (US4, US5, US6)
**Strategy:** Code changes + config updates

**US4 - Dynamic Leverage:**
- Add volatility-based leverage multiplier to `manager.go`
- Update position sizing logic to use dynamic leverage
- Add config parameters for leverage thresholds
- Integrate with existing ATR calculation

**US5 - Equity Curve Sizing:**
- Enhance existing `smart_sizing` in `risk_sizing.go`
- Add equity tracking and performance metrics
- Implement Kelly Criterion sizing formula
- Add consecutive loss/gain tracking

**US6 - Trading Hours:**
- Update `trading_hours.yaml` with new session configs
- Consider eliminating or reducing US session
- Add session performance analysis

**Risk:** Medium - code changes require testing

### Phase 3: Advanced Features (US7, US8, US9)
**Strategy:** New features + complex logic

**US7 - Micro-Grid Scalping:**
- Add new scalping mode to grid logic
- Implement ultra-short-term position management
- Add volatility threshold for mode switching

**US8 - Funding Rate Optimization:**
- Enhance existing `funding_rate` config
- Add position bias logic
- Implement funding cost tracking

**US9 - Correlation Hedging:**
- Add correlation calculation module
- Implement hedging logic
- Add multi-symbol support

**Risk:** High - complex features require extensive testing

## Testing Strategy

### Config Validation Tests
- Load config files and validate all parameters
- Check parameter ranges and constraints
- Validate parameter interactions

### Unit Tests
- Test dynamic leverage calculation
- Test Kelly Criterion sizing
- Test equity curve tracking
- Test funding rate bias logic

### Integration Tests
- Test config changes with bot in dry-run mode
- Test mode transitions (ranging → trending → volatile)
- Test leverage adjustments during volatility spikes
- Test position sizing adjustments after losses/gains

### Performance Tests
- Measure win rate improvement
- Measure drawdown reduction
- Measure volume changes
- Measure fill rate changes

## Deployment Strategy

### Phase 1 Deployment
1. Backup existing config files
2. Apply config changes
3. Restart bot in dry-run mode
4. Monitor for 24 hours
5. If successful, switch to live mode
6. Monitor for 1 week before proceeding to Phase 2

### Phase 2 Deployment
1. Deploy code changes
2. Update config files
3. Test in dry-run mode
4. Monitor for 48 hours
5. If successful, switch to live mode
6. Monitor for 2 weeks before proceeding to Phase 3

### Phase 3 Deployment
1. Deploy code changes
2. Update config files
3. Test in dry-run mode
4. Monitor for 1 week
5. If successful, switch to live mode
6. Continuous monitoring

## Rollback Plan

### Config Rollback
- Keep backup of all config files
- Simple file restore to rollback
- No code changes in Phase 1

### Code Rollback
- Git tags for each phase
- Simple git checkout to rollback
- Database/schema changes: N/A (no schema changes)

## Monitoring & Metrics

### Key Metrics to Track
- Daily PnL and drawdown
- Win rate by regime
- Average position duration
- Fill rate vs stuck positions
- Slippage percentage
- Funding rate impact
- Leverage utilization
- Margin usage percentage
- Volume per session

### Alerting
- Win rate drops below 50%
- Drawdown exceeds 15%
- Liquidation risk increases
- Config validation errors
- Mode transition failures

## Success Criteria

### Phase 1 Success
- Win rate improves from 55-60% to 65-70%
- Grid spread validated for leverage levels
- TP/SL hit rate improves
- Trending volume increases by 50%

### Phase 2 Success
- Liquidation risk reduced by 40%
- Position sizing adapts to equity curve
- Trading hours optimized for volume/risk
- No regression from Phase 1 improvements

### Phase 3 Success
- Scalping mode generates additional volume
- Funding cost reduced or neutralized
- Correlation hedging reduces risk
- All previous improvements maintained

## Timeline

### Phase 1: 1-2 days
- Config changes: 1 day
- Testing: 1 day
- Deployment: Immediate after testing

### Phase 2: 1-2 weeks
- Code development: 3-5 days
- Testing: 3-5 days
- Deployment: After Phase 1 validation

### Phase 3: 2-4 weeks
- Code development: 1-2 weeks
- Testing: 1-2 weeks
- Deployment: After Phase 2 validation

## Notes

- All changes are backward compatible
- No database schema changes required
- Config changes can be hot-reloaded (no restart needed for some)
- Dry-run mode available for safe testing
- Git version control for easy rollback
