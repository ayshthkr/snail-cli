package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/filesystem"
)

// GitOptimizer handles Git repository optimization for large email volumes
type GitOptimizer struct {
	repo         *git.Repository
	repoPath     string
	config       *OptimizationConfig
	lastGC       time.Time
	lastRepack   time.Time
	mu           sync.RWMutex
}

// OptimizationConfig contains configuration for Git optimization
type OptimizationConfig struct {
	// Garbage collection settings
	AutoGCThreshold     int           // Number of loose objects before auto GC
	GCInterval          time.Duration // Minimum interval between GC runs
	AggressiveGC        bool          // Use aggressive garbage collection
	
	// Compression settings
	CompressionLevel    int           // Git compression level (0-9)
	DeltaCompressionMax int           // Maximum delta compression depth
	
	// Pack file settings
	PackSizeLimit       int64         // Maximum pack file size in bytes
	AutoRepack          bool          // Enable automatic repacking
	RepackInterval      time.Duration // Minimum interval between repacks
	
	// Performance settings
	BatchSize           int           // Batch size for bulk operations
	ParallelWorkers     int           // Number of parallel workers
	MemoryLimit         int64         // Memory limit for Git operations
	
	// Maintenance settings
	EnableAutoMaintenance bool          // Enable automatic maintenance
	MaintenanceInterval   time.Duration // Interval for maintenance tasks
}

// NewGitOptimizer creates a new Git optimizer
func NewGitOptimizer(repo *git.Repository, repoPath string) *GitOptimizer {
	return &GitOptimizer{
		repo:     repo,
		repoPath: repoPath,
		config:   DefaultOptimizationConfig(),
	}
}

// DefaultOptimizationConfig returns default optimization configuration
func DefaultOptimizationConfig() *OptimizationConfig {
	return &OptimizationConfig{
		AutoGCThreshold:       1000,
		GCInterval:            24 * time.Hour,
		AggressiveGC:          false,
		CompressionLevel:      6,
		DeltaCompressionMax:   50,
		PackSizeLimit:         2 * 1024 * 1024 * 1024, // 2GB
		AutoRepack:            true,
		RepackInterval:        7 * 24 * time.Hour, // Weekly
		BatchSize:             100,
		ParallelWorkers:       4,
		MemoryLimit:           512 * 1024 * 1024, // 512MB
		EnableAutoMaintenance: true,
		MaintenanceInterval:   24 * time.Hour,
	}
}

// SetConfig updates the optimization configuration
func (go *GitOptimizer) SetConfig(config *OptimizationConfig) {
	go.mu.Lock()
	defer go.mu.Unlock()
	go.config = config
}

// OptimizeRepository performs comprehensive repository optimization
func (go *GitOptimizer) OptimizeRepository(ctx context.Context) error {
	go.mu.Lock()
	defer go.mu.Unlock()
	
	// Check if optimization is needed
	stats, err := go.getRepositoryStats()
	if err != nil {
		return fmt.Errorf("failed to get repository stats: %w", err)
	}
	
	optimizationNeeded := go.shouldOptimize(stats)
	if !optimizationNeeded {
		return nil // No optimization needed
	}
	
	// Perform optimization steps
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"Garbage Collection", go.performGarbageCollection},
		{"Repack Objects", go.repackObjects},
		{"Optimize References", go.optimizeReferences},
		{"Compress Loose Objects", go.compressLooseObjects},
		{"Update Configuration", go.updateGitConfig},
	}
	
	for _, step := range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := step.fn(ctx); err != nil {
				return fmt.Errorf("optimization step '%s' failed: %w", step.name, err)
			}
		}
	}
	
	return nil
}

// PerformGarbageCollection runs Git garbage collection
func (go *GitOptimizer) PerformGarbageCollection(ctx context.Context) error {
	go.mu.Lock()
	defer go.mu.Unlock()
	
	return go.performGarbageCollection(ctx)
}

// performGarbageCollection internal implementation
func (go *GitOptimizer) performGarbageCollection(ctx context.Context) error {
	// Check if GC is needed based on time interval
	if time.Since(go.lastGC) < go.config.GCInterval {
		return nil
	}
	
	// Use git command for more comprehensive GC
	args := []string{"gc"}
	if go.config.AggressiveGC {
		args = append(args, "--aggressive")
	}
	args = append(args, "--prune=now")
	
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = go.repoPath
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git gc failed: %w, output: %s", err, string(output))
	}
	
	go.lastGC = time.Now()
	return nil
}

// RepackObjects repacks Git objects for better compression
func (go *GitOptimizer) RepackObjects(ctx context.Context) error {
	go.mu.Lock()
	defer go.mu.Unlock()
	
	return go.repackObjects(ctx)
}

// repackObjects internal implementation
func (go *GitOptimizer) repackObjects(ctx context.Context) error {
	// Check if repack is needed based on time interval
	if time.Since(go.lastRepack) < go.config.RepackInterval {
		return nil
	}
	
	// Use git repack for better compression
	args := []string{"repack", "-a", "-d", "-f"}
	if go.config.CompressionLevel > 0 {
		args = append(args, fmt.Sprintf("--window=%d", go.config.DeltaCompressionMax))
	}
	
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = go.repoPath
	
	// Set memory limit
	if go.config.MemoryLimit > 0 {
		cmd.Env = append(os.Environ(), fmt.Sprintf("GIT_CONFIG_PARAMETERS='pack.windowMemory=%d'", go.config.MemoryLimit))
	}
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git repack failed: %w, output: %s", err, string(output))
	}
	
	go.lastRepack = time.Now()
	return nil
}

// optimizeReferences optimizes Git references
func (go *GitOptimizer) optimizeReferences(ctx context.Context) error {
	// Pack references for better performance
	cmd := exec.CommandContext(ctx, "git", "pack-refs", "--all", "--prune")
	cmd.Dir = go.repoPath
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git pack-refs failed: %w, output: %s", err, string(output))
	}
	
	return nil
}

// compressLooseObjects compresses loose objects
func (go *GitOptimizer) compressLooseObjects(ctx context.Context) error {
	// Find and compress loose objects
	cmd := exec.CommandContext(ctx, "git", "prune-packed")
	cmd.Dir = go.repoPath
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git prune-packed failed: %w, output: %s", err, string(output))
	}
	
	return nil
}

// updateGitConfig updates Git configuration for optimal performance
func (go *GitOptimizer) updateGitConfig(ctx context.Context) error {
	configs := map[string]string{
		"core.preloadindex":        "true",
		"core.fscache":            "true",
		"gc.auto":                 fmt.Sprintf("%d", go.config.AutoGCThreshold),
		"pack.compression":        fmt.Sprintf("%d", go.config.CompressionLevel),
		"pack.deltaCacheSize":     fmt.Sprintf("%d", go.config.MemoryLimit/4),
		"pack.packSizeLimit":      fmt.Sprintf("%d", go.config.PackSizeLimit),
		"pack.window":             fmt.Sprintf("%d", go.config.DeltaCompressionMax),
		"pack.threads":            fmt.Sprintf("%d", go.config.ParallelWorkers),
		"receive.fsckObjects":     "false", // Disable for performance
		"transfer.fsckObjects":    "false", // Disable for performance
	}
	
	for key, value := range configs {
		cmd := exec.CommandContext(ctx, "git", "config", key, value)
		cmd.Dir = go.repoPath
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set git config %s=%s: %w", key, value, err)
		}
	}
	
	return nil
}

// BatchCommitEmails commits multiple emails in batches for better performance
func (go *GitOptimizer) BatchCommitEmails(ctx context.Context, emails []EmailCommitInfo) error {
	if len(emails) == 0 {
		return nil
	}
	
	// Process emails in batches
	batchSize := go.config.BatchSize
	for i := 0; i < len(emails); i += batchSize {
		end := i + batchSize
		if end > len(emails) {
			end = len(emails)
		}
		
		batch := emails[i:end]
		if err := go.commitEmailBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to commit batch %d-%d: %w", i, end-1, err)
		}
		
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	
	return nil
}

// EmailCommitInfo represents information for committing an email
type EmailCommitInfo struct {
	FilePath  string
	Content   string
	Operation string
	Subject   string
	From      string
}

// commitEmailBatch commits a batch of emails
func (go *GitOptimizer) commitEmailBatch(ctx context.Context, batch []EmailCommitInfo) error {
	worktree, err := go.repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}
	
	// Write all files in the batch
	for _, email := range batch {
		fullPath := filepath.Join(go.repoPath, email.FilePath)
		
		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			return fmt.Errorf("failed to create directory for %s: %w", email.FilePath, err)
		}
		
		// Write file
		if err := os.WriteFile(fullPath, []byte(email.Content), 0644); err != nil {
			return fmt.Errorf("failed to write file %s: %w", email.FilePath, err)
		}
		
		// Add to Git
		if _, err := worktree.Add(email.FilePath); err != nil {
			return fmt.Errorf("failed to add file %s to Git: %w", email.FilePath, err)
		}
	}
	
	// Create batch commit message
	message := go.generateBatchCommitMessage(batch)
	
	// Commit all files in the batch
	_, err = worktree.Commit(message, &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Snail CLI",
			Email: "snail@localhost",
			When:  time.Now(),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create batch commit: %w", err)
	}
	
	return nil
}

// generateBatchCommitMessage generates a commit message for a batch of emails
func (go *GitOptimizer) generateBatchCommitMessage(batch []EmailCommitInfo) string {
	if len(batch) == 1 {
		email := batch[0]
		return fmt.Sprintf("%s email: %s", strings.Title(email.Operation), go.truncateSubject(email.Subject))
	}
	
	// Group by operation
	opCounts := make(map[string]int)
	for _, email := range batch {
		opCounts[email.Operation]++
	}
	
	var parts []string
	for op, count := range opCounts {
		parts = append(parts, fmt.Sprintf("%s: %d", op, count))
	}
	
	return fmt.Sprintf("Batch operation (%s)", strings.Join(parts, ", "))
}

// GetRepositoryStats returns statistics about the repository
func (go *GitOptimizer) GetRepositoryStats(ctx context.Context) (*RepositoryStats, error) {
	go.mu.RLock()
	defer go.mu.RUnlock()
	
	return go.getRepositoryStats()
}

// RepositoryStats contains repository statistics
type RepositoryStats struct {
	TotalObjects     int64
	LooseObjects     int64
	PackedObjects    int64
	PackFiles        int64
	RepositorySize   int64
	LastGC           time.Time
	LastRepack       time.Time
	NeedsOptimization bool
}

// getRepositoryStats internal implementation
func (go *GitOptimizer) getRepositoryStats() (*RepositoryStats, error) {
	stats := &RepositoryStats{
		LastGC:     go.lastGC,
		LastRepack: go.lastRepack,
	}
	
	// Get repository size
	size, err := go.getDirectorySize(go.repoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository size: %w", err)
	}
	stats.RepositorySize = size
	
	// Count objects using git count-objects
	cmd := exec.Command("git", "count-objects", "-v")
	cmd.Dir = go.repoPath
	
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to count objects: %w", err)
	}
	
	// Parse output
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		
		var value int64
		fmt.Sscanf(parts[1], "%d", &value)
		
		switch parts[0] {
		case "count":
			stats.LooseObjects = value
		case "in-pack":
			stats.PackedObjects = value
		case "packs":
			stats.PackFiles = value
		}
	}
	
	stats.TotalObjects = stats.LooseObjects + stats.PackedObjects
	stats.NeedsOptimization = go.shouldOptimize(stats)
	
	return stats, nil
}

// shouldOptimize determines if optimization is needed
func (go *GitOptimizer) shouldOptimize(stats *RepositoryStats) bool {
	// Check loose objects threshold
	if stats.LooseObjects > int64(go.config.AutoGCThreshold) {
		return true
	}
	
	// Check time-based intervals
	if time.Since(go.lastGC) > go.config.GCInterval {
		return true
	}
	
	if go.config.AutoRepack && time.Since(go.lastRepack) > go.config.RepackInterval {
		return true
	}
	
	// Check repository size (if too large, might need optimization)
	if stats.RepositorySize > go.config.PackSizeLimit*2 {
		return true
	}
	
	return false
}

// getDirectorySize calculates the total size of a directory
func (go *GitOptimizer) getDirectorySize(path string) (int64, error) {
	var size int64
	
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	
	return size, err
}

// truncateSubject truncates email subject for commit messages
func (go *GitOptimizer) truncateSubject(subject string) string {
	if len(subject) <= 50 {
		return subject
	}
	return subject[:47] + "..."
}

// StartAutoMaintenance starts automatic maintenance tasks
func (go *GitOptimizer) StartAutoMaintenance(ctx context.Context) error {
	if !go.config.EnableAutoMaintenance {
		return nil
	}
	
	ticker := time.NewTicker(go.config.MaintenanceInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := go.OptimizeRepository(ctx); err != nil {
				// Log error but continue maintenance
				continue
			}
		}
	}
}

// CompressEmailStorage implements email-specific compression strategies
func (go *GitOptimizer) CompressEmailStorage(ctx context.Context) error {
	// Use Git LFS for large attachments if available
	if go.hasGitLFS() {
		if err := go.configureGitLFS(ctx); err != nil {
			return fmt.Errorf("failed to configure Git LFS: %w", err)
		}
	}
	
	// Implement email-specific compression
	return go.compressEmailFiles(ctx)
}

// hasGitLFS checks if Git LFS is available
func (go *GitOptimizer) hasGitLFS() bool {
	cmd := exec.Command("git", "lfs", "version")
	return cmd.Run() == nil
}

// configureGitLFS configures Git LFS for large email attachments
func (go *GitOptimizer) configureGitLFS(ctx context.Context) error {
	// Track large files with Git LFS
	patterns := []string{
		"*.pdf",
		"*.doc",
		"*.docx",
		"*.zip",
		"*.tar.gz",
	}
	
	for _, pattern := range patterns {
		cmd := exec.CommandContext(ctx, "git", "lfs", "track", pattern)
		cmd.Dir = go.repoPath
		
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to track pattern %s with Git LFS: %w", pattern, err)
		}
	}
	
	return nil
}

// compressEmailFiles implements email-specific file compression
func (go *GitOptimizer) compressEmailFiles(ctx context.Context) error {
	// This could implement email-specific compression strategies
	// For now, rely on Git's built-in compression
	return nil
}

// CleanupRepository performs repository cleanup operations
func (go *GitOptimizer) CleanupRepository(ctx context.Context) error {
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{"Remove unreferenced objects", go.removeUnreferencedObjects},
		{"Clean temporary files", go.cleanTemporaryFiles},
		{"Optimize pack files", go.optimizePackFiles},
	}
	
	for _, step := range steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := step.fn(ctx); err != nil {
				return fmt.Errorf("cleanup step '%s' failed: %w", step.name, err)
			}
		}
	}
	
	return nil
}

// removeUnreferencedObjects removes objects that are no longer referenced
func (go *GitOptimizer) removeUnreferencedObjects(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "git", "prune", "--expire=now")
	cmd.Dir = go.repoPath
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git prune failed: %w, output: %s", err, string(output))
	}
	
	return nil
}

// cleanTemporaryFiles removes temporary files from the repository
func (go *GitOptimizer) cleanTemporaryFiles(ctx context.Context) error {
	// Remove temporary files that might accumulate
	patterns := []string{
		filepath.Join(go.repoPath, ".git", "objects", "tmp_*"),
		filepath.Join(go.repoPath, ".git", "*.tmp"),
		filepath.Join(go.repoPath, "*.tmp"),
	}
	
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		
		for _, match := range matches {
			os.Remove(match)
		}
	}
	
	return nil
}

// optimizePackFiles optimizes pack file structure
func (go *GitOptimizer) optimizePackFiles(ctx context.Context) error {
	// Rewrite pack files for optimal structure
	cmd := exec.CommandContext(ctx, "git", "repack", "-A", "-d")
	cmd.Dir = go.repoPath
	
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git repack failed: %w, output: %s", err, string(output))
	}
	
	return nil
}