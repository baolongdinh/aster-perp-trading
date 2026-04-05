package adaptive_grid

import (
	"container/list"
	"sync"
	"time"

	"go.uber.org/zap"
)

// FillEvent represents a processed fill event
type FillEvent struct {
	OrderID   string
	Timestamp time.Time
}

// FillEventDeduplicator prevents duplicate fill processing
type FillEventDeduplicator struct {
	events    *list.List
	eventMap  map[string]*list.Element
	maxSize   int
	window    time.Duration
	mu        sync.RWMutex
	logger    *zap.Logger
}

// NewFillEventDeduplicator creates a new deduplicator
func NewFillEventDeduplicator(logger *zap.Logger) *FillEventDeduplicator {
	return &FillEventDeduplicator{
		events:   list.New(),
		eventMap: make(map[string]*list.Element),
		maxSize:  100,
		window:   30 * time.Second,
		logger:   logger,
	}
}

// IsDuplicate checks if a fill event is a duplicate
func (d *FillEventDeduplicator) IsDuplicate(orderID string, timestamp time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Clean old events first
	d.cleanupOldEvents()

	// Check if event exists
	if elem, exists := d.eventMap[orderID]; exists {
		event := elem.Value.(*FillEvent)
		// Check if within deduplication window
		if timestamp.Sub(event.Timestamp) <= d.window {
			d.logger.Warn("Duplicate fill event detected",
				zap.String("orderID", orderID),
				zap.Time("original", event.Timestamp),
				zap.Time("duplicate", timestamp))
			return true
		}
	}

	// Add new event
	event := &FillEvent{
		OrderID:   orderID,
		Timestamp: timestamp,
	}
	elem := d.events.PushBack(event)
	d.eventMap[orderID] = elem

	// Maintain max size
	if d.events.Len() > d.maxSize {
		oldest := d.events.Front()
		if oldest != nil {
			oldEvent := oldest.Value.(*FillEvent)
			delete(d.eventMap, oldEvent.OrderID)
			d.events.Remove(oldest)
		}
	}

	return false
}

// cleanupOldEvents removes events outside the deduplication window
func (d *FillEventDeduplicator) cleanupOldEvents() {
	cutoff := time.Now().Add(-d.window)

	for {
		elem := d.events.Front()
		if elem == nil {
			break
		}

		event := elem.Value.(*FillEvent)
		if event.Timestamp.Before(cutoff) {
			delete(d.eventMap, event.OrderID)
			d.events.Remove(elem)
		} else {
			break // List is ordered by time
		}
	}
}

// RecordEvent manually records an event without checking
func (d *FillEventDeduplicator) RecordEvent(orderID string, timestamp time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.cleanupOldEvents()

	// Remove old entry if exists
	if elem, exists := d.eventMap[orderID]; exists {
		d.events.Remove(elem)
		delete(d.eventMap, orderID)
	}

	// Add new event
	event := &FillEvent{
		OrderID:   orderID,
		Timestamp: timestamp,
	}
	elem := d.events.PushBack(event)
	d.eventMap[orderID] = elem

	// Maintain max size
	if d.events.Len() > d.maxSize {
		oldest := d.events.Front()
		if oldest != nil {
			oldEvent := oldest.Value.(*FillEvent)
			delete(d.eventMap, oldEvent.OrderID)
			d.events.Remove(oldest)
		}
	}
}

// GetStats returns current deduplicator statistics
func (d *FillEventDeduplicator) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"event_count": d.events.Len(),
		"max_size":    d.maxSize,
		"window":      d.window.String(),
	}
}

// Reset clears all recorded events
func (d *FillEventDeduplicator) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.events.Init()
	d.eventMap = make(map[string]*list.Element)
}
