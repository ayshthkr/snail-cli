package services

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"snail-cli/internal/models"
)

// EmailService handles email operations
type EmailService struct {
	repository EmailRepository
}

// EmailRepository defines the interface for email storage operations
type EmailRepository interface {
	// Get retrieves an email by ID
	Get(ctx context.Context, id string) (*models.Email, error)
	
	// List returns emails matching the given criteria
	List(ctx context.Context, criteria *ListCriteria) ([]*models.Email, error)
	
	// Store saves an email to the repository
	Store(ctx context.Context, email *models.Email) error
	
	// Update modifies an existing email
	Update(ctx context.Context, email *models.Email) error
	
	// Delete removes an email from the repository
	Delete(ctx context.Context, id string) error
}

// ListCriteria defines criteria for listing emails
type ListCriteria struct {
	Folder   string
	Labels   []string
	Status   models.EmailStatus
	From     string
	Subject  string
	DateFrom *time.Time
	DateTo   *time.Time
	Limit    int
	Offset   int
	SortBy   string
	SortDesc bool
}

// NewEmailService creates a new email service
func NewEmailService(repository EmailRepository) *EmailService {
	return &EmailService{
		repository: repository,
	}
}

// ListEmails retrieves emails based on the given criteria
func (s *EmailService) ListEmails(ctx context.Context, criteria *ListCriteria) ([]*models.Email, error) {
	if criteria == nil {
		criteria = &ListCriteria{
			Limit:  20,
			Offset: 0,
			SortBy: "date",
			SortDesc: true,
		}
	}

	// Set defaults
	if criteria.Limit <= 0 {
		criteria.Limit = 20
	}
	if criteria.Offset < 0 {
		criteria.Offset = 0
	}
	if criteria.SortBy == "" {
		criteria.SortBy = "date"
	}

	emails, err := s.repository.List(ctx, criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to list emails: %w", err)
	}

	// Apply additional filtering that might not be handled by the repository
	emails = s.applyFilters(emails, criteria)

	// Sort emails
	s.sortEmails(emails, criteria.SortBy, criteria.SortDesc)

	// Apply pagination
	start := criteria.Offset
	end := start + criteria.Limit

	if start >= len(emails) {
		return []*models.Email{}, nil
	}

	if end > len(emails) {
		end = len(emails)
	}

	return emails[start:end], nil
}

// GetEmail retrieves a single email by ID and marks it as read
func (s *EmailService) GetEmail(ctx context.Context, id string, markAsRead bool) (*models.Email, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("email ID cannot be empty")
	}

	email, err := s.repository.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get email: %w", err)
	}

	if email == nil {
		return nil, fmt.Errorf("email not found: %s", id)
	}

	// Mark as read if requested and currently unread
	if markAsRead && email.Status == models.StatusUnread {
		email.Status = models.StatusRead
		if err := s.repository.Update(ctx, email); err != nil {
			// Log the error but don't fail the read operation
			// In a real implementation, we might want to log this properly
			fmt.Printf("Warning: failed to mark email as read: %v\n", err)
		}
	}

	return email, nil
}

// applyFilters applies additional filtering that might not be handled by the repository
func (s *EmailService) applyFilters(emails []*models.Email, criteria *ListCriteria) []*models.Email {
	if criteria == nil {
		return emails
	}

	filtered := make([]*models.Email, 0, len(emails))

	for _, email := range emails {
		// Filter by subject (case-insensitive partial match)
		if criteria.Subject != "" {
			if !strings.Contains(strings.ToLower(email.Subject), strings.ToLower(criteria.Subject)) {
				continue
			}
		}

		// Filter by sender (case-insensitive partial match)
		if criteria.From != "" {
			fromMatch := strings.Contains(strings.ToLower(email.From.Email), strings.ToLower(criteria.From)) ||
				strings.Contains(strings.ToLower(email.From.Name), strings.ToLower(criteria.From))
			if !fromMatch {
				continue
			}
		}

		// Filter by date range
		if criteria.DateFrom != nil && email.Date.Before(*criteria.DateFrom) {
			continue
		}
		if criteria.DateTo != nil && email.Date.After(*criteria.DateTo) {
			continue
		}

		// Filter by labels
		if len(criteria.Labels) > 0 {
			hasLabel := false
			for _, requiredLabel := range criteria.Labels {
				for _, emailLabel := range email.Labels {
					if strings.EqualFold(emailLabel, requiredLabel) {
						hasLabel = true
						break
					}
				}
				if hasLabel {
					break
				}
			}
			if !hasLabel {
				continue
			}
		}

		filtered = append(filtered, email)
	}

	return filtered
}

// sortEmails sorts emails based on the given criteria
func (s *EmailService) sortEmails(emails []*models.Email, sortBy string, desc bool) {
	sort.Slice(emails, func(i, j int) bool {
		var less bool

		switch strings.ToLower(sortBy) {
		case "date":
			less = emails[i].Date.Before(emails[j].Date)
		case "subject":
			less = strings.ToLower(emails[i].Subject) < strings.ToLower(emails[j].Subject)
		case "from":
			less = strings.ToLower(emails[i].From.Email) < strings.ToLower(emails[j].From.Email)
		case "status":
			less = string(emails[i].Status) < string(emails[j].Status)
		default:
			// Default to date sorting
			less = emails[i].Date.Before(emails[j].Date)
		}

		if desc {
			return !less
		}
		return less
	})
}

// SearchEmails searches for emails based on a query string
func (s *EmailService) SearchEmails(ctx context.Context, query string, criteria *ListCriteria) ([]*models.Email, error) {
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("search query cannot be empty")
	}

	// Get all emails first (or apply basic criteria)
	allEmails, err := s.repository.List(ctx, criteria)
	if err != nil {
		return nil, fmt.Errorf("failed to search emails: %w", err)
	}

	// Filter by search query
	query = strings.ToLower(strings.TrimSpace(query))
	filtered := make([]*models.Email, 0)

	for _, email := range allEmails {
		// Search in subject, body, sender name/email, and recipient emails
		if s.matchesQuery(email, query) {
			filtered = append(filtered, email)
		}
	}

	// Apply additional filters
	filtered = s.applyFilters(filtered, criteria)

	// Sort and paginate
	if criteria != nil {
		s.sortEmails(filtered, criteria.SortBy, criteria.SortDesc)

		// Apply pagination
		start := criteria.Offset
		end := start + criteria.Limit

		if start >= len(filtered) {
			return []*models.Email{}, nil
		}

		if end > len(filtered) {
			end = len(filtered)
		}

		return filtered[start:end], nil
	}

	return filtered, nil
}

// matchesQuery checks if an email matches the search query
func (s *EmailService) matchesQuery(email *models.Email, query string) bool {
	// Search in subject
	if strings.Contains(strings.ToLower(email.Subject), query) {
		return true
	}

	// Search in body
	if strings.Contains(strings.ToLower(email.Body), query) {
		return true
	}

	// Search in sender name and email
	if strings.Contains(strings.ToLower(email.From.Name), query) ||
		strings.Contains(strings.ToLower(email.From.Email), query) {
		return true
	}

	// Search in recipient emails
	for _, addr := range email.To {
		if strings.Contains(strings.ToLower(addr.Name), query) ||
			strings.Contains(strings.ToLower(addr.Email), query) {
			return true
		}
	}

	// Search in CC recipients
	for _, addr := range email.CC {
		if strings.Contains(strings.ToLower(addr.Name), query) ||
			strings.Contains(strings.ToLower(addr.Email), query) {
			return true
		}
	}

	// Search in labels
	for _, label := range email.Labels {
		if strings.Contains(strings.ToLower(label), query) {
			return true
		}
	}

	return false
}

// GetEmailStats returns statistics about emails
func (s *EmailService) GetEmailStats(ctx context.Context) (*EmailStats, error) {
	// For now, we'll get all emails to calculate stats
	// In a real implementation, this might be optimized with database queries
	allEmails, err := s.repository.List(ctx, &ListCriteria{
		Limit: -1, // Get all emails
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get email stats: %w", err)
	}

	stats := &EmailStats{
		Total:  len(allEmails),
		Unread: 0,
		Read:   0,
		Draft:  0,
		Sent:   0,
	}

	for _, email := range allEmails {
		switch email.Status {
		case models.StatusUnread:
			stats.Unread++
		case models.StatusRead:
			stats.Read++
		case models.StatusDraft:
			stats.Draft++
		case models.StatusSent:
			stats.Sent++
		}
	}

	return stats, nil
}

// EmailStats represents email statistics
type EmailStats struct {
	Total  int `json:"total"`
	Unread int `json:"unread"`
	Read   int `json:"read"`
	Draft  int `json:"draft"`
	Sent   int `json:"sent"`
}