package interfaces

import (
	"context"
	"time"
)

// SearchIndex defines the interface for email search indexing
type SearchIndex interface {
	// Initialize creates or opens the search index
	Initialize(indexPath string) error
	
	// IndexEmail adds or updates an email in the search index
	IndexEmail(ctx context.Context, email *Email) error
	
	// RemoveEmail removes an email from the search index
	RemoveEmail(ctx context.Context, emailID string) error
	
	// Search performs a full-text search and returns matching email IDs
	Search(ctx context.Context, query SearchQuery) (*SearchResult, error)
	
	// UpdateIndex rebuilds the index from the repository
	UpdateIndex(ctx context.Context, emails []*Email) error
	
	// GetIndexStats returns statistics about the search index
	GetIndexStats(ctx context.Context) (*IndexStats, error)
	
	// Close closes the search index
	Close() error
}

// SearchQuery represents a search query with various criteria
type SearchQuery struct {
	// Full-text search query
	Text string
	
	// Metadata filters
	From     string
	To       string
	Subject  string
	Labels   []string
	Status   EmailStatus
	
	// Date range filters
	DateFrom *time.Time
	DateTo   *time.Time
	
	// Result options
	Limit  int
	Offset int
	
	// Sort options
	SortBy    SortField
	SortOrder SortOrder
}

// SearchResult represents the result of a search query
type SearchResult struct {
	// Matching email IDs with relevance scores
	Matches []SearchMatch
	
	// Total number of matches (before limit/offset)
	Total int
	
	// Search execution time
	Duration time.Duration
	
	// Query that was executed
	Query SearchQuery
}

// SearchMatch represents a single search result
type SearchMatch struct {
	EmailID   string
	Score     float64
	Highlights []string
}

// IndexStats provides statistics about the search index
type IndexStats struct {
	TotalEmails     int
	IndexSize       int64
	LastUpdated     time.Time
	IndexVersion    string
	OptimizationNeeded bool
}

// SortField defines the field to sort search results by
type SortField string

const (
	SortByRelevance SortField = "relevance"
	SortByDate      SortField = "date"
	SortByFrom      SortField = "from"
	SortBySubject   SortField = "subject"
)

// SortOrder defines the sort order
type SortOrder string

const (
	SortAsc  SortOrder = "asc"
	SortDesc SortOrder = "desc"
)

// MetadataIndex defines the interface for fast metadata filtering
type MetadataIndex interface {
	// Initialize creates or opens the metadata index
	Initialize(indexPath string) error
	
	// IndexEmailMetadata adds or updates email metadata in the index
	IndexEmailMetadata(ctx context.Context, email *Email) error
	
	// RemoveEmailMetadata removes email metadata from the index
	RemoveEmailMetadata(ctx context.Context, emailID string) error
	
	// FilterByMetadata returns email IDs matching metadata criteria
	FilterByMetadata(ctx context.Context, criteria MetadataFilter) ([]string, error)
	
	// GetEmailsByLabel returns email IDs with specific labels
	GetEmailsByLabel(ctx context.Context, labels []string) ([]string, error)
	
	// GetEmailsByStatus returns email IDs with specific status
	GetEmailsByStatus(ctx context.Context, status EmailStatus) ([]string, error)
	
	// GetEmailsByDateRange returns email IDs within date range
	GetEmailsByDateRange(ctx context.Context, from, to *time.Time) ([]string, error)
	
	// Close closes the metadata index
	Close() error
}

// MetadataFilter represents metadata filtering criteria
type MetadataFilter struct {
	Labels   []string
	Status   EmailStatus
	From     string
	To       string
	DateFrom *time.Time
	DateTo   *time.Time
}