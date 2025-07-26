package models

import (
	"bufio"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"sync"
	"time"
)

// StreamingEmail represents an email with lazy-loaded content
type StreamingEmail struct {
	// Metadata (always loaded)
	ID          string            `json:"id"`
	MessageID   string            `json:"message_id"`
	From        Address           `json:"from"`
	To          []Address         `json:"to"`
	CC          []Address         `json:"cc"`
	BCC         []Address         `json:"bcc"`
	Subject     string            `json:"subject"`
	Date        time.Time         `json:"date"`
	Headers     map[string]string `json:"headers"`
	Labels      []string          `json:"labels"`
	Status      EmailStatus       `json:"status"`
	SyncID      string            `json:"sync_id"`
	
	// Content (lazy-loaded)
	bodyLoader        ContentLoader
	attachmentLoaders []AttachmentLoader
	
	// Cached content
	cachedBody        string
	cachedAttachments []Attachment
	bodyLoaded        bool
	attachmentsLoaded bool
	mu                sync.RWMutex
}

// ContentLoader interface for lazy loading email content
type ContentLoader interface {
	LoadContent() (string, error)
	GetSize() int64
	IsLoaded() bool
}

// AttachmentLoader interface for lazy loading attachments
type AttachmentLoader interface {
	LoadAttachment() (*Attachment, error)
	GetMetadata() AttachmentMetadata
	IsLoaded() bool
}

// AttachmentMetadata contains attachment info without the data
type AttachmentMetadata struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Index       int    `json:"index"`
}

// StreamingEmailParser handles streaming parsing of email messages
type StreamingEmailParser struct {
	maxHeaderSize int64
	bufferSize    int
}

// NewStreamingEmailParser creates a new streaming email parser
func NewStreamingEmailParser() *StreamingEmailParser {
	return &StreamingEmailParser{
		maxHeaderSize: 1024 * 1024, // 1MB max header size
		bufferSize:    8192,         // 8KB buffer
	}
}

// ParseStreamingEmail parses an email with streaming support
func (p *StreamingEmailParser) ParseStreamingEmail(reader io.Reader) (*StreamingEmail, error) {
	// Use a buffered reader for efficient parsing
	bufferedReader := bufio.NewReaderSize(reader, p.bufferSize)
	
	// Parse headers first (always loaded)
	msg, err := mail.ReadMessage(bufferedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse email headers: %w", err)
	}
	
	email := &StreamingEmail{
		Headers:           make(map[string]string),
		Labels:            make([]string, 0),
		attachmentLoaders: make([]AttachmentLoader, 0),
	}
	
	// Parse headers (lightweight operation)
	if err := p.parseHeaders(msg.Header, email); err != nil {
		return nil, fmt.Errorf("failed to parse headers: %w", err)
	}
	
	// Create content loaders for body and attachments
	if err := p.createContentLoaders(msg, email); err != nil {
		return nil, fmt.Errorf("failed to create content loaders: %w", err)
	}
	
	// Generate ID if not present
	if email.ID == "" {
		email.ID = p.generateEmailID(email)
	}
	
	return email, nil
}

// GetBody returns the email body, loading it if necessary
func (e *StreamingEmail) GetBody() (string, error) {
	e.mu.RLock()
	if e.bodyLoaded {
		body := e.cachedBody
		e.mu.RUnlock()
		return body, nil
	}
	e.mu.RUnlock()
	
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Double-check after acquiring write lock
	if e.bodyLoaded {
		return e.cachedBody, nil
	}
	
	if e.bodyLoader == nil {
		return "", nil
	}
	
	body, err := e.bodyLoader.LoadContent()
	if err != nil {
		return "", fmt.Errorf("failed to load email body: %w", err)
	}
	
	e.cachedBody = body
	e.bodyLoaded = true
	return body, nil
}

// GetBodySize returns the size of the email body without loading it
func (e *StreamingEmail) GetBodySize() int64 {
	if e.bodyLoader == nil {
		return 0
	}
	return e.bodyLoader.GetSize()
}

// IsBodyLoaded returns true if the body is currently loaded in memory
func (e *StreamingEmail) IsBodyLoaded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.bodyLoaded
}

// GetAttachments returns all attachments, loading them if necessary
func (e *StreamingEmail) GetAttachments() ([]Attachment, error) {
	e.mu.RLock()
	if e.attachmentsLoaded {
		attachments := make([]Attachment, len(e.cachedAttachments))
		copy(attachments, e.cachedAttachments)
		e.mu.RUnlock()
		return attachments, nil
	}
	e.mu.RUnlock()
	
	e.mu.Lock()
	defer e.mu.Unlock()
	
	// Double-check after acquiring write lock
	if e.attachmentsLoaded {
		attachments := make([]Attachment, len(e.cachedAttachments))
		copy(attachments, e.cachedAttachments)
		return attachments, nil
	}
	
	attachments := make([]Attachment, len(e.attachmentLoaders))
	for i, loader := range e.attachmentLoaders {
		attachment, err := loader.LoadAttachment()
		if err != nil {
			return nil, fmt.Errorf("failed to load attachment %d: %w", i, err)
		}
		attachments[i] = *attachment
	}
	
	e.cachedAttachments = attachments
	e.attachmentsLoaded = true
	return attachments, nil
}

// GetAttachment returns a specific attachment by index, loading it if necessary
func (e *StreamingEmail) GetAttachment(index int) (*Attachment, error) {
	if index < 0 || index >= len(e.attachmentLoaders) {
		return nil, fmt.Errorf("attachment index %d out of range", index)
	}
	
	e.mu.RLock()
	if e.attachmentsLoaded && index < len(e.cachedAttachments) {
		attachment := e.cachedAttachments[index]
		e.mu.RUnlock()
		return &attachment, nil
	}
	e.mu.RUnlock()
	
	// Load specific attachment
	loader := e.attachmentLoaders[index]
	return loader.LoadAttachment()
}

// GetAttachmentMetadata returns metadata for all attachments without loading data
func (e *StreamingEmail) GetAttachmentMetadata() []AttachmentMetadata {
	metadata := make([]AttachmentMetadata, len(e.attachmentLoaders))
	for i, loader := range e.attachmentLoaders {
		metadata[i] = loader.GetMetadata()
	}
	return metadata
}

// GetAttachmentCount returns the number of attachments
func (e *StreamingEmail) GetAttachmentCount() int {
	return len(e.attachmentLoaders)
}

// IsAttachmentsLoaded returns true if all attachments are loaded in memory
func (e *StreamingEmail) IsAttachmentsLoaded() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.attachmentsLoaded
}

// UnloadContent releases cached content to free memory
func (e *StreamingEmail) UnloadContent() {
	e.mu.Lock()
	defer e.mu.Unlock()
	
	e.cachedBody = ""
	e.cachedAttachments = nil
	e.bodyLoaded = false
	e.attachmentsLoaded = false
}

// GetMemoryUsage returns the approximate memory usage of cached content
func (e *StreamingEmail) GetMemoryUsage() int64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	var usage int64
	
	if e.bodyLoaded {
		usage += int64(len(e.cachedBody))
	}
	
	if e.attachmentsLoaded {
		for _, att := range e.cachedAttachments {
			usage += int64(len(att.Data))
		}
	}
	
	return usage
}

// ToEmail converts StreamingEmail to regular Email (loads all content)
func (e *StreamingEmail) ToEmail() (*Email, error) {
	email := &Email{
		ID:        e.ID,
		MessageID: e.MessageID,
		From:      e.From,
		To:        make([]Address, len(e.To)),
		CC:        make([]Address, len(e.CC)),
		BCC:       make([]Address, len(e.BCC)),
		Subject:   e.Subject,
		Date:      e.Date,
		Headers:   make(map[string]string),
		Labels:    make([]string, len(e.Labels)),
		Status:    e.Status,
		SyncID:    e.SyncID,
	}
	
	// Copy slices and maps
	copy(email.To, e.To)
	copy(email.CC, e.CC)
	copy(email.BCC, e.BCC)
	copy(email.Labels, e.Labels)
	for k, v := range e.Headers {
		email.Headers[k] = v
	}
	
	// Load body
	body, err := e.GetBody()
	if err != nil {
		return nil, fmt.Errorf("failed to load body: %w", err)
	}
	email.Body = body
	
	// Load attachments
	attachments, err := e.GetAttachments()
	if err != nil {
		return nil, fmt.Errorf("failed to load attachments: %w", err)
	}
	email.Attachments = attachments
	
	return email, nil
}

// Private helper methods

func (p *StreamingEmailParser) parseHeaders(header mail.Header, email *StreamingEmail) error {
	// Copy all headers
	for key, values := range header {
		if len(values) > 0 {
			email.Headers[key] = values[0]
		}
	}

	// Parse specific headers (same as regular parser)
	if messageID := header.Get("Message-ID"); messageID != "" {
		email.MessageID = strings.Trim(messageID, "<>")
	}

	if from := header.Get("From"); from != "" {
		addr, err := ParseAddress(from)
		if err != nil {
			return fmt.Errorf("invalid from address: %w", err)
		}
		email.From = *addr
	}

	if to := header.Get("To"); to != "" {
		addrs, err := ParseAddressList(to)
		if err != nil {
			return fmt.Errorf("invalid to addresses: %w", err)
		}
		email.To = addrs
	}

	if cc := header.Get("Cc"); cc != "" {
		addrs, err := ParseAddressList(cc)
		if err != nil {
			return fmt.Errorf("invalid cc addresses: %w", err)
		}
		email.CC = addrs
	}

	if bcc := header.Get("Bcc"); bcc != "" {
		addrs, err := ParseAddressList(bcc)
		if err != nil {
			return fmt.Errorf("invalid bcc addresses: %w", err)
		}
		email.BCC = addrs
	}

	email.Subject = header.Get("Subject")

	if dateStr := header.Get("Date"); dateStr != "" {
		date, err := mail.ParseDate(dateStr)
		if err != nil {
			return fmt.Errorf("invalid date format: %w", err)
		}
		email.Date = date
	}

	// Parse custom snail headers
	if status := header.Get("X-Snail-Status"); status != "" {
		email.Status = EmailStatus(status)
	}

	if labels := header.Get("X-Snail-Labels"); labels != "" {
		email.Labels = strings.Split(labels, ",")
		for i, label := range email.Labels {
			email.Labels[i] = strings.TrimSpace(label)
		}
	}

	if syncID := header.Get("X-Snail-Sync-ID"); syncID != "" {
		email.SyncID = syncID
	}

	return nil
}

func (p *StreamingEmailParser) createContentLoaders(msg *mail.Message, email *StreamingEmail) error {
	contentType := msg.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("failed to parse content type: %w", err)
	}

	switch {
	case strings.HasPrefix(mediaType, "multipart/"):
		return p.createMultipartLoaders(msg.Body, params["boundary"], email)
	case mediaType == "text/plain" || mediaType == "text/html":
		email.bodyLoader = NewReaderContentLoader(msg.Body)
		return nil
	default:
		// Treat as single attachment
		loader := NewReaderAttachmentLoader(
			msg.Body,
			AttachmentMetadata{
				Filename:    "attachment",
				ContentType: mediaType,
				Size:        -1, // Unknown size
				Index:       0,
			},
		)
		email.attachmentLoaders = append(email.attachmentLoaders, loader)
		return nil
	}
}

func (p *StreamingEmailParser) createMultipartLoaders(body io.Reader, boundary string, email *StreamingEmail) error {
	if boundary == "" {
		return fmt.Errorf("multipart message missing boundary")
	}

	mr := multipart.NewReader(body, boundary)
	attachmentIndex := 0
	
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read multipart: %w", err)
		}

		if err := p.createPartLoader(part, email, &attachmentIndex); err != nil {
			part.Close()
			return fmt.Errorf("failed to create part loader: %w", err)
		}
		part.Close()
	}

	return nil
}

func (p *StreamingEmailParser) createPartLoader(part *multipart.Part, email *StreamingEmail, attachmentIndex *int) error {
	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}

	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("failed to parse part content type: %w", err)
	}

	disposition := part.Header.Get("Content-Disposition")
	
	// Check if this is an attachment
	if strings.HasPrefix(disposition, "attachment") || 
	   (disposition == "" && part.FileName() != "") {
		
		filename := part.FileName()
		if filename == "" {
			filename = "attachment"
		}

		metadata := AttachmentMetadata{
			Filename:    filename,
			ContentType: contentType,
			Size:        -1, // Unknown size for streaming
			Index:       *attachmentIndex,
		}

		loader := NewReaderAttachmentLoader(part, metadata)
		email.attachmentLoaders = append(email.attachmentLoaders, loader)
		*attachmentIndex++
		return nil
	}

	// Handle nested multipart
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		return p.createMultipartLoaders(part, boundary, email)
	}

	// Handle text content
	if mediaType == "text/plain" || mediaType == "text/html" {
		// For now, prefer plain text over HTML
		if email.bodyLoader == nil || mediaType == "text/plain" {
			email.bodyLoader = NewReaderContentLoader(part)
		}
	}

	return nil
}

func (p *StreamingEmailParser) generateEmailID(email *StreamingEmail) string {
	if email.MessageID != "" {
		return email.MessageID
	}
	
	// Fallback: generate ID from hash of key fields
	hash := fmt.Sprintf("%s-%s-%d", 
		email.From.Email, 
		email.Subject, 
		email.Date.Unix())
	
	return fmt.Sprintf("snail-%x", hash)
}