package interfaces

import (
	"context"
	"time"
)

// SMTPClient defines the interface for SMTP operations
type SMTPClient interface {
	// Connect establishes connection to SMTP server
	Connect(ctx context.Context) error
	
	// Disconnect closes the SMTP connection
	Disconnect() error
	
	// Authenticate performs authentication with the server
	Authenticate(ctx context.Context, username, password string) error
	
	// AuthenticateOAuth2 performs OAuth2 authentication
	AuthenticateOAuth2(ctx context.Context, username, accessToken string) error
	
	// SendEmail sends an email message
	SendEmail(ctx context.Context, email *Email) error
	
	// IsConnected returns true if connected to server
	IsConnected() bool
	
	// GetConnectionInfo returns connection information
	GetConnectionInfo() *SMTPConnectionInfo
}

// SMTPConnectionInfo contains information about the SMTP connection
type SMTPConnectionInfo struct {
	Server      string
	Port        int
	TLS         bool
	Connected   bool
	ConnectedAt *time.Time
	LastError   error
	Extensions  []string
}

// OutgoingEmail represents an email ready to be sent via SMTP
type OutgoingEmail struct {
	*Email
	// Additional SMTP-specific fields
	ReturnPath string
	Priority   int
	DeliveryReceipt bool
	ReadReceipt     bool
}

// SMTPQueue manages queued outgoing emails for offline scenarios
type SMTPQueue interface {
	// Enqueue adds an email to the send queue
	Enqueue(ctx context.Context, email *OutgoingEmail) error
	
	// Dequeue retrieves the next email from the queue
	Dequeue(ctx context.Context) (*OutgoingEmail, error)
	
	// Peek returns the next email without removing it from the queue
	Peek(ctx context.Context) (*OutgoingEmail, error)
	
	// Remove removes a specific email from the queue
	Remove(ctx context.Context, emailID string) error
	
	// List returns all emails in the queue
	List(ctx context.Context) ([]*OutgoingEmail, error)
	
	// Size returns the number of emails in the queue
	Size(ctx context.Context) (int, error)
	
	// Clear removes all emails from the queue
	Clear(ctx context.Context) error
	
	// ProcessQueue attempts to send all queued emails
	ProcessQueue(ctx context.Context, client SMTPClient) error
}

// SMTPSender combines SMTP client and queue management
type SMTPSender interface {
	// SendEmail sends an email immediately or queues it if offline
	SendEmail(ctx context.Context, email *Email) error
	
	// SendEmailWithOptions sends an email with additional options
	SendEmailWithOptions(ctx context.Context, email *OutgoingEmail) error
	
	// ProcessOfflineQueue processes all queued emails
	ProcessOfflineQueue(ctx context.Context) error
	
	// GetQueueSize returns the number of queued emails
	GetQueueSize(ctx context.Context) (int, error)
	
	// IsOnline returns true if SMTP server is reachable
	IsOnline(ctx context.Context) bool
}