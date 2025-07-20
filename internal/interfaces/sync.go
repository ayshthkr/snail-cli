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