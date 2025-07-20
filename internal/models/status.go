package models

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// StatusManager handles email status and label operations
type StatusManager struct{}

// NewStatusManager creates a new status manager
func NewStatusManager() *StatusManager {
	return &StatusManager{}
}

// UpdateStatus updates the status of an email
func (sm *StatusManager) UpdateStatus(email *Email, status EmailStatus) error {
	if !status.IsValid() {
		return fmt.Errorf("invalid email status: %s", status)
	}
	
	email.Status = status
	return nil
}

// AddLabel adds a label to an email if it doesn't already exist
func (sm *StatusManager) AddLabel(email *Email, label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}
	
	// Check if label already exists
	for _, existingLabel := range email.Labels {
		if existingLabel == label {
			return nil // Label already exists, no error
		}
	}
	
	email.Labels = append(email.Labels, label)
	sort.Strings(email.Labels) // Keep labels sorted
	return nil
}

// RemoveLabel removes a label from an email
func (sm *StatusManager) RemoveLabel(email *Email, label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return fmt.Errorf("label cannot be empty")
	}
	
	for i, existingLabel := range email.Labels {
		if existingLabel == label {
			// Remove the label by slicing
			email.Labels = append(email.Labels[:i], email.Labels[i+1:]...)
			return nil
		}
	}
	
	return fmt.Errorf("label '%s' not found", label)
}

// HasLabel checks if an email has a specific label
func (sm *StatusManager) HasLabel(email *Email, label string) bool {
	label = strings.TrimSpace(label)
	for _, existingLabel := range email.Labels {
		if existingLabel == label {
			return true
		}
	}
	return false
}

// SetLabels replaces all labels on an email with the provided labels
func (sm *StatusManager) SetLabels(email *Email, labels []string) error {
	// Validate and clean labels
	cleanLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		label = strings.TrimSpace(label)
		if label == "" {
			continue // Skip empty labels
		}
		
		// Check for duplicates
		found := false
		for _, existing := range cleanLabels {
			if existing == label {
				found = true
				break
			}
		}
		
		if !found {
			cleanLabels = append(cleanLabels, label)
		}
	}
	
	sort.Strings(cleanLabels)
	email.Labels = cleanLabels
	return nil
}

// GetLabels returns a copy of the email's labels
func (sm *StatusManager) GetLabels(email *Email) []string {
	if len(email.Labels) == 0 {
		return []string{}
	}
	
	labels := make([]string, len(email.Labels))
	copy(labels, email.Labels)
	return labels
}

// ClearLabels removes all labels from an email
func (sm *StatusManager) ClearLabels(email *Email) {
	email.Labels = []string{}
}

// EmailMetadata represents persistent metadata for an email
type EmailMetadata struct {
	ID          string            `json:"id"`
	MessageID   string            `json:"message_id"`
	Status      EmailStatus       `json:"status"`
	Labels      []string          `json:"labels"`
	SyncID      string            `json:"sync_id"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	CustomData  map[string]string `json:"custom_data,omitempty"`
}

// MetadataManager handles persistence of email metadata
type MetadataManager struct {
	metadata map[string]*EmailMetadata
}

// NewMetadataManager creates a new metadata manager
func NewMetadataManager() *MetadataManager {
	return &MetadataManager{
		metadata: make(map[string]*EmailMetadata),
	}
}

// SaveMetadata saves email metadata
func (mm *MetadataManager) SaveMetadata(email *Email) error {
	if email.ID == "" {
		return fmt.Errorf("email ID is required")
	}
	
	now := time.Now()
	
	// Check if metadata already exists
	existing, exists := mm.metadata[email.ID]
	
	metadata := &EmailMetadata{
		ID:         email.ID,
		MessageID:  email.MessageID,
		Status:     email.Status,
		Labels:     make([]string, len(email.Labels)),
		SyncID:     email.SyncID,
		UpdatedAt:  now,
		CustomData: make(map[string]string),
	}
	
	// Copy labels
	copy(metadata.Labels, email.Labels)
	
	// Set created time
	if exists {
		metadata.CreatedAt = existing.CreatedAt
		// Preserve custom data
		if existing.CustomData != nil {
			for k, v := range existing.CustomData {
				metadata.CustomData[k] = v
			}
		}
	} else {
		metadata.CreatedAt = now
	}
	
	mm.metadata[email.ID] = metadata
	return nil
}

// LoadMetadata loads email metadata
func (mm *MetadataManager) LoadMetadata(emailID string) (*EmailMetadata, error) {
	if emailID == "" {
		return nil, fmt.Errorf("email ID is required")
	}
	
	metadata, exists := mm.metadata[emailID]
	if !exists {
		return nil, fmt.Errorf("metadata not found for email ID: %s", emailID)
	}
	
	// Return a copy to prevent external modification
	result := &EmailMetadata{
		ID:         metadata.ID,
		MessageID:  metadata.MessageID,
		Status:     metadata.Status,
		Labels:     make([]string, len(metadata.Labels)),
		SyncID:     metadata.SyncID,
		CreatedAt:  metadata.CreatedAt,
		UpdatedAt:  metadata.UpdatedAt,
		CustomData: make(map[string]string),
	}
	
	copy(result.Labels, metadata.Labels)
	
	if metadata.CustomData != nil {
		for k, v := range metadata.CustomData {
			result.CustomData[k] = v
		}
	}
	
	return result, nil
}

// DeleteMetadata removes email metadata
func (mm *MetadataManager) DeleteMetadata(emailID string) error {
	if emailID == "" {
		return fmt.Errorf("email ID is required")
	}
	
	if _, exists := mm.metadata[emailID]; !exists {
		return fmt.Errorf("metadata not found for email ID: %s", emailID)
	}
	
	delete(mm.metadata, emailID)
	return nil
}

// ListMetadata returns all metadata entries
func (mm *MetadataManager) ListMetadata() ([]*EmailMetadata, error) {
	result := make([]*EmailMetadata, 0, len(mm.metadata))
	
	for _, metadata := range mm.metadata {
		// Return copies to prevent external modification
		copy := &EmailMetadata{
			ID:         metadata.ID,
			MessageID:  metadata.MessageID,
			Status:     metadata.Status,
			Labels:     make([]string, len(metadata.Labels)),
			SyncID:     metadata.SyncID,
			CreatedAt:  metadata.CreatedAt,
			UpdatedAt:  metadata.UpdatedAt,
			CustomData: make(map[string]string),
		}
		
		copy.Labels = append(copy.Labels, metadata.Labels...)
		
		if metadata.CustomData != nil {
			for k, v := range metadata.CustomData {
				copy.CustomData[k] = v
			}
		}
		
		result = append(result, copy)
	}
	
	return result, nil
}

// UpdateCustomData updates custom metadata for an email
func (mm *MetadataManager) UpdateCustomData(emailID, key, value string) error {
	if emailID == "" {
		return fmt.Errorf("email ID is required")
	}
	
	if key == "" {
		return fmt.Errorf("key is required")
	}
	
	metadata, exists := mm.metadata[emailID]
	if !exists {
		return fmt.Errorf("metadata not found for email ID: %s", emailID)
	}
	
	if metadata.CustomData == nil {
		metadata.CustomData = make(map[string]string)
	}
	
	metadata.CustomData[key] = value
	metadata.UpdatedAt = time.Now()
	
	return nil
}

// GetCustomData retrieves custom metadata for an email
func (mm *MetadataManager) GetCustomData(emailID, key string) (string, error) {
	if emailID == "" {
		return "", fmt.Errorf("email ID is required")
	}
	
	if key == "" {
		return "", fmt.Errorf("key is required")
	}
	
	metadata, exists := mm.metadata[emailID]
	if !exists {
		return "", fmt.Errorf("metadata not found for email ID: %s", emailID)
	}
	
	if metadata.CustomData == nil {
		return "", fmt.Errorf("custom data key not found: %s", key)
	}
	
	value, exists := metadata.CustomData[key]
	if !exists {
		return "", fmt.Errorf("custom data key not found: %s", key)
	}
	
	return value, nil
}

// ApplyMetadataToEmail applies stored metadata to an email
func (mm *MetadataManager) ApplyMetadataToEmail(email *Email) error {
	if email.ID == "" {
		return fmt.Errorf("email ID is required")
	}
	
	metadata, err := mm.LoadMetadata(email.ID)
	if err != nil {
		return err
	}
	
	email.Status = metadata.Status
	email.Labels = make([]string, len(metadata.Labels))
	copy(email.Labels, metadata.Labels)
	email.SyncID = metadata.SyncID
	
	return nil
}

// ExtractMetadataFromEmail extracts metadata from an email and saves it
func (mm *MetadataManager) ExtractMetadataFromEmail(email *Email) error {
	return mm.SaveMetadata(email)
}