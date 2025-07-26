package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// RotationConfig defines configuration for log rotation
type RotationConfig struct {
	// MaxSize is the maximum size in bytes before rotation
	MaxSize int64
	// MaxAge is the maximum age in days before deletion
	MaxAge int
	// MaxBackups is the maximum number of backup files to keep
	MaxBackups int
	// Compress indicates whether to compress rotated files
	Compress bool
	// LocalTime determines if the time used for formatting the timestamps in
	// backup files is the computer's local time
	LocalTime bool
}

// DefaultRotationConfig returns a default rotation configuration
func DefaultRotationConfig() RotationConfig {
	return RotationConfig{
		MaxSize:    100 * 1024 * 1024, // 100MB
		MaxAge:     30,                 // 30 days
		MaxBackups: 10,                 // 10 backup files
		Compress:   true,
		LocalTime:  false,
	}
}

// RotatingFileWriter implements io.Writer with log rotation capabilities
type RotatingFileWriter struct {
	mu       sync.Mutex
	filename string
	config   RotationConfig
	file     *os.File
	size     int64
}

// NewRotatingFileWriter creates a new rotating file writer
func NewRotatingFileWriter(filename string, config RotationConfig) (*RotatingFileWriter, error) {
	w := &RotatingFileWriter{
		filename: filename,
		config:   config,
	}
	
	// Ensure directory exists
	dir := filepath.Dir(filename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log directory: %w", err)
	}
	
	// Open the file
	if err := w.openFile(); err != nil {
		return nil, err
	}
	
	return w, nil
}

// Write implements io.Writer
func (w *RotatingFileWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	// Check if rotation is needed
	if w.size+int64(len(p)) > w.config.MaxSize {
		if err := w.rotate(); err != nil {
			return 0, fmt.Errorf("failed to rotate log file: %w", err)
		}
	}
	
	// Write to current file
	n, err = w.file.Write(p)
	if err != nil {
		return n, err
	}
	
	w.size += int64(n)
	return n, nil
}

// Close closes the current log file
func (w *RotatingFileWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

// openFile opens the log file for writing
func (w *RotatingFileWriter) openFile() error {
	info, err := os.Stat(w.filename)
	if os.IsNotExist(err) {
		// File doesn't exist, create it
		w.file, err = os.OpenFile(w.filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return fmt.Errorf("failed to create log file: %w", err)
		}
		w.size = 0
		return nil
	}
	
	if err != nil {
		return fmt.Errorf("failed to stat log file: %w", err)
	}
	
	// File exists, open for appending
	w.file, err = os.OpenFile(w.filename, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	
	w.size = info.Size()
	return nil
}

// rotate rotates the current log file
func (w *RotatingFileWriter) rotate() error {
	// Close current file
	if w.file != nil {
		if err := w.file.Close(); err != nil {
			return fmt.Errorf("failed to close current log file: %w", err)
		}
	}
	
	// Generate backup filename
	now := time.Now()
	if !w.config.LocalTime {
		now = now.UTC()
	}
	
	backupName := fmt.Sprintf("%s.%s",
		w.filename,
		now.Format("2006-01-02T15-04-05.000"))
	
	// Rename current file to backup
	if err := os.Rename(w.filename, backupName); err != nil {
		return fmt.Errorf("failed to rename log file: %w", err)
	}
	
	// Compress backup if configured
	if w.config.Compress {
		if err := w.compressFile(backupName); err != nil {
			// Log compression failure but don't fail rotation
			fmt.Fprintf(os.Stderr, "Failed to compress log file %s: %v\n", backupName, err)
		}
	}
	
	// Clean up old backups
	if err := w.cleanupOldBackups(); err != nil {
		// Log cleanup failure but don't fail rotation
		fmt.Fprintf(os.Stderr, "Failed to cleanup old log files: %v\n", err)
	}
	
	// Open new file
	return w.openFile()
}

// compressFile compresses a log file using gzip
func (w *RotatingFileWriter) compressFile(filename string) error {
	// This is a placeholder for compression logic
	// In a real implementation, you would use gzip compression
	compressedName := filename + ".gz"
	
	// For now, just rename to indicate it should be compressed
	return os.Rename(filename, compressedName)
}

// cleanupOldBackups removes old backup files based on configuration
func (w *RotatingFileWriter) cleanupOldBackups() error {
	dir := filepath.Dir(w.filename)
	base := filepath.Base(w.filename)
	
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read log directory: %w", err)
	}
	
	var backups []backupInfo
	
	// Find all backup files
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		name := entry.Name()
		if !strings.HasPrefix(name, base+".") {
			continue
		}
		
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		backups = append(backups, backupInfo{
			name:    name,
			modTime: info.ModTime(),
			path:    filepath.Join(dir, name),
		})
	}
	
	// Sort by modification time (newest first)
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})
	
	// Remove files based on MaxBackups
	if w.config.MaxBackups > 0 && len(backups) > w.config.MaxBackups {
		for i := w.config.MaxBackups; i < len(backups); i++ {
			if err := os.Remove(backups[i].path); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to remove old backup %s: %v\n", backups[i].path, err)
			}
		}
		backups = backups[:w.config.MaxBackups]
	}
	
	// Remove files based on MaxAge
	if w.config.MaxAge > 0 {
		cutoff := time.Now().AddDate(0, 0, -w.config.MaxAge)
		for _, backup := range backups {
			if backup.modTime.Before(cutoff) {
				if err := os.Remove(backup.path); err != nil {
					fmt.Fprintf(os.Stderr, "Failed to remove old backup %s: %v\n", backup.path, err)
				}
			}
		}
	}
	
	return nil
}

// backupInfo holds information about a backup file
type backupInfo struct {
	name    string
	modTime time.Time
	path    string
}

// LogManager manages multiple loggers with rotation
type LogManager struct {
	mu      sync.RWMutex
	loggers map[string]*StructuredLogger
	writers map[string]*RotatingFileWriter
	config  RotationConfig
}

// NewLogManager creates a new log manager
func NewLogManager(config RotationConfig) *LogManager {
	return &LogManager{
		loggers: make(map[string]*StructuredLogger),
		writers: make(map[string]*RotatingFileWriter),
		config:  config,
	}
}

// GetLogger returns a logger for the specified component
func (m *LogManager) GetLogger(component string) (Logger, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if logger, exists := m.loggers[component]; exists {
		return logger, nil
	}
	
	// Create log file path
	logDir := filepath.Join(os.Getenv("HOME"), ".snail", "logs")
	logFile := filepath.Join(logDir, fmt.Sprintf("%s.log", component))
	
	// Create rotating writer
	writer, err := NewRotatingFileWriter(logFile, m.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create rotating writer for %s: %w", component, err)
	}
	
	// Create logger
	logger := NewLogger()
	logger.SetOutput(writer)
	logger = logger.WithFields(String("component", component)).(*StructuredLogger)
	
	m.loggers[component] = logger
	m.writers[component] = writer
	
	return logger, nil
}

// Close closes all loggers and writers
func (m *LogManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	var errors []string
	
	for name, writer := range m.writers {
		if err := writer.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("failed to close writer %s: %v", name, err))
		}
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("errors closing log manager: %s", strings.Join(errors, "; "))
	}
	
	return nil
}

// SetGlobalLevel sets the log level for all loggers
func (m *LogManager) SetGlobalLevel(level LogLevel) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, logger := range m.loggers {
		logger.SetLevel(level)
	}
}

// SetGlobalFormat sets the log format for all loggers
func (m *LogManager) SetGlobalFormat(format LogFormat) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	for _, logger := range m.loggers {
		logger.SetFormat(format)
	}
}