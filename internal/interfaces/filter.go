package interfaces

import (
	"context"
	"time"
)

// FilterEngine defines the interface for email filtering and processing
type FilterEngine interface {
	// RegisterFilter adds a new filter to the engine
	RegisterFilter(filter Filter) error
	
	// UnregisterFilter removes a filter from the engine
	UnregisterFilter(name string) error
	
	// ProcessEmail applies all applicable filters to an email
	ProcessEmail(ctx context.Context, email interface{}, event FilterEvent) (*FilterResult, error)
	
	// ListFilters returns all registered filters
	ListFilters() []Filter
	
	// GetFilter returns a specific filter by name
	GetFilter(name string) (Filter, error)
	
	// ValidateFilter checks if a filter configuration is valid
	ValidateFilter(filter Filter) error
}

// Filter represents an email filter
type Filter interface {
	// Name returns the filter name
	Name() string
	
	// Description returns the filter description
	Description() string
	
	// Events returns the events this filter handles
	Events() []FilterEvent
	
	// Execute processes an email and returns the result
	Execute(ctx context.Context, email *Email, event FilterEvent) (*FilterResult, error)
	
	// IsEnabled returns true if the filter is enabled
	IsEnabled() bool
	
	// SetEnabled enables or disables the filter
	SetEnabled(enabled bool)
}

// FilterEvent represents events that can trigger filters
type FilterEvent string

const (
	EventEmailReceived FilterEvent = "email_received"
	EventEmailSent     FilterEvent = "email_sent"
	EventEmailRead     FilterEvent = "email_read"
	EventEmailDeleted  FilterEvent = "email_deleted"
	EventEmailMoved    FilterEvent = "email_moved"
)

// FilterResult contains the result of filter execution
type FilterResult struct {
	Modified     bool
	Email        *Email
	Actions      []FilterAction
	Labels       []string
	StopProcessing bool
	Error        error
	Duration     time.Duration
}

// FilterAction represents an action to be taken on an email
type FilterAction struct {
	Type   ActionType
	Value  string
	Params map[string]interface{}
}

// ActionType defines types of filter actions
type ActionType string

const (
	ActionMove      ActionType = "move"
	ActionLabel     ActionType = "label"
	ActionDelete    ActionType = "delete"
	ActionMarkRead  ActionType = "mark_read"
	ActionForward   ActionType = "forward"
	ActionReply     ActionType = "reply"
	ActionExecute   ActionType = "execute"
	ActionSummarize ActionType = "summarize"
)

// ShellFilter represents a shell script-based filter
type ShellFilter struct {
	FilterName    string
	FilterDesc    string
	ScriptPath    string
	FilterEvents  []FilterEvent
	Timeout       time.Duration
	Enabled       bool
	Environment   map[string]string
}

// AIFilter represents an AI-powered filter
type AIFilter struct {
	FilterName   string
	FilterDesc   string
	Provider     string
	Model        string
	Prompt       string
	FilterEvents []FilterEvent
	Enabled      bool
	Config       map[string]interface{}
}