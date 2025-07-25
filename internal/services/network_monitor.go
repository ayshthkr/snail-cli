package services

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"
)

// NetworkMonitor monitors network connectivity and triggers sync operations
type NetworkMonitor struct {
	// Configuration
	checkInterval    time.Duration
	checkTimeout     time.Duration
	testHosts        []string
	
	// State
	isOnline         bool
	lastOnlineTime   *time.Time
	lastOfflineTime  *time.Time
	mu               sync.RWMutex
	
	// Channels and control
	statusChan       chan NetworkStatus
	stopChan         chan struct{}
	stopped          bool
	
	// Callbacks
	onOnlineCallback  func()
	onOfflineCallback func()
	
	// Dependencies
	syncManager      SyncManagerInterface
}

// NetworkStatus represents the current network status
type NetworkStatus struct {
	IsOnline      bool
	LastCheck     time.Time
	LastOnline    *time.Time
	LastOffline   *time.Time
	CheckDuration time.Duration
	Error         error
}

// SyncManagerInterface defines the interface for sync operations
type SyncManagerInterface interface {
	Sync(ctx context.Context) error
	IsOnline() bool
	SetOfflineMode(enabled bool)
}

// NetworkMonitorConfig contains configuration for network monitoring
type NetworkMonitorConfig struct {
	CheckInterval    time.Duration
	CheckTimeout     time.Duration
	TestHosts        []string
	AutoSync         bool
	SyncOnReconnect  bool
}

// NewNetworkMonitor creates a new network monitor
func NewNetworkMonitor(config *NetworkMonitorConfig, syncManager SyncManagerInterface) *NetworkMonitor {
	if config == nil {
		config = DefaultNetworkMonitorConfig()
	}
	
	nm := &NetworkMonitor{
		checkInterval:   config.CheckInterval,
		checkTimeout:    config.CheckTimeout,
		testHosts:       config.TestHosts,
		isOnline:        true, // Assume online initially
		statusChan:      make(chan NetworkStatus, 10),
		stopChan:        make(chan struct{}),
		syncManager:     syncManager,
	}
	
	// Set up callbacks for auto-sync
	if config.AutoSync && config.SyncOnReconnect {
		nm.onOnlineCallback = func() {
			go nm.triggerAutoSync()
		}
	}
	
	return nm
}

// DefaultNetworkMonitorConfig returns default network monitor configuration
func DefaultNetworkMonitorConfig() *NetworkMonitorConfig {
	return &NetworkMonitorConfig{
		CheckInterval:   30 * time.Second,
		CheckTimeout:    10 * time.Second,
		TestHosts:       []string{"8.8.8.8:53", "1.1.1.1:53", "gmail.com:993"},
		AutoSync:        true,
		SyncOnReconnect: true,
	}
}

// Start begins network monitoring
func (nm *NetworkMonitor) Start(ctx context.Context) error {
	nm.mu.Lock()
	if nm.stopped {
		nm.mu.Unlock()
		return fmt.Errorf("network monitor has been stopped and cannot be restarted")
	}
	nm.mu.Unlock()
	
	log.Println("Starting network monitor")
	
	// Perform initial connectivity check
	nm.checkConnectivity()
	
	// Start monitoring goroutine
	go nm.monitorLoop(ctx)
	
	return nil
}

// Stop stops network monitoring
func (nm *NetworkMonitor) Stop() {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	
	if nm.stopped {
		return
	}
	
	log.Println("Stopping network monitor")
	
	nm.stopped = true
	close(nm.stopChan)
	close(nm.statusChan)
}

// IsOnline returns the current online status
func (nm *NetworkMonitor) IsOnline() bool {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	return nm.isOnline
}

// GetStatus returns the current network status
func (nm *NetworkMonitor) GetStatus() NetworkStatus {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	
	return NetworkStatus{
		IsOnline:    nm.isOnline,
		LastCheck:   time.Now(),
		LastOnline:  nm.lastOnlineTime,
		LastOffline: nm.lastOfflineTime,
	}
}

// GetStatusChannel returns a channel for receiving network status updates
func (nm *NetworkMonitor) GetStatusChannel() <-chan NetworkStatus {
	return nm.statusChan
}

// SetOnlineCallback sets a callback function to be called when going online
func (nm *NetworkMonitor) SetOnlineCallback(callback func()) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.onOnlineCallback = callback
}

// SetOfflineCallback sets a callback function to be called when going offline
func (nm *NetworkMonitor) SetOfflineCallback(callback func()) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.onOfflineCallback = callback
}

// ForceCheck forces an immediate connectivity check
func (nm *NetworkMonitor) ForceCheck() NetworkStatus {
	return nm.checkConnectivity()
}

// monitorLoop runs the main monitoring loop
func (nm *NetworkMonitor) monitorLoop(ctx context.Context) {
	ticker := time.NewTicker(nm.checkInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			log.Println("Network monitor stopped due to context cancellation")
			return
		case <-nm.stopChan:
			log.Println("Network monitor stopped")
			return
		case <-ticker.C:
			nm.checkConnectivity()
		}
	}
}

// checkConnectivity performs a connectivity check
func (nm *NetworkMonitor) checkConnectivity() NetworkStatus {
	startTime := time.Now()
	
	// Test connectivity to multiple hosts
	online := nm.testConnectivity()
	
	duration := time.Since(startTime)
	now := time.Now()
	
	nm.mu.Lock()
	wasOnline := nm.isOnline
	nm.isOnline = online
	
	if online && !wasOnline {
		// Just came online
		nm.lastOnlineTime = &now
		log.Println("Network connectivity restored")
		
		// Trigger online callback
		if nm.onOnlineCallback != nil {
			go nm.onOnlineCallback()
		}
		
		// Update sync manager
		if nm.syncManager != nil {
			nm.syncManager.SetOfflineMode(false)
		}
	} else if !online && wasOnline {
		// Just went offline
		nm.lastOfflineTime = &now
		log.Println("Network connectivity lost")
		
		// Trigger offline callback
		if nm.onOfflineCallback != nil {
			go nm.onOfflineCallback()
		}
		
		// Update sync manager
		if nm.syncManager != nil {
			nm.syncManager.SetOfflineMode(true)
		}
	}
	nm.mu.Unlock()
	
	status := NetworkStatus{
		IsOnline:      online,
		LastCheck:     now,
		LastOnline:    nm.lastOnlineTime,
		LastOffline:   nm.lastOfflineTime,
		CheckDuration: duration,
	}
	
	// Send status update (non-blocking)
	select {
	case nm.statusChan <- status:
	default:
		// Channel is full, skip this update
	}
	
	return status
}

// testConnectivity tests connectivity to configured hosts
func (nm *NetworkMonitor) testConnectivity() bool {
	if len(nm.testHosts) == 0 {
		return true // No hosts to test, assume online
	}
	
	// Test connectivity to each host
	successCount := 0
	for _, host := range nm.testHosts {
		if nm.testHost(host) {
			successCount++
		}
	}
	
	// Consider online if at least half of the hosts are reachable
	threshold := (len(nm.testHosts) + 1) / 2
	return successCount >= threshold
}

// testHost tests connectivity to a single host
func (nm *NetworkMonitor) testHost(host string) bool {
	conn, err := net.DialTimeout("tcp", host, nm.checkTimeout)
	if err != nil {
		return false
	}
	
	conn.Close()
	return true
}

// triggerAutoSync triggers an automatic sync when connectivity is restored
func (nm *NetworkMonitor) triggerAutoSync() {
	if nm.syncManager == nil {
		return
	}
	
	log.Println("Triggering automatic sync due to connectivity restoration")
	
	// Create a context with timeout for the sync operation
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	
	if err := nm.syncManager.Sync(ctx); err != nil {
		log.Printf("Auto-sync failed: %v", err)
	} else {
		log.Println("Auto-sync completed successfully")
	}
}

// WaitForConnectivity waits until network connectivity is available
func (nm *NetworkMonitor) WaitForConnectivity(ctx context.Context) error {
	if nm.IsOnline() {
		return nil // Already online
	}
	
	log.Println("Waiting for network connectivity...")
	
	// Listen for status updates
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case status := <-nm.statusChan:
			if status.IsOnline {
				log.Println("Network connectivity available")
				return nil
			}
		case <-time.After(nm.checkInterval):
			// Force a check in case we missed a status update
			if nm.IsOnline() {
				return nil
			}
		}
	}
}

// GetConnectivityStats returns connectivity statistics
func (nm *NetworkMonitor) GetConnectivityStats() ConnectivityStats {
	nm.mu.RLock()
	defer nm.mu.RUnlock()
	
	stats := ConnectivityStats{
		IsOnline:        nm.isOnline,
		LastOnlineTime:  nm.lastOnlineTime,
		LastOfflineTime: nm.lastOfflineTime,
		CheckInterval:   nm.checkInterval,
		TestHosts:       make([]string, len(nm.testHosts)),
	}
	
	copy(stats.TestHosts, nm.testHosts)
	
	// Calculate uptime/downtime
	now := time.Now()
	if nm.isOnline && nm.lastOnlineTime != nil {
		uptime := now.Sub(*nm.lastOnlineTime)
		stats.CurrentUptime = &uptime
	}
	
	if !nm.isOnline && nm.lastOfflineTime != nil {
		downtime := now.Sub(*nm.lastOfflineTime)
		stats.CurrentDowntime = &downtime
	}
	
	return stats
}

// ConnectivityStats represents connectivity statistics
type ConnectivityStats struct {
	IsOnline         bool
	LastOnlineTime   *time.Time
	LastOfflineTime  *time.Time
	CurrentUptime    *time.Duration
	CurrentDowntime  *time.Duration
	CheckInterval    time.Duration
	TestHosts        []string
}

// NetworkAwareService provides network-aware functionality
type NetworkAwareService struct {
	networkMonitor *NetworkMonitor
	syncManager    SyncManagerInterface
	
	// Configuration
	autoSyncOnReconnect bool
	syncRetryInterval   time.Duration
	maxSyncRetries      int
	
	// State
	pendingSyncRetries int
	lastSyncAttempt    *time.Time
	mu                 sync.RWMutex
}

// NewNetworkAwareService creates a new network-aware service
func NewNetworkAwareService(networkMonitor *NetworkMonitor, syncManager SyncManagerInterface) *NetworkAwareService {
	return &NetworkAwareService{
		networkMonitor:      networkMonitor,
		syncManager:         syncManager,
		autoSyncOnReconnect: true,
		syncRetryInterval:   2 * time.Minute,
		maxSyncRetries:      3,
	}
}

// Start starts the network-aware service
func (nas *NetworkAwareService) Start(ctx context.Context) error {
	// Set up network status monitoring
	go nas.monitorNetworkStatus(ctx)
	
	return nil
}

// monitorNetworkStatus monitors network status changes and handles sync operations
func (nas *NetworkAwareService) monitorNetworkStatus(ctx context.Context) {
	statusChan := nas.networkMonitor.GetStatusChannel()
	
	for {
		select {
		case <-ctx.Done():
			return
		case status, ok := <-statusChan:
			if !ok {
				return // Channel closed
			}
			
			nas.handleNetworkStatusChange(ctx, status)
		}
	}
}

// handleNetworkStatusChange handles network status changes
func (nas *NetworkAwareService) handleNetworkStatusChange(ctx context.Context, status NetworkStatus) {
	if status.IsOnline && nas.autoSyncOnReconnect {
		nas.mu.Lock()
		shouldSync := nas.pendingSyncRetries < nas.maxSyncRetries
		if shouldSync {
			nas.pendingSyncRetries++
			now := time.Now()
			nas.lastSyncAttempt = &now
		}
		nas.mu.Unlock()
		
		if shouldSync {
			go nas.attemptSync(ctx)
		}
	}
}

// attemptSync attempts to perform a sync operation with retry logic
func (nas *NetworkAwareService) attemptSync(ctx context.Context) {
	log.Println("Attempting sync due to network connectivity")
	
	// Create a timeout context for the sync
	syncCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	
	err := nas.syncManager.Sync(syncCtx)
	
	nas.mu.Lock()
	defer nas.mu.Unlock()
	
	if err != nil {
		log.Printf("Sync attempt failed: %v", err)
		
		// Schedule retry if we haven't exceeded max retries
		if nas.pendingSyncRetries < nas.maxSyncRetries {
			go nas.scheduleRetry(ctx)
		} else {
			log.Printf("Max sync retries (%d) exceeded, giving up", nas.maxSyncRetries)
			nas.pendingSyncRetries = 0
		}
	} else {
		log.Println("Sync completed successfully")
		nas.pendingSyncRetries = 0
	}
}

// scheduleRetry schedules a sync retry after the configured interval
func (nas *NetworkAwareService) scheduleRetry(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(nas.syncRetryInterval):
		if nas.networkMonitor.IsOnline() {
			nas.attemptSync(ctx)
		}
	}
}

// GetSyncStatus returns the current sync status
func (nas *NetworkAwareService) GetSyncStatus() NetworkAwareSyncStatus {
	nas.mu.RLock()
	defer nas.mu.RUnlock()
	
	return NetworkAwareSyncStatus{
		IsOnline:           nas.networkMonitor.IsOnline(),
		PendingSyncRetries: nas.pendingSyncRetries,
		LastSyncAttempt:    nas.lastSyncAttempt,
		MaxSyncRetries:     nas.maxSyncRetries,
		SyncRetryInterval:  nas.syncRetryInterval,
	}
}

// NetworkAwareSyncStatus represents the status of network-aware sync operations
type NetworkAwareSyncStatus struct {
	IsOnline           bool
	PendingSyncRetries int
	LastSyncAttempt    *time.Time
	MaxSyncRetries     int
	SyncRetryInterval  time.Duration
}

// SetAutoSyncOnReconnect enables or disables automatic sync on reconnect
func (nas *NetworkAwareService) SetAutoSyncOnReconnect(enabled bool) {
	nas.mu.Lock()
	defer nas.mu.Unlock()
	nas.autoSyncOnReconnect = enabled
}

// ResetSyncRetries resets the sync retry counter
func (nas *NetworkAwareService) ResetSyncRetries() {
	nas.mu.Lock()
	defer nas.mu.Unlock()
	nas.pendingSyncRetries = 0
}