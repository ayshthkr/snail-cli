package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"snail-cli/internal/interfaces"
)

// GitEmailRepository implements EmailRepository using Git as the storage backend
type GitEmailRepository struct {
	repoPath      string
	repo          *git.Repository
	fileManager   *EmailFileManager
	commitManager *GitCommitManager
}

// NewGitEmailRepository creates a new Git-based email repository
func NewGitEmailRepository() *GitEmailRepository {
	return &GitEmailRepository{}
}

// Initialize creates a new Git repository for email storage
func (r *GitEmailRepository) Initialize(path string) error {
	r.repoPath = path
	
	// Create directory if it doesn't exist
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}
	
	// Check if repository already exists
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		// Repository exists, open it
		repo, err := git.PlainOpen(path)
		if err != nil {
			return fmt.Errorf("failed to open existing repository: %w", err)
		}
		r.repo = repo
		r.fileManager = NewEmailFileManager(path)
		r.commitManager = NewGitCommitManager(repo, path)
		return nil
	}
	
	// Initialize new Git repository
	repo, err := git.PlainInit(path, false)
	if err != nil {
		return fmt.Errorf("failed to initialize Git repository: %w", err)
	}
	
	r.repo = repo
	r.fileManager = NewEmailFileManager(path)
	r.commitManager = NewGitCommitManager(repo, path)
	
	// Create initial directory structure
	dirs := []string{"inbox", "sent", "drafts", "archive", "filters", "indices"}
	for _, dir := range dirs {
		dirPath := filepath.Join(path, dir)
		if err := os.MkdirAll(dirPath, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		
		// Create .gitkeep file to ensure directory is tracked
		gitkeepPath := filepath.Join(dirPath, ".gitkeep")
		if err := os.WriteFile(gitkeepPath, []byte(""), 0644); err != nil {
			return fmt.Errorf("failed to create .gitkeep in %s: %w", dir, err)
		}
	}
	
	// Create initial commit
	worktree, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	
	// Add all files
	if err := worktree.AddGlob("."); err != nil {
		return fmt.Errorf("failed to add files to Git: %w", err)
	}
	
	// Create initial commit
	_, err = worktree.Commit("Initial repository setup", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Snail CLI",
			Email: "snail@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create initial commit: %w", err)
	}
	
	return nil
}

// Store saves an email to the repository as a plain-text file
func (r *GitEmailRepository) Store(ctx context.Context, email *interfaces.Email) error {
	if r.repo == nil {
		return fmt.Errorf("repository not initialized")
	}
	
	// Generate file path using file manager with collision handling
	filePath, err := r.fileManager.GenerateFilePath(email)
	if err != nil {
		return fmt.Errorf("failed to generate file path: %w", err)
	}
	
	fullPath := filepath.Join(r.repoPath, filePath)
	
	// Ensure directory structure exists
	if err := r.fileManager.CreateDirectoryStructure(email); err != nil {
		return fmt.Errorf("failed to create directory structure: %w", err)
	}
	
	// Convert email to file format
	content, err := r.emailToFileContent(email)
	if err != nil {
		return fmt.Errorf("failed to convert email to file content: %w", err)
	}
	
	// Write email file
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write email file: %w", err)
	}
	
	return nil
}

// StoreWithCommit saves an email and creates a meaningful commit
func (r *GitEmailRepository) StoreWithCommit(ctx context.Context, email *interfaces.Email) error {
	// Store the email first
	if err := r.Store(ctx, email); err != nil {
		return err
	}
	
	// Generate file path for commit info
	filePath, err := r.fileManager.GenerateFilePath(email)
	if err != nil {
		return fmt.Errorf("failed to generate file path for commit: %w", err)
	}
	
	// Create commit with meaningful message
	commitInfo := &CommitInfo{
		Operation: OperationAdd,
		EmailID:   email.ID,
		Subject:   email.Subject,
		From:      r.formatAddress(email.From),
		Folder:    r.determineFolderFromEmail(email),
		Files:     []string{filePath},
	}
	
	return r.commitManager.CommitEmailOperation(ctx, commitInfo)
}

// Get retrieves an email by ID
func (r *GitEmailRepository) Get(ctx context.Context, id string) (*interfaces.Email, error) {
	if r.repo == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	
	// Find email file by ID
	filePath, err := r.findEmailFile(id)
	if err != nil {
		return nil, fmt.Errorf("failed to find email file: %w", err)
	}
	
	// Read email file
	content, err := os.ReadFile(filepath.Join(r.repoPath, filePath))
	if err != nil {
		return nil, fmt.Errorf("failed to read email file: %w", err)
	}
	
	// Extract ID from filename
	filename := strings.TrimSuffix(filepath.Base(filePath), ".eml")
	
	// Parse email from file content
	email, err := r.parseEmailFromContentWithID(string(content), filename)
	if err != nil {
		return nil, fmt.Errorf("failed to parse email: %w", err)
	}
	
	return email, nil
}

// List returns emails matching the given criteria
func (r *GitEmailRepository) List(ctx context.Context, criteria interfaces.ListCriteria) ([]*interfaces.Email, error) {
	if r.repo == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	
	var emails []*interfaces.Email
	
	// Walk through email files
	searchPath := r.repoPath
	if criteria.Folder != "" {
		searchPath = filepath.Join(r.repoPath, criteria.Folder)
	}
	
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories and non-email files
		if info.IsDir() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		
		// Read and parse email
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		
		// Extract ID from filename
		filename := strings.TrimSuffix(filepath.Base(path), ".eml")
		
		email, err := r.parseEmailFromContentWithID(string(content), filename)
		if err != nil {
			return err
		}
		
		// Apply criteria filters
		if r.matchesCriteria(email, criteria) {
			emails = append(emails, email)
		}
		
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to list emails: %w", err)
	}
	
	// Apply limit and offset
	if criteria.Offset > 0 && criteria.Offset < len(emails) {
		emails = emails[criteria.Offset:]
	}
	if criteria.Limit > 0 && criteria.Limit < len(emails) {
		emails = emails[:criteria.Limit]
	}
	
	return emails, nil
}

// Delete removes an email from the repository
func (r *GitEmailRepository) Delete(ctx context.Context, id string) error {
	if r.repo == nil {
		return fmt.Errorf("repository not initialized")
	}
	
	// Find email file by ID
	filePath, err := r.findEmailFile(id)
	if err != nil {
		return fmt.Errorf("failed to find email file: %w", err)
	}
	
	// Remove file
	fullPath := filepath.Join(r.repoPath, filePath)
	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("failed to remove email file: %w", err)
	}
	
	return nil
}

// DeleteWithCommit removes an email and creates a meaningful commit
func (r *GitEmailRepository) DeleteWithCommit(ctx context.Context, id string) error {
	// Get email info before deletion for commit message
	email, err := r.Get(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get email for commit info: %w", err)
	}
	
	// Find file path before deletion
	filePath, err := r.findEmailFile(id)
	if err != nil {
		return fmt.Errorf("failed to find email file: %w", err)
	}
	
	// Delete the email
	if err := r.Delete(ctx, id); err != nil {
		return err
	}
	
	// Create commit with meaningful message
	commitInfo := &CommitInfo{
		Operation: OperationDelete,
		EmailID:   email.ID,
		Subject:   email.Subject,
		From:      r.formatAddress(email.From),
		Folder:    r.determineFolderFromEmail(email),
		Files:     []string{filePath},
	}
	
	return r.commitManager.CommitEmailOperation(ctx, commitInfo)
}

// Update modifies an existing email
func (r *GitEmailRepository) Update(ctx context.Context, email *interfaces.Email) error {
	// For updates, we simply store the email again (overwrite)
	return r.Store(ctx, email)
}

// UpdateWithCommit modifies an existing email and creates a meaningful commit
func (r *GitEmailRepository) UpdateWithCommit(ctx context.Context, email *interfaces.Email) error {
	// Update the email first
	if err := r.Update(ctx, email); err != nil {
		return err
	}
	
	// Generate file path for commit info
	filePath, err := r.fileManager.GenerateFilePath(email)
	if err != nil {
		return fmt.Errorf("failed to generate file path for commit: %w", err)
	}
	
	// Create commit with meaningful message
	commitInfo := &CommitInfo{
		Operation: OperationUpdate,
		EmailID:   email.ID,
		Subject:   email.Subject,
		From:      r.formatAddress(email.From),
		Folder:    r.determineFolderFromEmail(email),
		Files:     []string{filePath},
	}
	
	return r.commitManager.CommitEmailOperation(ctx, commitInfo)
}

// Commit creates a Git commit with the current changes
func (r *GitEmailRepository) Commit(ctx context.Context, message string) error {
	if r.repo == nil {
		return fmt.Errorf("repository not initialized")
	}
	
	worktree, err := r.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	
	// Add all changes
	if err := worktree.AddGlob("."); err != nil {
		return fmt.Errorf("failed to add changes to Git: %w", err)
	}
	
	// Create commit
	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Snail CLI",
			Email: "snail@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}
	
	return nil
}

// GetHistory returns the Git history for an email
func (r *GitEmailRepository) GetHistory(ctx context.Context, id string) ([]*interfaces.CommitInfo, error) {
	if r.repo == nil {
		return nil, fmt.Errorf("repository not initialized")
	}
	
	// Find email file by ID
	filePath, err := r.findEmailFile(id)
	if err != nil {
		return nil, fmt.Errorf("failed to find email file: %w", err)
	}
	
	// Get commit history for the file
	commits, err := r.repo.Log(&git.LogOptions{
		FileName: &filePath,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get commit history: %w", err)
	}
	
	var history []*interfaces.CommitInfo
	err = commits.ForEach(func(commit *object.Commit) error {
		commitInfo := &interfaces.CommitInfo{
			Hash:    commit.Hash.String(),
			Message: commit.Message,
			Author:  commit.Author.Name,
			Date:    commit.Author.When,
		}
		
		// Get file changes in this commit
		if commit.NumParents() > 0 {
			parent, err := commit.Parent(0)
			if err == nil {
				changes, err := commit.Patch(parent)
				if err == nil {
					commitInfo.Changes = []string{changes.String()}
				}
			}
		}
		
		history = append(history, commitInfo)
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to iterate commit history: %w", err)
	}
	
	return history, nil
}

// generateFilePath creates a file path for an email based on its metadata
func (r *GitEmailRepository) generateFilePath(email *interfaces.Email) string {
	// Use date-based directory structure: folder/year/month/
	year := email.Date.Format("2006")
	month := email.Date.Format("01")
	
	// Determine folder based on labels or default to inbox
	folder := "inbox"
	for _, label := range email.Labels {
		if label == "sent" || label == "drafts" || label == "archive" {
			folder = label
			break
		}
	}
	
	// Generate filename from email ID or message ID
	filename := email.ID
	if filename == "" {
		// Generate ID from message ID hash
		hash := sha256.Sum256([]byte(email.MessageID))
		filename = hex.EncodeToString(hash[:8])
	}
	
	return filepath.Join(folder, year, month, filename+".eml")
}

// emailToFileContent converts an email to plain-text file format
func (r *GitEmailRepository) emailToFileContent(email *interfaces.Email) (string, error) {
	var content strings.Builder
	
	// Write headers
	content.WriteString(fmt.Sprintf("From: %s\n", r.formatAddress(email.From)))
	
	if len(email.To) > 0 {
		var toAddrs []string
		for _, addr := range email.To {
			toAddrs = append(toAddrs, r.formatAddress(addr))
		}
		content.WriteString(fmt.Sprintf("To: %s\n", strings.Join(toAddrs, ", ")))
	}
	
	if len(email.CC) > 0 {
		var ccAddrs []string
		for _, addr := range email.CC {
			ccAddrs = append(ccAddrs, r.formatAddress(addr))
		}
		content.WriteString(fmt.Sprintf("CC: %s\n", strings.Join(ccAddrs, ", ")))
	}
	
	content.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))
	content.WriteString(fmt.Sprintf("Date: %s\n", email.Date.Format(time.RFC3339)))
	content.WriteString(fmt.Sprintf("Message-ID: %s\n", email.MessageID))
	
	// Write custom headers
	content.WriteString(fmt.Sprintf("X-Snail-Status: %s\n", email.Status))
	if len(email.Labels) > 0 {
		content.WriteString(fmt.Sprintf("X-Snail-Labels: %s\n", strings.Join(email.Labels, ",")))
	}
	if email.SyncID != "" {
		content.WriteString(fmt.Sprintf("X-Snail-Sync-ID: %s\n", email.SyncID))
	}
	
	// Write additional headers
	for key, value := range email.Headers {
		if !strings.HasPrefix(key, "X-Snail-") {
			content.WriteString(fmt.Sprintf("%s: %s\n", key, value))
		}
	}
	
	// Empty line before body
	content.WriteString("\n")
	
	// Write body
	content.WriteString(email.Body)
	
	return content.String(), nil
}

// formatAddress formats an email address for display
func (r *GitEmailRepository) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// findEmailFile finds the file path for an email by ID
func (r *GitEmailRepository) findEmailFile(id string) (string, error) {
	var foundPath string
	
	err := filepath.Walk(r.repoPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories and non-email files
		if info.IsDir() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		
		// Extract ID from filename (handling collision patterns)
		filename := filepath.Base(path)
		fileID, _, _ := parseFilename(filename)
		
		if fileID == id {
			relPath, err := filepath.Rel(r.repoPath, path)
			if err != nil {
				return err
			}
			foundPath = relPath
			return io.EOF // Stop walking
		}
		
		return nil
	})
	
	if err != nil && err != io.EOF {
		return "", err
	}
	
	if foundPath == "" {
		return "", fmt.Errorf("email with ID %s not found", id)
	}
	
	return foundPath, nil
}

// parseEmailFromContent parses an email from file content
func (r *GitEmailRepository) parseEmailFromContent(content string) (*interfaces.Email, error) {
	return r.parseEmailFromContentWithID(content, "")
}

// parseEmailFromContentWithID parses an email from file content with optional ID
func (r *GitEmailRepository) parseEmailFromContentWithID(content string, id string) (*interfaces.Email, error) {
	lines := strings.Split(content, "\n")
	email := &interfaces.Email{
		Headers: make(map[string]string),
	}
	
	// Parse headers
	headerEnd := 0
	for i, line := range lines {
		if line == "" {
			headerEnd = i
			break
		}
		
		// Parse header line
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			
			switch key {
			case "From":
				email.From = r.parseAddress(value)
			case "To":
				email.To = r.parseAddressList(value)
			case "CC":
				email.CC = r.parseAddressList(value)
			case "Subject":
				email.Subject = value
			case "Date":
				if date, err := time.Parse(time.RFC3339, value); err == nil {
					email.Date = date
				}
			case "Message-ID":
				email.MessageID = value
			case "X-Snail-Status":
				email.Status = interfaces.EmailStatus(value)
			case "X-Snail-Labels":
				if value != "" {
					email.Labels = strings.Split(value, ",")
				}
			case "X-Snail-Sync-ID":
				email.SyncID = value
			default:
				email.Headers[key] = value
			}
		}
	}
	
	// Parse body
	if headerEnd < len(lines)-1 {
		email.Body = strings.Join(lines[headerEnd+1:], "\n")
	}
	
	// Use provided ID or generate from MessageID hash
	if id != "" {
		email.ID = id
	} else if email.ID == "" {
		hash := sha256.Sum256([]byte(email.MessageID))
		email.ID = hex.EncodeToString(hash[:8])
	}
	
	return email, nil
}

// parseAddress parses a single email address
func (r *GitEmailRepository) parseAddress(addr string) interfaces.Address {
	addr = strings.TrimSpace(addr)
	
	// Check for "Name <email>" format
	if strings.Contains(addr, "<") && strings.Contains(addr, ">") {
		parts := strings.Split(addr, "<")
		if len(parts) == 2 {
			name := strings.TrimSpace(parts[0])
			email := strings.TrimSpace(strings.TrimSuffix(parts[1], ">"))
			return interfaces.Address{Name: name, Email: email}
		}
	}
	
	// Just email address
	return interfaces.Address{Email: addr}
}

// parseAddressList parses a comma-separated list of email addresses
func (r *GitEmailRepository) parseAddressList(addrs string) []interfaces.Address {
	var addresses []interfaces.Address
	
	// Simple split by comma (could be improved for complex cases)
	parts := strings.Split(addrs, ",")
	for _, part := range parts {
		if addr := r.parseAddress(part); addr.Email != "" {
			addresses = append(addresses, addr)
		}
	}
	
	return addresses
}

// matchesCriteria checks if an email matches the given criteria
func (r *GitEmailRepository) matchesCriteria(email *interfaces.Email, criteria interfaces.ListCriteria) bool {
	// Check status
	if criteria.Status != "" && email.Status != criteria.Status {
		return false
	}
	
	// Check labels
	if len(criteria.Labels) > 0 {
		hasLabel := false
		for _, criteriaLabel := range criteria.Labels {
			for _, emailLabel := range email.Labels {
				if emailLabel == criteriaLabel {
					hasLabel = true
					break
				}
			}
			if hasLabel {
				break
			}
		}
		if !hasLabel {
			return false
		}
	}
	
	// Check from address
	if criteria.From != "" && !strings.Contains(strings.ToLower(email.From.Email), strings.ToLower(criteria.From)) {
		return false
	}
	
	// Check subject
	if criteria.Subject != "" && !strings.Contains(strings.ToLower(email.Subject), strings.ToLower(criteria.Subject)) {
		return false
	}
	
	// Check date range
	if criteria.DateFrom != nil && email.Date.Before(*criteria.DateFrom) {
		return false
	}
	if criteria.DateTo != nil && email.Date.After(*criteria.DateTo) {
		return false
	}
	
	return true
}

// determineFolderFromEmail determines the folder based on email labels and status
func (r *GitEmailRepository) determineFolderFromEmail(email *interfaces.Email) string {
	// Check labels first
	for _, label := range email.Labels {
		switch label {
		case "sent":
			return "sent"
		case "drafts":
			return "drafts"
		case "archive":
			return "archive"
		}
	}
	
	// Check status
	switch email.Status {
	case interfaces.StatusDraft:
		return "drafts"
	case interfaces.StatusSent:
		return "sent"
	default:
		return "inbox"
	}
}

// GetRepo returns the underlying Git repository (for testing purposes)
func (r *GitEmailRepository) GetRepo() *git.Repository {
	return r.repo
}

// GetCommitManager returns the commit manager (for testing purposes)
func (r *GitEmailRepository) GetCommitManager() *GitCommitManager {
	return r.commitManager
}

// parseFilename parses a filename to extract ID and collision information
func parseFilename(filename string) (id string, isCollision bool, collisionNum int) {
	// Remove .eml extension
	baseFilename := strings.TrimSuffix(filename, ".eml")
	
	// Check for collision pattern (ends with -NNN)
	if strings.Contains(baseFilename, "-") {
		parts := strings.Split(baseFilename, "-")
		if len(parts) >= 2 {
			lastPart := parts[len(parts)-1]
			// Check if last part is a 3-digit number
			if len(lastPart) == 3 {
				var num int
				if n, err := fmt.Sscanf(lastPart, "%03d", &num); n == 1 && err == nil {
					// This is a collision filename
					id = strings.Join(parts[:len(parts)-1], "-")
					isCollision = true
					collisionNum = num
					return
				}
			}
		}
	}
	
	// No collision
	id = baseFilename
	isCollision = false
	collisionNum = 0
	return
}