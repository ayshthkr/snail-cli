package filter

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
	"snail-cli/internal/models"
)

// FilterManager manages filter execution with error handling, timeouts, and monitoring
type FilterManager struct {
	engine      interfaces.FilterEngine
	config      *ManagerConfig
	metrics     *FilterMetrics
	errorLog    *ErrorLogger
	recovery    *RecoveryManager
	mutex       sync.RWMutex
}

// ManagerConfig holds configuration for the filter manager
type ManagerConfig struct {
	MaxConcurrentFilters int
	DefaultTimeout       time.Duration
	MaxRetries           int
	RetryDelay           time.Duration
	EnableRecovery       bool
	EnableMetrics        bool
	ErrorLogSize         int
	CircuitBreakerConfig *CircuitBreakerConfig
}

// CircuitBreakerConfig holds configuration for circuit breaker
type CircuitBreakerConfig struct {
	FailureThreshold int
	RecoveryTimeout  time.Duration
	HalfOpenRequests int
}

// FilterMetrics tracks performance and error metrics for filters
type FilterMetrics struct {
	mutex           sync.RWMutex
	filterStats     map[string]*FilterStats
	globalStats     *GlobalStats
	startTime       time.Time
}

// FilterStats holds statistics for a specific filter
type FilterStats struct {
	Name            string
	ExecutionCount  int64
	SuccessCount    int64
	ErrorCount      int64
	TimeoutCount    int64
	TotalDuration   time.Duration
	AverageDuration time.Duration
	LastExecution   time.Time
	LastError       error
	CircuitState    CircuitState
}

// GlobalStats holds global filter system statistics
type GlobalStats struct {
	TotalExecutions int64
	TotalSuccesses  int64
	TotalErrors     int64
	TotalTimeouts   int64
	AverageLatency  time.Duration
	Uptime          time.Duration
}

// CircuitState represents the state of a circuit breaker
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// ErrorLogger manages filter error logging
type ErrorLogger struct {
	mutex   sync.RWMutex
	errors  []FilterError
	maxSize int
}

// FilterError represents a filter execution error
type FilterError struct {
	FilterName string
	Error      error
	Email      *interfaces.Email
	Event      interfaces.FilterEvent
	Timestamp  time.Time
	Context    map[string]interface{}
}

// RecoveryManager handles filter recovery and circuit breaking
type RecoveryManager struct {
	mutex           sync.RWMutex
	circuitBreakers map[string]*CircuitBreaker
	config          *CircuitBreakerConfig
}

// CircuitBreaker implements circuit breaker pattern for filters
type CircuitBreaker struct {
	name            string
	state           CircuitState
	failureCount    int
	lastFailureTime time.Time
	halfOpenCount   int
	config          *CircuitBreakerConfig
	mutex           sync.RWMutex
}

// NewFilterManager creates a new filter manager
func NewFilterManager(engine interfaces.FilterEngine, config *ManagerConfig) *FilterManager {
	if config == nil {
		config = &ManagerConfig{
			MaxConcurrentFilters: 10,
			DefaultTimeout:       30 * time.Second,
			MaxRetries:           3,
			RetryDelay:           1 * time.Second,
			EnableRecovery:       true,
			EnableMetrics:        true,
			ErrorLogSize:         1000,
			CircuitBreakerConfig: &CircuitBreakerConfig{
				FailureThreshold: 5,
				RecoveryTimeout:  60 * time.Second,
				HalfOpenRequests: 3,
			},
		}
	}

	metrics := &FilterMetrics{
		filterStats: make(map[string]*FilterStats),
		globalStats: &GlobalStats{},
		startTime:   time.Now(),
	}

	errorLog := &ErrorLogger{
		errors:  make([]FilterError, 0, config.ErrorLogSize),
		maxSize: config.ErrorLogSize,
	}

	recovery := &RecoveryManager{
		circuitBreakers: make(map[string]*CircuitBreaker),
		config:          config.CircuitBreakerConfig,
	}

	return &FilterManager{
		engine:   engine,
		config:   config,
		metrics:  metrics,
		errorLog: errorLog,
		recovery: recovery,
	}
}

// ProcessEmailWithErrorHandling processes an email with comprehensive error handling
func (fm *FilterManager) ProcessEmailWithErrorHandling(ctx context.Context, email *models.Email, event interfaces.FilterEvent) (*interfaces.FilterResult, error) {
	start := time.Now()

	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, fm.config.DefaultTimeout)
	defer cancel()

	// Get applicable filters
	filters := fm.engine.ListFilters()
	applicableFilters := make([]interfaces.Filter, 0)
	
	for _, filter := range filters {
		if filter.IsEnabled() && fm.supportsEvent(filter, event) {
			// Check circuit breaker
			if fm.config.EnableRecovery && !fm.recovery.CanExecute(filter.Name()) {
				fm.logError(FilterError{
					FilterName: filter.Name(),
					Error:      fmt.Errorf("circuit breaker open"),
					Email:      convertModelsToInterface(email),
					Event:      event,
					Timestamp:  time.Now(),
					Context:    map[string]interface{}{"reason": "circuit_breaker_open"},
				})
				continue
			}
			applicableFilters = append(applicableFilters, filter)
		}
	}

	if len(applicableFilters) == 0 {
		return &interfaces.FilterResult{
			Modified: false,
			Email:    convertModelsToInterface(email),
			Actions:  []interfaces.FilterAction{},
			Labels:   []string{},
		}, nil
	}

	// Process filters with error handling
	result, err := fm.processFiltersWithRetry(timeoutCtx, email, event, applicableFilters)
	
	// Update global metrics
	if fm.config.EnableMetrics {
		fm.updateGlobalMetrics(time.Since(start), err == nil)
	}

	return result, err
}

// processFiltersWithRetry processes filters with retry logic
func (fm *FilterManager) processFiltersWithRetry(ctx context.Context, email *models.Email, event interfaces.FilterEvent, filters []interfaces.Filter) (*interfaces.FilterResult, error) {
	var lastErr error
	
	for attempt := 0; attempt <= fm.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-time.After(fm.config.RetryDelay * time.Duration(attempt)):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		result, err := fm.executeFilters(ctx, email, event, filters)
		if err == nil {
			return result, nil
		}

		lastErr = err
		
		// Check if error is retryable
		if !fm.isRetryableError(err) {
			break
		}
	}

	return nil, fmt.Errorf("filter execution failed after %d attempts: %w", fm.config.MaxRetries+1, lastErr)
}

// executeFilters executes filters with individual error handling
func (fm *FilterManager) executeFilters(ctx context.Context, email *models.Email, event interfaces.FilterEvent, filters []interfaces.Filter) (*interfaces.FilterResult, error) {
	interfaceEmail := convertModelsToInterface(email)
	currentEmail := interfaceEmail
	allActions := make([]interfaces.FilterAction, 0)
	allLabels := make([]string, 0)
	totalDuration := time.Duration(0)
	modified := false

	for _, filter := range filters {
		start := time.Now()
		
		// Execute filter with timeout and error handling
		result, err := fm.executeFilterSafely(ctx, filter, currentEmail, event)
		duration := time.Since(start)
		totalDuration += duration

		// Update filter metrics
		if fm.config.EnableMetrics {
			fm.updateFilterMetrics(filter.Name(), duration, err == nil, err)
		}

		// Handle filter error
		if err != nil {
			fm.handleFilterError(filter, err, interfaceEmail, event)
			
			// Continue with other filters unless it's a critical error
			if fm.isCriticalError(err) {
				return nil, fmt.Errorf("critical filter error in %s: %w", filter.Name(), err)
			}
			continue
		}

		// Update circuit breaker on success
		if fm.config.EnableRecovery {
			fm.recovery.RecordSuccess(filter.Name())
		}

		// Process filter result
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

// executeFilterSafely executes a filter with panic recovery
func (fm *FilterManager) executeFilterSafely(ctx context.Context, filter interfaces.Filter, email *interfaces.Email, event interfaces.FilterEvent) (result *interfaces.FilterResult, err error) {
	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("filter panic: %v", r)
			result = &interfaces.FilterResult{
				Modified: false,
				Email:    email,
				Actions:  []interfaces.FilterAction{},
				Labels:   []string{},
				Error:    err,
			}
		}
	}()

	// Execute filter
	return filter.Execute(ctx, email, event)
}

// handleFilterError handles filter execution errors
func (fm *FilterManager) handleFilterError(filter interfaces.Filter, err error, email *interfaces.Email, event interfaces.FilterEvent) {
	// Log error
	filterError := FilterError{
		FilterName: filter.Name(),
		Error:      err,
		Email:      email,
		Event:      event,
		Timestamp:  time.Now(),
		Context:    map[string]interface{}{"filter_type": fmt.Sprintf("%T", filter)},
	}
	
	fm.logError(filterError)

	// Update circuit breaker
	if fm.config.EnableRecovery {
		fm.recovery.RecordFailure(filter.Name())
	}

	// Log to system logger
	log.Printf("Filter error in %s: %v", filter.Name(), err)
}

// logError logs a filter error
func (fm *FilterManager) logError(filterError FilterError) {
	fm.errorLog.mutex.Lock()
	defer fm.errorLog.mutex.Unlock()

	// Add error to log
	fm.errorLog.errors = append(fm.errorLog.errors, filterError)

	// Trim log if it exceeds max size
	if len(fm.errorLog.errors) > fm.errorLog.maxSize {
		fm.errorLog.errors = fm.errorLog.errors[1:]
	}
}

// updateFilterMetrics updates metrics for a specific filter
func (fm *FilterManager) updateFilterMetrics(filterName string, duration time.Duration, success bool, err error) {
	fm.metrics.mutex.Lock()
	defer fm.metrics.mutex.Unlock()

	stats, exists := fm.metrics.filterStats[filterName]
	if !exists {
		stats = &FilterStats{
			Name:         filterName,
			CircuitState: CircuitClosed,
		}
		fm.metrics.filterStats[filterName] = stats
	}

	stats.ExecutionCount++
	stats.TotalDuration += duration
	stats.AverageDuration = time.Duration(int64(stats.TotalDuration) / stats.ExecutionCount)
	stats.LastExecution = time.Now()

	if success {
		stats.SuccessCount++
	} else {
		stats.ErrorCount++
		stats.LastError = err
		
		// Check for timeout
		if err == context.DeadlineExceeded {
			stats.TimeoutCount++
		}
	}

	// Update circuit state
	if cb := fm.recovery.getCircuitBreaker(filterName); cb != nil {
		stats.CircuitState = cb.GetState()
	}
}

// updateGlobalMetrics updates global system metrics
func (fm *FilterManager) updateGlobalMetrics(duration time.Duration, success bool) {
	fm.metrics.mutex.Lock()
	defer fm.metrics.mutex.Unlock()

	fm.metrics.globalStats.TotalExecutions++
	
	if success {
		fm.metrics.globalStats.TotalSuccesses++
	} else {
		fm.metrics.globalStats.TotalErrors++
	}

	// Update average latency
	totalDuration := time.Duration(int64(fm.metrics.globalStats.AverageLatency) * (fm.metrics.globalStats.TotalExecutions - 1))
	totalDuration += duration
	fm.metrics.globalStats.AverageLatency = time.Duration(int64(totalDuration) / fm.metrics.globalStats.TotalExecutions)

	// Update uptime
	fm.metrics.globalStats.Uptime = time.Since(fm.metrics.startTime)
}

// GetFilterMetrics returns metrics for all filters
func (fm *FilterManager) GetFilterMetrics() map[string]*FilterStats {
	fm.metrics.mutex.RLock()
	defer fm.metrics.mutex.RUnlock()

	result := make(map[string]*FilterStats)
	for name, stats := range fm.metrics.filterStats {
		// Create a copy to avoid race conditions
		statsCopy := *stats
		result[name] = &statsCopy
	}
	return result
}

// GetGlobalMetrics returns global system metrics
func (fm *FilterManager) GetGlobalMetrics() *GlobalStats {
	fm.metrics.mutex.RLock()
	defer fm.metrics.mutex.RUnlock()

	// Return a copy
	statsCopy := *fm.metrics.globalStats
	return &statsCopy
}

// GetRecentErrors returns recent filter errors
func (fm *FilterManager) GetRecentErrors(limit int) []FilterError {
	fm.errorLog.mutex.RLock()
	defer fm.errorLog.mutex.RUnlock()

	errors := fm.errorLog.errors
	if limit > 0 && len(errors) > limit {
		errors = errors[len(errors)-limit:]
	}

	// Return a copy
	result := make([]FilterError, len(errors))
	copy(result, errors)
	return result
}

// supportsEvent checks if a filter supports a specific event
func (fm *FilterManager) supportsEvent(filter interfaces.Filter, event interfaces.FilterEvent) bool {
	for _, supportedEvent := range filter.Events() {
		if supportedEvent == event {
			return true
		}
	}
	return false
}

// isRetryableError checks if an error is retryable
func (fm *FilterManager) isRetryableError(err error) bool {
	// Timeout errors are retryable
	if err == context.DeadlineExceeded {
		return true
	}

	// Network errors are typically retryable
	errStr := err.Error()
	retryableErrors := []string{
		"connection refused",
		"timeout",
		"temporary failure",
		"service unavailable",
	}

	for _, retryable := range retryableErrors {
		if strings.Contains(strings.ToLower(errStr), retryable) {
			return true
		}
	}

	return false
}

// isCriticalError checks if an error is critical and should stop processing
func (fm *FilterManager) isCriticalError(err error) bool {
	criticalErrors := []string{
		"out of memory",
		"security violation",
	}

	errStr := strings.ToLower(err.Error())
	for _, critical := range criticalErrors {
		if strings.Contains(errStr, critical) {
			return true
		}
	}

	return false
}

// convertModelsToInterface converts models.Email to interfaces.Email
func convertModelsToInterface(email *models.Email) *interfaces.Email {
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

// Circuit Breaker Implementation

// CanExecute checks if a filter can be executed based on circuit breaker state
func (rm *RecoveryManager) CanExecute(filterName string) bool {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	cb := rm.circuitBreakers[filterName]
	if cb == nil {
		return true
	}

	return cb.CanExecute()
}

// RecordSuccess records a successful filter execution
func (rm *RecoveryManager) RecordSuccess(filterName string) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	cb := rm.getOrCreateCircuitBreaker(filterName)
	cb.RecordSuccess()
}

// RecordFailure records a failed filter execution
func (rm *RecoveryManager) RecordFailure(filterName string) {
	rm.mutex.Lock()
	defer rm.mutex.Unlock()

	cb := rm.getOrCreateCircuitBreaker(filterName)
	cb.RecordFailure()
}

// getCircuitBreaker gets a circuit breaker for a filter
func (rm *RecoveryManager) getCircuitBreaker(filterName string) *CircuitBreaker {
	rm.mutex.RLock()
	defer rm.mutex.RUnlock()

	return rm.circuitBreakers[filterName]
}

// getOrCreateCircuitBreaker gets or creates a circuit breaker for a filter
func (rm *RecoveryManager) getOrCreateCircuitBreaker(filterName string) *CircuitBreaker {
	cb, exists := rm.circuitBreakers[filterName]
	if !exists {
		cb = &CircuitBreaker{
			name:   filterName,
			state:  CircuitClosed,
			config: rm.config,
		}
		rm.circuitBreakers[filterName] = cb
	}
	return cb
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, config *CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		name:   name,
		state:  CircuitClosed,
		config: config,
	}
}

// CanExecute checks if the circuit breaker allows execution
func (cb *CircuitBreaker) CanExecute() bool {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()

	if cb.config == nil {
		return true // Allow execution if no config
	}

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if recovery timeout has passed
		if time.Since(cb.lastFailureTime) > cb.config.RecoveryTimeout {
			return true // Will transition to half-open on next call
		}
		return false
	case CircuitHalfOpen:
		return cb.halfOpenCount < cb.config.HalfOpenRequests
	default:
		return false
	}
}

// RecordSuccess records a successful execution
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if cb.config == nil {
		return // Skip if no config
	}

	switch cb.state {
	case CircuitClosed:
		cb.failureCount = 0
	case CircuitHalfOpen:
		cb.halfOpenCount++
		if cb.halfOpenCount >= cb.config.HalfOpenRequests {
			cb.state = CircuitClosed
			cb.failureCount = 0
			cb.halfOpenCount = 0
		}
	case CircuitOpen:
		// Transition to half-open
		cb.state = CircuitHalfOpen
		cb.halfOpenCount = 1
		cb.failureCount = 0
	}
}

// RecordFailure records a failed execution
func (cb *CircuitBreaker) RecordFailure() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	if cb.config == nil {
		return // Skip if no config
	}

	cb.failureCount++
	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failureCount >= cb.config.FailureThreshold {
			cb.state = CircuitOpen
		}
	case CircuitHalfOpen:
		cb.state = CircuitOpen
		cb.halfOpenCount = 0
	}
}

// GetState returns the current circuit breaker state
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// GetFailureCount returns the current failure count
func (cb *CircuitBreaker) GetFailureCount() int {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.failureCount
}

// Reset resets the circuit breaker to closed state
func (cb *CircuitBreaker) Reset() {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	cb.state = CircuitClosed
	cb.failureCount = 0
	cb.halfOpenCount = 0
}

// String returns a string representation of the circuit state
func (cs CircuitState) String() string {
	switch cs {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}