package agentic

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"aster-bot/internal/config"

	"go.uber.org/zap"
)

// VFWhitelistController defines the interface for controlling VF whitelist
type VFWhitelistController interface {
	UpdateWhitelist(symbols []string) error
	GetActivePositions() ([]PositionStatus, error)
	TriggerEmergencyExit(reason string) error
	TriggerForcePlacement() error
}

// WhitelistManager manages the dynamic whitelist based on agentic decisions
type WhitelistManager struct {
	config       config.WhitelistConfig
	vfController VFWhitelistController
	logger       *zap.Logger

	mu              sync.RWMutex
	currentScores   map[string]SymbolScore
	activeWhitelist []string
	lastUpdate      time.Time
	updateCount     int
}

// NewWhitelistManager creates a new whitelist manager
func NewWhitelistManager(
	cfg config.WhitelistConfig,
	vfController VFWhitelistController,
	logger *zap.Logger,
) *WhitelistManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &WhitelistManager{
		config:          cfg,
		vfController:    vfController,
		logger:          logger.With(zap.String("component", "whitelist_manager")),
		currentScores:   make(map[string]SymbolScore),
		activeWhitelist: make([]string, 0),
	}
}

// UpdateWhitelist updates the whitelist based on new scores
func (wm *WhitelistManager) UpdateWhitelist(ctx context.Context, scores map[string]SymbolScore) error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Store scores
	wm.currentScores = scores

	// Get active positions from VF
	positions, err := wm.vfController.GetActivePositions()
	if err != nil {
		wm.logger.Warn("Failed to get active positions", zap.Error(err))
		// Continue without position data
		positions = []PositionStatus{}
	}

	// Build new whitelist
	newWhitelist := wm.buildWhitelist(scores, positions)

	// Check if whitelist changed
	if !slicesEqual(newWhitelist, wm.activeWhitelist) {
		// Update VF whitelist
		if err := wm.vfController.UpdateWhitelist(newWhitelist); err != nil {
			return fmt.Errorf("failed to update VF whitelist: %w", err)
		}

		// Log the change
		added, removed := diffSlices(wm.activeWhitelist, newWhitelist)
		wm.logger.Info("Whitelist updated",
			zap.Strings("previous", wm.activeWhitelist),
			zap.Strings("new", newWhitelist),
			zap.Strings("added", added),
			zap.Strings("removed", removed),
		)

		wm.activeWhitelist = newWhitelist
		wm.updateCount++
	}

	wm.lastUpdate = time.Now()
	return nil
}

// buildWhitelist builds the new whitelist based on scores and positions
func (wm *WhitelistManager) buildWhitelist(
	scores map[string]SymbolScore,
	positions []PositionStatus,
) []string {
	cfg := wm.config
	newWhitelist := make([]string, 0, cfg.MaxSymbols)
	reasoning := make(map[string]string)

	// Create a map of symbols with open positions
	symbolsWithPositions := make(map[string]bool)
	for _, pos := range positions {
		if pos.HasPosition {
			symbolsWithPositions[pos.Symbol] = true
		}
	}

	// 1. First, collect all symbols that should be in whitelist
	candidates := make([]SymbolScore, 0, len(scores))
	for _, score := range scores {
		candidates = append(candidates, score)
	}

	// 2. Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	// 3. Add HIGH and MEDIUM scoring symbols up to max_symbols
	for _, score := range candidates {
		if len(newWhitelist) >= cfg.MaxSymbols {
			break
		}

		// Add if score is good enough
		if score.Recommendation == RecHigh || score.Recommendation == RecMedium {
			if score.Score >= cfg.MinScoreToAdd {
				newWhitelist = append(newWhitelist, score.Symbol)
				reasoning[score.Symbol] = fmt.Sprintf("Score %.1f (%s)", score.Score, score.Recommendation)
			}
		}
	}

	// 4. Keep symbols with open positions (if configured)
	if cfg.KeepIfPositionOpen {
		for symbol := range symbolsWithPositions {
			if !contains(newWhitelist, symbol) {
				// Check if score is not too low (below remove threshold)
				if score, ok := scores[symbol]; ok {
					if score.Score >= cfg.ScoreToRemove {
						if len(newWhitelist) < cfg.MaxSymbols {
							newWhitelist = append(newWhitelist, symbol)
							reasoning[symbol] = fmt.Sprintf("Kept (has position, score %.1f)", score.Score)
						}
					} else {
						wm.logger.Warn("Symbol has position but score too low",
							zap.String("symbol", symbol),
							zap.Float64("score", score.Score),
							zap.Float64("threshold", cfg.ScoreToRemove),
						)
					}
				} else {
					// No score data, keep it to be safe
					if len(newWhitelist) < cfg.MaxSymbols {
						newWhitelist = append(newWhitelist, symbol)
						reasoning[symbol] = "Kept (has position, no score data)"
					}
				}
			}
		}
	}

	// 5. Final sort by score (descending)
	sort.Slice(newWhitelist, func(i, j int) bool {
		scoreI := scores[newWhitelist[i]].Score
		scoreJ := scores[newWhitelist[j]].Score
		return scoreI > scoreJ
	})

	// 6. Cap at max_symbols
	if len(newWhitelist) > cfg.MaxSymbols {
		removed := newWhitelist[cfg.MaxSymbols:]
		newWhitelist = newWhitelist[:cfg.MaxSymbols]
		for _, sym := range removed {
			wm.logger.Debug("Symbol capped from whitelist", zap.String("symbol", sym))
		}
	}

	return newWhitelist
}

// GetCurrentWhitelist returns the current active whitelist
func (wm *WhitelistManager) GetCurrentWhitelist() []string {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	whitelist := make([]string, len(wm.activeWhitelist))
	copy(whitelist, wm.activeWhitelist)
	return whitelist
}

// GetCurrentScores returns the current symbol scores
func (wm *WhitelistManager) GetCurrentScores() map[string]SymbolScore {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	scores := make(map[string]SymbolScore, len(wm.currentScores))
	for k, v := range wm.currentScores {
		scores[k] = v
	}
	return scores
}

// GetLastUpdate returns the last whitelist update time
func (wm *WhitelistManager) GetLastUpdate() time.Time {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return wm.lastUpdate
}

// GetUpdateCount returns the number of whitelist updates
func (wm *WhitelistManager) GetUpdateCount() int {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	return wm.updateCount
}

// GetSymbolScore returns the score for a specific symbol
func (wm *WhitelistManager) GetSymbolScore(symbol string) (SymbolScore, bool) {
	wm.mu.RLock()
	defer wm.mu.RUnlock()
	score, ok := wm.currentScores[symbol]
	return score, ok
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func diffSlices(old, new []string) (added, removed []string) {
	oldMap := make(map[string]bool)
	newMap := make(map[string]bool)

	for _, s := range old {
		oldMap[s] = true
	}
	for _, s := range new {
		newMap[s] = true
	}

	for s := range newMap {
		if !oldMap[s] {
			added = append(added, s)
		}
	}

	for s := range oldMap {
		if !newMap[s] {
			removed = append(removed, s)
		}
	}

	return added, removed
}
