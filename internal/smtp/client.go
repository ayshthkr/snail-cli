package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"snail-cli/internal/config"
	"snail-cli/internal/interfaces"
)

// Client implements the SMTPClient interface
type Client struct {
	config      *config.GmailConfig
	client      *smtp.Client
	mu          sync.RWMutex
	connected   bool
	connectedAt *time.Time
	lastError   error
	extensions  []string
}

// NewClient creates a new SMTP client
func NewClient(cfg *config.GmailConfig) *Client {
	return &Client{
		config: cfg,
	}
}

// Connect establishes connection to SMTP server
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	address := fmt.Sprintf("%s:%d", c.config.SMTPServer, c.config.SMTPPort)
	
	// Create connection with timeout
	dialer := &net.Dialer{
		Timeout: 30 * time.Second,
	}
	
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		c.lastError = fmt.Errorf("failed to connect to SMTP server: %w", err)
		return c.lastError
	}

	var client *smtp.Client

	if c.config.UseTLS {
		// Direct TLS connection
		tlsConfig := &tls.Config{
			ServerName:         c.config.SMTPServer,
			InsecureSkipVerify: false,
		}
		
		tlsConn := tls.Client(conn, tlsConfig)
		client, err = smtp.NewClient(tlsConn, c.config.SMTPServer)
		if err != nil {
			conn.Close()
			c.lastError = fmt.Errorf("failed to create TLS SMTP client: %w", err)
			return c.lastError
		}
	} else {
		// Plain connection (will use STARTTLS if available)
		client, err = smtp.NewClient(conn, c.config.SMTPServer)
		if err != nil {
			conn.Close()
			c.lastError = fmt.Errorf("failed to create SMTP client: %w", err)
			return c.lastError
		}

		// Use STARTTLS if supported and configured
		if c.config.UseStartTLS {
			if ok, _ := client.Extension("STARTTLS"); ok {
				tlsConfig := &tls.Config{
					ServerName:         c.config.SMTPServer,
					InsecureSkipVerify: false,
				}
				
				if err := client.StartTLS(tlsConfig); err != nil {
					client.Close()
					c.lastError = fmt.Errorf("failed to start TLS: %w", err)
					return c.lastError
				}
			}
		}
	}

	c.client = client

	// Get server extensions
	c.extensions = make([]string, 0)
	// Note: Go's smtp package doesn't provide a way to get all extensions
	// We'll populate this during authentication checks

	now := time.Now()
	c.connected = true
	c.connectedAt = &now
	c.lastError = nil

	return nil
}

// Disconnect closes the SMTP connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.cleanup()
}

// cleanup closes the connection and resets state (must be called with lock held)
func (c *Client) cleanup() error {
	var err error
	
	if c.client != nil {
		if quitErr := c.client.Quit(); quitErr != nil {
			err = fmt.Errorf("quit failed: %w", quitErr)
		}
		if closeErr := c.client.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("connection close failed: %w", closeErr)
		}
		c.client = nil
	}

	c.connected = false
	c.connectedAt = nil

	return err
}

// Authenticate performs authentication with the server
func (c *Client) Authenticate(ctx context.Context, username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to server")
	}

	// Check if server supports authentication
	if ok, _ := c.client.Extension("AUTH"); !ok {
		return fmt.Errorf("server does not support authentication")
	}

	// Try different authentication methods
	auth := smtp.PlainAuth("", username, password, c.config.SMTPServer)
	
	if err := c.client.Auth(auth); err != nil {
		c.lastError = fmt.Errorf("authentication failed: %w", err)
		return c.lastError
	}

	return nil
}

// AuthenticateOAuth2 performs OAuth2 authentication
func (c *Client) AuthenticateOAuth2(ctx context.Context, username, accessToken string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to server")
	}

	// Check if server supports OAuth2
	if ok, _ := c.client.Extension("AUTH"); ok {
		// Server supports AUTH, we'll try OAuth2 and let the auth mechanism handle the details
		// The actual method checking will be done in the OAuth2Auth.Start method
	} else {
		return fmt.Errorf("server does not support authentication")
	}

	// Create OAuth2 auth mechanism
	auth := &OAuth2Auth{
		Username: username,
		Token:    accessToken,
	}
	
	if err := c.client.Auth(auth); err != nil {
		c.lastError = fmt.Errorf("OAuth2 authentication failed: %w", err)
		return c.lastError
	}

	return nil
}

// SendEmail sends an email message
func (c *Client) SendEmail(ctx context.Context, email *interfaces.Email) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to server")
	}

	// Validate email
	if err := c.validateEmail(email); err != nil {
		return fmt.Errorf("email validation failed: %w", err)
	}

	// Set sender
	if err := c.client.Mail(email.From.Email); err != nil {
		c.lastError = fmt.Errorf("failed to set sender: %w", err)
		return c.lastError
	}

	// Set recipients
	recipients := make([]string, 0)
	
	// Add To recipients
	for _, addr := range email.To {
		if err := c.client.Rcpt(addr.Email); err != nil {
			c.lastError = fmt.Errorf("failed to add recipient %s: %w", addr.Email, err)
			return c.lastError
		}
		recipients = append(recipients, addr.Email)
	}
	
	// Add CC recipients
	for _, addr := range email.CC {
		if err := c.client.Rcpt(addr.Email); err != nil {
			c.lastError = fmt.Errorf("failed to add CC recipient %s: %w", addr.Email, err)
			return c.lastError
		}
		recipients = append(recipients, addr.Email)
	}
	
	// Add BCC recipients
	for _, addr := range email.BCC {
		if err := c.client.Rcpt(addr.Email); err != nil {
			c.lastError = fmt.Errorf("failed to add BCC recipient %s: %w", addr.Email, err)
			return c.lastError
		}
		recipients = append(recipients, addr.Email)
	}

	// Get data writer
	writer, err := c.client.Data()
	if err != nil {
		c.lastError = fmt.Errorf("failed to get data writer: %w", err)
		return c.lastError
	}
	defer writer.Close()

	// Format and send email content
	emailContent, err := c.formatEmail(email)
	if err != nil {
		c.lastError = fmt.Errorf("failed to format email: %w", err)
		return c.lastError
	}

	if _, err := writer.Write([]byte(emailContent)); err != nil {
		c.lastError = fmt.Errorf("failed to write email data: %w", err)
		return c.lastError
	}

	if err := writer.Close(); err != nil {
		c.lastError = fmt.Errorf("failed to close data writer: %w", err)
		return c.lastError
	}

	return nil
}

// IsConnected returns true if connected to server
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// GetConnectionInfo returns connection information
func (c *Client) GetConnectionInfo() *interfaces.SMTPConnectionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &interfaces.SMTPConnectionInfo{
		Server:      c.config.SMTPServer,
		Port:        c.config.SMTPPort,
		TLS:         c.config.UseTLS,
		Connected:   c.connected,
		ConnectedAt: c.connectedAt,
		LastError:   c.lastError,
		Extensions:  append([]string(nil), c.extensions...),
	}
}

// validateEmail validates the email before sending
func (c *Client) validateEmail(email *interfaces.Email) error {
	if email == nil {
		return fmt.Errorf("email is nil")
	}

	if email.From.Email == "" {
		return fmt.Errorf("sender email is required")
	}

	if len(email.To) == 0 && len(email.CC) == 0 && len(email.BCC) == 0 {
		return fmt.Errorf("at least one recipient is required")
	}

	if email.Subject == "" {
		return fmt.Errorf("subject is required")
	}

	return nil
}

// formatEmail formats the email into RFC 5322 format
func (c *Client) formatEmail(email *interfaces.Email) (string, error) {
	var builder strings.Builder

	// Write headers
	builder.WriteString(fmt.Sprintf("From: %s\r\n", c.formatAddress(email.From)))
	
	if len(email.To) > 0 {
		builder.WriteString(fmt.Sprintf("To: %s\r\n", c.formatAddressList(email.To)))
	}
	
	if len(email.CC) > 0 {
		builder.WriteString(fmt.Sprintf("Cc: %s\r\n", c.formatAddressList(email.CC)))
	}
	
	builder.WriteString(fmt.Sprintf("Subject: %s\r\n", email.Subject))
	
	// Add date if not present
	if email.Date.IsZero() {
		builder.WriteString(fmt.Sprintf("Date: %s\r\n", time.Now().Format(time.RFC1123Z)))
	} else {
		builder.WriteString(fmt.Sprintf("Date: %s\r\n", email.Date.Format(time.RFC1123Z)))
	}
	
	// Add message ID if present
	if email.MessageID != "" {
		builder.WriteString(fmt.Sprintf("Message-ID: %s\r\n", email.MessageID))
	}
	
	// Add MIME version for proper email formatting
	builder.WriteString("MIME-Version: 1.0\r\n")
	
	// Set content type
	if len(email.Attachments) > 0 {
		// Multipart email with attachments
		boundary := fmt.Sprintf("boundary_%d", time.Now().Unix())
		builder.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=\"%s\"\r\n", boundary))
		builder.WriteString("\r\n")
		
		// Email body part
		builder.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		builder.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
		builder.WriteString("\r\n")
		builder.WriteString(email.Body)
		builder.WriteString("\r\n")
		
		// Attachment parts (simplified - would need proper encoding in production)
		for _, att := range email.Attachments {
			builder.WriteString(fmt.Sprintf("--%s\r\n", boundary))
			builder.WriteString(fmt.Sprintf("Content-Type: %s\r\n", att.ContentType))
			builder.WriteString("Content-Transfer-Encoding: base64\r\n")
			builder.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n", att.Filename))
			builder.WriteString("\r\n")
			// Note: In production, would need to base64 encode the attachment data
			builder.WriteString("[Attachment data would be base64 encoded here]\r\n")
		}
		
		builder.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		// Simple text email
		builder.WriteString("Content-Type: text/plain; charset=utf-8\r\n")
		builder.WriteString("Content-Transfer-Encoding: 8bit\r\n")
		builder.WriteString("\r\n")
		builder.WriteString(email.Body)
	}

	return builder.String(), nil
}

// formatAddress formats an address for email headers
func (c *Client) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// formatAddressList formats a list of addresses for email headers
func (c *Client) formatAddressList(addrs []interfaces.Address) string {
	formatted := make([]string, len(addrs))
	for i, addr := range addrs {
		formatted[i] = c.formatAddress(addr)
	}
	return strings.Join(formatted, ", ")
}

// OAuth2Auth implements smtp.Auth for OAuth2 authentication
type OAuth2Auth struct {
	Username string
	Token    string
}

// Start begins OAuth2 authentication
func (a *OAuth2Auth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	// Try OAUTHBEARER first, then XOAUTH2
	if len(server.Auth) > 0 {
		for _, method := range server.Auth {
			if strings.ToUpper(method) == "OAUTHBEARER" {
				return "OAUTHBEARER", a.oauthBearerResponse(), nil
			}
		}
		for _, method := range server.Auth {
			if strings.ToUpper(method) == "XOAUTH2" {
				return "XOAUTH2", a.xoauth2Response(), nil
			}
		}
	}
	
	return "", nil, fmt.Errorf("server does not support OAuth2 authentication")
}

// Next continues OAuth2 authentication
func (a *OAuth2Auth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		// Server is asking for more data, but OAuth2 should be one-shot
		return nil, fmt.Errorf("unexpected server response during OAuth2 authentication")
	}
	return nil, nil
}

// oauthBearerResponse creates an OAUTHBEARER response
func (a *OAuth2Auth) oauthBearerResponse() []byte {
	return []byte(fmt.Sprintf("n,a=%s,\x01auth=Bearer %s\x01\x01", a.Username, a.Token))
}

// xoauth2Response creates an XOAUTH2 response
func (a *OAuth2Auth) xoauth2Response() []byte {
	return []byte(fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", a.Username, a.Token))
}