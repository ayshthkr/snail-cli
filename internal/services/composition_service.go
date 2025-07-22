package services

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"snail-cli/internal/models"
)

// CompositionService handles email composition operations
type CompositionService struct {
	repository EmailRepository
	editor     string
	tempDir    string
}

// NewCompositionService creates a new composition service
func NewCompositionService(repository EmailRepository, editor string) *CompositionService {
	tempDir := os.TempDir()
	if editor == "" {
		editor = getDefaultEditor()
	}
	
	return &CompositionService{
		repository: repository,
		editor:     editor,
		tempDir:    tempDir,
	}
}

// ComposeEmail opens an editor to compose a new email
func (s *CompositionService) ComposeEmail(ctx context.Context, template *EmailTemplate) (*models.Email, error) {
	// Create email template
	email := &models.Email{
		ID:        generateEmailID(),
		MessageID: generateMessageID(),
		Date:      time.Now(),
		Status:    models.StatusDraft,
		Headers:   make(map[string]string),
		Labels:    []string{"drafts"},
	}
	
	// Apply template if provided
	if template != nil {
		if template.To != "" {
			if addr, err := models.ParseAddress(template.To); err == nil {
				email.To = []models.Address{*addr}
			}
		}
		if template.Subject != "" {
			email.Subject = template.Subject
		}
		if template.Body != "" {
			email.Body = template.Body
		}
	}
	
	// Open editor for composition
	editedEmail, err := s.openEditorForEmail(email, false)
	if err != nil {
		return nil, fmt.Errorf("failed to compose email: %w", err)
	}
	
	// Save as draft
	if err := s.repository.Store(ctx, editedEmail); err != nil {
		return nil, fmt.Errorf("failed to save draft: %w", err)
	}
	
	return editedEmail, nil
}

// ReplyToEmail creates a reply to an existing email
func (s *CompositionService) ReplyToEmail(ctx context.Context, originalEmailID string, replyAll bool) (*models.Email, error) {
	// Get the original email
	originalEmail, err := s.repository.Get(ctx, originalEmailID)
	if err != nil {
		return nil, fmt.Errorf("failed to get original email: %w", err)
	}
	
	// Create reply email
	replyEmail := &models.Email{
		ID:        generateEmailID(),
		MessageID: generateMessageID(),
		From:      originalEmail.To[0], // Assume first recipient is the sender
		To:        []models.Address{originalEmail.From},
		Subject:   addReplyPrefix(originalEmail.Subject),
		Date:      time.Now(),
		Status:    models.StatusDraft,
		Headers:   make(map[string]string),
		Labels:    []string{"drafts"},
	}
	
	// Add In-Reply-To header
	replyEmail.Headers["In-Reply-To"] = originalEmail.MessageID
	replyEmail.Headers["References"] = originalEmail.MessageID
	
	// If reply all, add CC recipients
	if replyAll {
		replyEmail.CC = append(replyEmail.CC, originalEmail.To[1:]...)
		replyEmail.CC = append(replyEmail.CC, originalEmail.CC...)
	}
	
	// Create reply body with quoted original
	replyEmail.Body = createReplyBody(originalEmail)
	
	// Open editor for composition
	editedEmail, err := s.openEditorForEmail(replyEmail, true)
	if err != nil {
		return nil, fmt.Errorf("failed to compose reply: %w", err)
	}
	
	// Save as draft
	if err := s.repository.Store(ctx, editedEmail); err != nil {
		return nil, fmt.Errorf("failed to save reply draft: %w", err)
	}
	
	return editedEmail, nil
}

// ForwardEmail creates a forward of an existing email
func (s *CompositionService) ForwardEmail(ctx context.Context, originalEmailID string) (*models.Email, error) {
	// Get the original email
	originalEmail, err := s.repository.Get(ctx, originalEmailID)
	if err != nil {
		return nil, fmt.Errorf("failed to get original email: %w", err)
	}
	
	// Create forward email
	forwardEmail := &models.Email{
		ID:        generateEmailID(),
		MessageID: generateMessageID(),
		Subject:   addForwardPrefix(originalEmail.Subject),
		Date:      time.Now(),
		Status:    models.StatusDraft,
		Headers:   make(map[string]string),
		Labels:    []string{"drafts"},
	}
	
	// Create forward body with original email
	forwardEmail.Body = createForwardBody(originalEmail)
	
	// Open editor for composition
	editedEmail, err := s.openEditorForEmail(forwardEmail, false)
	if err != nil {
		return nil, fmt.Errorf("failed to compose forward: %w", err)
	}
	
	// Save as draft
	if err := s.repository.Store(ctx, editedEmail); err != nil {
		return nil, fmt.Errorf("failed to save forward draft: %w", err)
	}
	
	return editedEmail, nil
}

// EditDraft opens an existing draft for editing
func (s *CompositionService) EditDraft(ctx context.Context, draftID string) (*models.Email, error) {
	// Get the draft email
	draft, err := s.repository.Get(ctx, draftID)
	if err != nil {
		return nil, fmt.Errorf("failed to get draft: %w", err)
	}
	
	if draft.Status != models.StatusDraft {
		return nil, fmt.Errorf("email %s is not a draft", draftID)
	}
	
	// Open editor for editing
	editedEmail, err := s.openEditorForEmail(draft, false)
	if err != nil {
		return nil, fmt.Errorf("failed to edit draft: %w", err)
	}
	
	// Update the draft
	if err := s.repository.Update(ctx, editedEmail); err != nil {
		return nil, fmt.Errorf("failed to update draft: %w", err)
	}
	
	return editedEmail, nil
}

// SendEmail marks an email as ready to send
func (s *CompositionService) SendEmail(ctx context.Context, emailID string) error {
	email, err := s.repository.Get(ctx, emailID)
	if err != nil {
		return fmt.Errorf("failed to get email: %w", err)
	}
	
	// Validate email before sending
	if err := email.Validate(); err != nil {
		return fmt.Errorf("email validation failed: %w", err)
	}
	
	// Update status to sent and move to sent folder
	email.Status = models.StatusSent
	email.Labels = []string{"sent"}
	
	if err := s.repository.Update(ctx, email); err != nil {
		return fmt.Errorf("failed to update email status: %w", err)
	}
	
	return nil
}

// openEditorForEmail opens an external editor to edit an email
func (s *CompositionService) openEditorForEmail(email *models.Email, isReply bool) (*models.Email, error) {
	// Create temporary file
	tempFile, err := s.createTempEmailFile(email)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tempFile)
	
	// Open editor
	cmd := exec.Command(s.editor, tempFile)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("editor failed: %w", err)
	}
	
	// Parse edited content
	editedEmail, err := s.parseEmailFromFile(tempFile, email.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse edited email: %w", err)
	}
	
	return editedEmail, nil
}

// createTempEmailFile creates a temporary file with email content for editing
func (s *CompositionService) createTempEmailFile(email *models.Email) (string, error) {
	tempFile := filepath.Join(s.tempDir, fmt.Sprintf("snail-compose-%s.txt", email.ID))
	
	content := s.formatEmailForEditing(email)
	
	if err := ioutil.WriteFile(tempFile, []byte(content), 0600); err != nil {
		return "", fmt.Errorf("failed to write temp file: %w", err)
	}
	
	return tempFile, nil
}

// formatEmailForEditing formats an email for editing in a text editor
func (s *CompositionService) formatEmailForEditing(email *models.Email) string {
	var builder strings.Builder
	
	// Add headers that can be edited
	builder.WriteString("# Email Composition\n")
	builder.WriteString("# Lines starting with # are comments and will be ignored\n")
	builder.WriteString("# Edit the headers below, then add your message after the blank line\n\n")
	
	// To field
	if len(email.To) > 0 {
		toAddrs := make([]string, len(email.To))
		for i, addr := range email.To {
			toAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("To: %s\n", strings.Join(toAddrs, ", ")))
	} else {
		builder.WriteString("To: \n")
	}
	
	// CC field
	if len(email.CC) > 0 {
		ccAddrs := make([]string, len(email.CC))
		for i, addr := range email.CC {
			ccAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("CC: %s\n", strings.Join(ccAddrs, ", ")))
	}
	
	// Subject field
	builder.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))
	
	// Separator
	builder.WriteString("\n")
	
	// Body
	builder.WriteString(email.Body)
	
	return builder.String()
}

// parseEmailFromFile parses an edited email file back into an Email struct
func (s *CompositionService) parseEmailFromFile(filename, emailID string) (*models.Email, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}
	
	lines := strings.Split(string(content), "\n")
	email := &models.Email{
		ID:        emailID,
		MessageID: generateMessageID(),
		Date:      time.Now(),
		Status:    models.StatusDraft,
		Headers:   make(map[string]string),
		Labels:    []string{"drafts"},
	}
	
	var bodyStart int
	inHeaders := true
	
	// Parse headers and find body start
	for i, line := range lines {
		line = strings.TrimSpace(line)
		
		// Skip comments
		if strings.HasPrefix(line, "#") {
			continue
		}
		
		// Empty line marks end of headers
		if line == "" && inHeaders {
			inHeaders = false
			bodyStart = i + 1
			break
		}
		
		if inHeaders {
			// Parse header
			if colonIndex := strings.Index(line, ":"); colonIndex > 0 {
				key := strings.TrimSpace(line[:colonIndex])
				value := strings.TrimSpace(line[colonIndex+1:])
				
				switch strings.ToLower(key) {
				case "to":
					if value != "" {
						if addrs, err := models.ParseAddressList(value); err == nil {
							email.To = addrs
						}
					}
				case "cc":
					if value != "" {
						if addrs, err := models.ParseAddressList(value); err == nil {
							email.CC = addrs
						}
					}
				case "subject":
					email.Subject = value
				}
			}
		}
	}
	
	// Parse body
	if bodyStart < len(lines) {
		bodyLines := lines[bodyStart:]
		email.Body = strings.Join(bodyLines, "\n")
	}
	
	return email, nil
}

// Helper functions

func getDefaultEditor() string {
	if editor := os.Getenv("EDITOR"); editor != "" {
		return editor
	}
	if editor := os.Getenv("VISUAL"); editor != "" {
		return editor
	}
	// Default editors by platform
	if _, err := exec.LookPath("nano"); err == nil {
		return "nano"
	}
	if _, err := exec.LookPath("vim"); err == nil {
		return "vim"
	}
	if _, err := exec.LookPath("vi"); err == nil {
		return "vi"
	}
	return "cat" // Fallback that won't work for editing but won't crash
}

func generateEmailID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}

func generateMessageID() string {
	return fmt.Sprintf("<%d@snail-cli.local>", time.Now().UnixNano())
}

func addReplyPrefix(subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func addForwardPrefix(subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), "fwd:") {
		return subject
	}
	return "Fwd: " + subject
}

func createReplyBody(originalEmail *models.Email) string {
	var builder strings.Builder
	
	builder.WriteString("\n\n")
	builder.WriteString(fmt.Sprintf("On %s, %s wrote:\n", 
		originalEmail.Date.Format("2006-01-02 15:04"), 
		originalEmail.From.String()))
	
	// Quote original message
	originalLines := strings.Split(originalEmail.Body, "\n")
	for _, line := range originalLines {
		builder.WriteString("> " + line + "\n")
	}
	
	return builder.String()
}

func createForwardBody(originalEmail *models.Email) string {
	var builder strings.Builder
	
	builder.WriteString("\n\n")
	builder.WriteString("---------- Forwarded message ----------\n")
	builder.WriteString(fmt.Sprintf("From: %s\n", originalEmail.From.String()))
	builder.WriteString(fmt.Sprintf("Date: %s\n", originalEmail.Date.Format("2006-01-02 15:04:05")))
	builder.WriteString(fmt.Sprintf("Subject: %s\n", originalEmail.Subject))
	
	if len(originalEmail.To) > 0 {
		toAddrs := make([]string, len(originalEmail.To))
		for i, addr := range originalEmail.To {
			toAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("To: %s\n", strings.Join(toAddrs, ", ")))
	}
	
	builder.WriteString("\n")
	builder.WriteString(originalEmail.Body)
	
	return builder.String()
}

// EmailTemplate represents a template for composing emails
type EmailTemplate struct {
	To      string
	Subject string
	Body    string
}