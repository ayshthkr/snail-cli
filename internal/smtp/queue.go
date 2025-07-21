package smtp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
)

// FileQueue implements SMTPQueue using file-based storage
type FileQueue struct {
	queueDir string
	mu       sync.RWMutex
}

// NewFileQueue creates a new file-based email queue
func NewFileQueue(queueDir string) *FileQueue {
	return &FileQueue{
		queueDir: queueDir,
	}
}

// Enqueue adds an email to the send queue
func (q *FileQueue) Enqueue(ctx context.Context, email *interfaces.OutgoingEmail) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Ensure queue directory exists
	if err := os.MkdirAll(q.queueDir, 0755); err != nil {
		return fmt.Errorf("failed to create queue directory: %w", err)
	}

	// Generate unique filename based on timestamp and email ID
	timestamp := time.Now().UnixNano()
	filename := fmt.Sprintf("%d_%s.json", timestamp, email.ID)
	if email.ID == "" {
		filename = fmt.Sprintf("%d_email.json", timestamp)
	}

	filepath := filepath.Join(q.queueDir, filename)

	// Create queue entry
	queueEntry := &QueueEntry{
		Email:     email,
		QueuedAt:  time.Now(),
		Attempts:  0,
		LastError: "",
		NextRetry: time.Now(),
	}

	// Marshal to JSON
	data, err := json.MarshalIndent(queueEntry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal queue entry: %w", err)
	}

	// Write to file
	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write queue entry: %w", err)
	}

	return nil
}

// Dequeue retrieves the next email from the queue
func (q *FileQueue) Dequeue(ctx context.Context) (*interfaces.OutgoingEmail, error) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Get next available email
	entry, filename, err := q.getNextEntry()
	if err != nil {
		return nil, err
	}

	if entry == nil {
		return nil, nil // Queue is empty
	}

	// Remove from queue
	filepath := filepath.Join(q.queueDir, filename)
	if err := os.Remove(filepath); err != nil {
		return nil, fmt.Errorf("failed to remove queue entry: %w", err)
	}

	return entry.Email, nil
}

// Peek returns the next email without removing it from the queue
func (q *FileQueue) Peek(ctx context.Context) (*interfaces.OutgoingEmail, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	entry, _, err := q.getNextEntry()
	if err != nil {
		return nil, err
	}

	if entry == nil {
		return nil, nil // Queue is empty
	}

	return entry.Email, nil
}

// Remove removes a specific email from the queue
func (q *FileQueue) Remove(ctx context.Context, emailID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Find and remove the email with matching ID
	files, err := os.ReadDir(q.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Queue directory doesn't exist, nothing to remove
		}
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(q.queueDir, file.Name())
		entry, err := q.loadQueueEntry(filepath)
		if err != nil {
			continue // Skip corrupted entries
		}

		if entry.Email.ID == emailID {
			if err := os.Remove(filepath); err != nil {
				return fmt.Errorf("failed to remove queue entry: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("email with ID %s not found in queue", emailID)
}

// List returns all emails in the queue
func (q *FileQueue) List(ctx context.Context) ([]*interfaces.OutgoingEmail, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	files, err := os.ReadDir(q.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*interfaces.OutgoingEmail{}, nil // Queue directory doesn't exist
		}
		return nil, fmt.Errorf("failed to read queue directory: %w", err)
	}

	var emails []*interfaces.OutgoingEmail
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(q.queueDir, file.Name())
		entry, err := q.loadQueueEntry(filepath)
		if err != nil {
			continue // Skip corrupted entries
		}

		emails = append(emails, entry.Email)
	}

	return emails, nil
}

// Size returns the number of emails in the queue
func (q *FileQueue) Size(ctx context.Context) (int, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	files, err := os.ReadDir(q.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // Queue directory doesn't exist
		}
		return 0, fmt.Errorf("failed to read queue directory: %w", err)
	}

	count := 0
	for _, file := range files {
		if !file.IsDir() {
			count++
		}
	}

	return count, nil
}

// Clear removes all emails from the queue
func (q *FileQueue) Clear(ctx context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := os.ReadDir(q.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Queue directory doesn't exist, nothing to clear
		}
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(q.queueDir, file.Name())
		if err := os.Remove(filepath); err != nil {
			return fmt.Errorf("failed to remove queue entry %s: %w", file.Name(), err)
		}
	}

	return nil
}

// ProcessQueue attempts to send all queued emails
func (q *FileQueue) ProcessQueue(ctx context.Context, client interfaces.SMTPClient) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := os.ReadDir(q.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // Queue directory doesn't exist, nothing to process
		}
		return fmt.Errorf("failed to read queue directory: %w", err)
	}

	// Sort files by timestamp (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	var lastError error
	processed := 0
	failed := 0

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(q.queueDir, file.Name())
		entry, err := q.loadQueueEntry(filepath)
		if err != nil {
			// Skip corrupted entries but don't fail the entire process
			continue
		}

		// Check if it's time to retry
		if time.Now().Before(entry.NextRetry) {
			continue
		}

		// Try to send the email
		err = client.SendEmail(ctx, entry.Email.Email)
		if err != nil {
			// Update retry information
			entry.Attempts++
			entry.LastError = err.Error()
			entry.NextRetry = q.calculateNextRetry(entry.Attempts)

			// Save updated entry back to file
			if saveErr := q.saveQueueEntry(filepath, entry); saveErr != nil {
				lastError = fmt.Errorf("failed to update queue entry: %w", saveErr)
			} else {
				lastError = err
			}
			failed++
			continue
		}

		// Email sent successfully, remove from queue
		if err := os.Remove(filepath); err != nil {
			lastError = fmt.Errorf("failed to remove sent email from queue: %w", err)
		}
		processed++
	}

	if processed == 0 && failed > 0 && lastError != nil {
		return fmt.Errorf("failed to process queue: %w", lastError)
	}

	return nil
}

// getNextEntry returns the next queue entry to process (must be called with lock held)
func (q *FileQueue) getNextEntry() (*QueueEntry, string, error) {
	files, err := os.ReadDir(q.queueDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", nil // Queue directory doesn't exist
		}
		return nil, "", fmt.Errorf("failed to read queue directory: %w", err)
	}

	// Sort files by timestamp (oldest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	// Find the first entry that's ready to be processed
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filepath := filepath.Join(q.queueDir, file.Name())
		entry, err := q.loadQueueEntry(filepath)
		if err != nil {
			continue // Skip corrupted entries
		}

		// Check if it's time to retry
		if time.Now().Before(entry.NextRetry) {
			continue
		}

		return entry, file.Name(), nil
	}

	return nil, "", nil // No entries ready for processing
}

// loadQueueEntry loads a queue entry from file
func (q *FileQueue) loadQueueEntry(filepath string) (*QueueEntry, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read queue entry: %w", err)
	}

	var entry QueueEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("failed to unmarshal queue entry: %w", err)
	}

	return &entry, nil
}

// saveQueueEntry saves a queue entry to file
func (q *FileQueue) saveQueueEntry(filepath string, entry *QueueEntry) error {
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal queue entry: %w", err)
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write queue entry: %w", err)
	}

	return nil
}

// calculateNextRetry calculates the next retry time using exponential backoff
func (q *FileQueue) calculateNextRetry(attempts int) time.Time {
	// Exponential backoff: 1min, 2min, 4min, 8min, 16min, then 30min max
	delay := time.Duration(1<<uint(attempts-1)) * time.Minute
	if delay > 30*time.Minute {
		delay = 30 * time.Minute
	}
	return time.Now().Add(delay)
}

// QueueEntry represents an email in the queue with metadata
type QueueEntry struct {
	Email     *interfaces.OutgoingEmail `json:"email"`
	QueuedAt  time.Time                 `json:"queued_at"`
	Attempts  int                       `json:"attempts"`
	LastError string                    `json:"last_error"`
	NextRetry time.Time                 `json:"next_retry"`
}

// Sender combines SMTP client and queue management
type Sender struct {
	client interfaces.SMTPClient
	queue  interfaces.SMTPQueue
	mu     sync.RWMutex
}

// NewSender creates a new SMTP sender with queue management
func NewSender(client interfaces.SMTPClient, queue interfaces.SMTPQueue) *Sender {
	return &Sender{
		client: client,
		queue:  queue,
	}
}

// SendEmail sends an email immediately or queues it if offline
func (s *Sender) SendEmail(ctx context.Context, email *interfaces.Email) error {
	outgoingEmail := &interfaces.OutgoingEmail{
		Email: email,
	}
	return s.SendEmailWithOptions(ctx, outgoingEmail)
}

// SendEmailWithOptions sends an email with additional options
func (s *Sender) SendEmailWithOptions(ctx context.Context, email *interfaces.OutgoingEmail) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Try to send immediately if connected
	if s.client.IsConnected() {
		err := s.client.SendEmail(ctx, email.Email)
		if err == nil {
			return nil // Email sent successfully
		}
		// If sending fails, fall through to queue the email
	}

	// Queue the email for later delivery
	return s.queue.Enqueue(ctx, email)
}

// ProcessOfflineQueue processes all queued emails
func (s *Sender) ProcessOfflineQueue(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.client.IsConnected() {
		return fmt.Errorf("SMTP client is not connected")
	}

	return s.queue.ProcessQueue(ctx, s.client)
}

// GetQueueSize returns the number of queued emails
func (s *Sender) GetQueueSize(ctx context.Context) (int, error) {
	return s.queue.Size(ctx)
}

// IsOnline returns true if SMTP server is reachable
func (s *Sender) IsOnline(ctx context.Context) bool {
	return s.client.IsConnected()
}

// GetQueuedEmails returns all queued emails
func (s *Sender) GetQueuedEmails(ctx context.Context) ([]*interfaces.OutgoingEmail, error) {
	return s.queue.List(ctx)
}

// RemoveFromQueue removes a specific email from the queue
func (s *Sender) RemoveFromQueue(ctx context.Context, emailID string) error {
	return s.queue.Remove(ctx, emailID)
}

// ClearQueue removes all emails from the queue
func (s *Sender) ClearQueue(ctx context.Context) error {
	return s.queue.Clear(ctx)
}