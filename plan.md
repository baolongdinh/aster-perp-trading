# Adaptive Volume Farming Implementation Plan

## 🎯 Feature Overview

Implement adaptive market regime detection for volume farming bot to optimize trading parameters based on market conditions:
- **Trending markets**: Reduce exposure, widen spreads
- **Ranging markets**: Maximize volume with tight spreads  
- **Volatile markets**: Moderate exposure with balanced parameters

## 📋 Technical Context

### Known Components
- Volume farming engine exists in `backend/internal/farming/`
- Risk management system in `backend/internal/risk/`
- Configuration system in `backend/internal/config/`
- Grid manager handles order placement logic

### Unknowns Requiring Research
1. **Market regime detection algorithms** - Best approach for crypto futures
2. **ATR calculation methods** - Standard vs Wilder's vs exponential
3. **Real-time regime switching** - How to handle parameter transitions
4. **Performance impact** - Computational overhead of regime detection

### Dependencies
- Existing risk management system
- Current configuration structure
- WebSocket price feeds
- Grid order management

## 🔍 Research Phase

### Market Regime Detection Approaches

#### Option 1: ATR-Based Detection
```go
func detectRegimeATR(prices []float64, atr float64) MarketRegime {
    atrRatio := atr / average(prices)
    if atrRatio > 0.03 {
        return RegimeVolatile
    } else if atrRatio > 0.015 {
        return RegimeRanging
    }
    return RegimeTrending
}
```

**Pros**: Simple, uses existing ATR calculations
**Cons**: May lag in regime transitions

#### Option 2: Price Momentum Detection  
```go
func detectRegimeMomentum(prices []float64, period int) MarketRegime {
    shortMA := sma(prices, period/2)
    longMA := sma(prices, period)
    
    momentum := (shortMA - longMA) / longMA
    volatility := stdDev(prices, period) / average(prices)
    
    if math.Abs(momentum) > 0.02 {
        return RegimeTrending
    } else if volatility > 0.025 {
        return RegimeVolatile
    }
    return RegimeRanging
}
```

**Pros**: Responsive to momentum changes
**Cons**: More parameters to tune

#### Option 3: Hybrid Approach (Recommended)
```go
func detectRegimeHybrid(prices []float64, atr float64) MarketRegime {
    // Combine ATR and momentum for robustness
    atrRegime := detectRegimeATR(prices, atr)
    momentumRegime := detectRegimeMomentum(prices, 20)
    
    // Weight voting: ATR 60%, Momentum 40%
    if atrRegime == momentumRegime {
        return atrRegime // Strong signal
    }
    
    // In conflict, favor ATR for stability
    return atrRegime
}
```

**Decision**: Hybrid approach chosen for robustness and proven reliability

### Configuration Structure Design

#### Adaptive Config Schema
```yaml
adaptive_config:
  enabled: true
  detection:
    method: "hybrid"  # atr, momentum, hybrid
    atr_period: 14
    momentum_short: 10
    momentum_long: 20
    update_interval_seconds: 300  # 5 minutes
  
  regimes:
    trending:
      order_size_usdt: 2.0
      grid_spread_pct: 0.1
      max_orders_per_side: 1
      max_daily_loss_usdt: 25
      position_timeout_minutes: 30
      
    ranging:
      order_size_usdt: 5.0
      grid_spread_pct: 0.02
      max_orders_per_side: 3
      max_daily_loss_usdt: 50
      position_timeout_minutes: 45
      
    volatile:
      order_size_usdt: 3.0
      grid_spread_pct: 0.05
      max_orders_per_side: 2
      max_daily_loss_usdt: 35
      position_timeout_minutes: 35
```

## 🏗️ Implementation Design

### Phase 1: Core Regime Detection

#### 1.1 MarketRegime Enum and Detector
```go
// backend/internal/farming/market_regime.go
type MarketRegime string

const (
    RegimeTrending MarketRegime = "trending"
    RegimeRanging MarketRegime = "ranging" 
    RegimeVolatile MarketRegime = "volatile"
    RegimeUnknown MarketRegime = "unknown"
)

type RegimeDetector struct {
    priceHistory    map[string][]float64  // symbol -> price history
    atrHistory     map[string]float64    // symbol -> current ATR
    mu             sync.RWMutex
    updateInterval  time.Duration
    logger         *zap.Logger
}
```

#### 1.2 Configuration Integration
```go
// backend/internal/config/config.go - Add to VolumeFarmConfig
type AdaptiveConfig struct {
    Enabled              bool                   `mapstructure:"enabled"`
    Detection            DetectionConfig         `mapstructure:"detection"`
    Regimes             map[string]RegimeConfig `mapstructure:"regimes"`
}

type DetectionConfig struct {
    Method                string `mapstructure:"method"`
    ATRPeriod             int    `mapstructure:"atr_period"`
    MomentumShortPeriod    int    `mapstructure:"momentum_short"`
    MomentumLongPeriod     int    `mapstructure:"momentum_long"`
    UpdateIntervalSeconds   int    `mapstructure:"update_interval_seconds"`
}

type RegimeConfig struct {
    OrderSizeUSDT           float64 `mapstructure:"order_size_usdt"`
    GridSpreadPct          float64 `mapstructure:"grid_spread_pct"`
    MaxOrdersPerSide        int     `mapstructure:"max_orders_per_side"`
    MaxDailyLossUSDT       float64 `mapstructure:"max_daily_loss_usdt"`
    PositionTimeoutMinutes   int     `mapstructure:"position_timeout_minutes"`
}
```

### Phase 2: Dynamic Parameter Application

#### 2.1 Adaptive Grid Manager
```go
// backend/internal/farming/adaptive_grid_manager.go
type AdaptiveGridManager struct {
    *GridManager                    // Embed existing
    regimeDetector  *RegimeDetector
    adaptiveConfig  *config.AdaptiveConfig
    currentRegime  map[string]MarketRegime  // symbol -> current regime
}

func (a *AdaptiveGridManager) ApplyRegimeSpecificConfig(symbol string) {
    regime := a.regimeDetector.GetCurrentRegime(symbol)
    config := a.adaptiveConfig.Regimes[string(regime)]
    
    // Apply regime-specific parameters
    a.orderSizeUSDT = config.OrderSizeUSDT
    a.gridSpreadPct = config.GridSpreadPct
    a.maxOrdersSide = config.MaxOrdersPerSide
    
    a.logger.Info("Applied regime config",
        zap.String("symbol", symbol),
        zap.String("regime", string(regime)),
        zap.Float64("order_size", config.OrderSizeUSDT),
        zap.Float64("spread", config.GridSpreadPct),
        zap.Int("max_orders", config.MaxOrdersPerSide))
}
```

#### 2.2 Risk Manager Integration
```go
// backend/internal/risk/adaptive_risk_manager.go
func (m *Manager) GetRegimeSpecificLimits(symbol string) (float64, float64, int) {
    // Get current regime for symbol
    regime := m.regimeDetector.GetCurrentRegime(symbol)
    config := m.adaptiveConfig.Regimes[string(regime)]
    
    return config.OrderSizeUSDT, config.MaxDailyLossUSDT, config.MaxOrdersPerSide
}
```

### Phase 3: Real-time Updates

#### 3.1 Regime Transition Handling
```go
func (d *RegimeDetector) UpdateRegime(symbol string, newPrice float64) {
    d.mu.Lock()
    defer d.mu.Unlock()
    
    // Update price history
    prices := d.priceHistory[symbol]
    prices = append(prices[1:], newPrice) // Keep last N prices
    d.priceHistory[symbol] = prices
    
    // Calculate new regime
    newRegime := d.detectRegime(prices)
    oldRegime := d.currentRegime[symbol]
    
    if newRegime != oldRegime {
        d.logger.Info("Regime transition detected",
            zap.String("symbol", symbol),
            zap.String("from", string(oldRegime)),
            zap.String("to", string(newRegime)))
            
        d.currentRegime[symbol] = newRegime
        d.onRegimeChange(symbol, oldRegime, newRegime)
    }
}
```

#### 3.2 Smooth Parameter Transitions
```go
func (a *AdaptiveGridManager) onRegimeChange(symbol string, oldRegime, newRegime MarketRegime) {
    // Cancel existing grid orders
    a.cancelAllGridOrders(symbol)
    
    // Wait for transition period to avoid whipsaw
    time.Sleep(30 * time.Second)
    
    // Apply new regime configuration
    a.ApplyRegimeSpecificConfig(symbol)
    
    // Rebuild grid with new parameters
    a.rebuildGrid(symbol)
}
```

## 📊 Data Model

### Regime State Tracking
```go
type RegimeState struct {
    Symbol        string        `json:"symbol"`
    CurrentRegime MarketRegime  `json:"current_regime"`
    LastUpdate    time.Time     `json:"last_update"`
    Confidence    float64       `json:"confidence"`
    PriceHistory  []float64     `json:"price_history"`
    CurrentATR   float64       `json:"current_atr"`
}

type RegimeTransition struct {
    Symbol     string        `json:"symbol"`
    FromRegime MarketRegime  `json:"from_regime"`
    ToRegime   MarketRegime  `json:"to_regime"`
    Timestamp  time.Time     `json:"timestamp"`
    TriggerPrice float64       `json:"trigger_price"`
}
```

## 🚀 Implementation Phases

### Phase 0: Setup (Day 1)
- [ ] Create market regime detection module
- [ ] Extend configuration schema
- [ ] Set up build system integration
- [ ] Create unit test framework

### Phase 1: Core Implementation (Days 2-3)
- [ ] Implement RegimeDetector with hybrid algorithm
- [ ] Create AdaptiveGridManager extending GridManager
- [ ] Integrate with existing risk management
- [ ] Add configuration validation

### Phase 2: Integration (Days 4-5)
- [ ] Update VolumeFarmEngine to use adaptive components
- [ ] Implement smooth regime transitions
- [ ] Add regime change notifications
- [ ] Create monitoring and metrics

### Phase 3: Testing & Optimization (Days 6-7)
- [ ] Unit tests for regime detection
- [ ] Integration tests with historical data
- [ ] Performance benchmarking
- [ ] Configuration validation tests

### Phase 4: Deployment (Day 8)
- [ ] Update configuration files
- [ ] Migration scripts for existing deployments
- [ ] Documentation updates
- [ ] Production monitoring setup

## 🔧 Technical Requirements

### Dependencies
- Go 1.19+ (existing)
- Existing trading infrastructure
- WebSocket price feeds (existing)

### Performance Targets
- Regime detection: <10ms per symbol
- Parameter updates: <100ms
- Memory overhead: <50MB additional
- No impact on order execution latency

### Monitoring Requirements
- Regime transition frequency
- Parameter update success rate
- Performance impact on volume generation
- Error rates in regime detection

## ✅ Acceptance Criteria

1. **Correct Regime Detection**: >85% accuracy in backtesting
2. **Smooth Transitions**: No order execution gaps during regime changes
3. **Performance**: <5% overhead compared to current system
4. **Configuration**: Hot-reloadable without restart
5. **Monitoring**: Full visibility into regime states and transitions

## 🎯 Success Metrics

- **Volume Generation**: Maintain or increase current volume levels
- **Risk Reduction**: Reduce losses during trending markets by >40%
- **Efficiency**: Improve fill rates in ranging markets by >15%
- **Stability**: Zero regime detection errors in production

This plan provides a comprehensive framework for implementing adaptive volume farming that optimizes performance across all market conditions while maintaining system stability and risk management.
