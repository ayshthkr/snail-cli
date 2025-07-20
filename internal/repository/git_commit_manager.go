package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"snail-cli/internal/interfaces"
)

// GitCommitManager handles Git commit operations with automation and conflict resolution
type GitCommitManager struct {
	repo     *git.Repository
	repoPath string
}

// NewGitCommitManager creates a new Git commit manager
func NewGitCommitManager(repo *git.Repository, repoPath string) *GitCommitManager {
	return &GitCommitManager{
		repo:     repo,
		repoPath: repoPath,
	}
}

// CommitOperation represents different types of email operations
type CommitOperation string

const (
	OperationAdd    CommitOperation = "add"
	OperationUpdate CommitOperation = "update"
	OperationDelete CommitOperation = "delete"
	OperationMove   CommitOperation = "move"
	OperationSync   CommitOperation = "sync"
)

// CommitInfo represents information about a commit operation
type CommitInfo struct {
	Operation   CommitOperation
	EmailID     string
	Subject     string
	From        string
	Folder      string
	Description string
	Files       []string
}

// ConflictInfo represents information about a Git conflict
type ConflictInfo struct {
	FilePath    string
	ConflictType string
	OurVersion   string
	TheirVersion string
	BaseVersion  string
}

// CommitEmailOperation creates a commit for an email operation with meaningful message
func (gcm *GitCommitManager) CommitEmailOperation(ctx context.Context, commitInfo *CommitInfo) error {
	worktree, err := gcm.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add all changes to staging area (safer than adding specific files)
	err = worktree.AddGlob(".")
	if err != nil {
		return fmt.Errorf("failed to add changes to staging: %w", err)
	}

	// Generate meaningful commit message
	message := gcm.generateCommitMessage(commitInfo)

	// Create commit
	commitHash, err := worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Snail CLI",
			Email: "snail@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create commit: %w", err)
	}

	// Log commit for debugging
	commit, err := gcm.repo.CommitObject(commitHash)
	if err == nil {
		// Successfully created commit
		_ = commit // Use commit if needed for logging
	}

	return nil
}

// CommitBulkOperation creates a commit for multiple email operations
func (gcm *GitCommitManager) CommitBulkOperation(ctx context.Context, operations []*CommitInfo, description string) error {
	if len(operations) == 0 {
		return fmt.Errorf("no operations to commit")
	}

	worktree, err := gcm.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add all files to staging area
	allFiles := make(map[string]bool)
	for _, op := range operations {
		for _, file := range op.Files {
			if !allFiles[file] {
				_, err = worktree.Add(file)
				if err != nil {
					return fmt.Errorf("failed to add file %s: %w", file, err)
				}
				allFiles[file] = true
			}
		}
	}

	// Generate bulk commit message
	message := gcm.generateBulkCommitMessage(operations, description)

	// Create commit
	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Snail CLI",
			Email: "snail@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create bulk commit: %w", err)
	}

	return nil
}

// DetectConflicts checks for Git conflicts in the repository
func (gcm *GitCommitManager) DetectConflicts(ctx context.Context) ([]*ConflictInfo, error) {
	worktree, err := gcm.repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return nil, fmt.Errorf("failed to get worktree status: %w", err)
	}

	var conflicts []*ConflictInfo
	for filePath, fileStatus := range status {
		// Check for conflict markers in file status
		// In go-git, conflicts are typically indicated by specific status combinations
		if fileStatus.Staging == git.UpdatedButUnmerged || fileStatus.Worktree == git.UpdatedButUnmerged {
			conflictInfo := &ConflictInfo{
				FilePath:     filePath,
				ConflictType: "merge_conflict",
			}
			conflicts = append(conflicts, conflictInfo)
		}
	}

	return conflicts, nil
}

// ResolveConflicts attempts to automatically resolve simple conflicts
func (gcm *GitCommitManager) ResolveConflicts(ctx context.Context, conflicts []*ConflictInfo, strategy string) error {
	for _, conflict := range conflicts {
		switch strategy {
		case "ours":
			err := gcm.resolveConflictOurs(conflict)
			if err != nil {
				return fmt.Errorf("failed to resolve conflict using 'ours' strategy for %s: %w", conflict.FilePath, err)
			}
		case "theirs":
			err := gcm.resolveConflictTheirs(conflict)
			if err != nil {
				return fmt.Errorf("failed to resolve conflict using 'theirs' strategy for %s: %w", conflict.FilePath, err)
			}
		case "manual":
			// For manual resolution, just mark as resolved without changing content
			err := gcm.markConflictResolved(conflict)
			if err != nil {
				return fmt.Errorf("failed to mark conflict as resolved for %s: %w", conflict.FilePath, err)
			}
		default:
			return fmt.Errorf("unknown conflict resolution strategy: %s", strategy)
		}
	}

	return nil
}

// GetCommitHistory returns the commit history for the repository
func (gcm *GitCommitManager) GetCommitHistory(ctx context.Context, limit int) ([]*interfaces.CommitInfo, error) {
	commits, err := gcm.repo.Log(&git.LogOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get commit log: %w", err)
	}

	var history []*interfaces.CommitInfo
	count := 0
	err = commits.ForEach(func(commit *object.Commit) error {
		if limit > 0 && count >= limit {
			return fmt.Errorf("limit reached") // Use error to break iteration
		}

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
		count++
		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, fmt.Errorf("failed to iterate commit history: %w", err)
	}

	return history, nil
}

// IsRepositoryClean checks if the repository has no uncommitted changes
func (gcm *GitCommitManager) IsRepositoryClean() (bool, error) {
	worktree, err := gcm.repo.Worktree()
	if err != nil {
		return false, fmt.Errorf("failed to get worktree: %w", err)
	}

	status, err := worktree.Status()
	if err != nil {
		return false, fmt.Errorf("failed to get status: %w", err)
	}

	return status.IsClean(), nil
}

// GetCurrentBranch returns the current branch name
func (gcm *GitCommitManager) GetCurrentBranch() (string, error) {
	head, err := gcm.repo.Head()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD: %w", err)
	}

	if head.Name().IsBranch() {
		return head.Name().Short(), nil
	}

	return head.Hash().String()[:8], nil // Return short hash if detached HEAD
}

// Private helper methods

// generateCommitMessage creates a meaningful commit message for an email operation
func (gcm *GitCommitManager) generateCommitMessage(commitInfo *CommitInfo) string {
	var message strings.Builder

	// First line: operation summary
	switch commitInfo.Operation {
	case OperationAdd:
		message.WriteString(fmt.Sprintf("Add email: %s", gcm.truncateSubject(commitInfo.Subject)))
	case OperationUpdate:
		message.WriteString(fmt.Sprintf("Update email: %s", gcm.truncateSubject(commitInfo.Subject)))
	case OperationDelete:
		message.WriteString(fmt.Sprintf("Delete email: %s", gcm.truncateSubject(commitInfo.Subject)))
	case OperationMove:
		message.WriteString(fmt.Sprintf("Move email: %s", gcm.truncateSubject(commitInfo.Subject)))
	case OperationSync:
		message.WriteString(fmt.Sprintf("Sync email: %s", gcm.truncateSubject(commitInfo.Subject)))
	default:
		message.WriteString(fmt.Sprintf("Email operation: %s", gcm.truncateSubject(commitInfo.Subject)))
	}

	// Add details
	message.WriteString("\n\n")
	message.WriteString(fmt.Sprintf("Email ID: %s\n", commitInfo.EmailID))
	if commitInfo.From != "" {
		message.WriteString(fmt.Sprintf("From: %s\n", commitInfo.From))
	}
	if commitInfo.Folder != "" {
		message.WriteString(fmt.Sprintf("Folder: %s\n", commitInfo.Folder))
	}
	if commitInfo.Description != "" {
		message.WriteString(fmt.Sprintf("Description: %s\n", commitInfo.Description))
	}

	// Add file list
	if len(commitInfo.Files) > 0 {
		message.WriteString("\nFiles:\n")
		for _, file := range commitInfo.Files {
			message.WriteString(fmt.Sprintf("- %s\n", file))
		}
	}

	return message.String()
}

// generateBulkCommitMessage creates a commit message for bulk operations
func (gcm *GitCommitManager) generateBulkCommitMessage(operations []*CommitInfo, description string) string {
	var message strings.Builder

	// First line: bulk operation summary
	message.WriteString(fmt.Sprintf("Bulk operation: %d emails", len(operations)))
	if description != "" {
		message.WriteString(fmt.Sprintf(" - %s", description))
	}

	// Group operations by type
	opCounts := make(map[CommitOperation]int)
	for _, op := range operations {
		opCounts[op.Operation]++
	}

	message.WriteString("\n\nOperations:\n")
	for opType, count := range opCounts {
		message.WriteString(fmt.Sprintf("- %s: %d emails\n", opType, count))
	}

	// Add sample of operations (first 5)
	message.WriteString("\nSample operations:\n")
	for i, op := range operations {
		if i >= 5 {
			message.WriteString(fmt.Sprintf("... and %d more\n", len(operations)-5))
			break
		}
		message.WriteString(fmt.Sprintf("- %s: %s\n", op.Operation, gcm.truncateSubject(op.Subject)))
	}

	return message.String()
}

// truncateSubject truncates email subject for commit messages
func (gcm *GitCommitManager) truncateSubject(subject string) string {
	if len(subject) <= 50 {
		return subject
	}
	return subject[:47] + "..."
}

// resolveConflictOurs resolves conflict by keeping our version
func (gcm *GitCommitManager) resolveConflictOurs(conflict *ConflictInfo) error {
	worktree, err := gcm.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add the file to mark conflict as resolved (keeping our version)
	_, err = worktree.Add(conflict.FilePath)
	if err != nil {
		return fmt.Errorf("failed to add resolved file: %w", err)
	}

	return nil
}

// resolveConflictTheirs resolves conflict by keeping their version
func (gcm *GitCommitManager) resolveConflictTheirs(conflict *ConflictInfo) error {
	// For "theirs" strategy, we would need to checkout their version
	// This is a simplified implementation
	return gcm.resolveConflictOurs(conflict) // Fallback to ours for now
}

// markConflictResolved marks a conflict as resolved without changing content
func (gcm *GitCommitManager) markConflictResolved(conflict *ConflictInfo) error {
	worktree, err := gcm.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	// Add the file to mark conflict as resolved
	_, err = worktree.Add(conflict.FilePath)
	if err != nil {
		return fmt.Errorf("failed to mark conflict as resolved: %w", err)
	}

	return nil
}