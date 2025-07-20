package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"snail-cli/internal/interfaces"
)

// EmailFileManager handles email-to-file conversion and file management
type EmailFileManager struct {
	repoPath string
}

// NewEmailFileManager creates a new email file manager
func NewEmailFileManager(repoPath string) *EmailFileManager {
	return &EmailFileManager{
		repoPath: repoPath,
	}
}

// EmailFileInfo represents metadata about an email file
type EmailFileInfo struct {
	Path         string
	ID           string
	Folder       string
	Year         string
	Month        string
	Day          string
	Filename     string
	Size         int64
	ModTime      time.Time
	IsCollision  bool
	CollisionNum int
}

// GenerateFilePath creates a file path for an email with collision handling
func (efm *EmailFileManager) GenerateFilePath(email *interfaces.Email) (string, error) {
	// Determine folder based on labels or status
	folder := efm.determineFolderFromEmail(email)
	
	// Create date-based directory structure
	year := email.Date.Format("2006")
	month := email.Date.Format("01")
	day := email.Date.Format("02")
	
	// Generate base filename
	baseFilename := efm.generateBaseFilename(email)
	
	// Handle collisions
	filename, err := efm.handleFileCollision(folder, year, month, day, baseFilename)
	if err != nil {
		return "", fmt.Errorf("failed to handle file collision: %w", err)
	}
	
	return filepath.Join(folder, year, month, day, filename), nil
}

// ExtractMetadata extracts metadata from an email file
func (efm *EmailFileManager) ExtractMetadata(filePath string) (*EmailFileInfo, error) {
	fullPath := filepath.Join(efm.repoPath, filePath)
	
	// Get file info
	fileInfo, err := os.Stat(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}
	
	// Parse path components
	pathParts := strings.Split(filePath, string(filepath.Separator))
	if len(pathParts) < 4 {
		return nil, fmt.Errorf("invalid email file path structure: %s", filePath)
	}
	
	folder := pathParts[0]
	year := pathParts[1]
	month := pathParts[2]
	day := pathParts[3]
	filename := pathParts[4]
	
	// Extract ID and collision info from filename
	id, isCollision, collisionNum := efm.parseFilename(filename)
	
	return &EmailFileInfo{
		Path:         filePath,
		ID:           id,
		Folder:       folder,
		Year:         year,
		Month:        month,
		Day:          day,
		Filename:     filename,
		Size:         fileInfo.Size(),
		ModTime:      fileInfo.ModTime(),
		IsCollision:  isCollision,
		CollisionNum: collisionNum,
	}, nil
}

// CreateDirectoryStructure ensures the directory structure exists for an email
func (efm *EmailFileManager) CreateDirectoryStructure(email *interfaces.Email) error {
	folder := efm.determineFolderFromEmail(email)
	year := email.Date.Format("2006")
	month := email.Date.Format("01")
	day := email.Date.Format("02")
	
	dirPath := filepath.Join(efm.repoPath, folder, year, month, day)
	return os.MkdirAll(dirPath, 0755)
}

// ValidateEmailFile validates that an email file has the correct structure
func (efm *EmailFileManager) ValidateEmailFile(filePath string) error {
	fullPath := filepath.Join(efm.repoPath, filePath)
	
	// Check if file exists
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return fmt.Errorf("email file does not exist: %s", filePath)
	}
	
	// Validate path structure
	if !efm.isValidEmailPath(filePath) {
		return fmt.Errorf("invalid email file path structure: %s", filePath)
	}
	
	// Validate file extension
	if !strings.HasSuffix(filePath, ".eml") {
		return fmt.Errorf("email file must have .eml extension: %s", filePath)
	}
	
	return nil
}

// ListEmailFiles returns all email files in the repository
func (efm *EmailFileManager) ListEmailFiles(folder string) ([]*EmailFileInfo, error) {
	var emailFiles []*EmailFileInfo
	
	searchPath := efm.repoPath
	if folder != "" {
		searchPath = filepath.Join(efm.repoPath, folder)
	}
	
	err := filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip directories and non-email files
		if info.IsDir() || !strings.HasSuffix(path, ".eml") {
			return nil
		}
		
		// Get relative path
		relPath, err := filepath.Rel(efm.repoPath, path)
		if err != nil {
			return err
		}
		
		// Extract metadata
		emailFileInfo, err := efm.ExtractMetadata(relPath)
		if err != nil {
			return err
		}
		
		emailFiles = append(emailFiles, emailFileInfo)
		return nil
	})
	
	if err != nil {
		return nil, fmt.Errorf("failed to list email files: %w", err)
	}
	
	return emailFiles, nil
}

// CleanupEmptyDirectories removes empty directories in the email structure
func (efm *EmailFileManager) CleanupEmptyDirectories() error {
	folders := []string{"inbox", "sent", "drafts", "archive"}
	
	for _, folder := range folders {
		folderPath := filepath.Join(efm.repoPath, folder)
		if _, err := os.Stat(folderPath); os.IsNotExist(err) {
			continue
		}
		
		err := efm.removeEmptyDirs(folderPath)
		if err != nil {
			return fmt.Errorf("failed to cleanup empty directories in %s: %w", folder, err)
		}
	}
	
	return nil
}

// Private helper methods

// determineFolderFromEmail determines the folder based on email labels and status
func (efm *EmailFileManager) determineFolderFromEmail(email *interfaces.Email) string {
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

// generateBaseFilename generates a base filename for an email
func (efm *EmailFileManager) generateBaseFilename(email *interfaces.Email) string {
	// Use provided ID if available
	if email.ID != "" {
		return email.ID + ".eml"
	}
	
	// Generate from MessageID
	if email.MessageID != "" {
		hash := sha256.Sum256([]byte(email.MessageID))
		return hex.EncodeToString(hash[:8]) + ".eml"
	}
	
	// Generate from subject and date as fallback
	subject := strings.ReplaceAll(email.Subject, " ", "-")
	subject = regexp.MustCompile(`[^a-zA-Z0-9\-]`).ReplaceAllString(subject, "")
	if len(subject) > 30 {
		subject = subject[:30]
	}
	
	timestamp := email.Date.Format("20060102-150405")
	return fmt.Sprintf("%s-%s.eml", subject, timestamp)
}

// handleFileCollision handles filename collisions by appending a number
func (efm *EmailFileManager) handleFileCollision(folder, year, month, day, filename string) (string, error) {
	basePath := filepath.Join(efm.repoPath, folder, year, month, day)
	fullPath := filepath.Join(basePath, filename)
	
	// If file doesn't exist, no collision
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		return filename, nil
	}
	
	// Handle collision by appending number
	baseFilename := strings.TrimSuffix(filename, ".eml")
	for i := 1; i <= 999; i++ {
		collisionFilename := fmt.Sprintf("%s-%03d.eml", baseFilename, i)
		collisionPath := filepath.Join(basePath, collisionFilename)
		
		if _, err := os.Stat(collisionPath); os.IsNotExist(err) {
			return collisionFilename, nil
		}
	}
	
	return "", fmt.Errorf("too many file collisions for %s", filename)
}

// parseFilename parses a filename to extract ID and collision information
func (efm *EmailFileManager) parseFilename(filename string) (id string, isCollision bool, collisionNum int) {
	// Remove .eml extension
	baseFilename := strings.TrimSuffix(filename, ".eml")
	
	// Check for collision pattern (ends with -NNN)
	collisionPattern := regexp.MustCompile(`^(.+)-(\d{3})$`)
	if matches := collisionPattern.FindStringSubmatch(baseFilename); len(matches) == 3 {
		id = matches[1]
		isCollision = true
		// Parse collision number (ignore error, default to 0)
		fmt.Sscanf(matches[2], "%d", &collisionNum)
		return
	}
	
	// No collision
	id = baseFilename
	isCollision = false
	collisionNum = 0
	return
}

// isValidEmailPath validates the email file path structure
func (efm *EmailFileManager) isValidEmailPath(filePath string) bool {
	// Expected pattern: folder/year/month/day/filename.eml
	pathPattern := regexp.MustCompile(`^(inbox|sent|drafts|archive)/\d{4}/\d{2}/\d{2}/.+\.eml$`)
	return pathPattern.MatchString(filePath)
}

// removeEmptyDirs recursively removes empty directories
func (efm *EmailFileManager) removeEmptyDirs(dirPath string) error {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}
	
	// Recursively process subdirectories
	for _, entry := range entries {
		if entry.IsDir() {
			subDirPath := filepath.Join(dirPath, entry.Name())
			if err := efm.removeEmptyDirs(subDirPath); err != nil {
				return err
			}
		}
	}
	
	// Check if directory is empty (ignoring .gitkeep files)
	entries, err = os.ReadDir(dirPath)
	if err != nil {
		return err
	}
	
	isEmpty := true
	hasGitkeep := false
	for _, entry := range entries {
		if entry.Name() == ".gitkeep" {
			hasGitkeep = true
		} else {
			isEmpty = false
			break
		}
	}
	
	// Remove empty directory (but keep root folders and directories with .gitkeep)
	if isEmpty && !hasGitkeep && !efm.isRootFolder(dirPath) {
		return os.Remove(dirPath)
	}
	
	return nil
}

// isRootFolder checks if a directory is a root email folder
func (efm *EmailFileManager) isRootFolder(dirPath string) bool {
	relPath, err := filepath.Rel(efm.repoPath, dirPath)
	if err != nil {
		return false
	}
	
	rootFolders := []string{"inbox", "sent", "drafts", "archive", "filters", "indices"}
	for _, folder := range rootFolders {
		if relPath == folder {
			return true
		}
	}
	
	return false
}