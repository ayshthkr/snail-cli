package repository

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestGitOptimizerBasic(t *testing.T) {
	// Create temporary directory for test repository
	tempDir, err := ioutil.TempDir("", "snail-git-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize Git repository
	repo, err := git.PlainInit(tempDir, false)
	if err != nil {
		t.Fatalf("Failed to initialize Git repository: %v", err)
	}

	// Create optimizer
	optimizer := NewGitOptimizer(repo, tempDir)

	// Test default configuration
	config := DefaultOptimizationConfig()
	if config.AutoGCThreshold != 1000 {
		t.Errorf("Expected AutoGCThreshold 1000, got %d", config.AutoGCThreshold)
	}

	// Test setting custom configuration
	customConfig := &OptimizationConfig{
		AutoGCThreshold:       500,
		GCInterval:            12 * time.Hour,
		CompressionLevel:      9,
		BatchSize:             50,
		ParallelWorkers:       2,
		EnableAutoMaintenance: false,
	}
	optimizer.SetConfig(customConfig)

	// Test repository stats
	ctx := context.Background()
	stats, err := optimizer.GetRepositoryStats(ctx)
	if err != nil {
		t.Fatalf("Failed to get repository stats: %v", err)
	}

	if stats.TotalObjects < 0 {
		t.Errorf("Invalid total objects count: %d", stats.TotalObjects)
	}

	if stats.RepositorySize < 0 {
		t.Errorf("Invalid repository size: %d", stats.RepositorySize)
	}
}

func TestBatchCommitEmails(t *testing.T) {
	// Create temporary directory for test repository
	tempDir, err := ioutil.TempDir("", "snail-git-batch-test-")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize Git repository
	repo, err := git.PlainInit(tempDir, false)
	if err != nil {
		t.Fatalf("Failed to initialize Git repository: %v", err)
	}

	// Create optimizer with small batch size for testing
	optimizer := NewGitOptimizer(repo, tempDir)
	config := DefaultOptimizationConfig()
	config.BatchSize = 3 // Small batch size for testing
	optimizer.SetConfig(config)

	// Create test emails
	emails := make([]EmailCommitInfo, 10)
	for i := 0; i < 10; i++ {
		emails[i] = EmailCommitInfo{
			FilePath:  fmt.Sprintf("inbox/2024/01/email-%d.eml", i),
			Content:   fmt.Sprintf("From: test%d@example.com\nSubject: Test Email %d\n\nBody %d", i, i, i),
			Operation: "add",
			Subject:   fmt.Sprintf("Test Email %d", i),
			From:      fmt.Sprintf("test%d@example.com", i),
		}
	}

	// Test batch commit
	ctx := context.Background()
	err = optimizer.BatchCommitEmails(ctx, emails)
	if err != nil {
		t.Fatalf("Failed to batch commit emails: %v", err)
	}

	// Verify files were created
	for i := 0; i < 10; i++ {
		filePath := filepath.Join(tempDir, emails[i].FilePath)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Errorf("Email file %s was not created", filePath)
		}
	}

	// Verify commits were created
	commits, err := repo.Log(&git.LogOptions{})
	if err != nil {
		t.Fatalf("Failed to get commit log: %v", err)
	}

	commitCount := 0
	err = commits.ForEach(func(commit *object.Commit) error {
		commitCount++
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to iterate commits: %v", err)
	}

	// Should have multiple commits due to batching (10 emails / 3 batch size = 4 batches)
	expectedCommits := 4
	if commitCount != expectedCommits {
		t.Errorf("Expected %d commits, got %d", expectedCommits, commitCount)
	}
}

func BenchmarkBatchCommit(b *testing.B) {
	// Create temporary directory for test repository
	tempDir, err := ioutil.TempDir("", "snail-git-bench-")
	if err != nil {
		b.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Initialize Git repository
	repo, err := git.PlainInit(tempDir, false)
	if err != nil {
		b.Fatalf("Failed to initialize Git repository: %v", err)
	}

	// Create optimizer
	optimizer := NewGitOptimizer(repo, tempDir)

	b.Run("Small_10_emails", func(b *testing.B) {
		ctx := context.Background()
		
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Create small batch of emails
			emails := make([]EmailCommitInfo, 10)
			for j := 0; j < 10; j++ {
				emails[j] = EmailCommitInfo{
					FilePath:  fmt.Sprintf("bench/small/run-%d-email-%d.eml", i, j),
					Content:   fmt.Sprintf("From: bench%d@example.com\nSubject: Bench Email %d\n\nBody %d", j, j, j),
					Operation: "add",
					Subject:   fmt.Sprintf("Bench Email %d", j),
					From:      fmt.Sprintf("bench%d@example.com", j),
				}
			}
			
			err := optimizer.BatchCommitEmails(ctx, emails)
			if err != nil {
				b.Fatalf("Failed to batch commit emails: %v", err)
			}
		}
	})

	b.Run("Large_100_emails", func(b *testing.B) {
		ctx := context.Background()
		
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Create larger batch of emails
			emails := make([]EmailCommitInfo, 100)
			for j := 0; j < 100; j++ {
				emails[j] = EmailCommitInfo{
					FilePath:  fmt.Sprintf("bench/large/run-%d-email-%d.eml", i, j),
					Content:   fmt.Sprintf("From: bench%d@example.com\nSubject: Large Bench Email %d\n\n%s", j, j, generateTestEmailBody(1024)),
					Operation: "add",
					Subject:   fmt.Sprintf("Large Bench Email %d", j),
					From:      fmt.Sprintf("bench%d@example.com", j),
				}
			}
			
			err := optimizer.BatchCommitEmails(ctx, emails)
			if err != nil {
				b.Fatalf("Failed to batch commit emails: %v", err)
			}
		}
	})
}

func TestOptimizationConfig(t *testing.T) {
	config := DefaultOptimizationConfig()

	// Test default values
	if config.AutoGCThreshold != 1000 {
		t.Errorf("Expected AutoGCThreshold 1000, got %d", config.AutoGCThreshold)
	}

	if config.GCInterval != 24*time.Hour {
		t.Errorf("Expected GCInterval 24h, got %v", config.GCInterval)
	}

	if config.CompressionLevel != 6 {
		t.Errorf("Expected CompressionLevel 6, got %d", config.CompressionLevel)
	}

	if config.BatchSize != 100 {
		t.Errorf("Expected BatchSize 100, got %d", config.BatchSize)
	}

	if config.ParallelWorkers != 4 {
		t.Errorf("Expected ParallelWorkers 4, got %d", config.ParallelWorkers)
	}

	if !config.EnableAutoMaintenance {
		t.Error("Expected EnableAutoMaintenance to be true")
	}
}

// generateTestEmailBody generates a test email body of specified size
func generateTestEmailBody(size int) string {
	content := "This is a test email body with repeated content. "
	repeated := ""
	
	for len(repeated) < size {
		repeated += content
	}
	
	if len(repeated) > size {
		repeated = repeated[:size]
	}
	
	return repeated
}