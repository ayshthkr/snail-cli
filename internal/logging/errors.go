package logging

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// ErrorType represents different categories of errors
type ErrorType string

const (
	ErrorTypeNetwork      ErrorType = "network"
	ErrorTypeStorage      ErrorType = "storage"
	ErrorTypeSyncConflict ErrorType = "sync_conflict"
	ErrorTypeFilter       ErrorType = "filter"
	ErrorTypeAuth         ErrorType = "auth"
	ErrorTypeValidation   ErrorType = "validation"
	ErrorTypeSystem       ErrorType = "system"
)

// ErrorSeverity represents the severity level of an error
type ErrorSeverity string

const (
	SeverityLow      ErrorSeverity = "low"
	SeverityMedium   ErrorSeverity = "medium"
	SeverityHigh     ErrorSeverity = "high"
	SeverityCritical ErrorSeverity = "critical"
)

// RecoverableError represents an error that can potentially be recovered from
type RecoverableError struct {
	Type        ErrorType
	Severity    ErrorSeverity
	Message     string
	Cause       error
	Context     map[string]interface{}
	Timestamp   time.Time
	Recoverable bool
	Suggestions []string
}

// Error implements the error interface
func (e *RecoverableError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap returns the underlying error
func (e *RecoverableError) Unwrap() error {
	return e.Cause
}

// NewRecoverableError creates a new recoverable error
func NewRecoverableError(errType ErrorType, severity ErrorSeverity, message string, cause error) *RecoverableError {
	return &RecoverableError{
		Type:        errType,
		Severity:    severity,
		Message:     message,
		Cause:       cause,
		Context:     make(map[string]interface{}),
		Timestamp:   time.Now(),
		Recoverable: true,
		Suggestions: []string{},
	}
}

// WithContext adds context information to the error
func (e *RecoverableError) WithContext(key string, value interface{}) *RecoverableError {
	e.Context[key] = value
	return e
}

// WithSuggestion adds a recovery suggestion to the error
func (e *RecoverableError) WithSuggestion(suggestion string) *RecoverableError {
	e.Suggestions = append(e.Suggestions, suggestion)
	return e
}

// SetRecoverable sets whether the error is recoverable
func (e *RecoverableError) SetRecoverable(recoverable bool) *RecoverableError {
	e.Recoverable = recoverable
	return e
}

// FormatUserMessage formats a user-friendly error message with suggestions
func (e *RecoverableError) FormatUserMessage() string {
	var parts []string
	
	// Add the main error message
	parts = append(parts, fmt.Sprintf("Error: %s", e.Message))
	
	// Add error type and severity
	parts = append(parts, fmt.Sprintf("Type: %s, Severity: %s", e.Type, e.Severity))
	
	// Add context information if available
	if len(e.Context) > 0 {
		var contextParts []string
		for k, v := range e.Context {
			contextParts = append(contextParts, fmt.Sprintf("%s=%v", k, v))
		}
		parts = append(parts, fmt.Sprintf("Context: %s", strings.Join(contextParts, ", ")))
	}
	
	// Add suggestions if available
	if len(e.Suggestions) > 0 {
		parts = append(parts, "Suggested actions:")
		for i, suggestion := range e.Suggestions {
			parts = append(parts, fmt.Sprintf("  %d. %s", i+1, suggestion))
		}
	}
	
	return strings.Join(parts, "\n")
}

// RetryConfig defines configuration for retry operations
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors []ErrorType
}

// DefaultRetryConfig returns a default retry configuration
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts:   5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: []ErrorType{
			ErrorTypeNetwork,
			ErrorTypeSyncConflict,
		},
	}
}

// RetryableOperation represents an operation that can be retried
type RetryableOperation func() error

// RetryManager handles retry logic with exponential backoff
type RetryManager struct {
	config RetryConfig
	logger Logger
}

// NewRetryManager creates a new retry manager
func NewRetryManager(config RetryConfig, logger Logger) *RetryManager {
	return &RetryManager{
		config: config,
		logger: logger,
	}
}

// Execute executes an operation with retry logic
func (r *RetryManager) Execute(ctx context.Context, operation RetryableOperation, operationName string) error {
	var lastErr error
	delay := r.config.InitialDelay
	
	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		r.logger.Debug("Executing operation",
			String("operation", operationName),
			Int("attempt", attempt),
			Int("max_attempts", r.config.MaxAttempts),
		)
		
		err := operation()
		if err == nil {
			if attempt > 1 {
				r.logger.Info("Operation succeeded after retry",
					String("operation", operationName),
					Int("attempts", attempt),
				)
			}
			return nil
		}
		
		lastErr = err
		
		// Check if error is retryable
		if !r.isRetryable(err) {
			r.logger.Error("Operation failed with non-retryable error",
				fmt.Errorf("operation %s failed: %w", operationName, err),
				String("operation", operationName),
				Int("attempt", attempt),
			)
			return err
		}
		
		// Don't wait after the last attempt
		if attempt == r.config.MaxAttempts {
			break
		}
		
		r.logger.Warn("Operation failed, retrying",
			String("operation", operationName),
			Int("attempt", attempt),
			Duration("delay", delay),
			Error(err),
		)
		
		// Wait with context cancellation support
		select {
		case <-ctx.Done():
			return fmt.Errorf("operation cancelled: %w", ctx.Err())
		case <-time.After(delay):
		}
		
		// Calculate next delay with exponential backoff
		delay = time.Duration(float64(delay) * r.config.BackoffFactor)
		if delay > r.config.MaxDelay {
			delay = r.config.MaxDelay
		}
	}
	
	r.logger.Error("Operation failed after all retry attempts",
		fmt.Errorf("operation %s failed after %d attempts: %w", operationName, r.config.MaxAttempts, lastErr),
		String("operation", operationName),
		Int("max_attempts", r.config.MaxAttempts),
	)
	
	return fmt.Errorf("operation %s failed after %d attempts: %w", operationName, r.config.MaxAttempts, lastErr)
}

// isRetryable checks if an error is retryable based on configuration
func (r *RetryManager) isRetryable(err error) bool {
	if recErr, ok := err.(*RecoverableError); ok {
		if !recErr.Recoverable {
			return false
		}
		
		for _, retryableType := range r.config.RetryableErrors {
			if recErr.Type == retryableType {
				return true
			}
		}
		return false
	}
	
	// For non-RecoverableError types, check common patterns
	errStr := strings.ToLower(err.Error())
	networkKeywords := []string{"timeout", "connection", "network", "dns", "temporary"}
	
	for _, keyword := range networkKeywords {
		if strings.Contains(errStr, keyword) {
			return true
		}
	}
	
	return false
}

// HealthChecker defines interface for system health checks
type HealthChecker interface {
	CheckHealth(ctx context.Context) *HealthStatus
	GetName() string
}

// HealthStatus represents the health status of a component
type HealthStatus struct {
	Component string                 `json:"component"`
	Status    string                 `json:"status"` // "healthy", "degraded", "unhealthy"
	Message   string                 `json:"message,omitempty"`
	Details   map[string]interface{} `json:"details,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
	Duration  time.Duration          `json:"duration"`
}

// IsHealthy returns true if the component is healthy
func (h *HealthStatus) IsHealthy() bool {
	return h.Status == "healthy"
}

// HealthManager manages health checks for system components
type HealthManager struct {
	checkers []HealthChecker
	logger   Logger
}

// NewHealthManager creates a new health manager
func NewHealthManager(logger Logger) *HealthManager {
	return &HealthManager{
		checkers: make([]HealthChecker, 0),
		logger:   logger,
	}
}

// RegisterChecker registers a health checker
func (h *HealthManager) RegisterChecker(checker HealthChecker) {
	h.checkers = append(h.checkers, checker)
	h.logger.Debug("Registered health checker", String("component", checker.GetName()))
}

// CheckAll performs health checks on all registered components
func (h *HealthManager) CheckAll(ctx context.Context) map[string]*HealthStatus {
	results := make(map[string]*HealthStatus)
	
	for _, checker := range h.checkers {
		start := time.Now()
		status := checker.CheckHealth(ctx)
		status.Duration = time.Since(start)
		status.Timestamp = time.Now()
		
		results[checker.GetName()] = status
		
		h.logger.Debug("Health check completed",
			String("component", checker.GetName()),
			String("status", status.Status),
			Duration("duration", status.Duration),
		)
	}
	
	return results
}

// GetOverallHealth returns the overall system health status
func (h *HealthManager) GetOverallHealth(ctx context.Context) *HealthStatus {
	results := h.CheckAll(ctx)
	
	overall := &HealthStatus{
		Component: "system",
		Status:    "healthy",
		Message:   "All components are healthy",
		Details:   make(map[string]interface{}),
		Timestamp: time.Now(),
	}
	
	unhealthyCount := 0
	degradedCount := 0
	
	for name, status := range results {
		overall.Details[name] = status.Status
		
		switch status.Status {
		case "unhealthy":
			unhealthyCount++
		case "degraded":
			degradedCount++
		}
	}
	
	// Determine overall status
	if unhealthyCount > 0 {
		overall.Status = "unhealthy"
		overall.Message = fmt.Sprintf("%d components are unhealthy", unhealthyCount)
	} else if degradedCount > 0 {
		overall.Status = "degraded"
		overall.Message = fmt.Sprintf("%d components are degraded", degradedCount)
	}
	
	return overall
}

// ErrorRecoveryManager manages automatic error recovery
type ErrorRecoveryManager struct {
	retryManager  *RetryManager
	healthManager *HealthManager
	logger        Logger
	recoveryFuncs map[ErrorType]func(error) error
}

// NewErrorRecoveryManager creates a new error recovery manager
func NewErrorRecoveryManager(retryConfig RetryConfig, logger Logger) *ErrorRecoveryManager {
	return &ErrorRecoveryManager{
		retryManager:  NewRetryManager(retryConfig, logger),
		healthManager: NewHealthManager(logger),
		logger:        logger,
		recoveryFuncs: make(map[ErrorType]func(error) error),
	}
}

// RegisterRecoveryFunc registers a recovery function for a specific error type
func (e *ErrorRecoveryManager) RegisterRecoveryFunc(errType ErrorType, recoveryFunc func(error) error) {
	e.recoveryFuncs[errType] = recoveryFunc
	e.logger.Debug("Registered recovery function", String("error_type", string(errType)))
}

// AttemptRecovery attempts to recover from an error
func (e *ErrorRecoveryManager) AttemptRecovery(ctx context.Context, err error) error {
	if recErr, ok := err.(*RecoverableError); ok {
		e.logger.Info("Attempting error recovery",
			String("error_type", string(recErr.Type)),
			String("error_message", recErr.Message),
		)
		
		if recoveryFunc, exists := e.recoveryFuncs[recErr.Type]; exists {
			if recoveryErr := recoveryFunc(err); recoveryErr != nil {
				e.logger.Error("Error recovery failed", recoveryErr,
					String("error_type", string(recErr.Type)),
				)
				return fmt.Errorf("recovery failed: %w", recoveryErr)
			}
			
			e.logger.Info("Error recovery successful",
				String("error_type", string(recErr.Type)),
			)
			return nil
		}
		
		e.logger.Warn("No recovery function registered",
			String("error_type", string(recErr.Type)),
		)
	}
	
	return err
}

// ExecuteWithRecovery executes an operation with automatic retry and recovery
func (e *ErrorRecoveryManager) ExecuteWithRecovery(ctx context.Context, operation RetryableOperation, operationName string) error {
	return e.retryManager.Execute(ctx, func() error {
		err := operation()
		if err != nil {
			// Attempt recovery before retrying
			if recoveryErr := e.AttemptRecovery(ctx, err); recoveryErr == nil {
				// Recovery successful, try operation again
				return operation()
			}
		}
		return err
	}, operationName)
}

// GetHealthManager returns the health manager
func (e *ErrorRecoveryManager) GetHealthManager() *HealthManager {
	return e.healthManager
}

// GetRetryManager returns the retry manager
func (e *ErrorRecoveryManager) GetRetryManager() *RetryManager {
	return e.retryManager
}