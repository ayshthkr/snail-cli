package filter

import (
	"context"
	"fmt"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
	"snail-cli/internal/models"
)

// Engine implements the FilterEngine interface
type Engine struct {
	filters map[string]interfaces.Filter
	mutex   sync.RWMutex
	config  *Config
}

// Config holds configuration for the filter engine
type Config struct {
	MaxConcurrentFilters int
	DefaultTimeout       time.Duration
	EnableSandboxing     bool
	WorkingDirectory     string
}

// NewEngine creates a new filter engine
func NewEngine(config *Config) *Engine {
	if config == nil {
		config = &Config{
			MaxConcurrentFilters: 10,
			DefaultTimeout:       30 * time.Second,
			EnableSandboxing:     true,
			WorkingDirectory:     "/tmp/snail-filters",
		}
	}

	return &Engine{
		filters: make(map[string]interfaces.Filter),
		config:  config,
	}
}

// RegisterFilter adds a new filter to the engine
func (e *Engine) RegisterFilter(filter interfaces.Filter) error {
	if filter == nil {
		return fmt.Errorf("filter cannot be nil")
	}

	if err := e.ValidateFilter(filter); err != nil {
		return fmt.Errorf("filter validation failed: %w", err)
	}

	e.mutex.Lock()
	defer e.mutex.Unlock()

	name := filter.Name()
	if _, exists := e.filters[name]; exists {
		return fmt.Errorf("filter with name '%s' already exists", name)
	}

	e.filters[name] = filter
	return nil
}

// UnregisterFilter removes a filter from the engine
func (e *Engine) UnregisterFilter(name string) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if _, exists := e.filters[name]; !exists {
		return fmt.Errorf("filter with name '%s' not found", name)
	}

	delete(e.filters, name)
	return nil
}

// ProcessEmail applies all applicable filters to an email
func (e *Engine) ProcessEmail(ctx context.Context, email interface{}, event interfaces.FilterEvent) (*interfaces.FilterResult, error) {
	if email == nil {
		return nil, fmt.Errorf("email cannot be nil")
	}

	// Convert to models.Email if needed
	var modelsEmail *models.Email
	switch v := email.(type) {
	case *models.Email:
		modelsEmail = v
	case *interfaces.Email:
		// Convert interfaces.Email to models.Email
		modelsEmail = e.convertInterfaceEmailToModels(v)
	default:
		return nil, fmt.Errorf("unsupported email type: %T", email)
	}

	// Convert models.Email to interfaces.Email for compatibility
	interfaceEmail := e.convertModelsEmailToInterface(modelsEmail)
	if interfaceEmail == nil {
		return nil, fmt.Errorf("failed to convert email")
	}

	e.mutex.RLock()
	applicableFilters := make([]interfaces.Filter, 0)
	for _, filter := range e.filters {
		if filter.IsEnabled() && e.supportsEvent(filter, event) {
			applicableFilters = append(applicableFilters, filter)
		}
	}
	e.mutex.RUnlock()

	if len(applicableFilters) == 0 {
		return &interfaces.FilterResult{
			Modified: false,
			Email:    interfaceEmail,
			Actions:  []interfaces.FilterAction{},
			Labels:   []string{},
		}, nil
	}

	// Process filters sequentially to maintain order
	currentEmail := interfaceEmail
	allActions := make([]interfaces.FilterAction, 0)
	allLabels := make([]string, 0)
	totalDuration := time.Duration(0)
	modified := false

	for _, filter := range applicableFilters {
		start := time.Now()
		
		// Create timeout context for this filter
		filterCtx, cancel := context.WithTimeout(ctx, e.config.DefaultTimeout)
		
		result, err := filter.Execute(filterCtx, currentEmail, event)
		cancel()
		
		duration := time.Since(start)
		totalDuration += duration

		if err != nil {
			// Log error but continue with other filters
			continue
		}

		if result != nil {
			if result.Modified {
				modified = true
				currentEmail = result.Email
			}
			
			allActions = append(allActions, result.Actions...)
			allLabels = append(allLabels, result.Labels...)

			// If filter requests to stop processing, break
			if result.StopProcessing {
				break
			}
		}
	}

	return &interfaces.FilterResult{
		Modified: modified,
		Email:    currentEmail,
		Actions:  allActions,
		Labels:   allLabels,
		Duration: totalDuration,
	}, nil
}

// ListFilters returns all registered filters
func (e *Engine) ListFilters() []interfaces.Filter {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	filters := make([]interfaces.Filter, 0, len(e.filters))
	for _, filter := range e.filters {
		filters = append(filters, filter)
	}
	return filters
}

// GetFilter returns a specific filter by name
func (e *Engine) GetFilter(name string) (interfaces.Filter, error) {
	e.mutex.RLock()
	defer e.mutex.RUnlock()

	filter, exists := e.filters[name]
	if !exists {
		return nil, fmt.Errorf("filter with name '%s' not found", name)
	}
	return filter, nil
}

// ValidateFilter checks if a filter configuration is valid
func (e *Engine) ValidateFilter(filter interfaces.Filter) error {
	if filter.Name() == "" {
		return fmt.Errorf("filter name cannot be empty")
	}

	events := filter.Events()
	if len(events) == 0 {
		return fmt.Errorf("filter must handle at least one event")
	}

	for _, event := range events {
		if !e.isValidEvent(event) {
			return fmt.Errorf("invalid event: %s", event)
		}
	}

	return nil
}

// supportsEvent checks if a filter supports a specific event
func (e *Engine) supportsEvent(filter interfaces.Filter, event interfaces.FilterEvent) bool {
	for _, supportedEvent := range filter.Events() {
		if supportedEvent == event {
			return true
		}
	}
	return false
}

// isValidEvent checks if an event is valid
func (e *Engine) isValidEvent(event interfaces.FilterEvent) bool {
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

// convertModelsEmailToInterface converts a models.Email to interfaces.Email
func (e *Engine) convertModelsEmailToInterface(email *models.Email) *interfaces.Email {
	if email == nil {
		return nil
	}
	
	interfaceEmail := &interfaces.Email{
		ID:          email.ID,
		MessageID:   email.MessageID,
		From:        interfaces.Address{Name: email.From.Name, Email: email.From.Email},
		Subject:     email.Subject,
		Date:        email.Date,
		Body:        email.Body,
		Headers:     email.Headers,
		Labels:      email.Labels,
		Status:      interfaces.EmailStatus(email.Status),
		SyncID:      email.SyncID,
	}

	// Convert To addresses
	for _, addr := range email.To {
		interfaceEmail.To = append(interfaceEmail.To, interfaces.Address{
			Name:  addr.Name,
			Email: addr.Email,
		})
	}

	// Convert CC addresses
	for _, addr := range email.CC {
		interfaceEmail.CC = append(interfaceEmail.CC, interfaces.Address{
			Name:  addr.Name,
			Email: addr.Email,
		})
	}

	// Convert BCC addresses
	for _, addr := range email.BCC {
		interfaceEmail.BCC = append(interfaceEmail.BCC, interfaces.Address{
			Name:  addr.Name,
			Email: addr.Email,
		})
	}

	// Convert attachments
	for _, att := range email.Attachments {
		interfaceEmail.Attachments = append(interfaceEmail.Attachments, interfaces.Attachment{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        att.Size,
			Data:        att.Data,
		})
	}

	return interfaceEmail
}

// convertInterfaceEmailToModels converts an interfaces.Email to models.Email
func (e *Engine) convertInterfaceEmailToModels(email *interfaces.Email) *models.Email {
	modelsEmail := &models.Email{
		ID:          email.ID,
		MessageID:   email.MessageID,
		From:        models.Address{Name: email.From.Name, Email: email.From.Email},
		Subject:     email.Subject,
		Date:        email.Date,
		Body:        email.Body,
		Headers:     email.Headers,
		Labels:      email.Labels,
		Status:      models.EmailStatus(email.Status),
		SyncID:      email.SyncID,
	}

	// Convert To addresses
	for _, addr := range email.To {
		modelsEmail.To = append(modelsEmail.To, models.Address{
			Name:  addr.Name,
			Email: addr.Email,
		})
	}

	// Convert CC addresses
	for _, addr := range email.CC {
		modelsEmail.CC = append(modelsEmail.CC, models.Address{
			Name:  addr.Name,
			Email: addr.Email,
		})
	}

	// Convert BCC addresses
	for _, addr := range email.BCC {
		modelsEmail.BCC = append(modelsEmail.BCC, models.Address{
			Name:  addr.Name,
			Email: addr.Email,
		})
	}

	// Convert attachments
	for _, att := range email.Attachments {
		modelsEmail.Attachments = append(modelsEmail.Attachments, models.Attachment{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        att.Size,
			Data:        att.Data,
		})
	}

	return modelsEmail
}