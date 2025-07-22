package search

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
)

// TextSearchIndex implements full-text search indexing for emails
type TextSearchIndex struct {
	indexPath string
	index     map[string]*EmailDocument
	wordIndex map[string]map[string]float64 // word -> emailID -> score
	mutex     sync.RWMutex
	stopWords map[string]bool
}

// EmailDocument represents an indexed email document
type EmailDocument struct {
	EmailID     string            `json:"email_id"`
	Subject     string            `json:"subject"`
	From        string            `json:"from"`
	To          []string          `json:"to"`
	Body        string            `json:"body"`
	Labels      []string          `json:"labels"`
	Status      string            `json:"status"`
	Date        time.Time         `json:"date"`
	WordCounts  map[string]int    `json:"word_counts"`
	IndexedAt   time.Time         `json:"indexed_at"`
}

// NewTextSearchIndex creates a new text search index
func NewTextSearchIndex() *TextSearchIndex {
	return &TextSearchIndex{
		index:     make(map[string]*EmailDocument),
		wordIndex: make(map[string]map[string]float64),
		stopWords: getStopWords(),
	}
}

// Initialize creates or opens the search index
func (idx *TextSearchIndex) Initialize(indexPath string) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	idx.indexPath = indexPath
	
	// Create index directory if it doesn't exist
	if err := os.MkdirAll(indexPath, 0755); err != nil {
		return fmt.Errorf("failed to create index directory: %w", err)
	}
	
	// Load existing index if it exists
	indexFile := filepath.Join(indexPath, "text_index.json")
	if _, err := os.Stat(indexFile); err == nil {
		if err := idx.loadIndex(indexFile); err != nil {
			return fmt.Errorf("failed to load existing index: %w", err)
		}
	}
	
	return nil
}

// IndexEmail adds or updates an email in the search index
func (idx *TextSearchIndex) IndexEmail(ctx context.Context, email *interfaces.Email) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Create email document
	doc := &EmailDocument{
		EmailID:   email.ID,
		Subject:   email.Subject,
		From:      idx.formatAddress(email.From),
		To:        idx.formatAddresses(email.To),
		Body:      email.Body,
		Labels:    email.Labels,
		Status:    string(email.Status),
		Date:      email.Date,
		IndexedAt: time.Now(),
	}
	
	// Extract and count words
	doc.WordCounts = idx.extractWords(email)
	
	// Remove old document if it exists
	if oldDoc, exists := idx.index[email.ID]; exists {
		idx.removeFromWordIndex(oldDoc)
	}
	
	// Add new document to index
	idx.index[email.ID] = doc
	idx.addToWordIndex(doc)
	
	// Save index
	return idx.saveIndex()
}

// RemoveEmail removes an email from the search index
func (idx *TextSearchIndex) RemoveEmail(ctx context.Context, emailID string) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Get document to remove
	doc, exists := idx.index[emailID]
	if !exists {
		return nil // Already removed
	}
	
	// Remove from word index
	idx.removeFromWordIndex(doc)
	
	// Remove from main index
	delete(idx.index, emailID)
	
	// Save index
	return idx.saveIndex()
}

// Search performs a full-text search and returns matching email IDs
func (idx *TextSearchIndex) Search(ctx context.Context, query interfaces.SearchQuery) (*interfaces.SearchResult, error) {
	idx.mutex.RLock()
	defer idx.mutex.RUnlock()
	
	startTime := time.Now()
	
	var matches []interfaces.SearchMatch
	
	if query.Text != "" {
		// Perform text search
		textMatches := idx.searchText(query.Text)
		matches = append(matches, textMatches...)
	} else {
		// If no text search, start with all emails for metadata filtering
		for emailID := range idx.index {
			matches = append(matches, interfaces.SearchMatch{
				EmailID: emailID,
				Score:   1.0, // Default score for metadata-only searches
			})
		}
	}
	
	// Apply metadata filters
	filteredMatches := idx.applyMetadataFilters(matches, query)
	
	// Sort results
	idx.sortMatches(filteredMatches, query.SortBy, query.SortOrder)
	
	// Apply limit and offset
	total := len(filteredMatches)
	if query.Offset > 0 && query.Offset < len(filteredMatches) {
		filteredMatches = filteredMatches[query.Offset:]
	}
	if query.Limit > 0 && query.Limit < len(filteredMatches) {
		filteredMatches = filteredMatches[:query.Limit]
	}
	
	return &interfaces.SearchResult{
		Matches:  filteredMatches,
		Total:    total,
		Duration: time.Since(startTime),
		Query:    query,
	}, nil
}

// UpdateIndex rebuilds the index from the repository
func (idx *TextSearchIndex) UpdateIndex(ctx context.Context, emails []*interfaces.Email) error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Clear existing index
	idx.index = make(map[string]*EmailDocument)
	idx.wordIndex = make(map[string]map[string]float64)
	
	// Index all emails
	for _, email := range emails {
		doc := &EmailDocument{
			EmailID:   email.ID,
			Subject:   email.Subject,
			From:      idx.formatAddress(email.From),
			To:        idx.formatAddresses(email.To),
			Body:      email.Body,
			Labels:    email.Labels,
			Status:    string(email.Status),
			Date:      email.Date,
			IndexedAt: time.Now(),
		}
		
		doc.WordCounts = idx.extractWords(email)
		idx.index[email.ID] = doc
		idx.addToWordIndex(doc)
	}
	
	return idx.saveIndex()
}

// GetIndexStats returns statistics about the search index
func (idx *TextSearchIndex) GetIndexStats(ctx context.Context) (*interfaces.IndexStats, error) {
	idx.mutex.RLock()
	defer idx.mutex.RUnlock()
	
	// Calculate index size
	var indexSize int64
	if idx.indexPath != "" {
		indexFile := filepath.Join(idx.indexPath, "text_index.json")
		if info, err := os.Stat(indexFile); err == nil {
			indexSize = info.Size()
		}
	}
	
	// Find last updated time
	var lastUpdated time.Time
	for _, doc := range idx.index {
		if doc.IndexedAt.After(lastUpdated) {
			lastUpdated = doc.IndexedAt
		}
	}
	
	return &interfaces.IndexStats{
		TotalEmails:        len(idx.index),
		IndexSize:          indexSize,
		LastUpdated:        lastUpdated,
		IndexVersion:       "1.0",
		OptimizationNeeded: len(idx.index) > 10000, // Suggest optimization for large indices
	}, nil
}

// Close closes the search index
func (idx *TextSearchIndex) Close() error {
	idx.mutex.Lock()
	defer idx.mutex.Unlock()
	
	// Save index before closing
	return idx.saveIndex()
}

// extractWords extracts and counts words from an email for indexing
func (idx *TextSearchIndex) extractWords(email *interfaces.Email) map[string]int {
	wordCounts := make(map[string]int)
	
	// Extract words from subject (higher weight)
	subjectWords := idx.tokenize(email.Subject)
	for _, word := range subjectWords {
		if !idx.stopWords[word] {
			wordCounts[word] += 3 // Subject words get higher weight
		}
	}
	
	// Extract words from body
	bodyWords := idx.tokenize(email.Body)
	for _, word := range bodyWords {
		if !idx.stopWords[word] {
			wordCounts[word]++
		}
	}
	
	// Extract words from sender name
	fromWords := idx.tokenize(email.From.Name)
	for _, word := range fromWords {
		if !idx.stopWords[word] {
			wordCounts[word] += 2 // Sender name gets medium weight
		}
	}
	
	return wordCounts
}

// tokenize breaks text into words and normalizes them
func (idx *TextSearchIndex) tokenize(text string) []string {
	// Convert to lowercase
	text = strings.ToLower(text)
	
	// Remove punctuation and split on whitespace
	reg := regexp.MustCompile(`[^\p{L}\p{N}]+`)
	text = reg.ReplaceAllString(text, " ")
	
	// Split into words
	words := strings.Fields(text)
	
	// Filter out short words
	var filtered []string
	for _, word := range words {
		if len(word) >= 2 {
			filtered = append(filtered, word)
		}
	}
	
	return filtered
}

// addToWordIndex adds a document to the word index
func (idx *TextSearchIndex) addToWordIndex(doc *EmailDocument) {
	for word, count := range doc.WordCounts {
		if idx.wordIndex[word] == nil {
			idx.wordIndex[word] = make(map[string]float64)
		}
		
		// Calculate TF-IDF score (simplified)
		tf := float64(count)
		idf := 1.0 // Simplified, could calculate actual IDF
		score := tf * idf
		
		idx.wordIndex[word][doc.EmailID] = score
	}
}

// removeFromWordIndex removes a document from the word index
func (idx *TextSearchIndex) removeFromWordIndex(doc *EmailDocument) {
	for word := range doc.WordCounts {
		if emailScores, exists := idx.wordIndex[word]; exists {
			delete(emailScores, doc.EmailID)
			
			// Remove word entry if no emails contain it
			if len(emailScores) == 0 {
				delete(idx.wordIndex, word)
			}
		}
	}
}

// searchText performs text search and returns matches
func (idx *TextSearchIndex) searchText(query string) []interfaces.SearchMatch {
	queryWords := idx.tokenize(query)
	if len(queryWords) == 0 {
		return nil
	}
	
	// Calculate scores for each email
	emailScores := make(map[string]float64)
	
	for _, word := range queryWords {
		if wordEmailScores, exists := idx.wordIndex[word]; exists {
			for emailID, score := range wordEmailScores {
				emailScores[emailID] += score
			}
		}
	}
	
	// Convert to search matches
	var matches []interfaces.SearchMatch
	for emailID, score := range emailScores {
		matches = append(matches, interfaces.SearchMatch{
			EmailID: emailID,
			Score:   score,
		})
	}
	
	return matches
}

// applyMetadataFilters applies metadata filters to search results
func (idx *TextSearchIndex) applyMetadataFilters(matches []interfaces.SearchMatch, query interfaces.SearchQuery) []interfaces.SearchMatch {
	if len(matches) == 0 {
		return matches
	}
	
	var filtered []interfaces.SearchMatch
	
	for _, match := range matches {
		doc, exists := idx.index[match.EmailID]
		if !exists {
			continue
		}
		
		// Apply filters
		if query.From != "" && !strings.Contains(strings.ToLower(doc.From), strings.ToLower(query.From)) {
			continue
		}
		
		if query.Subject != "" && !strings.Contains(strings.ToLower(doc.Subject), strings.ToLower(query.Subject)) {
			continue
		}
		
		if query.Status != "" && doc.Status != string(query.Status) {
			continue
		}
		
		if len(query.Labels) > 0 {
			hasLabel := false
			for _, queryLabel := range query.Labels {
				for _, docLabel := range doc.Labels {
					if docLabel == queryLabel {
						hasLabel = true
						break
					}
				}
				if hasLabel {
					break
				}
			}
			if !hasLabel {
				continue
			}
		}
		
		if query.DateFrom != nil && doc.Date.Before(*query.DateFrom) {
			continue
		}
		
		if query.DateTo != nil && doc.Date.After(*query.DateTo) {
			continue
		}
		
		filtered = append(filtered, match)
	}
	
	return filtered
}

// sortMatches sorts search results based on the specified criteria
func (idx *TextSearchIndex) sortMatches(matches []interfaces.SearchMatch, sortBy interfaces.SortField, order interfaces.SortOrder) {
	sort.Slice(matches, func(i, j int) bool {
		var less bool
		
		switch sortBy {
		case interfaces.SortByRelevance:
			less = matches[i].Score > matches[j].Score // Higher score first
		case interfaces.SortByDate:
			docI := idx.index[matches[i].EmailID]
			docJ := idx.index[matches[j].EmailID]
			if docI != nil && docJ != nil {
				less = docI.Date.After(docJ.Date) // Newer first
			}
		case interfaces.SortByFrom:
			docI := idx.index[matches[i].EmailID]
			docJ := idx.index[matches[j].EmailID]
			if docI != nil && docJ != nil {
				less = docI.From < docJ.From
			}
		case interfaces.SortBySubject:
			docI := idx.index[matches[i].EmailID]
			docJ := idx.index[matches[j].EmailID]
			if docI != nil && docJ != nil {
				less = docI.Subject < docJ.Subject
			}
		default:
			less = matches[i].Score > matches[j].Score
		}
		
		if order == interfaces.SortAsc {
			return !less
		}
		return less
	})
}

// formatAddress formats an email address for indexing
func (idx *TextSearchIndex) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// formatAddresses formats a list of email addresses for indexing
func (idx *TextSearchIndex) formatAddresses(addrs []interfaces.Address) []string {
	var formatted []string
	for _, addr := range addrs {
		formatted = append(formatted, idx.formatAddress(addr))
	}
	return formatted
}

// saveIndex saves the index to disk
func (idx *TextSearchIndex) saveIndex() error {
	if idx.indexPath == "" {
		return nil
	}
	
	indexFile := filepath.Join(idx.indexPath, "text_index.json")
	
	// Create index data structure
	indexData := struct {
		Documents map[string]*EmailDocument `json:"documents"`
		Version   string                    `json:"version"`
		UpdatedAt time.Time                 `json:"updated_at"`
	}{
		Documents: idx.index,
		Version:   "1.0",
		UpdatedAt: time.Now(),
	}
	
	// Marshal to JSON
	data, err := json.MarshalIndent(indexData, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal index data: %w", err)
	}
	
	// Write to file
	if err := os.WriteFile(indexFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write index file: %w", err)
	}
	
	return nil
}

// loadIndex loads the index from disk
func (idx *TextSearchIndex) loadIndex(indexFile string) error {
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return fmt.Errorf("failed to read index file: %w", err)
	}
	
	// Unmarshal JSON
	var indexData struct {
		Documents map[string]*EmailDocument `json:"documents"`
		Version   string                    `json:"version"`
		UpdatedAt time.Time                 `json:"updated_at"`
	}
	
	if err := json.Unmarshal(data, &indexData); err != nil {
		return fmt.Errorf("failed to unmarshal index data: %w", err)
	}
	
	// Load documents
	idx.index = indexData.Documents
	
	// Rebuild word index
	idx.wordIndex = make(map[string]map[string]float64)
	for _, doc := range idx.index {
		idx.addToWordIndex(doc)
	}
	
	return nil
}

// getStopWords returns a set of common stop words to exclude from indexing
func getStopWords() map[string]bool {
	stopWords := []string{
		"a", "an", "and", "are", "as", "at", "be", "by", "for", "from",
		"has", "he", "in", "is", "it", "its", "of", "on", "that", "the",
		"to", "was", "will", "with", "the", "this", "but", "they", "have",
		"had", "what", "said", "each", "which", "she", "do", "how", "their",
		"if", "up", "out", "many", "then", "them", "these", "so", "some",
		"her", "would", "make", "like", "into", "him", "time", "two", "more",
		"go", "no", "way", "could", "my", "than", "first", "been", "call",
		"who", "oil", "sit", "now", "find", "down", "day", "did", "get",
		"come", "made", "may", "part",
	}
	
	stopWordSet := make(map[string]bool)
	for _, word := range stopWords {
		stopWordSet[word] = true
	}
	
	return stopWordSet
}