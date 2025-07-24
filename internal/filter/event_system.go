package filter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
	"snail-cli/internal/models"
)

// EventSystem manages email events and filter triggering
type EventSystem struct {
	engine      interfaces.FilterEngine
	subscribers map[interfaces.FilterEvent][]EventSubscriber
	mutex       sync.RWMutex
	config      *EventSystemConfig
}

// EventSystemConfig holds configuration for the event system
type EventSystemConfig struct {
	MaxConcurrentEvents int
	EventTimeout        time.Duration
	EnableAsync         bool
	BufferSize          int
}

// EventSubscriber represents a subscriber to email events
type EventSubscriber interface {
	OnEvent(ctx context.Context, email *models.Email, event interfaces.FilterEvent) error
	Name() string
}

// EmailEventData contains data for an email event
type EmailEventData struct {
	Email     *models.Email
	Event     interfaces.FilterEvent
	Timestamp time.Time
	Context   context.Context
}

// NewEventSystem creates a new event system
func NewEventSystem(engine interfaces.FilterEngine, config *EventSystemConfig) *EventSystem {
	if config == nil {
		config = &EventSystemConfig{
			MaxConcurrentEvents: 50,
			EventTimeout:        60 * time.Second,
			EnableAsync:         true,
			BufferSize:          1000,
		}
	}

	return &EventSystem{
		engine:      engine,
		subscribers: make(map[interfaces.FilterEvent][]EventSubscriber),
		config:      config,
	}
}

// Subscribe adds a subscriber for specific events
func (es *EventSystem) Subscribe(events []interfaces.FilterEvent, subscriber EventSubscriber) error {
	if subscriber == nil {
		return fmt.Errorf("subscriber cannot be nil")
	}

	es.mutex.Lock()
	defer es.mutex.Unlock()

	for _, event := range events {
		if !es.isValidEvent(event) {
			return fmt.Errorf("invalid event: %s", event)
		}

		if es.subscribers[event] == nil {
			es.subscribers[event] = make([]EventSubscriber, 0)
		}

		// Check if subscriber already exists
		for _, existing := range es.subscribers[event] {
			if existing.Name() == subscriber.Name() {
				return fmt.Errorf("subscriber '%s' already registered for event '%s'", subscriber.Name(), event)
			}
		}

		es.subscribers[event] = append(es.subscribers[event], subscriber)
	}

	return nil
}

// Unsubscribe removes a subscriber from specific events
func (es *EventSystem) Unsubscribe(events []interfaces.FilterEvent, subscriberName string) error {
	es.mutex.Lock()
	defer es.mutex.Unlock()

	for _, event := range events {
		subscribers := es.subscribers[event]
		for i, subscriber := range subscribers {
			if subscriber.Name() == subscriberName {
				// Remove subscriber from slice
				es.subscribers[event] = append(subscribers[:i], subscribers[i+1:]...)
				break
			}
		}
	}

	return nil
}

// TriggerEvent triggers an email event and processes it through filters
func (es *EventSystem) TriggerEvent(ctx context.Context, email *models.Email, event interfaces.FilterEvent) (*interfaces.FilterResult, error) {
	if email == nil {
		return nil, fmt.Errorf("email cannot be nil")
	}

	if !es.isValidEvent(event) {
		return nil, fmt.Errorf("invalid event: %s", event)
	}

	// Create event context with timeout
	eventCtx, cancel := context.WithTimeout(ctx, es.config.EventTimeout)
	defer cancel()

	// Process through filter engine first
	result, err := es.engine.ProcessEmail(eventCtx, email, event)
	if err != nil {
		return nil, fmt.Errorf("filter processing failed: %w", err)
	}

	// Notify subscribers
	if err := es.notifySubscribers(eventCtx, email, event); err != nil {
		// Log error but don't fail the entire event processing
		// In a production system, you'd want proper logging here
	}

	return result, nil
}

// TriggerEventAsync triggers an email event asynchronously
func (es *EventSystem) TriggerEventAsync(ctx context.Context, email *models.Email, event interfaces.FilterEvent) <-chan *EventResult {
	resultChan := make(chan *EventResult, 1)

	go func() {
		defer close(resultChan)

		result, err := es.TriggerEvent(ctx, email, event)
		resultChan <- &EventResult{
			FilterResult: result,
			Error:        err,
			Timestamp:    time.Now(),
		}
	}()

	return resultChan
}

// EventResult contains the result of an event processing
type EventResult struct {
	FilterResult *interfaces.FilterResult
	Error        error
	Timestamp    time.Time
}

// notifySubscribers notifies all subscribers of an event
func (es *EventSystem) notifySubscribers(ctx context.Context, email *models.Email, event interfaces.FilterEvent) error {
	es.mutex.RLock()
	subscribers := es.subscribers[event]
	es.mutex.RUnlock()

	if len(subscribers) == 0 {
		return nil
	}

	// Create a wait group for concurrent notification
	var wg sync.WaitGroup
	errorChan := make(chan error, len(subscribers))

	for _, subscriber := range subscribers {
		wg.Add(1)
		go func(sub EventSubscriber) {
			defer wg.Done()
			
			// Create timeout context for this subscriber
			subCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			
			if err := sub.OnEvent(subCtx, email, event); err != nil {
				errorChan <- fmt.Errorf("subscriber '%s' failed: %w", sub.Name(), err)
			}
		}(subscriber)
	}

	// Wait for all subscribers to complete
	wg.Wait()
	close(errorChan)

	// Collect any errors
	var errors []error
	for err := range errorChan {
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return fmt.Errorf("subscriber errors: %v", errors)
	}

	return nil
}

// isValidEvent checks if an event is valid
func (es *EventSystem) isValidEvent(event interfaces.FilterEvent) bool {
	switch event {
	case interfaces.EventEmailReceived,
		interfaces.EventEmailSent,
		interfaces.EventEmailRead,
		interfaces.EventEmailDeleted,
		interfaces.EventEmailMoved:
		return true
	default:
		return false
	}
}

// GetSubscribers returns all subscribers for a specific event
func (es *EventSystem) GetSubscribers(event interfaces.FilterEvent) []EventSubscriber {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	subscribers := es.subscribers[event]
	result := make([]EventSubscriber, len(subscribers))
	copy(result, subscribers)
	return result
}

// GetAllEvents returns all events that have subscribers
func (es *EventSystem) GetAllEvents() []interfaces.FilterEvent {
	es.mutex.RLock()
	defer es.mutex.RUnlock()

	events := make([]interfaces.FilterEvent, 0, len(es.subscribers))
	for event := range es.subscribers {
		events = append(events, event)
	}
	return events
}

// FilterEventSubscriber is a subscriber that processes events through the filter engine
type FilterEventSubscriber struct {
	name   string
	engine interfaces.FilterEngine
}

// NewFilterEventSubscriber creates a new filter event subscriber
func NewFilterEventSubscriber(name string, engine interfaces.FilterEngine) *FilterEventSubscriber {
	return &FilterEventSubscriber{
		name:   name,
		engine: engine,
	}
}

// OnEvent processes an email event through the filter engine
func (fes *FilterEventSubscriber) OnEvent(ctx context.Context, email *models.Email, event interfaces.FilterEvent) error {
	_, err := fes.engine.ProcessEmail(ctx, email, event)
	return err
}

// Name returns the subscriber name
func (fes *FilterEventSubscriber) Name() string {
	return fes.name
}

// EmailEventLogger is a subscriber that logs email events
type EmailEventLogger struct {
	name string
}

// NewEmailEventLogger creates a new email event logger
func NewEmailEventLogger(name string) *EmailEventLogger {
	return &EmailEventLogger{
		name: name,
	}
}

// OnEvent logs an email event
func (eel *EmailEventLogger) OnEvent(ctx context.Context, email *models.Email, event interfaces.FilterEvent) error {
	// In a production system, you'd use a proper logger here
	fmt.Printf("[%s] Event: %s, Email: %s, Subject: %s\n", 
		time.Now().Format(time.RFC3339), 
		event, 
		email.ID, 
		email.Subject)
	return nil
}

// Name returns the subscriber name
func (eel *EmailEventLogger) Name() string {
	return eel.name
}