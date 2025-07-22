package search

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
)

// MetadataSearchIndex implements fast metadata filtering for emails
type MetadataSearchIndex struct {
	indexPath string
	
	// Metadata indices for fast filtering
	labelIndex  map[string]map[string]bool // label -> emailID -> exists
	statusIndex map[string]map[string]bool // status -> emailID -> exists
	fromIndex   map[string]map[string]bool // from -> emailID -> exists
	toIndex     map[string]map[string]bool // to -> emailID -> exists
	dateIndex   map[string]*EmailMetadata  // emailID -> metadata
	
	mutex sync.RWMutex
}

// EmailMetadata represents indexed email metadata
type EmailMetadata struct {
	EmailID   string              `json:"email_id"`
	From      string              `json:"from"`
	To        []string            `json:"to"`
	CC        []string            `json:"cc"`
	Subject   string              `json:"subject"`
	Date      time.Time           `json:"date"`
	Labels    []string            `json:"labels"`
	Status    string              `json:"status"`
	IndexedAt time.Time           `json:"indexed_at"`
}

// NewMetadataSearchIndex creates a new metadata search index
func NewMetadataSearchIndex() *MetadataSearchIndex {
	return &MetadataSearchIndex{
		labelIndex:  make(map[string]map[string]bool),
		statusIndex: make(map[string]map[string]bool),
		fromIndex:   make(map[string]map[string]bool),
		toIndex:     make(map[string]map[string]bool),
		dateIndex:   make(map[string]*EmailMetadata),
	}
}

// Initialize creates or opens the metadata index
func (idx *MetadataSearchIndex) Initialize(indexPath string) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	idx.indexPath = indexPath
	
	// Create index directory if it doesn't exist
	if err := os.MkdirAll(indexPath, 0755); err != nil {
		return fmt.Errorf("failed to create index directory: %w", err)
	}
	
	// Load existing index if it exists
	indexFile := filepath.Join(indexPath, "metadata_index.json")
	if _, err := os.Stat(indexFile); err == nil {
		if err := idx.loadIndex(indexFile); err != nil {
			return fmt.Errorf("failed to load existing metadata index: %w", err)
		}
	}
	
	return nil
}

// IndexEmailMetadata adds or updates email metadata in the index
func (idx *MetadataSearchIndex) IndexEmailMetadata(ctx context.Context, email *interfaces.Email) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Remove old metadata if it exists
	if oldMetadata, exists := idx.dateIndex[email.ID]; exists {
		idx.removeFromIndices(oldMetadata)
	}
	
	// Create new metadata
	metadata := &EmailMetadata{
		EmailID:   email.ID,
		From:      idx.formatAddress(email.From),
		To:        idx.formatAddresses(email.To),
		CC:        idx.formatAddresses(email.CC),
		Subject:   email.Subject,
		Date:      email.Date,
		Labels:    email.Labels,
		Status:    string(email.Status),
		IndexedAt: time.Now(),
	}
	
	// Add to indices
	idx.addToIndices(metadata)
	
	// Save index
	return idx.saveIndex()
}

// RemoveEmailMetadata removes email metadata from the index
func (idx *MetadataSearchIndex) RemoveEmailMetadata(ctx context.Context, emailID string) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Get metadata to remove
	metadata, exists := idx.dateIndex[emailID]
	if !exists {
		return nil // Already removed
	}
	
	// Remove from indices
	idx.removeFromIndices(metadata)
	
	// Save index
	return idx.saveIndex()
}

// FilterByMetadata returns email IDs matching metadata criteria
func (idx *MetadataSearchIndex) FilterByMetadata(ctx context.Context, criteria interfaces.MetadataFilter) ([]string, error) {
	idx.mutex.RLock()
	defer idx.mutex.RUnlock()
	
	var candidateEmails map[string]bool
	
	// Start with all emails if no specific criteria
	if len(criteria.Labels) == 0 && criteria.Status == "" && criteria.From == "" && criteria.To == "" {
		candidateEmails = make(map[string]bool)
		for emailID := range idx.dateIndex {
			candidateEmails[emailID] = true
		}
	}
	
	// Filter by labels
	if len(criteria.Labels) > 0 {
		labelEmails := make(map[string]bool)
		for _, label := range criteria.Labels {
			if emails, exists := idx.labelIndex[strings.ToLower(label)]; exists {
				for emailID := range emails {
					labelEmails[emailID] = true
				}
			}
		}
		candidateEmails = idx.intersectSets(candidateEmails, labelEmails)
	}
	
	// Filter by status
	if criteria.Status != "" {
		statusEmails := idx.statusIndex[string(criteria.Status)]
		candidateEmails = idx.intersectSets(candidateEmails, statusEmails)
	}
	
	// Filter by from address
	if criteria.From != "" {
		fromEmails := make(map[string]bool)
		fromLower := strings.ToLower(criteria.From)
		for from, emails := range idx.fromIndex {
			if strings.Contains(strings.ToLower(from), fromLower) {
				for emailID := range emails {
					fromEmails[emailID] = true
				}
			}
		}
		candidateEmails = idx.intersectSets(candidateEmails, fromEmails)
	}
	
	// Filter by to address
	if criteria.To != "" {
		toEmails := make(map[string]bool)
		toLower := strings.ToLower(criteria.To)
		for to, emails := range idx.toIndex {
			if strings.Contains(strings.ToLower(to), toLower) {
				for emailID := range emails {
					toEmails[emailID] = true
				}
			}
		}
		candidateEmails = idx.intersectSets(candidateEmails, toEmails)
	}
	
	// Filter by date range
	var result []string
	for emailID := range candidateEmails {
		metadata := idx.dateIndex[emailID]
		if metadata == nil {
			continue
		}
		
		// Check date range
		if criteria.DateFrom != nil && metadata.Date.Before(*criteria.DateFrom) {
			continue
		}
		if criteria.DateTo != nil && metadata.Date.After(*criteria.DateTo) {
			continue
		}
		
		result = append(result, emailID)
	}
	
	return result, nil
}

// GetEmailsByLabel returns email IDs with specific labels
func (idx *MetadataSearchIndex) GetEmailsByLabel(ctx context.Context, labels []string) ([]string, error) {
	idx.mutex.RLock()
	defer idx.mutex.RUnlock()
	
	if len(labels) == 0 {
		return []string{}, nil
	}
	
	var result []string
	emailSet := make(map[string]bool)
	
	for _, label := range labels {
		if emails, exists := idx.labelIndex[strings.ToLower(label)]; exists {
			for emailID := range emails {
				if !emailSet[emailID] {
					emailSet[emailID] = true
					result = append(result, emailID)
				}
			}
		}
	}
	
	return result, nil
}

// GetEmailsByStatus returns email IDs with specific status
func (idx *MetadataSearchIndex) GetEmailsByStatus(ctx context.Context, status interfaces.EmailStatus) ([]string, error) {
	idx.mutex.RLock()
	defer idx.mutex.RUnlock()
	
	var result []string
	if emails, exists := idx.statusIndex[string(status)]; exists {
		for emailID := range emails {
			result = append(result, emailID)
		}
	}
	
	return result, nil
}

// GetEmailsByDateRange returns email IDs within date range
func (idx *MetadataSearchIndex) GetEmailsByDateRange(ctx context.Context, from, to *time.Time) ([]string, error) {
	idx.mutex.RLock()
	defer idx.mutex.RUnlock()
	
	var result []string
	
	for emailID, metadata := range idx.dateIndex {
		if from != nil && metadata.Date.Before(*from) {
			continue
		}
		if to != nil && metadata.Date.After(*to) {
			continue
		}
		result = append(result, emailID)
	}
	
	return result, nil
}

// Close closes the metadata index
func (idx *MetadataSearchIndex) Close() error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Save index before closing
	return idx.saveIndex()
}

// addToIndices adds metadata to all relevant indices
func (idx *MetadataSearchIndex) addToIndices(metadata *EmailMetadata) {
	// Add to date index
	idx.dateIndex[metadata.EmailID] = metadata
	
	// Add to label index
	for _, label := range metadata.Labels {
		labelKey := strings.ToLower(label)
		if idx.labelIndex[labelKey] == nil {
			idx.labelIndex[labelKey] = make(map[string]bool)
		}
		idx.labelIndex[labelKey][metadata.EmailID] = true
	}
	
	// Add to status index
	if idx.statusIndex[metadata.Status] == nil {
		idx.statusIndex[metadata.Status] = make(map[string]bool)
	}
	idx.statusIndex[metadata.Status][metadata.EmailID] = true
	
	// Add to from index
	fromKey := strings.ToLower(metadata.From)
	if idx.fromIndex[fromKey] == nil {
		idx.fromIndex[fromKey] = make(map[string]bool)
	}
	idx.fromIndex[fromKey][metadata.EmailID] = true
	
	// Add to to index
	for _, to := range metadata.To {
		toKey := strings.ToLower(to)
		if idx.toIndex[toKey] == nil {
			idx.toIndex[toKey] = make(map[string]bool)
		}
		idx.toIndex[toKey][metadata.EmailID] = true
	}
	
	// Add CC addresses to to index as well
	for _, cc := range metadata.CC {
		ccKey := strings.ToLower(cc)
		if idx.toIndex[ccKey] == nil {
			idx.toIndex[ccKey] = make(map[string]bool)
		}
		idx.toIndex[ccKey][metadata.EmailID] = true
	}
}

// removeFromIndices removes metadata from all relevant indices
func (idx *MetadataSearchIndex) removeFromIndices(metadata *EmailMetadata) {
	// Remove from date index
	delete(idx.dateIndex, metadata.EmailID)
	
	// Remove from label index
	for _, label := range metadata.Labels {
		labelKey := strings.ToLower(label)
		if emails, exists := idx.labelIndex[labelKey]; exists {
			delete(emails, metadata.EmailID)
			if len(emails) == 0 {
				delete(idx.labelIndex, labelKey)
			}
		}
	}
	
	// Remove from status index
	if emails, exists := idx.statusIndex[metadata.Status]; exists {
		delete(emails, metadata.EmailID)
		if len(emails) == 0 {
			delete(idx.statusIndex, metadata.Status)
		}
	}
	
	// Remove from from index
	fromKey := strings.ToLower(metadata.From)
	if emails, exists := idx.fromIndex[fromKey]; exists {
		delete(emails, metadata.EmailID)
		if len(emails) == 0 {
			delete(idx.fromIndex, fromKey)
		}
	}
	
	// Remove from to index
	for _, to := range metadata.To {
		toKey := strings.ToLower(to)
		if emails, exists := idx.toIndex[toKey]; exists {
			delete(emails, metadata.EmailID)
			if len(emails) == 0 {
				delete(idx.toIndex, toKey)
			}
		}
	}
	
	// Remove CC addresses from to index
	for _, cc := range metadata.CC {
		ccKey := strings.ToLower(cc)
		if emails, exists := idx.toIndex[ccKey]; exists {
			delete(emails, metadata.EmailID)
			if len(emails) == 0 {
				delete(idx.toIndex, ccKey)
			}
		}
	}
}

// intersectSets returns the intersection of two email ID sets
func (idx *MetadataSearchIndex) intersectSets(set1, set2 map[string]bool) map[string]bool {
	if set1 == nil {
		return set2
	}
	if set2 == nil {
		return set1
	}
	
	result := make(map[string]bool)
	for emailID := range set1 {
		if set2[emailID] {
			result[emailID] = true
		}
	}
	
	return result
}

// formatAddress formats an email address for indexing
func (idx *MetadataSearchIndex) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// formatAddresses formats a list of email addresses for indexing
func (idx *MetadataSearchIndex) formatAddresses(addrs []interfaces.Address) []string {
	var formatted []string
	for _, addr := range addrs {
		formatted = append(formatted, idx.formatAddress(addr))
	}
	return formatted
}

// saveIndex saves the metadata index to disk
func (idx *MetadataSearchIndex) saveIndex() error {
	if idx.indexPath == "" {
		return nil
	}
	
	indexFile := filepath.Join(idx.indexPath, "metadata_index.json")
	
	// Create index data structure
	indexData := struct {
		Metadata  map[string]*EmailMetadata `json:"metadata"`
		Version   string                    `json:"version"`
		UpdatedAt time.Time                 `json:"updated_at"`
	}{
		Metadata:  idx.dateIndex,
		Version:   "1.0",
		UpdatedAt: time.Now(),
	}
	
	// Marshal to JSON
	data, err := json.MarshalIndent(indexData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata index: %w", err)
	}
	
	// Write to file
	if err := os.WriteFile(indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write metadata index file: %w", err)
	}
	
	return nil
}

// loadIndex loads the metadata index from disk
func (idx *MetadataSearchIndex) loadIndex(indexFile string) error {
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return fmt.Errorf("failed to read metadata index file: %w", err)
	}
	
	// Unmarshal JSON
	var indexData struct {
		Metadata  map[string]*EmailMetadata `json:"metadata"`
		Version   string                    `json:"version"`
		UpdatedAt time.Time                 `json:"updated_at"`
	}
	
	if err := json.Unmarshal(data, &indexData); err != nil {
		return fmt.Errorf("failed to unmarshal metadata index: %w", err)
	}
	
	// Clear existing indices
	idx.labelIndex = make(map[string]map[string]bool)
	idx.statusIndex = make(map[string]map[string]bool)
	idx.fromIndex = make(map[string]map[string]bool)
	idx.toIndex = make(map[string]map[string]bool)
	idx.dateIndex = make(map[string]*EmailMetadata)
	
	// Rebuild indices from loaded metadata
	for _, metadata := range indexData.Metadata {
		idx.addToIndices(metadata)
	}
	
	return nil
}