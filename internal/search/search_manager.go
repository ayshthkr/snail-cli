package search

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"snail-cli/internal/interfaces"
)

// SearchManager coordinates both text and metadata search indices
type SearchManager struct {
	textIndex     interfaces.SearchIndex
	metadataIndex interfaces.MetadataIndex
	indexPath     string
}

// NewSearchManager creates a new search manager with both text and metadata indices
func NewSearchManager() *SearchManager {
	return &SearchManager{
		textIndex:     NewTextSearchIndex(),
		metadataIndex: NewMetadataSearchIndex(),
	}
}

// Initialize initializes both search indices
func (sm *SearchManager) Initialize(indexPath string) error {
	sm.indexPath = indexPath
	
	// Initialize text search index
	if err := sm.textIndex.Initialize(indexPath); err != nil {
		return fmt.Errorf("failed to initialize text search index: %w", err)
	}
	
	// Initialize metadata search index
	if err := sm.metadataIndex.Initialize(indexPath); err != nil {
		return fmt.Errorf("failed to initialize metadata search index: %w", err)
	}
	
	return nil
}

// IndexEmail adds or updates an email in both search indices
func (sm *SearchManager) IndexEmail(ctx context.Context, email *interfaces.Email) error {
	// Index in text search
	if err := sm.textIndex.IndexEmail(ctx, email); err != nil {
		return fmt.Errorf("failed to index email in text search: %w", err)
	}
	
	// Index in metadata search
	if err := sm.metadataIndex.IndexEmailMetadata(ctx, email); err != nil {
		return fmt.Errorf("failed to index email metadata: %w", err)
	}
	
	return nil
}

// RemoveEmail removes an email from both search indices
func (sm *SearchManager) RemoveEmail(ctx context.Context, emailID string) error {
	// Remove from text search
	if err := sm.textIndex.RemoveEmail(ctx, emailID); err != nil {
		return fmt.Errorf("failed to remove email from text search: %w", err)
	}
	
	// Remove from metadata search
	if err := sm.metadataIndex.RemoveEmailMetadata(ctx, emailID); err != nil {
		return fmt.Errorf("failed to remove email metadata: %w", err)
	}
	
	return nil
}

// Search performs a comprehensive search using both text and metadata indices
func (sm *SearchManager) Search(ctx context.Context, query interfaces.SearchQuery) (*interfaces.SearchResult, error) {
	// Use text search index for comprehensive search (it handles both text and metadata)
	return sm.textIndex.Search(ctx, query)
}

// SearchByMetadata performs a metadata-only search for fast filtering
func (sm *SearchManager) SearchByMetadata(ctx context.Context, filter interfaces.MetadataFilter) ([]string, error) {
	return sm.metadataIndex.FilterByMetadata(ctx, filter)
}

// GetEmailsByLabel returns email IDs with specific labels
func (sm *SearchManager) GetEmailsByLabel(ctx context.Context, labels []string) ([]string, error) {
	return sm.metadataIndex.GetEmailsByLabel(ctx, labels)
}

// GetEmailsByStatus returns email IDs with specific status
func (sm *SearchManager) GetEmailsByStatus(ctx context.Context, status interfaces.EmailStatus) ([]string, error) {
	return sm.metadataIndex.GetEmailsByStatus(ctx, status)
}

// GetEmailsByDateRange returns email IDs within date range
func (sm *SearchManager) GetEmailsByDateRange(ctx context.Context, from, to *time.Time) ([]string, error) {
	return sm.metadataIndex.GetEmailsByDateRange(ctx, from, to)
}

// UpdateIndex rebuilds both indices from the repository
func (sm *SearchManager) UpdateIndex(ctx context.Context, emails []*interfaces.Email) error {
	// Update text search index
	if err := sm.textIndex.UpdateIndex(ctx, emails); err != nil {
		return fmt.Errorf("failed to update text search index: %w", err)
	}
	
	// Update metadata search index by indexing each email
	for _, email := range emails {
		if err := sm.metadataIndex.IndexEmailMetadata(ctx, email); err != nil {
			return fmt.Errorf("failed to update metadata index for email %s: %w", email.ID, err)
		}
	}
	
	return nil
}

// GetIndexStats returns combined statistics from both indices
func (sm *SearchManager) GetIndexStats(ctx context.Context) (*interfaces.IndexStats, error) {
	// Get stats from text index (primary index)
	textStats, err := sm.textIndex.GetIndexStats(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get text index stats: %w", err)
	}
	
	// Get metadata index file size
	var metadataSize int64
	if sm.indexPath != "" {
		metadataFile := filepath.Join(sm.indexPath, "metadata_index.json")
		if info, err := os.Stat(metadataFile); err == nil {
			metadataSize = info.Size()
		}
	}
	
	// Combine stats
	combinedStats := &interfaces.IndexStats{
		TotalEmails:        textStats.TotalEmails,
		IndexSize:          textStats.IndexSize + metadataSize,
		LastUpdated:        textStats.LastUpdated,
		IndexVersion:       textStats.IndexVersion,
		OptimizationNeeded: textStats.OptimizationNeeded,
	}
	
	return combinedStats, nil
}

// OptimizeIndex performs optimization on both indices
func (sm *SearchManager) OptimizeIndex(ctx context.Context) error {
	// For now, optimization means rebuilding the indices
	// In a more sophisticated implementation, this could include:
	// - Compacting index files
	// - Rebuilding word indices with better IDF scores
	// - Removing unused entries
	
	// Get current stats to check if optimization is needed
	stats, err := sm.GetIndexStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get index stats: %w", err)
	}
	
	if !stats.OptimizationNeeded {
		return nil // No optimization needed
	}
	
	// For now, just save both indices to ensure they're persisted
	if err := sm.textIndex.Close(); err != nil {
		return fmt.Errorf("failed to close text index during optimization: %w", err)
	}
	
	if err := sm.metadataIndex.Close(); err != nil {
		return fmt.Errorf("failed to close metadata index during optimization: %w", err)
	}
	
	// Reinitialize indices
	return sm.Initialize(sm.indexPath)
}

// Close closes both search indices
func (sm *SearchManager) Close() error {
	var textErr, metadataErr error
	
	// Close text index
	if sm.textIndex != nil {
		textErr = sm.textIndex.Close()
	}
	
	// Close metadata index
	if sm.metadataIndex != nil {
		metadataErr = sm.metadataIndex.Close()
	}
	
	// Return any errors
	if textErr != nil {
		return fmt.Errorf("failed to close text index: %w", textErr)
	}
	if metadataErr != nil {
		return fmt.Errorf("failed to close metadata index: %w", metadataErr)
	}
	
	return nil
}

// GetTextIndex returns the text search index (for advanced usage)
func (sm *SearchManager) GetTextIndex() interfaces.SearchIndex {
	return sm.textIndex
}

// GetMetadataIndex returns the metadata search index (for advanced usage)
func (sm *SearchManager) GetMetadataIndex() interfaces.MetadataIndex {
	return sm.metadataIndex
}