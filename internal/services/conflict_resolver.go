package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"snail-cli/internal/interfaces"
)

// ConflictResolver handles synchronization conflict resolution
type ConflictResolver struct {
	repository   interfaces.EmailRepository
	stateManager interfaces.SyncStateManager
}

// NewConflictResolver creates a new conflict resolver
func NewConflictResolver(repository interfaces.EmailRepository, stateManager interfaces.SyncStateManager) *ConflictResolver {
	return &ConflictResolver{
		repository:   repository,
		stateManager: stateManager,
	}
}

// ResolveConflict resolves a synchronization conflict based on the specified resolution strategy
func (cr *ConflictResolver) ResolveConflict(conflict *interfaces.SyncConflict, resolution interfaces.ConflictResolution) (*interfaces.Email, error) {
	if conflict == nil {
		return nil, fmt.Errorf("conflict cannot be nil")
	}
	
	log.Printf("Resolving conflict for email %s (type: %s, resolution: %s)", 
		conflict.EmailID, conflict.ConflictType, resolution)
	
	switch resolution {
	case interfaces.ResolutionLocal:
		return cr.resolveWithLocal(conflict)
	case interfaces.ResolutionRemote:
		return cr.resolveWithRemote(conflict)
	case interfaces.ResolutionMerge:
		return cr.resolveWithMerge(conflict)
	case interfaces.ResolutionManual:
		return cr.resolveManually(conflict)
	default:
		return nil, fmt.Errorf("unsupported conflict resolution strategy: %s", resolution)
	}
}

// resolveWithLocal resolves conflict by keeping the local version
func (cr *ConflictResolver) resolveWithLocal(conflict *interfaces.SyncConflict) (*interfaces.Email, error) {
	if conflict.LocalEmail == nil {
		return nil, fmt.Errorf("local email is nil for conflict %s", conflict.EmailID)
	}
	
	log.Printf("Resolving conflict %s with local version", conflict.EmailID)
	
	// Return local email as-is
	resolvedEmail := *conflict.LocalEmail
	resolvedEmail.Headers = cr.addResolutionMetadata(resolvedEmail.Headers, "local", time.Now())
	
	return &resolvedEmail, nil
}

// resolveWithRemote resolves conflict by keeping the remote version
func (cr *ConflictResolver) resolveWithRemote(conflict *interfaces.SyncConflict) (*interfaces.Email, error) {
	if conflict.RemoteEmail == nil {
		// Remote email is nil - this means it was deleted remotely
		return cr.handleRemoteDeleted(conflict)
	}
	
	log.Printf("Resolving conflict %s with remote version", conflict.EmailID)
	
	// Return remote email as-is
	resolvedEmail := *conflict.RemoteEmail
	resolvedEmail.Headers = cr.addResolutionMetadata(resolvedEmail.Headers, "remote", time.Now())
	
	return &resolvedEmail, nil
}

// resolveWithMerge resolves conflict by merging local and remote changes
func (cr *ConflictResolver) resolveWithMerge(conflict *interfaces.SyncConflict) (*interfaces.Email, error) {
	log.Printf("Resolving conflict %s with merge strategy", conflict.EmailID)
	
	if conflict.LocalEmail == nil && conflict.RemoteEmail == nil {
		return nil, fmt.Errorf("both local and remote emails are nil for conflict %s", conflict.EmailID)
	}
	
	if conflict.LocalEmail == nil {
		return cr.resolveWithRemote(conflict)
	}
	
	if conflict.RemoteEmail == nil {
		return cr.resolveWithLocal(conflict)
	}
	
	// Perform intelligent merge based on conflict type
	switch conflict.ConflictType {
	case interfaces.ConflictModified:
		return cr.mergeModifiedEmails(conflict.LocalEmail, conflict.RemoteEmail)
	case interfaces.ConflictMoved:
		return cr.mergeMovedEmails(conflict.LocalEmail, conflict.RemoteEmail)
	case interfaces.ConflictDeleted:
		return cr.mergeDeletedEmails(conflict.LocalEmail, conflict.RemoteEmail)
	default:
		// Default merge strategy
		return cr.mergeModifiedEmails(conflict.LocalEmail, conflict.RemoteEmail)
	}
}

// resolveManually handles manual conflict resolution (placeholder for interactive resolution)
func (cr *ConflictResolver) resolveManually(conflict *interfaces.SyncConflict) (*interfaces.Email, error) {
	log.Printf("Manual resolution required for conflict %s", conflict.EmailID)
	
	// For now, we'll use a simple heuristic-based resolution
	// In a full implementation, this would prompt the user or use a more sophisticated strategy
	
	// If it's a read status conflict, prefer read status
	if conflict.LocalEmail != nil && conflict.RemoteEmail != nil {
		if conflict.LocalEmail.Status == interfaces.StatusRead && conflict.RemoteEmail.Status == interfaces.StatusUnread {
			return cr.resolveWithLocal(conflict)
		}
		if conflict.RemoteEmail.Status == interfaces.StatusRead && conflict.LocalEmail.Status == interfaces.StatusUnread {
			return cr.resolveWithRemote(conflict)
		}
	}
	
	// Default to merge strategy for manual resolution
	return cr.resolveWithMerge(conflict)
}

// handleRemoteDeleted handles the case where an email was deleted remotely
func (cr *ConflictResolver) handleRemoteDeleted(conflict *interfaces.SyncConflict) (*interfaces.Email, error) {
	if conflict.LocalEmail == nil {
		return nil, fmt.Errorf("both local and remote emails are nil")
	}
	
	log.Printf("Email %s was deleted remotely, keeping local copy with deleted flag", conflict.EmailID)
	
	// Keep local email but mark it as potentially deleted
	resolvedEmail := *conflict.LocalEmail
	resolvedEmail.Labels = cr.addLabel(resolvedEmail.Labels, "remote-deleted")
	resolvedEmail.Headers = cr.addResolutionMetadata(resolvedEmail.Headers, "remote-deleted", time.Now())
	
	return &resolvedEmail, nil
}

// mergeModifiedEmails merges two emails that have been modified
func (cr *ConflictResolver) mergeModifiedEmails(local, remote *interfaces.Email) (*interfaces.Email, error) {
	log.Printf("Merging modified emails for %s", local.ID)
	
	// Start with remote email as base (server is authoritative for most fields)
	merged := *remote
	
	// Merge read status - if either is read, mark as read
	if local.Status == interfaces.StatusRead || remote.Status == interfaces.StatusRead {
		merged.Status = interfaces.StatusRead
	}
	
	// Merge labels - union of both sets
	merged.Labels = cr.mergeLabels(local.Labels, remote.Labels)
	
	// Add resolution metadata
	merged.Headers = cr.addResolutionMetadata(merged.Headers, "merged", time.Now())
	
	return &merged, nil
}

// mergeMovedEmails merges emails that have been moved between folders
func (cr *ConflictResolver) mergeMovedEmails(local, remote *interfaces.Email) (*interfaces.Email, error) {
	log.Printf("Merging moved emails for %s", local.ID)
	
	// For moved emails, prefer remote location but keep local read status
	merged := *remote
	
	// Keep local read status if it's more advanced
	if local.Status == interfaces.StatusRead && remote.Status == interfaces.StatusUnread {
		merged.Status = interfaces.StatusRead
	}
	
	// Merge labels from both versions
	merged.Labels = cr.mergeLabels(local.Labels, remote.Labels)
	
	// Add resolution metadata
	merged.Headers = cr.addResolutionMetadata(merged.Headers, "moved-merged", time.Now())
	
	return &merged, nil
}

// mergeDeletedEmails handles emails that have been deleted in one location
func (cr *ConflictResolver) mergeDeletedEmails(local, remote *interfaces.Email) (*interfaces.Email, error) {
	log.Printf("Merging deleted emails for %s", local.ID)
	
	// If remote is nil, email was deleted remotely
	if remote == nil {
		return cr.handleRemoteDeleted(&interfaces.SyncConflict{
			EmailID:    local.ID,
			LocalEmail: local,
		})
	}
	
	// If local is nil, email was deleted locally - prefer remote
	if local == nil {
		merged := *remote
		merged.Headers = cr.addResolutionMetadata(merged.Headers, "restored-from-remote", time.Now())
		return &merged, nil
	}
	
	// Both exist - this shouldn't happen for deleted conflicts
	return cr.mergeModifiedEmails(local, remote)
}

// mergeLabels merges two sets of labels, removing duplicates
func (cr *ConflictResolver) mergeLabels(localLabels, remoteLabels []string) []string {
	labelSet := make(map[string]bool)
	
	// Add local labels
	for _, label := range localLabels {
		if label != "" {
			labelSet[strings.ToLower(label)] = true
		}
	}
	
	// Add remote labels
	for _, label := range remoteLabels {
		if label != "" {
			labelSet[strings.ToLower(label)] = true
		}
	}
	
	// Convert back to slice
	merged := make([]string, 0, len(labelSet))
	for label := range labelSet {
		merged = append(merged, label)
	}
	
	return merged
}

// addLabel adds a label to a list if it doesn't already exist
func (cr *ConflictResolver) addLabel(labels []string, newLabel string) []string {
	// Check if label already exists
	for _, label := range labels {
		if strings.EqualFold(label, newLabel) {
			return labels
		}
	}
	
	// Add new label
	return append(labels, newLabel)
}

// addResolutionMetadata adds metadata about conflict resolution to email headers
func (cr *ConflictResolver) addResolutionMetadata(headers map[string]string, resolution string, timestamp time.Time) map[string]string {
	if headers == nil {
		headers = make(map[string]string)
	}
	
	headers["X-Snail-Conflict-Resolution"] = resolution
	headers["X-Snail-Conflict-Resolved-At"] = timestamp.Format(time.RFC3339)
	
	return headers
}

// BatchResolveConflicts resolves multiple conflicts using the same strategy
func (cr *ConflictResolver) BatchResolveConflicts(conflicts []interfaces.SyncConflict, resolution interfaces.ConflictResolution) ([]*interfaces.Email, []error) {
	resolvedEmails := make([]*interfaces.Email, 0, len(conflicts))
	errors := make([]error, 0)
	
	for i := range conflicts {
		resolvedEmail, err := cr.ResolveConflict(&conflicts[i], resolution)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to resolve conflict %s: %w", conflicts[i].EmailID, err))
			continue
		}
		
		resolvedEmails = append(resolvedEmails, resolvedEmail)
	}
	
	return resolvedEmails, errors
}

// AnalyzeConflict analyzes a conflict and suggests the best resolution strategy
func (cr *ConflictResolver) AnalyzeConflict(conflict *interfaces.SyncConflict) interfaces.ConflictResolution {
	if conflict == nil {
		return interfaces.ResolutionManual
	}
	
	// If one side is nil, prefer the existing one
	if conflict.LocalEmail == nil {
		return interfaces.ResolutionRemote
	}
	if conflict.RemoteEmail == nil {
		return interfaces.ResolutionLocal
	}
	
	// Analyze based on conflict type
	switch conflict.ConflictType {
	case interfaces.ConflictModified:
		// For modified emails, check what changed
		if cr.isOnlyReadStatusChange(conflict.LocalEmail, conflict.RemoteEmail) {
			// If only read status changed, merge to keep read status
			return interfaces.ResolutionMerge
		}
		// For other modifications, prefer remote (server is authoritative)
		return interfaces.ResolutionRemote
		
	case interfaces.ConflictMoved:
		// For moved emails, merge to preserve both location and read status
		return interfaces.ResolutionMerge
		
	case interfaces.ConflictDeleted:
		// For deleted emails, need manual decision
		return interfaces.ResolutionManual
		
	default:
		return interfaces.ResolutionMerge
	}
}

// isOnlyReadStatusChange checks if the only difference between emails is read status
func (cr *ConflictResolver) isOnlyReadStatusChange(local, remote *interfaces.Email) bool {
	if local == nil || remote == nil {
		return false
	}
	
	// Check if only status is different
	if local.Status != remote.Status {
		// Check if all other fields are the same
		if local.Subject == remote.Subject &&
			local.MessageID == remote.MessageID &&
			len(local.Labels) == len(remote.Labels) {
			
			// Check labels are the same
			localLabelSet := make(map[string]bool)
			for _, label := range local.Labels {
				localLabelSet[label] = true
			}
			
			for _, label := range remote.Labels {
				if !localLabelSet[label] {
					return false
				}
			}
			
			return true
		}
	}
	
	return false
}

// GetConflictSummary returns a human-readable summary of a conflict
func (cr *ConflictResolver) GetConflictSummary(conflict *interfaces.SyncConflict) string {
	if conflict == nil {
		return "Unknown conflict"
	}
	
	switch conflict.ConflictType {
	case interfaces.ConflictModified:
		return fmt.Sprintf("Email '%s' was modified both locally and remotely", 
			cr.getEmailSubject(conflict.LocalEmail, conflict.RemoteEmail))
			
	case interfaces.ConflictMoved:
		return fmt.Sprintf("Email '%s' was moved to different folders", 
			cr.getEmailSubject(conflict.LocalEmail, conflict.RemoteEmail))
			
	case interfaces.ConflictDeleted:
		if conflict.RemoteEmail == nil {
			return fmt.Sprintf("Email '%s' was deleted remotely but modified locally", 
				cr.getEmailSubject(conflict.LocalEmail, nil))
		}
		return fmt.Sprintf("Email '%s' was deleted locally but modified remotely", 
			cr.getEmailSubject(nil, conflict.RemoteEmail))
			
	default:
		return fmt.Sprintf("Unknown conflict type for email '%s'", 
			cr.getEmailSubject(conflict.LocalEmail, conflict.RemoteEmail))
	}
}

// getEmailSubject returns the subject of an email, preferring local over remote
func (cr *ConflictResolver) getEmailSubject(local, remote *interfaces.Email) string {
	if local != nil && local.Subject != "" {
		return local.Subject
	}
	if remote != nil && remote.Subject != "" {
		return remote.Subject
	}
	return "Unknown Subject"
}

// ValidateResolution checks if a resolution is valid for a given conflict
func (cr *ConflictResolver) ValidateResolution(conflict *interfaces.SyncConflict, resolution interfaces.ConflictResolution) error {
	if conflict == nil {
		return fmt.Errorf("conflict cannot be nil")
	}
	
	switch resolution {
	case interfaces.ResolutionLocal:
		if conflict.LocalEmail == nil {
			return fmt.Errorf("cannot resolve with local version: local email is nil")
		}
		
	case interfaces.ResolutionRemote:
		if conflict.RemoteEmail == nil && conflict.ConflictType != interfaces.ConflictDeleted {
			return fmt.Errorf("cannot resolve with remote version: remote email is nil")
		}
		
	case interfaces.ResolutionMerge:
		if conflict.LocalEmail == nil && conflict.RemoteEmail == nil {
			return fmt.Errorf("cannot merge: both local and remote emails are nil")
		}
		
	case interfaces.ResolutionManual:
		// Manual resolution is always valid
		
	default:
		return fmt.Errorf("unknown resolution strategy: %s", resolution)
	}
	
	return nil
}