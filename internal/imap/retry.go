package imap

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"net"
	"strings"
	"time"

	"snail-cli/internal/interfaces"
)

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxRetries      int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	Jitter          bool
	RetryableErrors []string
}

// DefaultRetryConfig returns a sensible default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:    3,
		InitialDelay:  1 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
		Jitter:        true,
		RetryableErrors: []string{
			"connection reset",
			"connection refused",
			"timeout",
			"temporary failure",
			"network is unreachable",
			"no such host",
			"i/o timeout",
		},
	}
}

// RetryableOperation represents an operation that can be retried
type RetryableOperation func(ctx context.Context) error

// WithRetry executes an operation with retry logic
func WithRetry(ctx context.Context, config *RetryConfig, operation RetryableOperation) error {
	var lastErr error
	
	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Execute the operation
		err := operation(ctx)
		if err == nil {
			return nil // Success
		}
		
		lastErr = err
		
		// Check if we should retry
		if attempt == config.MaxRetries || !isRetryableError(err, config.RetryableErrors) {
			break
		}
		
		// Calculate delay for next attempt
		delay := calculateDelay(attempt, config)
		
		// Wait before retrying, but respect context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			// Continue to next attempt
		}
	}
	
	return fmt.Errorf("operation failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}

// isRetryableError checks if an error should trigger a retry
func isRetryableError(err error, retryableErrors []string) bool {
	if err == nil {
		return false
	}
	
	errStr := strings.ToLower(err.Error())
	
	// Check for network errors
	if isNetworkError(err) {
		return true
	}
	
	// Check for specific error patterns
	for _, pattern := range retryableErrors {
		if strings.Contains(errStr, strings.ToLower(pattern)) {
			return true
		}
	}
	
	return false
}

// isNetworkError checks if an error is a network-related error
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	
	// Check for specific network error types
	if _, ok := err.(net.Error); ok {
		return true
	}
	
	if _, ok := err.(*net.OpError); ok {
		return true
	}
	
	if _, ok := err.(*net.DNSError); ok {
		return true
	}
	
	return false
}

// calculateDelay calculates the delay for the next retry attempt
func calculateDelay(attempt int, config *RetryConfig) time.Duration {
	// Calculate exponential backoff
	delay := float64(config.InitialDelay) * math.Pow(config.BackoffFactor, float64(attempt))
	
	// Apply maximum delay limit
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}
	
	// Add jitter to prevent thundering herd
	if config.Jitter {
		jitter := delay * 0.1 * (rand.Float64()*2 - 1) // ±10% jitter
		delay += jitter
	}
	
	return time.Duration(delay)
}

// RetryableClient wraps an IMAP client with retry logic
type RetryableClient struct {
	client interfaces.IMAPClient
	config *RetryConfig
}

// NewRetryableClient creates a new retryable IMAP client
func NewRetryableClient(client interfaces.IMAPClient, config *RetryConfig) *RetryableClient {
	if config == nil {
		config = DefaultRetryConfig()
	}
	
	return &RetryableClient{
		client: client,
		config: config,
	}
}

// Connect establishes connection with retry logic
func (r *RetryableClient) Connect(ctx context.Context) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.Connect(ctx)
	})
}

// Authenticate performs authentication with retry logic
func (r *RetryableClient) Authenticate(ctx context.Context, username, password string) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.Authenticate(ctx, username, password)
	})
}

// AuthenticateOAuth2 performs OAuth2 authentication with retry logic
func (r *RetryableClient) AuthenticateOAuth2(ctx context.Context, username, accessToken string) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.AuthenticateOAuth2(ctx, username, accessToken)
	})
}

// SelectFolder selects a folder with retry logic
func (r *RetryableClient) SelectFolder(ctx context.Context, folder string) (*interfaces.FolderInfo, error) {
	var result *interfaces.FolderInfo
	err := WithRetry(ctx, r.config, func(ctx context.Context) error {
		var err error
		result, err = r.client.SelectFolder(ctx, folder)
		return err
	})
	return result, err
}

// ListFolders lists folders with retry logic
func (r *RetryableClient) ListFolders(ctx context.Context) ([]*interfaces.FolderInfo, error) {
	var result []*interfaces.FolderInfo
	err := WithRetry(ctx, r.config, func(ctx context.Context) error {
		var err error
		result, err = r.client.ListFolders(ctx)
		return err
	})
	return result, err
}

// FetchEmails fetches emails with retry logic
func (r *RetryableClient) FetchEmails(ctx context.Context, criteria *interfaces.FetchCriteria) ([]*interfaces.Email, error) {
	var result []*interfaces.Email
	err := WithRetry(ctx, r.config, func(ctx context.Context) error {
		var err error
		result, err = r.client.FetchEmails(ctx, criteria)
		return err
	})
	return result, err
}

// FetchEmailByUID fetches an email by UID with retry logic
func (r *RetryableClient) FetchEmailByUID(ctx context.Context, uid uint32) (*interfaces.Email, error) {
	var result *interfaces.Email
	err := WithRetry(ctx, r.config, func(ctx context.Context) error {
		var err error
		result, err = r.client.FetchEmailByUID(ctx, uid)
		return err
	})
	return result, err
}

// MarkAsRead marks emails as read with retry logic
func (r *RetryableClient) MarkAsRead(ctx context.Context, uids []uint32) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.MarkAsRead(ctx, uids)
	})
}

// MarkAsUnread marks emails as unread with retry logic
func (r *RetryableClient) MarkAsUnread(ctx context.Context, uids []uint32) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.MarkAsUnread(ctx, uids)
	})
}

// DeleteEmails deletes emails with retry logic
func (r *RetryableClient) DeleteEmails(ctx context.Context, uids []uint32) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.DeleteEmails(ctx, uids)
	})
}

// MoveEmails moves emails with retry logic
func (r *RetryableClient) MoveEmails(ctx context.Context, uids []uint32, destFolder string) error {
	return WithRetry(ctx, r.config, func(ctx context.Context) error {
		return r.client.MoveEmails(ctx, uids, destFolder)
	})
}

// Disconnect disconnects from the server
func (r *RetryableClient) Disconnect() error {
	return r.client.Disconnect()
}

// IsConnected returns connection status
func (r *RetryableClient) IsConnected() bool {
	return r.client.IsConnected()
}

// GetConnectionInfo returns connection information
func (r *RetryableClient) GetConnectionInfo() *interfaces.ConnectionInfo {
	return r.client.GetConnectionInfo()
}