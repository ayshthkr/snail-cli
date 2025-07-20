package interfaces

import (
	"context"
	"time"
)

// EmailRepository defines the interface for email storage operations
type EmailRepository interface {
	// Initialize creates a new email repository
	Initialize(path string) error
	
	// Store saves an email to the repository
	Store(ctx context.Context, email *Email) error
	
	// Get retrieves an email by ID
	Get(ctx context.Context, id string) (*Email, error)
	
	// List returns emails matching the given criteria
	List(ctx context.Context, criteria ListCriteria) ([]*Email, error)
	
	// Delete removes an email from the repository
	Delete(ctx context.Context, id string) error
	
	// Update modifies an existing email
	Update(ctx context.Context, email *Email) error
	
	// Commit creates a Git commit with the current changes
	Commit(ctx context.Context, message string) error
	
	// GetHistory returns the Git history for an email
	GetHistory(ctx context.Context, id string) ([]*CommitInfo, error)
}

// Email represents an email message
type Email struct {
	ID          string
	MessageID   string
	From        Address
	To          []Address
	CC          []Address
	BCC         []Address
	Subject     string
	Date        time.Time
	Body        string
	Headers     map[string]string
	Attachments []Attachment
	Labels      []string
	Status      EmailStatus
	SyncID      string
}

// Address represents an email address
type Address struct {
	Name  string
	Email string
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string
	ContentType string
	Size        int64
	Data        []byte
}

// EmailStatus represents the status of an email
type EmailStatus string

const (
	StatusUnread EmailStatus = "unread"
	StatusRead   EmailStatus = "read"
	StatusDraft  EmailStatus = "draft"
	StatusSent   EmailStatus = "sent"
)

// ListCriteria defines criteria for listing emails
type ListCriteria struct {
	Folder   string
	Labels   []string
	Status   EmailStatus
	From     string
	Subject  string
	DateFrom *time.Time
	DateTo   *time.Time
	Limit    int
	Offset   int
}

// CommitInfo represents Git commit information
type CommitInfo struct {
	Hash      string
	Message   string
	Author    string
	Date      time.Time
	Changes   []string
}