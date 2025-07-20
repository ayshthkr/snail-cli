package models

import (
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"time"
)

// EmailParser handles parsing of raw email messages
type EmailParser struct{}

// NewEmailParser creates a new email parser
func NewEmailParser() *EmailParser {
	return &EmailParser{}
}

// ParseRawEmail parses a raw email message into an Email struct
func (p *EmailParser) ParseRawEmail(rawEmail string) (*Email, error) {
	reader := strings.NewReader(rawEmail)
	msg, err := mail.ReadMessage(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to parse email message: %w", err)
	}

	email := NewEmail()
	
	// Parse headers
	if err := p.parseHeaders(msg.Header, email); err != nil {
		return nil, fmt.Errorf("failed to parse headers: %w", err)
	}

	// Parse body and attachments
	if err := p.parseBody(msg, email); err != nil {
		return nil, fmt.Errorf("failed to parse body: %w", err)
	}

	// Generate ID if not present
	if email.ID == "" {
		email.ID = p.generateEmailID(email)
	}

	return email, nil
}

// parseHeaders extracts email headers into the Email struct
func (p *EmailParser) parseHeaders(header mail.Header, email *Email) error {
	// Initialize headers map
	email.Headers = make(map[string]string)
	
	// Copy all headers
	for key, values := range header {
		if len(values) > 0 {
			email.Headers[key] = values[0]
		}
	}

	// Parse specific headers
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

// parseBody extracts the email body and attachments
func (p *EmailParser) parseBody(msg *mail.Message, email *Email) error {
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
		return p.parseMultipart(msg.Body, params["boundary"], email)
	case mediaType == "text/plain" || mediaType == "text/html":
		body, err := io.ReadAll(msg.Body)
		if err != nil {
			return fmt.Errorf("failed to read body: %w", err)
		}
		email.Body = string(body)
		return nil
	default:
		// Treat as attachment
		return p.parseAttachment(msg.Body, textproto.MIMEHeader(msg.Header), email)
	}
}

// parseMultipart handles multipart email messages
func (p *EmailParser) parseMultipart(body io.Reader, boundary string, email *Email) error {
	if boundary == "" {
		return fmt.Errorf("multipart message missing boundary")
	}

	mr := multipart.NewReader(body, boundary)
	
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read multipart: %w", err)
		}

		if err := p.parsePart(part, email); err != nil {
			part.Close()
			return fmt.Errorf("failed to parse part: %w", err)
		}
		part.Close()
	}

	return nil
}

// parsePart handles individual parts of a multipart message
func (p *EmailParser) parsePart(part *multipart.Part, email *Email) error {
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
		return p.parsePartAsAttachment(part, email)
	}

	// Handle nested multipart
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		return p.parseMultipart(part, boundary, email)
	}

	// Handle text content
	if mediaType == "text/plain" || mediaType == "text/html" {
		content, err := io.ReadAll(part)
		if err != nil {
			return fmt.Errorf("failed to read part content: %w", err)
		}
		
		// For now, prefer plain text over HTML
		if email.Body == "" || mediaType == "text/plain" {
			email.Body = string(content)
		}
	}

	return nil
}

// parsePartAsAttachment handles attachment parts
func (p *EmailParser) parsePartAsAttachment(part *multipart.Part, email *Email) error {
	filename := part.FileName()
	if filename == "" {
		filename = "attachment"
	}

	contentType := part.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	data, err := io.ReadAll(part)
	if err != nil {
		return fmt.Errorf("failed to read attachment data: %w", err)
	}

	// Handle content transfer encoding
	encoding := part.Header.Get("Content-Transfer-Encoding")
	if encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(string(data))
		if err != nil {
			return fmt.Errorf("failed to decode base64 attachment: %w", err)
		}
		data = decoded
	}

	attachment := Attachment{
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(data)),
		Data:        data,
	}

	if err := attachment.Validate(); err != nil {
		return fmt.Errorf("invalid attachment: %w", err)
	}

	email.Attachments = append(email.Attachments, attachment)
	return nil
}

// parseAttachment handles single attachment messages
func (p *EmailParser) parseAttachment(body io.Reader, header textproto.MIMEHeader, email *Email) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("failed to read attachment body: %w", err)
	}

	contentType := header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Try to get filename from Content-Disposition
	disposition := header.Get("Content-Disposition")
	filename := "attachment"
	if disposition != "" {
		_, params, err := mime.ParseMediaType(disposition)
		if err == nil && params["filename"] != "" {
			filename = params["filename"]
		}
	}

	attachment := Attachment{
		Filename:    filename,
		ContentType: contentType,
		Size:        int64(len(data)),
		Data:        data,
	}

	if err := attachment.Validate(); err != nil {
		return fmt.Errorf("invalid attachment: %w", err)
	}

	email.Attachments = append(email.Attachments, attachment)
	return nil
}

// generateEmailID creates a unique ID for the email
func (p *EmailParser) generateEmailID(email *Email) string {
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

// FormatEmail converts an Email struct back to raw email format
func (p *EmailParser) FormatEmail(email *Email) (string, error) {
	if err := email.Validate(); err != nil {
		return "", fmt.Errorf("invalid email: %w", err)
	}

	var builder strings.Builder
	
	// Write headers
	if email.MessageID != "" {
		builder.WriteString(fmt.Sprintf("Message-ID: <%s>\r\n", email.MessageID))
	}
	
	builder.WriteString(fmt.Sprintf("From: %s\r\n", email.From.String()))
	
	if len(email.To) > 0 {
		toAddrs := make([]string, len(email.To))
		for i, addr := range email.To {
			toAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(toAddrs, ", ")))
	}
	
	if len(email.CC) > 0 {
		ccAddrs := make([]string, len(email.CC))
		for i, addr := range email.CC {
			ccAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(ccAddrs, ", ")))
	}
	
	if len(email.BCC) > 0 {
		bccAddrs := make([]string, len(email.BCC))
		for i, addr := range email.BCC {
			bccAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("Bcc: %s\r\n", strings.Join(bccAddrs, ", ")))
	}
	
	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", email.Subject))
	builder.WriteString(fmt.Sprintf("Date: %s\r\n", email.Date.Format(time.RFC1123Z)))
	
	// Write custom snail headers
	if email.Status != "" {
		builder.WriteString(fmt.Sprintf("X-Snail-Status: %s\r\n", email.Status))
	}
	
	if len(email.Labels) > 0 {
		builder.WriteString(fmt.Sprintf("X-Snail-Labels: %s\r\n", strings.Join(email.Labels, ",")))
	}
	
	if email.SyncID != "" {
		builder.WriteString(fmt.Sprintf("X-Snail-Sync-ID: %s\r\n", email.SyncID))
	}
	
	// Write other headers
	for key, value := range email.Headers {
		if !p.isStandardHeader(key) {
			builder.WriteString(fmt.Sprintf("%s: %s\r\n", key, value))
		}
	}
	
	builder.WriteString("\r\n") // Empty line separating headers from body
	
	// Write body
	builder.WriteString(email.Body)
	
	return builder.String(), nil
}

// isStandardHeader checks if a header is a standard email header
func (p *EmailParser) isStandardHeader(header string) bool {
	standardHeaders := []string{
		"Message-ID", "From", "To", "Cc", "Bcc", "Subject", "Date",
		"X-Snail-Status", "X-Snail-Labels", "X-Snail-Sync-ID",
	}
	
	for _, std := range standardHeaders {
		if strings.EqualFold(header, std) {
			return true
		}
	}
	
	return false
}