package imap

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"snail-cli/internal/config"
	"snail-cli/internal/interfaces"
)

// ConnectionPool implements IMAPConnectionPool interface
type ConnectionPool struct {
	config      *config.GmailConfig
	maxSize     int
	idleTimeout time.Duration
	
	mu          sync.RWMutex
	connections []*pooledConnection
	active      int
	
	// Statistics
	hits     int64
	misses   int64
	timeouts int64
}

// pooledConnection wraps an IMAP client with pool metadata
type pooledConnection struct {
	client    interfaces.IMAPClient
	createdAt time.Time
	lastUsed  time.Time
	inUse     bool
}

// NewConnectionPool creates a new IMAP connection pool
func NewConnectionPool(cfg *config.GmailConfig, maxSize int, idleTimeout time.Duration) *ConnectionPool {
	pool := &ConnectionPool{
		config:      cfg,
		maxSize:     maxSize,
		idleTimeout: idleTimeout,
		connections: make([]*pooledConnection, 0, maxSize),
	}
	
	// Start cleanup goroutine
	go pool.cleanup()
	
	return pool
}

// Get retrieves a connection from the pool
func (p *ConnectionPool) Get(ctx context.Context) (interfaces.IMAPClient, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to find an idle connection
	for _, conn := range p.connections {
		if !conn.inUse && conn.client.IsConnected() {
			// Check if connection is still valid
			if time.Since(conn.lastUsed) < p.idleTimeout {
				conn.inUse = true
				conn.lastUsed = time.Now()
				p.active++
				atomic.AddInt64(&p.hits, 1)
				return conn.client, nil
			}
			// Connection is stale, remove it
			conn.client.Disconnect()
			p.removeConnection(conn)
		}
	}

	// No idle connection available, create new one if under limit
	if len(p.connections) < p.maxSize {
		client := NewClient(p.config)
		
		if err := client.Connect(ctx); err != nil {
			atomic.AddInt64(&p.misses, 1)
			return nil, fmt.Errorf("failed to create new connection: %w", err)
		}

		conn := &pooledConnection{
			client:    client,
			createdAt: time.Now(),
			lastUsed:  time.Now(),
			inUse:     true,
		}

		p.connections = append(p.connections, conn)
		p.active++
		atomic.AddInt64(&p.misses, 1)
		return client, nil
	}

	// Pool is full, wait for a connection to become available
	atomic.AddInt64(&p.timeouts, 1)
	return nil, fmt.Errorf("connection pool exhausted")
}

// Put returns a connection to the pool
func (p *ConnectionPool) Put(client interfaces.IMAPClient) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Find the connection in the pool
	for _, conn := range p.connections {
		if conn.client == client {
			if conn.inUse {
				conn.inUse = false
				conn.lastUsed = time.Now()
				p.active--
			}
			return nil
		}
	}

	// Connection not found in pool, disconnect it
	return client.Disconnect()
}

// Close closes all connections in the pool
func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for _, conn := range p.connections {
		if err := conn.client.Disconnect(); err != nil {
			lastErr = err
		}
	}

	p.connections = p.connections[:0]
	p.active = 0

	return lastErr
}

// Stats returns pool statistics
func (p *ConnectionPool) Stats() *interfaces.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	idle := len(p.connections) - p.active

	return &interfaces.PoolStats{
		Active:   p.active,
		Idle:     idle,
		Total:    len(p.connections),
		MaxSize:  p.maxSize,
		Hits:     atomic.LoadInt64(&p.hits),
		Misses:   atomic.LoadInt64(&p.misses),
		Timeouts: atomic.LoadInt64(&p.timeouts),
	}
}

// removeConnection removes a connection from the pool (must be called with lock held)
func (p *ConnectionPool) removeConnection(target *pooledConnection) {
	for i, conn := range p.connections {
		if conn == target {
			// Remove from slice
			p.connections = append(p.connections[:i], p.connections[i+1:]...)
			if conn.inUse {
				p.active--
			}
			break
		}
	}
}

// cleanup periodically removes stale connections
func (p *ConnectionPool) cleanup() {
	ticker := time.NewTicker(p.idleTimeout / 2)
	defer ticker.Stop()

	for range ticker.C {
		p.mu.Lock()
		
		for i := len(p.connections) - 1; i >= 0; i-- {
			conn := p.connections[i]
			
			// Remove stale or disconnected connections
			if !conn.inUse && (time.Since(conn.lastUsed) > p.idleTimeout || !conn.client.IsConnected()) {
				conn.client.Disconnect()
				p.removeConnection(conn)
			}
		}
		
		p.mu.Unlock()
	}
}