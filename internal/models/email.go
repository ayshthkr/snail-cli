package models

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"time"
)

// Email represents an email message with validation
type Email struct {
	ID          string            `json:"id"`
	MessageID   string            `json:"message_id"`
	From        Address           `json:"from"`
	To          []Address         `json:"to"`
	CC          []Address         `json:"cc"`
	BCC         []Address         `json:"bcc"`
	Subject     string            `json:"subject"`
	Date        time.Time         `json:"date"`
	Body        string            `json:"body"`
	Headers     map[string]string `json:"headers"`
	Attachments []Attachment      `json:"attachments"`
	Labels      []string          `json:"labels"`
	Status      EmailStatus       `json:"status"`
	SyncID      string            `json:"sync_id"`
}

// Address represents an email address with validation
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Data        []byte `json:"data,omitempty"`
}

// EmailStatus represents the status of an email
type EmailStatus string

const (
	StatusUnread EmailStatus = "unread"
	StatusRead   EmailStatus = "read"
	StatusDraft  EmailStatus = "draft"
	StatusSent   EmailStatus = "sent"
)

// Email validation regex patterns
var (
	emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)
	nameRegex  = regexp.MustCompile(`^[^<>@]+$`)
)

// NewEmail creates a new email with default values
func NewEmail() *Email {
	return &Email{
		Headers:     make(map[string]string),
		Labels:      make([]string, 0),
		Attachments: make([]Attachment, 0),
		Status:      StatusUnread,
		Date:        time.Now(),
	}
}

// Validate validates the email structure and required fields
func (e *Email) Validate() error {
	if e.MessageID == "" {
		return fmt.Errorf("message ID is required")
	}

	if err := e.From.Validate(); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}

	if len(e.To) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	for i, addr := range e.To {
		if err := addr.Validate(); err != nil {
			return fmt.Errorf("invalid to address at index %d: %w", i, err)
		}
	}

	for i, addr := range e.CC {
		if err := addr.Validate(); err != nil {
			return fmt.Errorf("invalid cc address at index %d: %w", i, err)
		}
	}

	for i, addr := range e.BCC {
		if err := addr.Validate(); err != nil {
			return fmt.Errorf("invalid bcc address at index %d: %w", i, err)
		}
	}

	if e.Subject == "" {
		return fmt.Errorf("subject is required")
	}

	if e.Date.IsZero() {
		return fmt.Errorf("date is required")
	}

	if !e.Status.IsValid() {
		return fmt.Errorf("invalid email status: %s", e.Status)
	}

	// Validate attachments
	for i, att := range e.Attachments {
		if err := att.Validate(); err != nil {
			return fmt.Errorf("invalid attachment at index %d: %w", i, err)
		}
	}

	return nil
}

// IsValid returns true if the email status is valid
func (s EmailStatus) IsValid() bool {
	switch s {
	case StatusUnread, StatusRead, StatusDraft, StatusSent:
		return true
	default:
		return false
	}
}

// String returns the string representation of the email status
func (s EmailStatus) String() string {
	return string(s)
}

// NewAddress creates a new address with validation
func NewAddress(email, name string) (*Address, error) {
	addr := &Address{
		Email: strings.TrimSpace(email),
		Name:  strings.TrimSpace(name),
	}
	
	if err := addr.Validate(); err != nil {
		return nil, err
	}
	
	return addr, nil
}

// Validate validates the email address format
func (a *Address) Validate() error {
	if a.Email == "" {
		return fmt.Errorf("email address is required")
	}

	if !emailRegex.MatchString(a.Email) {
		return fmt.Errorf("invalid email format: %s", a.Email)
	}

	if a.Name != "" && !nameRegex.MatchString(a.Name) {
		return fmt.Errorf("invalid name format: %s", a.Name)
	}

	return nil
}

// String returns the RFC 5322 formatted address
func (a *Address) String() string {
	if a.Name != "" {
		return fmt.Sprintf("%s <%s>", a.Name, a.Email)
	}
	return a.Email
}

// ParseAddress parses an RFC 5322 formatted address
func ParseAddress(addr string) (*Address, error) {
	parsed, err := mail.ParseAddress(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse address: %w", err)
	}

	return &Address{
		Name:  parsed.Name,
		Email: parsed.Address,
	}, nil
}

// ParseAddressList parses a comma-separated list of addresses
func ParseAddressList(addrs string) ([]Address, error) {
	if strings.TrimSpace(addrs) == "" {
		return []Address{}, nil
	}

	parsed, err := mail.ParseAddressList(addrs)
	if err != nil {
		return nil, fmt.Errorf("failed to parse address list: %w", err)
	}

	result := make([]Address, len(parsed))
	for i, addr := range parsed {
		result[i] = Address{
			Name:  addr.Name,
			Email: addr.Address,
		}
	}

	return result, nil
}

// NewAttachment creates a new attachment with validation
func NewAttachment(filename, contentType string, data []byte) (*Attachment, error) {
	att := &Attachment{
		Filename:    strings.TrimSpace(filename),
		ContentType: strings.TrimSpace(contentType),
		Size:        int64(len(data)),
		Data:        data,
	}
	
	if err := att.Validate(); err != nil {
		return nil, err
	}
	
	return att, nil
}

// Validate validates the attachment
func (a *Attachment) Validate() error {
	if a.Filename == "" {
		return fmt.Errorf("filename is required")
	}

	if a.ContentType == "" {
		return fmt.Errorf("content type is required")
	}

	if a.Size < 0 {
		return fmt.Errorf("size cannot be negative")
	}

	if a.Data != nil && int64(len(a.Data)) != a.Size {
		return fmt.Errorf("data size mismatch: expected %d, got %d", a.Size, len(a.Data))
	}

	return nil
}

// HasData returns true if the attachment has data loaded
func (a *Attachment) HasData() bool {
	return a.Data != nil && len(a.Data) > 0
}