package interfaces

import (
	"context"
	"time"
)

// SyncManager defines the interface for email synchronization
type SyncManager interface {
	// Sync performs bidirectional synchronization with remote server
	Sync(ctx context.Context) (*SyncResult, error)
	
	// SyncIncoming downloads new emails from remote server
	SyncIncoming(ctx context.Context) (*SyncResult, error)
	
	// SyncOutgoing sends queued emails to remote server
	SyncOutgoing(ctx context.Context) (*SyncResult, error)
	
	// GetSyncStatus returns current synchronization status
	GetSyncStatus(ctx context.Context) (*SyncStatus, error)
	
	// QueueEmail adds an email to the outgoing queue
	QueueEmail(ctx context.Context, email *Email) error
	
	// GetQueuedEmails returns emails waiting to be sent
	GetQueuedEmails(ctx context.Context) ([]*Email, error)
	
	// SetOfflineMode enables or disables offline mode
	SetOfflineMode(enabled bool)
	
	// IsOnline returns true if connected to remote server
	IsOnline() bool
}

// SyncResult contains the results of a sync operation
type SyncResult struct {
	EmailsDownloaded int
	EmailsSent       int
	EmailsDeleted    int
	EmailsUpdated    int
	Conflicts        []SyncConflict
	Errors           []error
	Duration         time.Duration
}

// SyncStatus represents the current sync state
type SyncStatus struct {
	LastSync         *time.Time
	IsOnline         bool
	QueuedEmails     int
	PendingConflicts int
	NextSyncAt       *time.Time
}

// SyncConflict represents a synchronization conflict
type SyncConflict struct {
	EmailID     string
	ConflictType ConflictType
	LocalEmail  *Email
	RemoteEmail *Email
	Resolution  ConflictResolution
}

// ConflictType defines types of sync conflicts
type ConflictType string

const (
	ConflictModified ConflictType = "modified"
	ConflictDeleted  ConflictType = "deleted"
	ConflictMoved    ConflictType = "moved"
)

// ConflictResolution defines how conflicts should be resolved
type ConflictResolution string

const (
	ResolutionLocal  ConflictResolution = "local"
	ResolutionRemote ConflictResolution = "remote"
	ResolutionMerge  ConflictResolution = "merge"
	ResolutionManual ConflictResolution = "manual"
)

// SyncStateManager defines the interface for managing IMAP sync state
type SyncStateManager interface {
	// LoadState loads the sync state for a folder
	LoadState(folderName string) (*SyncState, error)
	
	// SaveState saves the sync state for a folder
	SaveState(state *SyncState) error
	
	// UpdateState updates the sync state based on folder info
	UpdateState(folderName string, folderInfo *FolderInfo) (*SyncState, error)
	
	// MarkFolderDeleted marks a folder as deleted
	MarkFolderDeleted(folderName string) error
	
	// GetSyncRange returns the UID range that needs to be synced
	GetSyncRange(folderName string, folderInfo *FolderInfo) (*UIDRange, bool, error)
	
	// UpdateLastUID updates the last synced UID
	UpdateLastUID(folderName string, uid uint32) error
	
	// DetectConflicts detects synchronization conflicts
	DetectConflicts(folderName string, localEmails []*Email, remoteEmails []*Email) ([]SyncConflict, error)
	
	// ResolveConflict resolves a synchronization conflict
	ResolveConflict(conflict *SyncConflict, resolution ConflictResolution) (*Email, error)
	
	// ListStates returns all sync states
	ListStates() ([]*SyncState, error)
	
	// CleanupDeletedFolders removes sync states for folders that no longer exist
	CleanupDeletedFolders(existingFolders []string) error
	
	// Reset resets all sync states (useful for testing)
	Reset() error
}

// SyncState represents the synchronization state for an IMAP folder
type SyncState struct {
	FolderName    string    `json:"folder_name"`
	UIDValidity   uint32    `json:"uid_validity"`
	LastUID       uint32    `json:"last_uid"`
	LastSyncTime  time.Time `json:"last_sync_time"`
	MessageCount  uint32    `json:"message_count"`
	RecentCount   uint32    `json:"recent_count"`
	UnseenCount   uint32    `json:"unseen_count"`
	UIDNext       uint32    `json:"uid_next"`
	Exists        bool      `json:"exists"`
	DeletedUIDs   []uint32  `json:"deleted_uids,omitempty"`
	ModifiedUIDs  []uint32  `json:"modified_uids,omitempty"`
}