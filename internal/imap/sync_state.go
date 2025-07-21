package imap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
)

// SyncStateManager manages synchronization state for IMAP folders
type SyncStateManager struct {
	stateDir string
	states   map[string]*interfaces.SyncState
	mu       sync.RWMutex
}

// NewSyncStateManager creates a new sync state manager
func NewSyncStateManager(stateDir string) *SyncStateManager {
	return &SyncStateManager{
		stateDir: stateDir,
		states:   make(map[string]*interfaces.SyncState),
	}
}

// LoadState loads the sync state for a folder
func (ssm *SyncStateManager) LoadState(folderName string) (*interfaces.SyncState, error) {
	ssm.mu.RLock()
	if state, exists := ssm.states[folderName]; exists {
		ssm.mu.RUnlock()
		return state, nil
	}
	ssm.mu.RUnlock()

	// Try to load from disk
	statePath := ssm.getStatePath(folderName)
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create new state
			state := &interfaces.SyncState{
				FolderName:   folderName,
				UIDValidity:  0,
				LastUID:      0,
				LastSyncTime: time.Time{},
				Exists:       false,
				DeletedUIDs:  make([]uint32, 0),
				ModifiedUIDs: make([]uint32, 0),
			}
			ssm.mu.Lock()
			ssm.states[folderName] = state
			ssm.mu.Unlock()
			return state, nil
		}
		return nil, fmt.Errorf("failed to read sync state file: %w", err)
	}

	var state interfaces.SyncState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse sync state: %w", err)
	}

	ssm.mu.Lock()
	ssm.states[folderName] = &state
	ssm.mu.Unlock()

	return &state, nil
}

// SaveState saves the sync state for a folder
func (ssm *SyncStateManager) SaveState(state *interfaces.SyncState) error {
	if state == nil {
		return fmt.Errorf("state cannot be nil")
	}

	// Ensure state directory exists
	if err := os.MkdirAll(ssm.stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	// Update in-memory state
	ssm.mu.Lock()
	ssm.states[state.FolderName] = state
	ssm.mu.Unlock()

	// Save to disk
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sync state: %w", err)
	}

	statePath := ssm.getStatePath(state.FolderName)
	if err := os.WriteFile(statePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write sync state file: %w", err)
	}

	return nil
}

// UpdateState updates the sync state based on folder info
func (ssm *SyncStateManager) UpdateState(folderName string, folderInfo *interfaces.FolderInfo) (*interfaces.SyncState, error) {
	state, err := ssm.LoadState(folderName)
	if err != nil {
		return nil, fmt.Errorf("failed to load sync state: %w", err)
	}

	// Check if this is the first sync or if UID validity changed
	if state.UIDValidity == 0 || state.UIDValidity != folderInfo.UIDValidity {
		// UID validity changed - need full resync
		state.UIDValidity = folderInfo.UIDValidity
		state.LastUID = 0
		state.DeletedUIDs = make([]uint32, 0)
		state.ModifiedUIDs = make([]uint32, 0)
	}

	// Update folder information
	state.MessageCount = folderInfo.MessageCount
	state.RecentCount = folderInfo.RecentCount
	state.UnseenCount = folderInfo.UnseenCount
	state.UIDNext = folderInfo.UIDNext
	state.Exists = true
	state.LastSyncTime = time.Now()

	if err := ssm.SaveState(state); err != nil {
		return nil, fmt.Errorf("failed to save sync state: %w", err)
	}

	return state, nil
}

// MarkFolderDeleted marks a folder as deleted
func (ssm *SyncStateManager) MarkFolderDeleted(folderName string) error {
	state, err := ssm.LoadState(folderName)
	if err != nil {
		return fmt.Errorf("failed to load sync state: %w", err)
	}

	state.Exists = false
	state.LastSyncTime = time.Now()

	if err := ssm.SaveState(state); err != nil {
		return fmt.Errorf("failed to save sync state: %w", err)
	}

	return nil
}

// GetSyncRange returns the UID range that needs to be synced
func (ssm *SyncStateManager) GetSyncRange(folderName string, folderInfo *interfaces.FolderInfo) (*interfaces.UIDRange, bool, error) {
	state, err := ssm.LoadState(folderName)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load sync state: %w", err)
	}

	// Check if UID validity changed
	needsFullSync := false
	if state.UIDValidity == 0 || state.UIDValidity != folderInfo.UIDValidity {
		needsFullSync = true
		return &interfaces.UIDRange{Start: 1, End: 0}, needsFullSync, nil
	}

	// If no messages in folder, no sync needed
	if folderInfo.MessageCount == 0 {
		return nil, needsFullSync, nil
	}

	// If this is the first sync, fetch all
	if state.LastUID == 0 {
		return &interfaces.UIDRange{Start: 1, End: 0}, needsFullSync, nil
	}

	// Incremental sync - fetch messages after last known UID
	if folderInfo.UIDNext > state.LastUID+1 {
		return &interfaces.UIDRange{Start: state.LastUID + 1, End: 0}, needsFullSync, nil
	}

	// No new messages
	return nil, needsFullSync, nil
}

// UpdateLastUID updates the last synced UID
func (ssm *SyncStateManager) UpdateLastUID(folderName string, uid uint32) error {
	state, err := ssm.LoadState(folderName)
	if err != nil {
		return fmt.Errorf("failed to load sync state: %w", err)
	}

	if uid > state.LastUID {
		state.LastUID = uid
		state.LastSyncTime = time.Now()

		if err := ssm.SaveState(state); err != nil {
			return fmt.Errorf("failed to save sync state: %w", err)
		}
	}

	return nil
}

// DetectConflicts detects synchronization conflicts
func (ssm *SyncStateManager) DetectConflicts(folderName string, localEmails []*interfaces.Email, remoteEmails []*interfaces.Email) ([]interfaces.SyncConflict, error) {
	conflicts := make([]interfaces.SyncConflict, 0)

	// Create maps for efficient lookup
	localMap := make(map[string]*interfaces.Email)
	remoteMap := make(map[string]*interfaces.Email)

	for _, email := range localEmails {
		if email.MessageID != "" {
			localMap[email.MessageID] = email
		}
	}

	for _, email := range remoteEmails {
		if email.MessageID != "" {
			remoteMap[email.MessageID] = email
		}
	}

	// Check for conflicts
	for messageID, localEmail := range localMap {
		if remoteEmail, exists := remoteMap[messageID]; exists {
			// Email exists in both local and remote
			conflict := ssm.compareEmails(localEmail, remoteEmail)
			if conflict != nil {
				conflicts = append(conflicts, *conflict)
			}
		} else {
			// Email exists locally but not remotely - might be deleted remotely
			conflicts = append(conflicts, interfaces.SyncConflict{
				EmailID:      localEmail.ID,
				ConflictType: interfaces.ConflictDeleted,
				LocalEmail:   localEmail,
				RemoteEmail:  nil,
				Resolution:   interfaces.ResolutionManual,
			})
		}
	}

	return conflicts, nil
}

// compareEmails compares local and remote emails for conflicts
func (ssm *SyncStateManager) compareEmails(local, remote *interfaces.Email) *interfaces.SyncConflict {
	// Check if emails are different
	if ssm.emailsEqual(local, remote) {
		return nil
	}

	// Determine conflict type
	conflictType := interfaces.ConflictModified
	if len(local.Labels) != len(remote.Labels) {
		// Labels changed
		conflictType = interfaces.ConflictMoved
	}

	return &interfaces.SyncConflict{
		EmailID:      local.ID,
		ConflictType: conflictType,
		LocalEmail:   local,
		RemoteEmail:  remote,
		Resolution:   interfaces.ResolutionRemote, // Default to remote wins
	}
}

// emailsEqual compares two emails for equality
func (ssm *SyncStateManager) emailsEqual(email1, email2 *interfaces.Email) bool {
	if email1.Status != email2.Status {
		return false
	}

	// Compare labels
	if len(email1.Labels) != len(email2.Labels) {
		return false
	}

	labelMap := make(map[string]bool)
	for _, label := range email1.Labels {
		labelMap[label] = true
	}

	for _, label := range email2.Labels {
		if !labelMap[label] {
			return false
		}
	}

	return true
}

// ResolveConflict resolves a synchronization conflict
func (ssm *SyncStateManager) ResolveConflict(conflict *interfaces.SyncConflict, resolution interfaces.ConflictResolution) (*interfaces.Email, error) {
	switch resolution {
	case interfaces.ResolutionLocal:
		return conflict.LocalEmail, nil
	case interfaces.ResolutionRemote:
		return conflict.RemoteEmail, nil
	case interfaces.ResolutionMerge:
		return ssm.mergeEmails(conflict.LocalEmail, conflict.RemoteEmail)
	default:
		return nil, fmt.Errorf("unsupported conflict resolution: %s", resolution)
	}
}

// mergeEmails merges two conflicting emails
func (ssm *SyncStateManager) mergeEmails(local, remote *interfaces.Email) (*interfaces.Email, error) {
	if local == nil {
		return remote, nil
	}
	if remote == nil {
		return local, nil
	}

	// Create merged email based on remote with local modifications
	merged := *remote

	// Merge labels
	labelSet := make(map[string]bool)
	for _, label := range local.Labels {
		labelSet[label] = true
	}
	for _, label := range remote.Labels {
		labelSet[label] = true
	}

	merged.Labels = make([]string, 0, len(labelSet))
	for label := range labelSet {
		merged.Labels = append(merged.Labels, label)
	}

	// Use local status if it's more recent (read status)
	if local.Status == interfaces.StatusRead && remote.Status == interfaces.StatusUnread {
		merged.Status = interfaces.StatusRead
	}

	return &merged, nil
}

// ListStates returns all sync states
func (ssm *SyncStateManager) ListStates() ([]*interfaces.SyncState, error) {
	ssm.mu.RLock()
	defer ssm.mu.RUnlock()

	states := make([]*interfaces.SyncState, 0, len(ssm.states))
	for _, state := range ssm.states {
		// Return copy to prevent external modification
		stateCopy := *state
		states = append(states, &stateCopy)
	}

	return states, nil
}

// CleanupDeletedFolders removes sync states for folders that no longer exist
func (ssm *SyncStateManager) CleanupDeletedFolders(existingFolders []string) error {
	folderSet := make(map[string]bool)
	for _, folder := range existingFolders {
		folderSet[folder] = true
	}

	// Get list of states to update (without holding lock)
	ssm.mu.RLock()
	statesToUpdate := make([]*interfaces.SyncState, 0)
	for folderName, state := range ssm.states {
		if !folderSet[folderName] && state.Exists {
			statesToUpdate = append(statesToUpdate, state)
		}
	}
	ssm.mu.RUnlock()

	// Update states (SaveState will handle its own locking)
	for _, state := range statesToUpdate {
		state.Exists = false
		state.LastSyncTime = time.Now()

		if err := ssm.SaveState(state); err != nil {
			return fmt.Errorf("failed to update deleted folder state: %w", err)
		}
	}

	return nil
}

// getStatePath returns the file path for a folder's sync state
func (ssm *SyncStateManager) getStatePath(folderName string) string {
	// Sanitize folder name for file system
	safeName := filepath.Base(folderName)
	safeName = fmt.Sprintf("%s.json", safeName)
	return filepath.Join(ssm.stateDir, safeName)
}

// GetStateDir returns the state directory path
func (ssm *SyncStateManager) GetStateDir() string {
	return ssm.stateDir
}

// Reset resets all sync states (useful for testing)
func (ssm *SyncStateManager) Reset() error {
	ssm.mu.Lock()
	defer ssm.mu.Unlock()

	ssm.states = make(map[string]*interfaces.SyncState)

	// Remove all state files
	if err := os.RemoveAll(ssm.stateDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove state directory: %w", err)
	}

	return nil
}