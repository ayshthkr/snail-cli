package repository

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"snail-cli/internal/models"
	"snail-cli/internal/services"
)

// MockEmailRepository is a mock implementation of EmailRepository for testing
type MockEmailRepository struct {
	emails map[string]*models.Email
	nextID int
}

// NewMockEmailRepository creates a new mock email repository with sample data
func NewMockEmailRepository() *MockEmailRepository {
	repo := &MockEmailRepository{
		emails: make(map[string]*models.Email),
		nextID: 1,
	}
	
	// Add some sample emails for testing
	repo.addSampleEmails()
	
	return repo
}

// Get retrieves an email by ID
func (r *MockEmailRepository) Get(ctx context.Context, id string) (*models.Email, error) {
	email, exists := r.emails[id]
	if !exists {
		return nil, fmt.Errorf("email not found: %s", id)
	}
	
	// Return a copy to prevent external modification
	return r.copyEmail(email), nil
}

// List returns emails matching the given criteria
func (r *MockEmailRepository) List(ctx context.Context, criteria *services.ListCriteria) ([]*models.Email, error) {
	if criteria == nil {
		criteria = &services.ListCriteria{}
	}
	
	var emails []*models.Email
	
	for _, email := range r.emails {
		// Apply basic filters
		if criteria.Status != "" && email.Status != criteria.Status {
			continue
		}
		
		if criteria.Folder != "" {
			// For mock, we'll check if folder is in labels
			hasFolder := false
			for _, label := range email.Labels {
				if strings.EqualFold(label, criteria.Folder) {
					hasFolder = true
					break
				}
			}
			if !hasFolder {
				continue
			}
		}
		
		emails = append(emails, r.copyEmail(email))
	}
	
	// Sort by date (newest first) by default
	sort.Slice(emails, func(i, j int) bool {
		return emails[i].Date.After(emails[j].Date)
	})
	
	return emails, nil
}

// Store saves an email to the repository
func (r *MockEmailRepository) Store(ctx context.Context, email *models.Email) error {
	if email.ID == "" {
		email.ID = fmt.Sprintf("msg-%03d", r.nextID)
		r.nextID++
	}
	
	r.emails[email.ID] = r.copyEmail(email)
	return nil
}

// Update modifies an existing email
func (r *MockEmailRepository) Update(ctx context.Context, email *models.Email) error {
	if _, exists := r.emails[email.ID]; !exists {
		return fmt.Errorf("email not found: %s", email.ID)
	}
	
	r.emails[email.ID] = r.copyEmail(email)
	return nil
}

// Delete removes an email from the repository
func (r *MockEmailRepository) Delete(ctx context.Context, id string) error {
	if _, exists := r.emails[id]; !exists {
		return fmt.Errorf("email not found: %s", id)
	}
	
	delete(r.emails, id)
	return nil
}

// copyEmail creates a deep copy of an email
func (r *MockEmailRepository) copyEmail(email *models.Email) *models.Email {
	emailCopy := &models.Email{
		ID:        email.ID,
		MessageID: email.MessageID,
		From:      email.From,
		Subject:   email.Subject,
		Date:      email.Date,
		Body:      email.Body,
		Status:    email.Status,
		SyncID:    email.SyncID,
	}
	
	// Copy slices
	emailCopy.To = make([]models.Address, len(email.To))
	copy(emailCopy.To, email.To)
	
	emailCopy.CC = make([]models.Address, len(email.CC))
	copy(emailCopy.CC, email.CC)
	
	emailCopy.BCC = make([]models.Address, len(email.BCC))
	copy(emailCopy.BCC, email.BCC)
	
	emailCopy.Labels = make([]string, len(email.Labels))
	copy(emailCopy.Labels, email.Labels)
	
	emailCopy.Attachments = make([]models.Attachment, len(email.Attachments))
	copy(emailCopy.Attachments, email.Attachments)
	
	// Copy headers map
	emailCopy.Headers = make(map[string]string)
	for k, v := range email.Headers {
		emailCopy.Headers[k] = v
	}
	
	return emailCopy
}

// addSampleEmails adds sample emails for testing
func (r *MockEmailRepository) addSampleEmails() {
	now := time.Now()
	
	emails := []*models.Email{
		{
			ID:        "msg-001",
			MessageID: "<msg001@example.com>",
			From:      models.Address{Name: "John Doe", Email: "john@example.com"},
			To:        []models.Address{{Name: "Jane Smith", Email: "jane@example.com"}},
			Subject:   "Welcome to Snail CLI",
			Date:      now.Add(-24 * time.Hour),
			Body:      "Welcome to Snail CLI! This is your first email in the system.\n\nThis email client stores emails as plain text files in a Git repository, giving you version control over your email history.\n\nEnjoy using Snail CLI!",
			Status:    models.StatusUnread,
			Labels:    []string{"inbox", "welcome"},
			Headers:   map[string]string{"X-Priority": "1"},
		},
		{
			ID:        "msg-002",
			MessageID: "<msg002@example.com>",
			From:      models.Address{Name: "Alice Johnson", Email: "alice@company.com"},
			To:        []models.Address{{Name: "Jane Smith", Email: "jane@example.com"}},
			CC:        []models.Address{{Name: "Bob Wilson", Email: "bob@company.com"}},
			Subject:   "Project Update - Q1 2024",
			Date:      now.Add(-12 * time.Hour),
			Body:      "Hi team,\n\nI wanted to provide an update on our Q1 2024 project status.\n\nKey achievements:\n- Completed user authentication module\n- Implemented email synchronization\n- Added comprehensive test coverage\n\nNext steps:\n- Deploy to staging environment\n- Conduct user acceptance testing\n- Prepare for production release\n\nPlease let me know if you have any questions.\n\nBest regards,\nAlice",
			Status:    models.StatusRead,
			Labels:    []string{"inbox", "work", "project"},
			Headers:   map[string]string{"X-Project": "Q1-2024"},
		},
		{
			ID:        "msg-003",
			MessageID: "<msg003@example.com>",
			From:      models.Address{Name: "Newsletter Bot", Email: "newsletter@techblog.com"},
			To:        []models.Address{{Name: "Jane Smith", Email: "jane@example.com"}},
			Subject:   "Weekly Tech Newsletter - Latest Trends",
			Date:      now.Add(-6 * time.Hour),
			Body:      "This week in tech:\n\n1. AI developments in software engineering\n2. New frameworks for web development\n3. Cloud computing trends\n4. Cybersecurity best practices\n\nRead more at techblog.com",
			Status:    models.StatusUnread,
			Labels:    []string{"inbox", "newsletter", "tech"},
			Headers:   map[string]string{"List-Unsubscribe": "<mailto:unsubscribe@techblog.com>"},
		},
		{
			ID:        "msg-004",
			MessageID: "<msg004@example.com>",
			From:      models.Address{Name: "Support Team", Email: "support@service.com"},
			To:        []models.Address{{Name: "Jane Smith", Email: "jane@example.com"}},
			Subject:   "Your ticket #12345 has been resolved",
			Date:      now.Add(-2 * time.Hour),
			Body:      "Dear Jane,\n\nYour support ticket #12345 regarding email synchronization issues has been resolved.\n\nSolution applied:\n- Updated IMAP configuration\n- Cleared sync cache\n- Restarted synchronization service\n\nPlease test the functionality and let us know if you experience any further issues.\n\nThank you for your patience.\n\nSupport Team",
			Status:    models.StatusRead,
			Labels:    []string{"inbox", "support", "resolved"},
			Headers:   map[string]string{"X-Ticket-ID": "12345"},
		},
		{
			ID:        "msg-005",
			MessageID: "<msg005@example.com>",
			From:      models.Address{Name: "Jane Smith", Email: "jane@example.com"},
			To:        []models.Address{{Name: "Mom", Email: "mom@family.com"}},
			Subject:   "Re: Family dinner this weekend",
			Date:      now.Add(-30 * time.Minute),
			Body:      "Hi Mom,\n\nYes, I'll be there for dinner on Sunday at 6 PM. Should I bring anything?\n\nLooking forward to seeing everyone!\n\nLove,\nJane",
			Status:    models.StatusSent,
			Labels:    []string{"sent", "family"},
			Headers:   map[string]string{"In-Reply-To": "<family-dinner@family.com>"},
		},
		{
			ID:        "msg-006",
			MessageID: "<msg006@example.com>",
			From:      models.Address{Name: "Jane Smith", Email: "jane@example.com"},
			To:        []models.Address{{Name: "Team Lead", Email: "lead@company.com"}},
			Subject:   "Draft: Proposal for new email client features",
			Date:      now.Add(-10 * time.Minute),
			Body:      "Hi Team Lead,\n\nI've been working on a proposal for new features in our email client:\n\n1. Advanced search capabilities\n2. Email templates\n3. Automated filtering\n4. Integration with calendar\n\nI'll finish this proposal and send it tomorrow.\n\nBest regards,\nJane",
			Status:    models.StatusDraft,
			Labels:    []string{"drafts", "work", "proposal"},
			Headers:   map[string]string{"X-Draft": "true"},
		},
		{
			ID:        "msg-007",
			MessageID: "<msg007@example.com>",
			From:      models.Address{Name: "Security Alert", Email: "security@example.com"},
			To:        []models.Address{{Name: "Jane Smith", Email: "jane@example.com"}},
			Subject:   "Security Alert: New login detected",
			Date:      now.Add(-45 * time.Minute),
			Body:      "We detected a new login to your account from:\n\nLocation: San Francisco, CA\nDevice: Chrome on Linux\nTime: " + now.Add(-45*time.Minute).Format("2006-01-02 15:04:05") + "\n\nIf this was you, you can ignore this message. If not, please secure your account immediately.\n\nSecurity Team",
			Status:    models.StatusUnread,
			Labels:    []string{"inbox", "security", "important"},
			Headers:   map[string]string{"X-Priority": "1", "X-Security-Alert": "login"},
		},
		{
			ID:        "msg-008",
			MessageID: "<msg008@example.com>",
			From:      models.Address{Name: "Calendar", Email: "calendar@example.com"},
			To:        []models.Address{{Name: "Jane Smith", Email: "jane@example.com"}},
			Subject:   "Reminder: Team meeting in 1 hour",
			Date:      now.Add(-15 * time.Minute),
			Body:      "This is a reminder that you have a team meeting scheduled in 1 hour.\n\nMeeting: Weekly Team Standup\nTime: " + now.Add(45*time.Minute).Format("2006-01-02 15:04") + "\nLocation: Conference Room A\n\nAgenda:\n- Sprint review\n- Planning for next week\n- Q&A session\n\nSee you there!",
			Status:    models.StatusRead,
			Labels:    []string{"inbox", "calendar", "meeting"},
			Headers:   map[string]string{"X-Calendar-Event": "team-standup"},
		},
	}
	
	for _, email := range emails {
		r.emails[email.ID] = email
	}
	
	r.nextID = len(emails) + 1
}