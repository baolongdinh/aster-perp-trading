package adaptive_grid

import (
	"sort"
	"sync"
	"time"

	"go.uber.org/zap"
)

// ClusterStopLossStatus represents the status of a cluster
type ClusterStopLossStatus int

const (
	ClusterStatusNormal ClusterStopLossStatus = iota
	ClusterStatusMonitor
	ClusterStatusEmergencyClose
	ClusterStatusStaleClose
	ClusterStatusBreakevenExit
)

// Cluster represents a group of positions at similar levels
type Cluster struct {
	Symbol        string
	Level         int
	Side          string
	Positions     []PositionInfo
	EntryTime     time.Time
	TotalSize     float64
	TotalNotional float64
	AvgEntryPrice float64
	Status        ClusterStopLossStatus
}

// ClusterManager manages position clusters and stop-loss logic
type ClusterManager struct {
	clusters map[string][]Cluster // symbol -> clusters
	config   *ClusterStopLossConfig
	logger   *zap.Logger
	mu       sync.RWMutex
}

// ClusterStopLossConfig holds configuration
type ClusterStopLossConfig struct {
	MonitorHours       float64 `yaml:"monitor_hours"`         // 2 hours
	EmergencyHours     float64 `yaml:"emergency_hours"`       // 4 hours
	StaleHours         float64 `yaml:"stale_hours"`           // 8 hours
	MonitorDrawdown    float64 `yaml:"monitor_drawdown"`      // -0.5%
	EmergencyDrawdown  float64 `yaml:"emergency_drawdown"`    // -1.0%
	Breakeven50PctAt   float64 `yaml:"breakeven_50_pct_at"`   // -0.2%
	Breakeven100PctAt  float64 `yaml:"breakeven_100_pct_at"`  // 0.0%
	MinDrawdownForExit float64 `yaml:"min_drawdown_for_exit"` // -2.0%
}

// DefaultClusterStopLossConfig returns default configuration
func DefaultClusterStopLossConfig() *ClusterStopLossConfig {
	return &ClusterStopLossConfig{
		MonitorHours:       2,
		EmergencyHours:     4,
		StaleHours:         8,
		MonitorDrawdown:    -0.005,
		EmergencyDrawdown:  -0.01,
		Breakeven50PctAt:   -0.002,
		Breakeven100PctAt:  0.0,
		MinDrawdownForExit: -0.02,
	}
}

// NewClusterManager creates new cluster manager
func NewClusterManager(config *ClusterStopLossConfig, logger *zap.Logger) *ClusterManager {
	if config == nil {
		config = DefaultClusterStopLossConfig()
	}

	return &ClusterManager{
		clusters: make(map[string][]Cluster),
		config:   config,
		logger:   logger,
	}
}

// TrackClusterEntry tracks a new cluster entry
func (cm *ClusterManager) TrackClusterEntry(symbol string, level int, side string, positions []PositionInfo) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	totalSize := 0.0
	totalNotional := 0.0
	var earliestEntry time.Time

	for _, pos := range positions {
		totalSize += pos.Size
		totalNotional += pos.NotionalValue
		if earliestEntry.IsZero() || pos.EntryTime.Before(earliestEntry) {
			earliestEntry = pos.EntryTime
		}
	}

	avgPrice := 0.0
	if totalSize > 0 {
		avgPrice = totalNotional / totalSize
	}

	cluster := Cluster{
		Symbol:        symbol,
		Level:         level,
		Side:          side,
		Positions:     positions,
		EntryTime:     earliestEntry,
		TotalSize:     totalSize,
		TotalNotional: totalNotional,
		AvgEntryPrice: avgPrice,
		Status:        ClusterStatusNormal,
	}

	if _, exists := cm.clusters[symbol]; !exists {
		cm.clusters[symbol] = make([]Cluster, 0)
	}
	cm.clusters[symbol] = append(cm.clusters[symbol], cluster)

	cm.logger.Debug("Cluster tracked",
		zap.String("symbol", symbol),
		zap.Int("level", level),
		zap.String("side", side),
		zap.Float64("total_notional", totalNotional))
}

// GetClusterAge returns age of cluster in hours
func (cm *ClusterManager) GetClusterAge(symbol string, level int) float64 {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clusters, exists := cm.clusters[symbol]
	if !exists {
		return 0
	}

	for _, cluster := range clusters {
		if cluster.Level == level {
			return time.Since(cluster.EntryTime).Hours()
		}
	}
	return 0
}

// CheckTimeBasedStopLoss checks and returns status for all clusters
func (cm *ClusterManager) CheckTimeBasedStopLoss(symbol string, currentPrice float64) []Cluster {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	clusters, exists := cm.clusters[symbol]
	if !exists {
		return nil
	}

	updatedClusters := make([]Cluster, 0)

	for i := range clusters {
		cluster := clusters[i]
		age := time.Since(cluster.EntryTime).Hours()

		// Calculate current PnL
		pnlPct := cm.calculateClusterPnL(cluster, currentPrice)

		newStatus := ClusterStatusNormal

		switch {
		case age >= cm.config.StaleHours:
			newStatus = ClusterStatusStaleClose
			cm.logger.Warn("Stale cluster - closing",
				zap.String("symbol", symbol),
				zap.Int("level", cluster.Level),
				zap.Float64("age_hours", age))

		case age >= cm.config.EmergencyHours && pnlPct <= cm.config.EmergencyDrawdown:
			newStatus = ClusterStatusEmergencyClose
			cm.logger.Error("Emergency cluster stop-loss",
				zap.String("symbol", symbol),
				zap.Int("level", cluster.Level),
				zap.Float64("age_hours", age),
				zap.Float64("pnl_pct", pnlPct))

		case age >= cm.config.MonitorHours && pnlPct <= cm.config.MonitorDrawdown:
			newStatus = ClusterStatusMonitor
			cm.logger.Warn("Cluster entering monitor state",
				zap.String("symbol", symbol),
				zap.Int("level", cluster.Level),
				zap.Float64("age_hours", age),
				zap.Float64("pnl_pct", pnlPct))
		}

		if newStatus != cluster.Status {
			clusters[i].Status = newStatus
			updatedClusters = append(updatedClusters, clusters[i])
		}
	}

	return updatedClusters
}

// CheckBreakevenExit checks if breakeven exit should trigger
func (cm *ClusterManager) CheckBreakevenExit(symbol string, currentPrice float64) []Cluster {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	clusters, exists := cm.clusters[symbol]
	if !exists {
		return nil
	}

	exitClusters := make([]Cluster, 0)

	for i := range clusters {
		cluster := clusters[i]
		pnlPct := cm.calculateClusterPnL(cluster, currentPrice)

		// Check for 50% exit (after significant drawdown, partial recovery)
		if pnlPct >= cm.config.Breakeven50PctAt && pnlPct < 0 {
			// Need to check if we had significant drawdown first
			// For now, trigger if in recovery zone
			clusters[i].Status = ClusterStatusBreakevenExit
			exitClusters = append(exitClusters, clusters[i])
			cm.logger.Info("Breakeven 50% exit triggered",
				zap.String("symbol", symbol),
				zap.Int("level", cluster.Level),
				zap.Float64("pnl_pct", pnlPct))
		}

		// Check for 100% exit (at breakeven after significant drawdown)
		if pnlPct >= cm.config.Breakeven100PctAt && pnlPct < 0.001 {
			clusters[i].Status = ClusterStatusBreakevenExit
			exitClusters = append(exitClusters, clusters[i])
			cm.logger.Info("Breakeven 100% exit triggered",
				zap.String("symbol", symbol),
				zap.Int("level", cluster.Level),
				zap.Float64("pnl_pct", pnlPct))
		}
	}

	return exitClusters
}

// calculateClusterPnL calculates PnL percentage for a cluster
func (cm *ClusterManager) calculateClusterPnL(cluster Cluster, currentPrice float64) float64 {
	if cluster.AvgEntryPrice == 0 {
		return 0
	}

	if cluster.Side == "LONG" {
		return (currentPrice - cluster.AvgEntryPrice) / cluster.AvgEntryPrice
	}
	return (cluster.AvgEntryPrice - currentPrice) / cluster.AvgEntryPrice
}

// GenerateClusterHeatMap generates heat map of all clusters
func (cm *ClusterManager) GenerateClusterHeatMap(symbol string) map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clusters, exists := cm.clusters[symbol]
	if !exists {
		return map[string]interface{}{
			"symbol":   symbol,
			"clusters": 0,
		}
	}

	type ClusterInfo struct {
		Level       int     `json:"level"`
		Side        string  `json:"side"`
		Size        float64 `json:"size"`
		EntryPrice  float64 `json:"entry_price"`
		AgeHours    float64 `json:"age_hours"`
		Status      string  `json:"status"`
		Recommended string  `json:"recommended_action"`
	}

	clusterInfos := make([]ClusterInfo, 0, len(clusters))

	for _, cluster := range clusters {
		age := time.Since(cluster.EntryTime).Hours()

		recommended := "HOLD"
		switch cluster.Status {
		case ClusterStatusMonitor:
			recommended = "MONITOR_CLOSE"
		case ClusterStatusEmergencyClose:
			recommended = "EMERGENCY_CLOSE"
		case ClusterStatusStaleClose:
			recommended = "STALE_CLOSE"
		case ClusterStatusBreakevenExit:
			recommended = "BREAKEVEN_EXIT"
		}

		clusterInfos = append(clusterInfos, ClusterInfo{
			Level:       cluster.Level,
			Side:        cluster.Side,
			Size:        cluster.TotalSize,
			EntryPrice:  cluster.AvgEntryPrice,
			AgeHours:    age,
			Status:      cluster.Status.String(),
			Recommended: recommended,
		})
	}

	// Sort by level
	sort.Slice(clusterInfos, func(i, j int) bool {
		return clusterInfos[i].Level < clusterInfos[j].Level
	})

	return map[string]interface{}{
		"symbol":        symbol,
		"cluster_count": len(clusters),
		"clusters":      clusterInfos,
		"generated_at":  time.Now(),
	}
}

// RemoveCluster removes a cluster after closing
func (cm *ClusterManager) RemoveCluster(symbol string, level int) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	clusters, exists := cm.clusters[symbol]
	if !exists {
		return
	}

	// Find and remove cluster
	for i, cluster := range clusters {
		if cluster.Level == level {
			cm.clusters[symbol] = append(clusters[:i], clusters[i+1:]...)
			cm.logger.Info("Cluster removed",
				zap.String("symbol", symbol),
				zap.Int("level", level))
			return
		}
	}
}

// GetStatus returns cluster manager status
func (cm *ClusterManager) GetStatus(symbol string) map[string]interface{} {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	clusters, exists := cm.clusters[symbol]
	if !exists {
		return map[string]interface{}{
			"symbol":         symbol,
			"cluster_count":  0,
			"total_notional": 0,
		}
	}

	totalNotional := 0.0
	statusCounts := make(map[string]int)

	for _, cluster := range clusters {
		totalNotional += cluster.TotalNotional
		statusCounts[cluster.Status.String()]++
	}

	return map[string]interface{}{
		"symbol":         symbol,
		"cluster_count":  len(clusters),
		"total_notional": totalNotional,
		"status_counts":  statusCounts,
	}
}

// String returns string representation of status
func (s ClusterStopLossStatus) String() string {
	switch s {
	case ClusterStatusNormal:
		return "NORMAL"
	case ClusterStatusMonitor:
		return "MONITOR"
	case ClusterStatusEmergencyClose:
		return "EMERGENCY_CLOSE"
	case ClusterStatusStaleClose:
		return "STALE_CLOSE"
	case ClusterStatusBreakevenExit:
		return "BREAKEVEN_EXIT"
	default:
		return "UNKNOWN"
	}
}

// Reset clears all clusters
func (cm *ClusterManager) Reset() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.clusters = make(map[string][]Cluster)
}
