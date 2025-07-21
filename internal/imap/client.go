package imap

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-sasl"

	"snail-cli/internal/config"
	"snail-cli/internal/interfaces"
)

// Client implements the IMAPClient interface
type Client struct {
	config     *config.GmailConfig
	client     *imapclient.Client
	mu         sync.RWMutex
	connected  bool
	connectedAt *time.Time
	lastError  error
	capabilities []string
	selectedFolder string
}

// NewClient creates a new IMAP client
func NewClient(cfg *config.GmailConfig) *Client {
	return &Client{
		config: cfg,
	}
}

// Connect establishes connection to IMAP server
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	var client *imapclient.Client
	var err error

	address := fmt.Sprintf("%s:%d", c.config.IMAPServer, c.config.IMAPPort)
	options := &imapclient.Options{}

	if c.config.UseTLS {
		// Direct TLS connection
		client, err = imapclient.DialTLS(address, options)
	} else if c.config.UseStartTLS {
		// STARTTLS connection
		client, err = imapclient.DialStartTLS(address, options)
	} else {
		// Insecure connection
		client, err = imapclient.DialInsecure(address, options)
	}

	if err != nil {
		c.lastError = fmt.Errorf("failed to connect to IMAP server: %w", err)
		return c.lastError
	}

	c.client = client

	// Wait for greeting
	if err := c.client.WaitGreeting(); err != nil {
		c.lastError = fmt.Errorf("failed to receive greeting: %w", err)
		c.cleanup()
		return c.lastError
	}

	// Get server capabilities
	caps := c.client.Caps()
	c.capabilities = make([]string, 0, len(caps))
	for cap := range caps {
		c.capabilities = append(c.capabilities, string(cap))
	}

	now := time.Now()
	c.connected = true
	c.connectedAt = &now
	c.lastError = nil

	return nil
}

// Disconnect closes the IMAP connection
func (c *Client) Disconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.cleanup()
}

// cleanup closes the connection and resets state (must be called with lock held)
func (c *Client) cleanup() error {
	var err error
	
	if c.client != nil {
		if logoutErr := c.client.Logout().Wait(); logoutErr != nil {
			err = fmt.Errorf("logout failed: %w", logoutErr)
		}
		if closeErr := c.client.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("connection close failed: %w", closeErr)
		}
		c.client = nil
	}

	c.connected = false
	c.connectedAt = nil
	c.selectedFolder = ""

	return err
}

// Authenticate performs authentication with the server
func (c *Client) Authenticate(ctx context.Context, username, password string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to server")
	}

	if err := c.client.Login(username, password).Wait(); err != nil {
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
	caps := c.client.Caps()
	supportsOAuth := false
	for cap := range caps {
		if strings.Contains(strings.ToUpper(string(cap)), "AUTH=OAUTHBEARER") {
			supportsOAuth = true
			break
		}
	}

	if !supportsOAuth {
		return fmt.Errorf("server does not support OAuth2 authentication")
	}

	// Create OAuth2 SASL client
	saslClient := sasl.NewOAuthBearerClient(&sasl.OAuthBearerOptions{
		Username:    username,
		Token:       accessToken,
	})

	if err := c.client.Authenticate(saslClient); err != nil {
		c.lastError = fmt.Errorf("OAuth2 authentication failed: %w", err)
		return c.lastError
	}

	return nil
}

// SelectFolder selects an IMAP folder
func (c *Client) SelectFolder(ctx context.Context, folder string) (*interfaces.FolderInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected to server")
	}

	data, err := c.client.Select(folder, nil).Wait()
	if err != nil {
		c.lastError = fmt.Errorf("failed to select folder %s: %w", folder, err)
		return nil, c.lastError
	}

	c.selectedFolder = folder

	folderInfo := &interfaces.FolderInfo{
		Name:         folder,
		MessageCount: data.NumMessages,
		RecentCount:  data.NumRecent,
		UnseenCount:  0, // NumUnseen not available in SelectData
		UIDValidity:  data.UIDValidity,
		UIDNext:      uint32(data.UIDNext),
	}

	return folderInfo, nil
}

// ListFolders returns list of available folders
func (c *Client) ListFolders(ctx context.Context) ([]*interfaces.FolderInfo, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected to server")
	}

	folders := make([]*interfaces.FolderInfo, 0)
	
	cmd := c.client.List("", "*", nil)
	defer cmd.Close()

	for {
		data := cmd.Next()
		if data == nil {
			break
		}

		folderInfo := &interfaces.FolderInfo{
			Name:       data.Mailbox,
			Delimiter:  string(data.Delim),
			Attributes: make([]string, 0, len(data.Attrs)),
		}

		for _, attr := range data.Attrs {
			folderInfo.Attributes = append(folderInfo.Attributes, string(attr))
		}

		folders = append(folders, folderInfo)
	}

	if err := cmd.Close(); err != nil {
		return nil, fmt.Errorf("failed to list folders: %w", err)
	}

	return folders, nil
}

// FetchEmails fetches emails from the selected folder with UID-based incremental sync
func (c *Client) FetchEmails(ctx context.Context, criteria *interfaces.FetchCriteria) ([]*interfaces.Email, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected to server")
	}

	if c.selectedFolder == "" {
		return nil, fmt.Errorf("no folder selected")
	}

	// Build UID set based on criteria
	var uidSet imap.UIDSet
	if criteria != nil && criteria.UIDRange != nil {
		if criteria.UIDRange.End == 0 {
			// From start UID to end - use max UID value
			uidSet = imap.UIDSet{imap.UIDRange{Start: imap.UID(criteria.UIDRange.Start), Stop: 0xFFFFFFFF}}
		} else {
			// Specific range
			uidSet = imap.UIDSet{imap.UIDRange{Start: imap.UID(criteria.UIDRange.Start), Stop: imap.UID(criteria.UIDRange.End)}}
		}
	} else {
		// Fetch all emails
		uidSet = imap.UIDSet{imap.UIDRange{Start: 1, Stop: 0xFFFFFFFF}}
	}

	// Build fetch options
	fetchOptions := &imap.FetchOptions{
		UID:      true,
		Flags:    true,
		Envelope: true,
	}

	// Add body structure and content based on criteria
	if criteria == nil || !criteria.HeadersOnly {
		fetchOptions.BodyStructure = &imap.FetchItemBodyStructure{}
		fetchOptions.BodySection = []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader},
			{Specifier: imap.PartSpecifierText},
		}
	}

	// Execute fetch command
	cmd := c.client.Fetch(uidSet, fetchOptions)
	defer cmd.Close()

	emails := make([]*interfaces.Email, 0)
	count := 0

	for {
		msg := cmd.Next()
		if msg == nil {
			break
		}

		// Check limit
		if criteria != nil && criteria.Limit > 0 && count >= criteria.Limit {
			break
		}

		email, err := c.parseIMAPMessage(msg)
		if err != nil {
			// Log error but continue processing other emails
			continue
		}

		// Apply date filters if specified
		if criteria != nil {
			if criteria.Since != nil && email.Date.Before(*criteria.Since) {
				continue
			}
			if criteria.Before != nil && email.Date.After(*criteria.Before) {
				continue
			}
			if criteria.UnseenOnly && email.Status == interfaces.StatusRead {
				continue
			}
		}

		emails = append(emails, email)
		count++
	}

	if err := cmd.Close(); err != nil {
		return nil, fmt.Errorf("failed to fetch emails: %w", err)
	}

	return emails, nil
}

// FetchEmailByUID fetches a single email by UID
func (c *Client) FetchEmailByUID(ctx context.Context, uid uint32) (*interfaces.Email, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected to server")
	}

	if c.selectedFolder == "" {
		return nil, fmt.Errorf("no folder selected")
	}

	// Create UID set for single email
	uidSet := imap.UIDSetNum(imap.UID(uid))

	// Build fetch options for complete email
	fetchOptions := &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		Envelope:      true,
		BodyStructure: &imap.FetchItemBodyStructure{},
		BodySection: []*imap.FetchItemBodySection{
			{Specifier: imap.PartSpecifierHeader},
			{Specifier: imap.PartSpecifierText},
		},
	}

	// Execute fetch command
	cmd := c.client.Fetch(uidSet, fetchOptions)
	defer cmd.Close()

	msg := cmd.Next()
	if msg == nil {
		return nil, fmt.Errorf("email with UID %d not found", uid)
	}

	email, err := c.parseIMAPMessage(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse email: %w", err)
	}

	if err := cmd.Close(); err != nil {
		return nil, fmt.Errorf("failed to fetch email: %w", err)
	}

	return email, nil
}

// MarkAsRead marks emails as read
func (c *Client) MarkAsRead(ctx context.Context, uids []uint32) error {
	return c.updateFlags(ctx, uids, []imap.Flag{imap.FlagSeen}, imap.StoreFlagsSet)
}

// MarkAsUnread marks emails as unread
func (c *Client) MarkAsUnread(ctx context.Context, uids []uint32) error {
	return c.updateFlags(ctx, uids, []imap.Flag{imap.FlagSeen}, imap.StoreFlagsDel)
}

// DeleteEmails moves emails to trash
func (c *Client) DeleteEmails(ctx context.Context, uids []uint32) error {
	return c.updateFlags(ctx, uids, []imap.Flag{imap.FlagDeleted}, imap.StoreFlagsSet)
}

// MoveEmails moves emails to another folder (simplified implementation)
func (c *Client) MoveEmails(ctx context.Context, uids []uint32, destFolder string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to server")
	}

	if c.selectedFolder == "" {
		return fmt.Errorf("no folder selected")
	}

	// Convert UIDs to UIDSet
	uidNums := make([]imap.UID, len(uids))
	for i, uid := range uids {
		uidNums[i] = imap.UID(uid)
	}
	uidSet := imap.UIDSetNum(uidNums...)

	// Check if server supports MOVE extension
	caps := c.client.Caps()
	supportsMove := false
	for cap := range caps {
		if strings.ToUpper(string(cap)) == "MOVE" {
			supportsMove = true
			break
		}
	}

	if supportsMove {
		// Use MOVE command if supported
		_, err := c.client.Move(uidSet, destFolder).Wait()
		if err != nil {
			return fmt.Errorf("failed to move emails: %w", err)
		}
	} else {
		// Fallback to COPY + STORE + EXPUNGE
		_, err := c.client.Copy(uidSet, destFolder).Wait()
		if err != nil {
			return fmt.Errorf("failed to copy emails: %w", err)
		}

		// Mark as deleted
		if err := c.updateFlags(ctx, uids, []imap.Flag{imap.FlagDeleted}, imap.StoreFlagsSet); err != nil {
			return fmt.Errorf("failed to mark emails as deleted: %w", err)
		}

		// Expunge to actually delete
		if err := c.client.Expunge().Close(); err != nil {
			return fmt.Errorf("failed to expunge emails: %w", err)
		}
	}

	return nil
}

// updateFlags updates flags for the specified UIDs (simplified implementation)
func (c *Client) updateFlags(ctx context.Context, uids []uint32, flags []imap.Flag, op imap.StoreFlagsOp) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected to server")
	}

	if c.selectedFolder == "" {
		return fmt.Errorf("no folder selected")
	}

	// Convert UIDs to UIDSet
	uidNums := make([]imap.UID, len(uids))
	for i, uid := range uids {
		uidNums[i] = imap.UID(uid)
	}
	uidSet := imap.UIDSetNum(uidNums...)

	storeFlags := &imap.StoreFlags{
		Op:    op,
		Flags: flags,
	}

	if err := c.client.Store(uidSet, storeFlags, nil).Close(); err != nil {
		return fmt.Errorf("failed to update flags: %w", err)
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
func (c *Client) GetConnectionInfo() *interfaces.ConnectionInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return &interfaces.ConnectionInfo{
		Server:       c.config.IMAPServer,
		Port:         c.config.IMAPPort,
		TLS:          c.config.UseTLS,
		Connected:    c.connected,
		ConnectedAt:  c.connectedAt,
		LastError:    c.lastError,
		Capabilities: append([]string(nil), c.capabilities...),
	}
}

// parseIMAPMessage converts an IMAP fetch response to an Email struct
func (c *Client) parseIMAPMessage(msg *imapclient.FetchMessageData) (*interfaces.Email, error) {
	// Collect the message data into a buffer
	buffer, err := msg.Collect()
	if err != nil {
		return nil, fmt.Errorf("failed to collect message data: %w", err)
	}

	email := &interfaces.Email{
		Headers:     make(map[string]string),
		Labels:      make([]string, 0),
		Attachments: make([]interfaces.Attachment, 0),
		Status:      interfaces.StatusUnread,
	}

	// Set UID as sync ID
	if buffer.UID != 0 {
		email.SyncID = fmt.Sprintf("imap:%d", buffer.UID)
		email.ID = fmt.Sprintf("%s:%d", c.selectedFolder, buffer.UID)
	}

	// Parse flags to determine status
	for _, flag := range buffer.Flags {
		switch flag {
		case imap.FlagSeen:
			email.Status = interfaces.StatusRead
		case imap.FlagDraft:
			email.Status = interfaces.StatusDraft
		}
	}

	// Parse envelope for basic email information
	if buffer.Envelope != nil {
		env := buffer.Envelope
		
		// Set subject
		email.Subject = env.Subject
		
		// Set date
		if !env.Date.IsZero() {
			email.Date = env.Date
		}
		
		// Set message ID
		email.MessageID = env.MessageID
		
		// Parse addresses
		if len(env.From) > 0 {
			email.From = c.parseIMAPAddress(&env.From[0])
		}
		
		email.To = c.parseIMAPAddressList(env.To)
		email.CC = c.parseIMAPAddressList(env.Cc)
		email.BCC = c.parseIMAPAddressList(env.Bcc)
	}

	// Parse body sections
	for _, bodySection := range buffer.BodySection {
		switch bodySection.Section.Specifier {
		case imap.PartSpecifierHeader:
			// Parse headers
			headers, err := c.parseHeaders(string(bodySection.Bytes))
			if err == nil {
				email.Headers = headers
				// Extract additional information from headers
				c.extractHeaderInfo(email, headers)
			}
		case imap.PartSpecifierText:
			// Set body content
			email.Body = string(bodySection.Bytes)
		}
	}

	// Add folder as label
	if c.selectedFolder != "" {
		email.Labels = append(email.Labels, c.mapIMAPFolderToLocal(c.selectedFolder))
	}

	return email, nil
}

// parseIMAPAddress converts an IMAP address to our Address struct
func (c *Client) parseIMAPAddress(addr *imap.Address) interfaces.Address {
	if addr == nil {
		return interfaces.Address{}
	}
	
	email := addr.Mailbox
	if addr.Host != "" {
		email = fmt.Sprintf("%s@%s", addr.Mailbox, addr.Host)
	}
	
	return interfaces.Address{
		Name:  addr.Name,
		Email: email,
	}
}

// parseIMAPAddressList converts a list of IMAP addresses to our Address structs
func (c *Client) parseIMAPAddressList(addrs []imap.Address) []interfaces.Address {
	result := make([]interfaces.Address, 0, len(addrs))
	for _, addr := range addrs {
		result = append(result, c.parseIMAPAddress(&addr))
	}
	return result
}

// parseHeaders parses email headers from raw header content
func (c *Client) parseHeaders(headerContent string) (map[string]string, error) {
	headers := make(map[string]string)
	
	// Parse using Go's mail package
	msg, err := mail.ReadMessage(strings.NewReader(headerContent + "\r\n\r\n"))
	if err != nil {
		return nil, fmt.Errorf("failed to parse headers: %w", err)
	}
	
	for key, values := range msg.Header {
		if len(values) > 0 {
			headers[key] = values[0]
		}
	}
	
	return headers, nil
}

// extractHeaderInfo extracts additional information from parsed headers
func (c *Client) extractHeaderInfo(email *interfaces.Email, headers map[string]string) {
	// Extract date if not already set
	if email.Date.IsZero() {
		if dateStr, exists := headers["Date"]; exists {
			if parsedDate, err := mail.ParseDate(dateStr); err == nil {
				email.Date = parsedDate
			}
		}
	}
	
	// Extract message ID if not already set
	if email.MessageID == "" {
		if msgID, exists := headers["Message-Id"]; exists {
			email.MessageID = msgID
		}
	}
	
	// Extract additional labels from headers
	if xLabels, exists := headers["X-Gmail-Labels"]; exists {
		labels := strings.Split(xLabels, ",")
		for _, label := range labels {
			label = strings.TrimSpace(label)
			if label != "" {
				email.Labels = append(email.Labels, label)
			}
		}
	}
}

// mapIMAPFolderToLocal maps IMAP folder names to local folder structure
func (c *Client) mapIMAPFolderToLocal(imapFolder string) string {
	// Common Gmail folder mappings
	folderMappings := map[string]string{
		"INBOX":           "inbox",
		"[Gmail]/Sent Mail": "sent",
		"[Gmail]/Drafts":   "drafts",
		"[Gmail]/Trash":    "trash",
		"[Gmail]/Spam":     "spam",
		"[Gmail]/All Mail": "archive",
		"[Gmail]/Starred":  "starred",
		"[Gmail]/Important": "important",
	}
	
	// Check for exact match first
	if localName, exists := folderMappings[imapFolder]; exists {
		return localName
	}
	
	// Handle nested folders by replacing separators
	localFolder := strings.ReplaceAll(imapFolder, "/", "_")
	localFolder = strings.ReplaceAll(localFolder, " ", "_")
	localFolder = strings.ToLower(localFolder)
	
	// Remove special characters
	localFolder = strings.ReplaceAll(localFolder, "[", "")
	localFolder = strings.ReplaceAll(localFolder, "]", "")
	
	return localFolder
}

// GetLocalFolderPath returns the local file system path for an IMAP folder
func (c *Client) GetLocalFolderPath(imapFolder string) string {
	localFolder := c.mapIMAPFolderToLocal(imapFolder)
	
	// Create date-based subdirectory structure for better organization
	now := time.Now()
	year := now.Format("2006")
	month := now.Format("01")
	
	return fmt.Sprintf("%s/%s/%s", localFolder, year, month)
}

// FetchEmailsWithIncrementalSync performs incremental sync based on UID validity and last known UID
func (c *Client) FetchEmailsWithIncrementalSync(ctx context.Context, folder string, lastUID uint32, uidValidity uint32) ([]*interfaces.Email, error) {
	// Select the folder first
	folderInfo, err := c.SelectFolder(ctx, folder)
	if err != nil {
		return nil, fmt.Errorf("failed to select folder %s: %w", folder, err)
	}
	
	// Check UID validity - if it changed, we need to resync everything
	if uidValidity != 0 && folderInfo.UIDValidity != uidValidity {
		// UID validity changed, need full resync
		criteria := &interfaces.FetchCriteria{}
		return c.FetchEmails(ctx, criteria)
	}
	
	// Perform incremental sync from last known UID
	if lastUID > 0 {
		criteria := &interfaces.FetchCriteria{
			UIDRange: &interfaces.UIDRange{
				Start: lastUID + 1,
				End:   0, // Fetch to end
			},
		}
		return c.FetchEmails(ctx, criteria)
	}
	
	// First sync - fetch all emails
	criteria := &interfaces.FetchCriteria{}
	return c.FetchEmails(ctx, criteria)
}