package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log entry
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogFormat represents the output format for log entries
type LogFormat int

const (
	FormatText LogFormat = iota
	FormatJSON
	FormatStructured
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     LogLevel               `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Caller    string                 `json:"caller,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

// Logger defines the interface for structured logging
type Logger interface {
	// Log methods with different levels
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, err error, fields ...Field)
	Fatal(msg string, err error, fields ...Field)
	
	// Context-aware logging
	WithContext(ctx context.Context) Logger
	WithFields(fields ...Field) Logger
	
	// Configuration
	SetLevel(level LogLevel)
	SetFormat(format LogFormat)
	SetOutput(writer io.Writer)
	
	// Utility methods
	IsLevelEnabled(level LogLevel) bool
	Close() error
}

// Field represents a key-value pair for structured logging
type Field struct {
	Key   string
	Value interface{}
}

// NewField creates a new logging field
func NewField(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// Common field constructors
func String(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func Duration(key string, value time.Duration) Field {
	return Field{Key: key, Value: value.String()}
}

func Error(err error) Field {
	return Field{Key: "error", Value: err.Error()}
}

// StructuredLogger implements the Logger interface
type StructuredLogger struct {
	mu       sync.RWMutex
	level    LogLevel
	format   LogFormat
	output   io.Writer
	fields   map[string]interface{}
	context  context.Context
	caller   bool
}

// NewLogger creates a new structured logger
func NewLogger() *StructuredLogger {
	return &StructuredLogger{
		level:  LevelInfo,
		format: FormatText,
		output: os.Stdout,
		fields: make(map[string]interface{}),
		caller: true,
	}
}

// SetLevel sets the minimum log level
func (l *StructuredLogger) SetLevel(level LogLevel) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
}

// SetFormat sets the output format
func (l *StructuredLogger) SetFormat(format LogFormat) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.format = format
}

// SetOutput sets the output writer
func (l *StructuredLogger) SetOutput(writer io.Writer) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.output = writer
}

// IsLevelEnabled checks if a log level is enabled
func (l *StructuredLogger) IsLevelEnabled(level LogLevel) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return level >= l.level
}

// WithContext returns a logger with context
func (l *StructuredLogger) WithContext(ctx context.Context) Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	
	newLogger := &StructuredLogger{
		level:   l.level,
		format:  l.format,
		output:  l.output,
		fields:  make(map[string]interface{}),
		context: ctx,
		caller:  l.caller,
	}
	
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	
	return newLogger
}

// WithFields returns a logger with additional fields
func (l *StructuredLogger) WithFields(fields ...Field) Logger {
	l.mu.RLock()
	defer l.mu.RUnlock()
	
	newLogger := &StructuredLogger{
		level:   l.level,
		format:  l.format,
		output:  l.output,
		fields:  make(map[string]interface{}),
		context: l.context,
		caller:  l.caller,
	}
	
	// Copy existing fields
	for k, v := range l.fields {
		newLogger.fields[k] = v
	}
	
	// Add new fields
	for _, field := range fields {
		newLogger.fields[field.Key] = field.Value
	}
	
	return newLogger
}

// Debug logs a debug message
func (l *StructuredLogger) Debug(msg string, fields ...Field) {
	l.log(LevelDebug, msg, nil, fields...)
}

// Info logs an info message
func (l *StructuredLogger) Info(msg string, fields ...Field) {
	l.log(LevelInfo, msg, nil, fields...)
}

// Warn logs a warning message
func (l *StructuredLogger) Warn(msg string, fields ...Field) {
	l.log(LevelWarn, msg, nil, fields...)
}

// Error logs an error message
func (l *StructuredLogger) Error(msg string, err error, fields ...Field) {
	l.log(LevelError, msg, err, fields...)
}

// Fatal logs a fatal message and exits
func (l *StructuredLogger) Fatal(msg string, err error, fields ...Field) {
	l.log(LevelFatal, msg, err, fields...)
	os.Exit(1)
}

// log is the internal logging method
func (l *StructuredLogger) log(level LogLevel, msg string, err error, fields ...Field) {
	if !l.IsLevelEnabled(level) {
		return
	}
	
	l.mu.RLock()
	defer l.mu.RUnlock()
	
	entry := LogEntry{
		Timestamp: time.Now().UTC(),
		Level:     level,
		Message:   msg,
		Fields:    make(map[string]interface{}),
	}
	
	// Add caller information if enabled
	if l.caller {
		if caller := getCaller(); caller != "" {
			entry.Caller = caller
		}
	}
	
	// Add error if present
	if err != nil {
		entry.Error = err.Error()
	}
	
	// Copy existing fields
	for k, v := range l.fields {
		entry.Fields[k] = v
	}
	
	// Add new fields
	for _, field := range fields {
		entry.Fields[field.Key] = field.Value
	}
	
	// Add context fields if present
	if l.context != nil {
		if requestID := l.context.Value("request_id"); requestID != nil {
			entry.Fields["request_id"] = requestID
		}
		if userID := l.context.Value("user_id"); userID != nil {
			entry.Fields["user_id"] = userID
		}
	}
	
	// Format and write the log entry
	output := l.formatEntry(entry)
	fmt.Fprintln(l.output, output)
}

// formatEntry formats a log entry according to the configured format
func (l *StructuredLogger) formatEntry(entry LogEntry) string {
	switch l.format {
	case FormatJSON:
		return l.formatJSON(entry)
	case FormatStructured:
		return l.formatStructured(entry)
	default:
		return l.formatText(entry)
	}
}

// formatText formats a log entry as plain text
func (l *StructuredLogger) formatText(entry LogEntry) string {
	var parts []string
	
	// Timestamp
	parts = append(parts, entry.Timestamp.Format("2006-01-02T15:04:05.000Z"))
	
	// Level
	parts = append(parts, fmt.Sprintf("[%s]", entry.Level.String()))
	
	// Caller
	if entry.Caller != "" {
		parts = append(parts, fmt.Sprintf("(%s)", entry.Caller))
	}
	
	// Message
	parts = append(parts, entry.Message)
	
	// Error
	if entry.Error != "" {
		parts = append(parts, fmt.Sprintf("error=%s", entry.Error))
	}
	
	// Fields
	for k, v := range entry.Fields {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	
	return strings.Join(parts, " ")
}

// formatJSON formats a log entry as JSON
func (l *StructuredLogger) formatJSON(entry LogEntry) string {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Sprintf("ERROR: Failed to marshal log entry: %v", err)
	}
	return string(data)
}

// formatStructured formats a log entry in a structured format
func (l *StructuredLogger) formatStructured(entry LogEntry) string {
	var parts []string
	
	parts = append(parts, fmt.Sprintf("time=%s", entry.Timestamp.Format(time.RFC3339)))
	parts = append(parts, fmt.Sprintf("level=%s", strings.ToLower(entry.Level.String())))
	
	if entry.Caller != "" {
		parts = append(parts, fmt.Sprintf("caller=%s", entry.Caller))
	}
	
	parts = append(parts, fmt.Sprintf("msg=\"%s\"", entry.Message))
	
	if entry.Error != "" {
		parts = append(parts, fmt.Sprintf("error=\"%s\"", entry.Error))
	}
	
	for k, v := range entry.Fields {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	
	return strings.Join(parts, " ")
}

// getCaller returns the caller information
func getCaller() string {
	// Skip getCaller, log, and the actual logging method
	_, file, line, ok := runtime.Caller(3)
	if !ok {
		return ""
	}
	
	// Get just the filename, not the full path
	filename := filepath.Base(file)
	return fmt.Sprintf("%s:%d", filename, line)
}

// Close closes the logger (no-op for basic logger)
func (l *StructuredLogger) Close() error {
	return nil
}