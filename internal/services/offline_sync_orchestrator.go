package services

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"snail-cli/internal/interfaces"
)

// OfflineSyncOrchestrator coordinates offline-first synchronization with network awareness
type OfflineSyncOrchestrator struct {
	syncManager    *SyncManager
	networkMonitor *NetworkMonitor
	networkService *NetworkAwareService
	
	// Configuration
	config *OfflineSyncConfig
	
	// State
	isRunning      bool
	lastSyncResult *interfaces.SyncResult
	mu             sync.RWMutex
	
	// Control
	stopChan       chan struct{}
	syncTrigger    chan struct{}
}

// OfflineSyncConfig contains configuration for offline sync orchestration
type OfflineSyncConfig struct {
	// Auto sync settings
	EnableAutoSync       bool
	AutoSyncInterval     time.Duration
	SyncOnReconnect      bool
	
	// Network monitoring
	NetworkCheckInterval time.Duration
	NetworkCheckTimeout  time.Duration
	NetworkTestHosts     []string
	
	// Retry settings
	MaxSyncRetries       int
	SyncRetryInterval    time.Duration
	
	// Offline behavior
	QueueEmailsWhenOffline bool
	MaxOfflineQueueSize    int
	
	// Conflict resolution
	AutoResolveConflicts   bool
	DefaultResolution      interfaces.ConflictResolution
}

// NewOfflineSyncOrchestrator creates a new offline sync orchestrator
func NewOfflineSyncOrchestrator(
	syncManager *SyncManager,
	config *OfflineSyncConfig,
) *OfflineSyncOrchestrator {
	if config == nil {
		config = DefaultOfflineSyncConfig()
	}
	
	// Create network monitor configuration
	networkConfig := &NetworkMonitorConfig{
		CheckInterval:   config.NetworkCheckInterval,
		CheckTimeout:    config.NetworkCheckTimeout,
		TestHosts:       config.NetworkTestHosts,
		AutoSync:        config.EnableAutoSync,
		SyncOnReconnect: config.SyncOnReconnect,
	}
	
	// Create network monitor
	networkMonitor := NewNetworkMonitor(networkConfig, syncManager)
	
	// Create network-aware service
	networkService := NewNetworkAwareService(networkMonitor, syncManager)
	
	orchestrator := &OfflineSyncOrchestrator{
		syncManager:    syncManager,
		networkMonitor: networkMonitor,
		networkService: networkService,
		config:         config,
		stopChan:       make(chan struct{}),
		syncTrigger:    make(chan struct{}, 10),
	}
	
	return orchestrator
}

// DefaultOfflineSyncConfig returns default offline sync configuration
func DefaultOfflineSyncConfig() *OfflineSyncConfig {
	return &OfflineSyncConfig{
		EnableAutoSync:         true,
		AutoSyncInterval:       15 * time.Minute,
		SyncOnReconnect:        true,
		NetworkCheckInterval:   30 * time.Second,
		NetworkCheckTimeout:    10 * time.Second,
		NetworkTestHosts:       []string{"8.8.8.8:53", "1.1.1.1:53", "imap.gmail.com:993"},
		MaxSyncRetries:         3,
		SyncRetryInterval:      2 * time.Minute,
		QueueEmailsWhenOffline: true,
		MaxOfflineQueueSize:    1000,
		AutoResolveConflicts:   true,
		DefaultResolution:      interfaces.ResolutionRemote,
	}
}

// Start starts the offline sync orchestrator
func (oso *OfflineSyncOrchestrator) Start(ctx context.Context) error {
	oso.mu.Lock()
	defer oso.mu.Unlock()
	
	if oso.isRunning {
		return fmt.Errorf("offline sync orchestrator is already running")
	}
	
	log.Println("Starting offline sync orchestrator")
	
	// Start network monitor
	if err := oso.networkMonitor.Start(ctx); err != nil {
		return fmt.Errorf("failed to start network monitor: %w", err)
	}
	
	// Start network-aware service
	if err := oso.networkService.Start(ctx); err != nil {
		return fmt.Errorf("failed to start network-aware service: %w", err)
	}
	
	oso.isRunning = true
	
	// Start orchestration loop
	go oso.orchestrationLoop(ctx)
	
	// Start auto sync if enabled
	if oso.config.EnableAutoSync {
		go oso.autoSyncLoop(ctx)
	}
	
	log.Println("Offline sync orchestrator started successfully")
	return nil
}

// Stop stops the offline sync orchestrator
func (oso *OfflineSyncOrchestrator) Stop() {
	oso.mu.Lock()
	defer oso.mu.Unlock()
	
	if !oso.isRunning {
		return
	}
	
	log.Println("Stopping offline sync orchestrator")
	
	oso.isRunning = false
	
	// Stop network monitor
	oso.networkMonitor.Stop()
	
	// Signal stop to all goroutines
	close(oso.stopChan)
	
	log.Println("Offline sync orchestrator stopped")
}

// TriggerSync manually triggers a sync operation
func (oso *OfflineSyncOrchestrator) TriggerSync() {
	select {
	case oso.syncTrigger <- struct{}{}:
		log.Println("Manual sync triggered")
	default:
		log.Println("Sync trigger channel is full, sync already pending")
	}
}

// orchestrationLoop runs the main orchestration logic
func (oso *OfflineSyncOrchestrator) orchestrationLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			log.Println("Orchestration loop stopped due to context cancellation")
			return
		case <-oso.stopChan:
			log.Println("Orchestration loop stopped")
			return
		case <-oso.syncTrigger:
			oso.performSync(ctx)
		}
	}
}

// autoSyncLoop runs automatic sync operations at configured intervals
func (oso *OfflineSyncOrchestrator) autoSyncLoop(ctx context.Context) {
	ticker := time.NewTicker(oso.config.AutoSyncInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			log.Println("Auto sync loop stopped due to context cancellation")
			return
		case <-oso.stopChan:
			log.Println("Auto sync loop stopped")
			return
		case <-ticker.C:
			if oso.networkMonitor.IsOnline() {
				log.Println("Auto sync triggered by timer")
				oso.TriggerSync()
			} else {
				log.Println("Skipping auto sync - offline")
			}
		}
	}
}

// performSync performs a sync operation with proper error handling and retry logic
func (oso *OfflineSyncOrchestrator) performSync(ctx context.Context) {
	if !oso.networkMonitor.IsOnline() {
		log.Println("Skipping sync - network is offline")
		return
	}
	
	log.Println("Starting sync operation")
	
	// Create timeout context for sync
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	
	// Perform sync
	result, err := oso.syncManager.SyncFull(syncCtx)
	
	oso.mu.Lock()
	oso.lastSyncResult = result
	oso.mu.Unlock()
	
	if err != nil {
		log.Printf("Sync failed: %v", err)
		
		// Handle sync failure
		oso.handleSyncFailure(ctx, err)
	} else {
		log.Printf("Sync completed successfully: downloaded=%d, sent=%d, updated=%d, deleted=%d, conflicts=%d",
			result.EmailsDownloaded, result.EmailsSent, result.EmailsUpdated,
			result.EmailsDeleted, len(result.Conflicts))
		
		// Reset retry counter on successful sync
		oso.networkService.ResetSyncRetries()
	}
}

// handleSyncFailure handles sync failures with appropriate retry logic
func (oso *OfflineSyncOrchestrator) handleSyncFailure(ctx context.Context, err error) {
	syncStatus := oso.networkService.GetSyncStatus()
	
	if syncStatus.PendingSyncRetries < oso.config.MaxSyncRetries {
		log.Printf("Scheduling sync retry in %v (attempt %d/%d)",
			oso.config.SyncRetryInterval, syncStatus.PendingSyncRetries+1, oso.config.MaxSyncRetries)
		
		// Schedule retry
		go oso.scheduleRetry(ctx)
	} else {
		log.Printf("Max sync retries (%d) exceeded, giving up", oso.config.MaxSyncRetries)
	}
}

// scheduleRetry schedules a sync retry after the configured interval
func (oso *OfflineSyncOrchestrator) scheduleRetry(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-oso.stopChan:
		return
	case <-time.After(oso.config.SyncRetryInterval):
		if oso.networkMonitor.IsOnline() {
			log.Println("Retry sync triggered")
			oso.TriggerSync()
		} else {
			log.Println("Skipping retry sync - network is offline")
		}
	}
}

// GetStatus returns the current status of the offline sync orchestrator
func (oso *OfflineSyncOrchestrator) GetStatus() *OfflineSyncStatus {
	oso.mu.RLock()
	defer oso.mu.RUnlock()
	
	networkStats := oso.networkMonitor.GetConnectivityStats()
	syncStatus := oso.networkService.GetSyncStatus()
	
	status := &OfflineSyncStatus{
		IsRunning:          oso.isRunning,
		IsOnline:           networkStats.IsOnline,
		LastSyncResult:     oso.lastSyncResult,
		NetworkStats:       networkStats,
		SyncStatus:         syncStatus,
		Config:             *oso.config,
	}
	
	return status
}

// OfflineSyncStatus represents the current status of offline sync operations
type OfflineSyncStatus struct {
	IsRunning          bool
	IsOnline           bool
	LastSyncResult     *interfaces.SyncResult
	NetworkStats       ConnectivityStats
	SyncStatus         NetworkAwareSyncStatus
	Config             OfflineSyncConfig
}

// WaitForSync waits for the next sync operation to complete
func (oso *OfflineSyncOrchestrator) WaitForSync(ctx context.Context, timeout time.Duration) error {
	// Get current sync result
	oso.mu.RLock()
	initialResult := oso.lastSyncResult
	oso.mu.RUnlock()
	
	// Create timeout context
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	// Trigger a sync
	oso.TriggerSync()
	
	// Poll for sync completion
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for sync to complete")
		case <-ticker.C:
			oso.mu.RLock()
			currentResult := oso.lastSyncResult
			oso.mu.RUnlock()
			
			// Check if sync completed (result changed)
			if currentResult != initialResult {
				return nil
			}
		}
	}
}

// GetQueueStatus returns the status of the offline email queue
func (oso *OfflineSyncOrchestrator) GetQueueStatus(ctx context.Context) (*QueueStatus, error) {
	queuedEmails, err := oso.syncManager.GetQueuedEmails(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get queued emails: %w", err)
	}
	
	status := &QueueStatus{
		QueuedCount:    len(queuedEmails),
		MaxQueueSize:   oso.config.MaxOfflineQueueSize,
		IsQueueFull:    len(queuedEmails) >= oso.config.MaxOfflineQueueSize,
		QueuedEmails:   queuedEmails,
	}
	
	return status, nil
}

// QueueStatus represents the status of the offline email queue
type QueueStatus struct {
	QueuedCount    int
	MaxQueueSize   int
	IsQueueFull    bool
	QueuedEmails   []*interfaces.Email
}

// QueueEmail queues an email for sending when online
func (oso *OfflineSyncOrchestrator) QueueEmail(ctx context.Context, email *interfaces.Email) error {
	if !oso.config.QueueEmailsWhenOffline && !oso.networkMonitor.IsOnline() {
		return fmt.Errorf("cannot queue email - offline queueing is disabled and network is offline")
	}
	
	// Check queue size limit
	queueStatus, err := oso.GetQueueStatus(ctx)
	if err != nil {
		return fmt.Errorf("failed to check queue status: %w", err)
	}
	
	if queueStatus.IsQueueFull {
		return fmt.Errorf("cannot queue email - queue is full (%d/%d)", 
			queueStatus.QueuedCount, queueStatus.MaxQueueSize)
	}
	
	// Queue the email
	if err := oso.syncManager.QueueEmail(ctx, email); err != nil {
		return fmt.Errorf("failed to queue email: %w", err)
	}
	
	log.Printf("Email queued for sending: %s", email.Subject)
	
	// Trigger sync if online
	if oso.networkMonitor.IsOnline() {
		oso.TriggerSync()
	}
	
	return nil
}

// ForceSync forces an immediate sync operation, bypassing normal scheduling
func (oso *OfflineSyncOrchestrator) ForceSync(ctx context.Context) (*interfaces.SyncResult, error) {
	if !oso.networkMonitor.IsOnline() {
		return nil, fmt.Errorf("cannot force sync - network is offline")
	}
	
	log.Println("Forcing immediate sync")
	
	// Create timeout context for sync
	syncCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	
	// Perform sync directly
	result, err := oso.syncManager.SyncFull(syncCtx)
	
	oso.mu.Lock()
	oso.lastSyncResult = result
	oso.mu.Unlock()
	
	if err != nil {
		return result, fmt.Errorf("forced sync failed: %w", err)
	}
	
	log.Printf("Forced sync completed successfully: downloaded=%d, sent=%d, updated=%d, deleted=%d, conflicts=%d",
		result.EmailsDownloaded, result.EmailsSent, result.EmailsUpdated,
		result.EmailsDeleted, len(result.Conflicts))
	
	return result, nil
}

// SetAutoSync enables or disables automatic synchronization
func (oso *OfflineSyncOrchestrator) SetAutoSync(enabled bool) {
	oso.mu.Lock()
	defer oso.mu.Unlock()
	
	oso.config.EnableAutoSync = enabled
	oso.networkService.SetAutoSyncOnReconnect(enabled && oso.config.SyncOnReconnect)
	
	log.Printf("Auto sync %s", map[bool]string{true: "enabled", false: "disabled"}[enabled])
}

// IsAutoSyncEnabled returns whether automatic sync is enabled
func (oso *OfflineSyncOrchestrator) IsAutoSyncEnabled() bool {
	oso.mu.RLock()
	defer oso.mu.RUnlock()
	return oso.config.EnableAutoSync
}