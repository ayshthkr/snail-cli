package logging

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitRepositoryRecovery provides recovery functions for Git repository issues
type GitRepositoryRecovery struct {
	repoPath string
	logger   Logger
}

// NewGitRepositoryRecovery creates a new Git repository recovery handler
func NewGitRepositoryRecovery(repoPath string, logger Logger) *GitRepositoryRecovery {
	return &GitRepositoryRecovery{
		repoPath: repoPath,
		logger:   logger,
	}
}

// RecoverCorruptedRepository attempts to recover a corrupted Git repository
func (g *GitRepositoryRecovery) RecoverCorruptedRepository(err error) error {
	g.logger.Info("Attempting Git repository recovery",
		String("repo_path", g.repoPath),
		String("error", err.Error()),
	)
	
	// Check if .git directory exists
	gitDir := filepath.Join(g.repoPath, ".git")
	if _, statErr := os.Stat(gitDir); os.IsNotExist(statErr) {
		return g.reinitializeRepository()
	}
	
	// Try to repair the repository
	if repairErr := g.repairRepository(); repairErr != nil {
		g.logger.Warn("Repository repair failed, attempting backup and reinitialize",
			String("repair_error", repairErr.Error()),
		)
		return g.backupAndReinitialize()
	}
	
	return nil
}

// repairRepository attempts to repair a corrupted Git repository
func (g *GitRepositoryRecovery) repairRepository() error {
	// Try git fsck to check repository integrity
	cmd := exec.Command("git", "fsck", "--full")
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		g.logger.Error("Git fsck failed", err,
			String("output", string(output)),
		)
		
		// Try to recover using git reflog
		if reflogErr := g.recoverFromReflog(); reflogErr != nil {
			return fmt.Errorf("reflog recovery failed: %w", reflogErr)
		}
	}
	
	// Try to garbage collect and repack
	if gcErr := g.garbageCollect(); gcErr != nil {
		g.logger.Warn("Garbage collection failed", String("error", gcErr.Error()))
	}
	
	return nil
}

// recoverFromReflog attempts to recover using Git reflog
func (g *GitRepositoryRecovery) recoverFromReflog() error {
	cmd := exec.Command("git", "reflog", "expire", "--expire-unreachable=now", "--all")
	cmd.Dir = g.repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("reflog expire failed: %w", err)
	}
	
	cmd = exec.Command("git", "gc", "--prune=now")
	cmd.Dir = g.repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gc prune failed: %w", err)
	}
	
	return nil
}

// garbageCollect performs Git garbage collection
func (g *GitRepositoryRecovery) garbageCollect() error {
	cmd := exec.Command("git", "gc", "--aggressive", "--prune=now")
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("garbage collection failed: %w, output: %s", err, string(output))
	}
	
	g.logger.Info("Git garbage collection completed successfully")
	return nil
}

// backupAndReinitialize creates a backup and reinitializes the repository
func (g *GitRepositoryRecovery) backupAndReinitialize() error {
	backupPath := g.repoPath + ".backup." + fmt.Sprintf("%d", os.Getpid())
	
	// Create backup
	if err := os.Rename(g.repoPath, backupPath); err != nil {
		return fmt.Errorf("failed to create backup: %w", err)
	}
	
	g.logger.Info("Created repository backup", String("backup_path", backupPath))
	
	// Reinitialize repository
	if err := g.reinitializeRepository(); err != nil {
		// Restore backup if reinitialize fails
		os.Rename(backupPath, g.repoPath)
		return fmt.Errorf("failed to reinitialize repository: %w", err)
	}
	
	// Try to recover data from backup
	if err := g.recoverDataFromBackup(backupPath); err != nil {
		g.logger.Warn("Failed to recover data from backup", String("error", err.Error()))
	}
	
	return nil
}

// reinitializeRepository creates a new Git repository
func (g *GitRepositoryRecovery) reinitializeRepository() error {
	// Create directory if it doesn't exist
	if err := os.MkdirAll(g.repoPath, 0755); err != nil {
		return fmt.Errorf("failed to create repository directory: %w", err)
	}
	
	// Initialize Git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		return fmt.Errorf("git init failed: %w, output: %s", err, string(output))
	}
	
	g.logger.Info("Repository reinitialized successfully")
	return nil
}

// recoverDataFromBackup attempts to recover email files from backup
func (g *GitRepositoryRecovery) recoverDataFromBackup(backupPath string) error {
	// Look for email files in backup (excluding .git directory)
	err := filepath.Walk(backupPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		
		// Skip .git directory
		if strings.Contains(path, ".git") {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		
		// Skip directories
		if info.IsDir() {
			return nil
		}
		
		// Copy email files
		relPath, err := filepath.Rel(backupPath, path)
		if err != nil {
			return err
		}
		
		destPath := filepath.Join(g.repoPath, relPath)
		destDir := filepath.Dir(destPath)
		
		// Create destination directory
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return err
		}
		
		// Copy file
		return g.copyFile(path, destPath)
	})
	
	if err != nil {
		return fmt.Errorf("failed to recover data from backup: %w", err)
	}
	
	// Commit recovered files
	return g.commitRecoveredFiles()
}

// copyFile copies a file from src to dst
func (g *GitRepositoryRecovery) copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	
	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	
	_, err = dstFile.ReadFrom(srcFile)
	return err
}

// commitRecoveredFiles commits the recovered files to Git
func (g *GitRepositoryRecovery) commitRecoveredFiles() error {
	// Add all files
	cmd := exec.Command("git", "add", ".")
	cmd.Dir = g.repoPath
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git add failed: %w", err)
	}
	
	// Commit files
	cmd = exec.Command("git", "commit", "-m", "Recovered emails after repository corruption")
	cmd.Dir = g.repoPath
	output, err := cmd.CombinedOutput()
	
	if err != nil {
		// It's okay if there's nothing to commit
		if strings.Contains(string(output), "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit failed: %w, output: %s", err, string(output))
	}
	
	g.logger.Info("Recovered files committed successfully")
	return nil
}

// NetworkRecovery provides recovery functions for network-related issues
type NetworkRecovery struct {
	logger Logger
}

// NewNetworkRecovery creates a new network recovery handler
func NewNetworkRecovery(logger Logger) *NetworkRecovery {
	return &NetworkRecovery{
		logger: logger,
	}
}

// RecoverNetworkConnection attempts to recover from network connection issues
func (n *NetworkRecovery) RecoverNetworkConnection(err error) error {
	n.logger.Info("Attempting network connection recovery",
		String("error", err.Error()),
	)
	
	// Check basic connectivity
	if !n.checkConnectivity() {
		return NewRecoverableError(
			ErrorTypeNetwork,
			SeverityHigh,
			"no network connectivity detected",
			err,
		).WithSuggestion("Check your internet connection").
			WithSuggestion("Verify network settings").
			WithSuggestion("Try connecting to a different network")
	}
	
	// Check DNS resolution
	if !n.checkDNSResolution() {
		return NewRecoverableError(
			ErrorTypeNetwork,
			SeverityMedium,
			"DNS resolution failed",
			err,
		).WithSuggestion("Check DNS settings").
			WithSuggestion("Try using a different DNS server (8.8.8.8, 1.1.1.1)").
			WithSuggestion("Flush DNS cache")
	}
	
	// Network appears to be working, but there was still an error
	// Return suggestions for common network issues
	n.logger.Info("Network connectivity appears normal, providing general suggestions")
	return NewRecoverableError(
		ErrorTypeNetwork,
		SeverityLow,
		"network connectivity appears normal but error persists",
		err,
	).WithSuggestion("Wait a moment and retry the operation").
		WithSuggestion("Check if the remote server is accessible").
		WithSuggestion("Verify firewall settings").
		WithSuggestion("Try using a VPN if behind a corporate firewall")
}

// checkConnectivity performs a basic connectivity check
func (n *NetworkRecovery) checkConnectivity() bool {
	// Try to ping a reliable host
	cmd := exec.Command("ping", "-c", "1", "-W", "5", "8.8.8.8")
	err := cmd.Run()
	return err == nil
}

// checkDNSResolution checks if DNS resolution is working
func (n *NetworkRecovery) checkDNSResolution() bool {
	// Try to resolve a common hostname
	cmd := exec.Command("nslookup", "google.com")
	err := cmd.Run()
	return err == nil
}

// SyncConflictRecovery provides recovery functions for sync conflicts
type SyncConflictRecovery struct {
	logger Logger
}

// NewSyncConflictRecovery creates a new sync conflict recovery handler
func NewSyncConflictRecovery(logger Logger) *SyncConflictRecovery {
	return &SyncConflictRecovery{
		logger: logger,
	}
}

// RecoverSyncConflict attempts to resolve sync conflicts
func (s *SyncConflictRecovery) RecoverSyncConflict(err error) error {
	s.logger.Info("Attempting sync conflict recovery",
		String("error", err.Error()),
	)
	
	// For now, this is a placeholder for sync conflict resolution
	// In a real implementation, this would analyze the conflict and
	// attempt automatic resolution based on conflict resolution strategies
	
	return NewRecoverableError(
		ErrorTypeSyncConflict,
		SeverityMedium,
		"sync conflict detected",
		err,
	).WithSuggestion("Review conflicting changes manually").
		WithSuggestion("Use 'snail sync --force-server' to prefer server version").
		WithSuggestion("Use 'snail sync --force-local' to prefer local version").
		WithSuggestion("Resolve conflicts and retry sync")
}

// FilterRecovery provides recovery functions for filter execution issues
type FilterRecovery struct {
	logger Logger
}

// NewFilterRecovery creates a new filter recovery handler
func NewFilterRecovery(logger Logger) *FilterRecovery {
	return &FilterRecovery{
		logger: logger,
	}
}

// RecoverFilterExecution attempts to recover from filter execution failures
func (f *FilterRecovery) RecoverFilterExecution(err error) error {
	f.logger.Info("Attempting filter execution recovery",
		String("error", err.Error()),
	)
	
	// Check if it's a timeout error
	if strings.Contains(strings.ToLower(err.Error()), "timeout") {
		return NewRecoverableError(
			ErrorTypeFilter,
			SeverityMedium,
			"filter execution timeout",
			err,
		).WithSuggestion("Increase filter timeout in configuration").
			WithSuggestion("Optimize filter script for better performance").
			WithSuggestion("Consider disabling the problematic filter temporarily")
	}
	
	// Check if it's a permission error
	if strings.Contains(strings.ToLower(err.Error()), "permission") {
		return NewRecoverableError(
			ErrorTypeFilter,
			SeverityHigh,
			"filter permission denied",
			err,
		).WithSuggestion("Check filter script permissions (chmod +x)").
			WithSuggestion("Verify script path is correct").
			WithSuggestion("Check if script dependencies are installed")
	}
	
	// Generic filter error
	return NewRecoverableError(
		ErrorTypeFilter,
		SeverityMedium,
		"filter execution failed",
		err,
	).WithSuggestion("Check filter script syntax").
		WithSuggestion("Verify filter dependencies are installed").
		WithSuggestion("Review filter logs for detailed error information").
		WithSuggestion("Test filter script manually")
}

// RegisterCommonRecoveryFunctions registers common recovery functions with an error recovery manager
func RegisterCommonRecoveryFunctions(manager *ErrorRecoveryManager, repoPath string, logger Logger) {
	gitRecovery := NewGitRepositoryRecovery(repoPath, logger)
	networkRecovery := NewNetworkRecovery(logger)
	syncRecovery := NewSyncConflictRecovery(logger)
	filterRecovery := NewFilterRecovery(logger)
	
	// Register recovery functions
	manager.RegisterRecoveryFunc(ErrorTypeStorage, gitRecovery.RecoverCorruptedRepository)
	manager.RegisterRecoveryFunc(ErrorTypeNetwork, networkRecovery.RecoverNetworkConnection)
	manager.RegisterRecoveryFunc(ErrorTypeSyncConflict, syncRecovery.RecoverSyncConflict)
	manager.RegisterRecoveryFunc(ErrorTypeFilter, filterRecovery.RecoverFilterExecution)
	
	logger.Info("Registered common recovery functions",
		String("repo_path", repoPath),
	)
}