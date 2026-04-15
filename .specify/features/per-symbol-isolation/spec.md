# Per-Symbol Isolation Specification

## Overview

Ensure each trading symbol operates independently with its own signals, chart data, volume tracking, loss tracking, and cooldown state. No symbol's behavior should affect other symbols.

## Problem Statement

Currently, the trading bot has global state that causes one symbol's trading behavior to affect other symbols:

1. **TradeTracker**: Global results array mixes all symbols, causing win rate and consecutive loss calculations to span across symbols
2. **ExposureManager**: Global consecutive loss and cooldown state causes one symbol's losses to trigger cooldown for all symbols
3. **ModeManager**: Has both global and per-symbol state, risking accidental global overrides

## Functional Requirements

### FR-1: Per-Symbol Trade Tracking
- TradeTracker must track wins/losses per-symbol independently
- Win rate calculation must be per-symbol
- Consecutive loss counting must be per-symbol
- One symbol's losses must not affect another symbol's consecutive loss count

### FR-2: Per-Symbol Cooldown Management
- ExposureManager must track consecutive losses per-symbol
- Cooldown state must be per-symbol
- One symbol hitting loss threshold must not trigger cooldown for other symbols
- Each symbol must have independent cooldown timers

### FR-3: Mode Manager Isolation
- Ensure all trading mode decisions are per-symbol
- Remove or deprecate global mode methods
- Prevent accidental global mode overrides

## Success Criteria

### SC-1: Trade Isolation
- BTC can have 3 consecutive losses while ETH has 0
- ETH's win rate is calculated independently from BTC
- ETH's consecutive loss count remains 0 despite BTC's losses

### SC-2: Cooldown Isolation
- BTC can enter cooldown after 3 losses
- ETH continues trading normally while BTC is in cooldown
- ETH's sizing is not reduced by BTC's losses

### SC-3: Mode Isolation
- Each symbol has independent trading mode
- No global mode state affects per-symbol mode decisions

## Edge Cases

### EC-1: New Symbol Added
- New symbol starts with fresh state (0 losses, 50% default win rate)
- Not affected by existing symbols' history

### EC-2: Symbol Removed
- Removing a symbol should not affect other symbols' state
- Clean up symbol-specific data to prevent memory leaks

### EC-3: Simultaneous Losses
- Multiple symbols can have losses at the same time
- Each symbol's consecutive loss count is independent

## Non-Functional Requirements

### NFR-1: Performance
- Per-symbol tracking should not significantly impact performance
- Map lookups should be O(1) average case

### NFR-2: Memory
- Per-symbol data should be cleaned up when symbols are removed
- Prevent unbounded memory growth

### NFR-3: Backward Compatibility
- Minimize breaking changes to existing interfaces
- Provide migration path for callers

## User Stories

### US-1: Independent Symbol Trading
As a trader, I want each symbol to trade independently so that losses in one symbol don't stop trading in profitable symbols.

### US-2: Accurate Per-Symbol Metrics
As a trader, I want to see accurate win rates and loss counts per symbol so I can evaluate each symbol's performance independently.

### US-3: Isolated Cooldowns
As a trader, I want cooldowns to be per-symbol so that one symbol's volatility doesn't prevent trading in stable symbols.
