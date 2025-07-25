package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
)

// SyncManager implements bidirectional email synchronization
type SyncManager struct {
	imapClient     interfaces.IMAPClient
	smtpSender     *SMTPSender
	repository     interfaces.EmailRepository
	stateManager   interfaces.SyncStateManager
	conflictResolver *ConflictResolver
	
	// Configuration
	config         *SyncConfig
	
	// State
	isOnline       bool
	lastSync       *time.Time
	mu             sync.RWMutex
	
	// Progress tracking
	progressChan   chan *SyncProgress
	cancelFunc     context.CancelFunc
}

// SyncConfig contains configuration for sync operations
type SyncConfig struct {
	// Sync intervals
	AutoSyncInterval    time.Duration
	RetryInterval       time.Duration
	MaxRetries          int
	
	// Conflict resolution
	DefaultResolution   interfaces.ConflictResolution
	AutoResolveConflicts bool
	
	// Performance
	MaxConcurrentFolders int
	BatchSize           int
	
	// Folders to sync
	FoldersToSync       []string
	ExcludedFolders     []string
}

// SMTPSender wraps SMTP operations for sync manager
type SMTPSender struct {
	client interfaces.SMTPClient
	queue  interfaces.SMTPQueue
}

// SyncProgress represents sync operation progress
type SyncProgress struct {
	Phase           SyncPhase
	FolderName      string
	ProcessedCount  int
	TotalCount      int
	Message         string
	Error           error
	Timestamp       time.Time
}

// SyncPhase represents different phases of synchronization
type SyncPhase string

const (
	PhaseStarting      SyncPhase = "starting"
	PhaseConnecting    SyncPhase = "connecting"
	PhaseFetchingFolders SyncPhase = "fetching_folders"
	PhaseSyncingIncoming SyncPhase = "syncing_incoming"
	PhaseSyncingOutgoing SyncPhase = "syncing_outgoing"
	PhaseResolvingConflicts SyncPhase = "resolving_conflicts"
	PhaseCompleted     SyncPhase = "completed"
	PhaseFailed        SyncPhase = "failed"
)

// NewSyncManager creates a new sync manager
func NewSyncManager(
	imapClient interfaces.IMAPClient,
	smtpClient interfaces.SMTPClient,
	smtpQueue interfaces.SMTPQueue,
	repository interfaces.EmailRepository,
	stateManager interfaces.SyncStateManager,
	config *SyncConfig,
) *SyncManager {
	if config == nil {
		config = DefaultSyncConfig()
	}
	
	return &SyncManager{
		imapClient:     imapClient,
		smtpSender:     &SMTPSender{client: smtpClient, queue: smtpQueue},
		repository:     repository,
		stateManager:   stateManager,
		conflictResolver: NewConflictResolver(repository, stateManager),
		config:         config,
		progressChan:   make(chan *SyncProgress, 100),
	}
}

// DefaultSyncConfig returns default sync configuration
func DefaultSyncConfig() *SyncConfig {
	return &SyncConfig{
		AutoSyncInterval:     15 * time.Minute,
		RetryInterval:        5 * time.Minute,
		MaxRetries:           3,
		DefaultResolution:    interfaces.ResolutionRemote,
		AutoResolveConflicts: true,
		MaxConcurrentFolders: 3,
		BatchSize:           50,
		FoldersToSync:       []string{"INBOX", "Sent", "Drafts"},
		ExcludedFolders:     []string{"Trash", "Spam"},
	}
}

// Sync performs bidirectional synchronization (implements SyncManagerInterface)
func (sm *SyncManager) Sync(ctx context.Context) error {
	result, err := sm.SyncFull(ctx)
	if err != nil {
		return err
	}
	
	// Log sync results
	log.Printf("Sync completed: downloaded=%d, sent=%d, updated=%d, deleted=%d, conflicts=%d", 
		result.EmailsDownloaded, result.EmailsSent, result.EmailsUpdated, 
		result.EmailsDeleted, len(result.Conflicts))
	
	return nil
}

// SyncFull performs bidirectional synchronization and returns detailed results
func (sm *SyncManager) SyncFull(ctx context.Context) (*interfaces.SyncResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	// Create cancellable context
	syncCtx, cancel := context.WithCancel(ctx)
	sm.cancelFunc = cancel
	defer cancel()
	
	result := &interfaces.SyncResult{
		Conflicts: make([]interfaces.SyncConflict, 0),
		Errors:    make([]error, 0),
	}
	
	startTime := time.Now()
	defer func() {
		result.Duration = time.Since(startTime)
		sm.lastSync = &startTime
	}()
	
	sm.sendProgress(syncCtx, &SyncProgress{
		Phase:     PhaseStarting,
		Message:   "Starting synchronization",
		Timestamp: time.Now(),
	})
	
	// Check connectivity
	if err := sm.checkConnectivity(syncCtx); err != nil {
		sm.isOnline = false
		sm.sendProgress(syncCtx, &SyncProgress{
			Phase:     PhaseFailed,
			Message:   "Connectivity check failed",
			Error:     err,
			Timestamp: time.Now(),
		})
		result.Errors = append(result.Errors, err)
		return result, fmt.Errorf("connectivity check failed: %w", err)
	}
	
	sm.isOnline = true
	
	// Sync incoming emails
	incomingResult, err := sm.SyncIncoming(syncCtx)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.EmailsDownloaded += incomingResult.EmailsDownloaded
		result.EmailsUpdated += incomingResult.EmailsUpdated
		result.EmailsDeleted += incomingResult.EmailsDeleted
		result.Conflicts = append(result.Conflicts, incomingResult.Conflicts...)
	}
	
	// Sync outgoing emails
	outgoingResult, err := sm.SyncOutgoing(syncCtx)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.EmailsSent += outgoingResult.EmailsSent
	}
	
	// Resolve conflicts if auto-resolution is enabled
	if sm.config.AutoResolveConflicts && len(result.Conflicts) > 0 {
		sm.sendProgress(syncCtx, &SyncProgress{
			Phase:     PhaseResolvingConflicts,
			Message:   fmt.Sprintf("Resolving %d conflicts", len(result.Conflicts)),
			Timestamp: time.Now(),
		})
		
		resolvedCount := 0
		for i := range result.Conflicts {
			if err := sm.resolveConflict(syncCtx, &result.Conflicts[i]); err != nil {
				result.Errors = append(result.Errors, err)
			} else {
				resolvedCount++
			}
		}
		
		log.Printf("Auto-resolved %d out of %d conflicts", resolvedCount, len(result.Conflicts))
	}
	
	sm.sendProgress(syncCtx, &SyncProgress{
		Phase:     PhaseCompleted,
		Message:   "Synchronization completed",
		Timestamp: time.Now(),
	})
	
	return result, nil
}

// SyncIncoming downloads new emails from remote server
func (sm *SyncManager) SyncIncoming(ctx context.Context) (*interfaces.SyncResult, error) {
	result := &interfaces.SyncResult{
		Conflicts: make([]interfaces.SyncConflict, 0),
		Errors:    make([]error, 0),
	}
	
	sm.sendProgress(ctx, &SyncProgress{
		Phase:     PhaseFetchingFolders,
		Message:   "Fetching folder list",
		Timestamp: time.Now(),
	})
	
	// Get list of folders to sync
	folders, err := sm.imapClient.ListFolders(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to list folders: %w", err)
	}
	
	// Filter folders based on configuration
	foldersToSync := sm.filterFolders(folders)
	
	sm.sendProgress(ctx, &SyncProgress{
		Phase:      PhaseSyncingIncoming,
		Message:    fmt.Sprintf("Syncing %d folders", len(foldersToSync)),
		TotalCount: len(foldersToSync),
		Timestamp:  time.Now(),
	})
	
	// Sync each folder
	for i, folder := range foldersToSync {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		
		sm.sendProgress(ctx, &SyncProgress{
			Phase:          PhaseSyncingIncoming,
			FolderName:     folder.Name,
			ProcessedCount: i,
			TotalCount:     len(foldersToSync),
			Message:        fmt.Sprintf("Syncing folder: %s", folder.Name),
			Timestamp:      time.Now(),
		})
		
		folderResult, err := sm.syncFolder(ctx, folder)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to sync folder %s: %w", folder.Name, err))
			continue
		}
		
		result.EmailsDownloaded += folderResult.EmailsDownloaded
		result.EmailsUpdated += folderResult.EmailsUpdated
		result.EmailsDeleted += folderResult.EmailsDeleted
		result.Conflicts = append(result.Conflicts, folderResult.Conflicts...)
	}
	
	return result, nil
}

// SyncOutgoing sends queued emails to remote server
func (sm *SyncManager) SyncOutgoing(ctx context.Context) (*interfaces.SyncResult, error) {
	result := &interfaces.SyncResult{
		Errors: make([]error, 0),
	}
	
	sm.sendProgress(ctx, &SyncProgress{
		Phase:     PhaseSyncingOutgoing,
		Message:   "Sending queued emails",
		Timestamp: time.Now(),
	})
	
	// Get queued emails
	queuedEmails, err := sm.smtpSender.queue.List(ctx)
	if err != nil {
		return result, fmt.Errorf("failed to get queued emails: %w", err)
	}
	
	if len(queuedEmails) == 0 {
		return result, nil
	}
	
	sm.sendProgress(ctx, &SyncProgress{
		Phase:      PhaseSyncingOutgoing,
		Message:    fmt.Sprintf("Sending %d queued emails", len(queuedEmails)),
		TotalCount: len(queuedEmails),
		Timestamp:  time.Now(),
	})
	
	// Process each queued email
	for i, queuedEmail := range queuedEmails {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
		
		sm.sendProgress(ctx, &SyncProgress{
			Phase:          PhaseSyncingOutgoing,
			ProcessedCount: i,
			TotalCount:     len(queuedEmails),
			Message:        fmt.Sprintf("Sending email: %s", queuedEmail.Email.Subject),
			Timestamp:      time.Now(),
		})
		
		// Try to send the email
		if err := sm.smtpSender.client.SendEmail(ctx, queuedEmail.Email); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to send email %s: %w", queuedEmail.Email.ID, err))
			continue
		}
		
		// Remove from queue on successful send
		if err := sm.smtpSender.queue.Remove(ctx, queuedEmail.Email.ID); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to remove sent email from queue: %w", err))
		}
		
		// Update email status in repository
		queuedEmail.Email.Status = interfaces.StatusSent
		if err := sm.repository.Update(ctx, queuedEmail.Email); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("failed to update sent email status: %w", err))
		}
		
		result.EmailsSent++
	}
	
	return result, nil
}

// syncFolder synchronizes a single folder
func (sm *SyncManager) syncFolder(ctx context.Context, folder *interfaces.FolderInfo) (*interfaces.SyncResult, error) {
	result := &interfaces.SyncResult{
		Conflicts: make([]interfaces.SyncConflict, 0),
		Errors:    make([]error, 0),
	}
	
	// Update sync state
	syncState, err := sm.stateManager.UpdateState(folder.Name, folder)
	if err != nil {
		return result, fmt.Errorf("failed to update sync state: %w", err)
	}
	
	// Get sync range
	syncRange, needsFullSync, err := sm.stateManager.GetSyncRange(folder.Name, folder)
	if err != nil {
		return result, fmt.Errorf("failed to get sync range: %w", err)
	}
	
	if syncRange == nil {
		// No new messages to sync
		return result, nil
	}
	
	// Fetch emails from server
	remoteEmails, err := sm.imapClient.FetchEmails(ctx, folder.Name, syncRange)
	if err != nil {
		return result, fmt.Errorf("failed to fetch emails: %w", err)
	}
	
	// Get local emails for conflict detection
	var localEmails []*interfaces.Email
	if !needsFullSync {
		localEmails, err = sm.repository.GetEmailsByFolder(ctx, folder.Name)
		if err != nil {
			return result, fmt.Errorf("failed to get local emails: %w", err)
		}
	}
	
	// Detect conflicts
	conflicts, err := sm.stateManager.DetectConflicts(folder.Name, localEmails, remoteEmails)
	if err != nil {
		return result, fmt.Errorf("failed to detect conflicts: %w", err)
	}
	
	result.Conflicts = conflicts
	
	// Process remote emails
	for _, email := range remoteEmails {
		// Check if email already exists locally
		existingEmail, err := sm.repository.GetByMessageID(ctx, email.MessageID)
		if err != nil && err != interfaces.ErrEmailNotFound {
			result.Errors = append(result.Errors, fmt.Errorf("failed to check existing email: %w", err))
			continue
		}
		
		if existingEmail == nil {
			// New email - store it
			if err := sm.repository.Store(ctx, email); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to store email: %w", err))
				continue
			}
			result.EmailsDownloaded++
		} else {
			// Email exists - check for updates
			if sm.emailNeedsUpdate(existingEmail, email) {
				if err := sm.repository.Update(ctx, email); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("failed to update email: %w", err))
					continue
				}
				result.EmailsUpdated++
			}
		}
		
		// Update last UID
		if email.UID > syncState.LastUID {
			if err := sm.stateManager.UpdateLastUID(folder.Name, email.UID); err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("failed to update last UID: %w", err))
			}
		}
	}
	
	return result, nil
}

// emailNeedsUpdate checks if a local email needs to be updated with remote changes
func (sm *SyncManager) emailNeedsUpdate(local, remote *interfaces.Email) bool {
	// Check status changes
	if local.Status != remote.Status {
		return true
	}
	
	// Check label changes
	if len(local.Labels) != len(remote.Labels) {
		return true
	}
	
	localLabels := make(map[string]bool)
	for _, label := range local.Labels {
		localLabels[label] = true
	}
	
	for _, label := range remote.Labels {
		if !localLabels[label] {
			return true
		}
	}
	
	return false
}

// filterFolders filters folders based on configuration
func (sm *SyncManager) filterFolders(folders []*interfaces.FolderInfo) []*interfaces.FolderInfo {
	filtered := make([]*interfaces.FolderInfo, 0)
	
	for _, folder := range folders {
		// Check if folder is excluded
		excluded := false
		for _, excludedFolder := range sm.config.ExcludedFolders {
			if folder.Name == excludedFolder {
				excluded = true
				break
			}
		}
		
		if excluded {
			continue
		}
		
		// Check if folder is in sync list (if specified)
		if len(sm.config.FoldersToSync) > 0 {
			included := false
			for _, syncFolder := range sm.config.FoldersToSync {
				if folder.Name == syncFolder {
					included = true
					break
				}
			}
			
			if !included {
				continue
			}
		}
		
		filtered = append(filtered, folder)
	}
	
	return filtered
}

// checkConnectivity checks if we can connect to remote servers
func (sm *SyncManager) checkConnectivity(ctx context.Context) error {
	sm.sendProgress(ctx, &SyncProgress{
		Phase:     PhaseConnecting,
		Message:   "Checking IMAP connectivity",
		Timestamp: time.Now(),
	})
	
	// Check IMAP connectivity
	if !sm.imapClient.IsConnected() {
		if err := sm.imapClient.Connect(ctx); err != nil {
			return fmt.Errorf("IMAP connection failed: %w", err)
		}
	}
	
	sm.sendProgress(ctx, &SyncProgress{
		Phase:     PhaseConnecting,
		Message:   "Checking SMTP connectivity",
		Timestamp: time.Now(),
	})
	
	// Check SMTP connectivity
	if !sm.smtpSender.client.IsConnected() {
		if err := sm.smtpSender.client.Connect(ctx); err != nil {
			return fmt.Errorf("SMTP connection failed: %w", err)
		}
	}
	
	return nil
}

// resolveConflict resolves a single conflict
func (sm *SyncManager) resolveConflict(ctx context.Context, conflict *interfaces.SyncConflict) error {
	resolution := sm.config.DefaultResolution
	
	// Use conflict resolver to resolve the conflict
	resolvedEmail, err := sm.conflictResolver.ResolveConflict(conflict, resolution)
	if err != nil {
		return fmt.Errorf("failed to resolve conflict: %w", err)
	}
	
	// Update the email in repository
	if err := sm.repository.Update(ctx, resolvedEmail); err != nil {
		return fmt.Errorf("failed to update resolved email: %w", err)
	}
	
	return nil
}

// sendProgress sends progress update to the progress channel
func (sm *SyncManager) sendProgress(ctx context.Context, progress *SyncProgress) {
	select {
	case sm.progressChan <- progress:
	case <-ctx.Done():
	default:
		// Channel is full, skip this progress update
	}
}

// GetSyncStatus returns current synchronization status
func (sm *SyncManager) GetSyncStatus(ctx context.Context) (*interfaces.SyncStatus, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	// Get queued email count
	queuedCount, err := sm.smtpSender.queue.Size(ctx)
	if err != nil {
		queuedCount = 0
	}
	
	// Get pending conflicts count
	states, err := sm.stateManager.ListStates()
	if err != nil {
		return nil, fmt.Errorf("failed to get sync states: %w", err)
	}
	
	pendingConflicts := 0
	for _, state := range states {
		pendingConflicts += len(state.ModifiedUIDs)
	}
	
	// Calculate next sync time
	var nextSyncAt *time.Time
	if sm.lastSync != nil {
		nextSync := sm.lastSync.Add(sm.config.AutoSyncInterval)
		nextSyncAt = &nextSync
	}
	
	return &interfaces.SyncStatus{
		LastSync:         sm.lastSync,
		IsOnline:         sm.isOnline,
		QueuedEmails:     queuedCount,
		PendingConflicts: pendingConflicts,
		NextSyncAt:       nextSyncAt,
	}, nil
}

// QueueEmail adds an email to the outgoing queue
func (sm *SyncManager) QueueEmail(ctx context.Context, email *interfaces.Email) error {
	outgoingEmail := &interfaces.OutgoingEmail{
		Email: email,
	}
	return sm.smtpSender.queue.Enqueue(ctx, outgoingEmail)
}

// GetQueuedEmails returns emails waiting to be sent
func (sm *SyncManager) GetQueuedEmails(ctx context.Context) ([]*interfaces.Email, error) {
	queuedEmails, err := sm.smtpSender.queue.List(ctx)
	if err != nil {
		return nil, err
	}
	
	emails := make([]*interfaces.Email, len(queuedEmails))
	for i, queuedEmail := range queuedEmails {
		emails[i] = queuedEmail.Email
	}
	
	return emails, nil
}

// SetOfflineMode enables or disables offline mode
func (sm *SyncManager) SetOfflineMode(enabled bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	sm.isOnline = !enabled
}

// IsOnline returns true if connected to remote server
func (sm *SyncManager) IsOnline() bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	
	return sm.isOnline
}

// GetProgressChannel returns the progress channel for monitoring sync operations
func (sm *SyncManager) GetProgressChannel() <-chan *SyncProgress {
	return sm.progressChan
}

// Stop stops the sync manager and cancels any ongoing operations
func (sm *SyncManager) Stop() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	
	if sm.cancelFunc != nil {
		sm.cancelFunc()
	}
	
	close(sm.progressChan)
}