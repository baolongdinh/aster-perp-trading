package agentic

import (
	"sync"

	"go.uber.org/zap"
)

// EventPublisher publishes state transition events to subscribers
// Used for coordinating between workers (GRID, TREND, RISK)
type EventPublisher struct {
	subscribers []chan<- StateTransition
	mu          sync.RWMutex
	logger      *zap.Logger
}

// NewEventPublisher creates a new event publisher
func NewEventPublisher(logger *zap.Logger) *EventPublisher {
	return &EventPublisher{
		subscribers: make([]chan<- StateTransition, 0),
		logger:      logger.With(zap.String("component", "event_publisher")),
	}
}

// Subscribe registers a channel to receive state transitions
func (ep *EventPublisher) Subscribe(ch chan<- StateTransition) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	
	ep.subscribers = append(ep.subscribers, ch)
	ep.logger.Debug("New subscriber registered", zap.Int("total_subscribers", len(ep.subscribers)))
}

// Unsubscribe removes a subscriber
func (ep *EventPublisher) Unsubscribe(ch chan<- StateTransition) {
	ep.mu.Lock()
	defer ep.mu.Unlock()
	
	for i, sub := range ep.subscribers {
		if sub == ch {
			// Remove by swapping with last and truncating
			ep.subscribers[i] = ep.subscribers[len(ep.subscribers)-1]
			ep.subscribers = ep.subscribers[:len(ep.subscribers)-1]
			break
		}
	}
}

// Publish broadcasts a transition to all subscribers (non-blocking)
func (ep *EventPublisher) Publish(transition StateTransition) {
	ep.mu.RLock()
	subscribers := make([]chan<- StateTransition, len(ep.subscribers))
	copy(subscribers, ep.subscribers)
	ep.mu.RUnlock()
	
	dropped := 0
	for _, ch := range subscribers {
		select {
		case ch <- transition:
			// Successfully sent
		default:
			// Channel full, drop the event
			dropped++
		}
	}
	
	if dropped > 0 {
		ep.logger.Warn("Dropped transitions due to full channels",
			zap.Int("dropped", dropped),
			zap.String("from", string(transition.FromState)),
			zap.String("to", string(transition.ToState)),
		)
	}
}

// SubscriberCount returns the number of active subscribers
func (ep *EventPublisher) SubscriberCount() int {
	ep.mu.RLock()
	defer ep.mu.RUnlock()
	return len(ep.subscribers)
}
