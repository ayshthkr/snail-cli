package interfaces

import (
	"context"
	"time"
)

// IMAPClient defines the interface for IMAP operations
type IMAPClient interface {
	// Connect establishes connection to IMAP server
	Connect(ctx context.Context) error
	
	// Disconnect closes the IMAP connection
	Disconnect() error
	
	// Authenticate performs authentication with the server
	Authenticate(ctx context.Context, username, password string) error
	
	// AuthenticateOAuth2 performs OAuth2 authentication
	AuthenticateOAuth2(ctx context.Context, username, accessToken string) error
	
	// SelectFolder selects an IMAP folder
	SelectFolder(ctx context.Context, folder string) (*FolderInfo, error)
	
	// ListFolders returns list of available folders
	ListFolders(ctx context.Context) ([]*FolderInfo, error)
	
	// FetchEmails fetches emails from the selected folder
	FetchEmails(ctx context.Context, criteria *FetchCriteria) ([]*Email, error)
	
	// FetchEmailByUID fetches a single email by UID
	FetchEmailByUID(ctx context.Context, uid uint32) (*Email, error)
	
	// MarkAsRead marks emails as read
	MarkAsRead(ctx context.Context, uids []uint32) error
	
	// MarkAsUnread marks emails as unread
	MarkAsUnread(ctx context.Context, uids []uint32) error
	
	// DeleteEmails moves emails to trash
	DeleteEmails(ctx context.Context, uids []uint32) error
	
	// MoveEmails moves emails to another folder
	MoveEmails(ctx context.Context, uids []uint32, destFolder string) error
	
	// IsConnected returns true if connected to server
	IsConnected() bool
	
	// GetConnectionInfo returns connection information
	GetConnectionInfo() *ConnectionInfo
}

// FolderInfo contains information about an IMAP folder
type FolderInfo struct {
	Name         string
	Delimiter    string
	Attributes   []string
	MessageCount uint32
	RecentCount  uint32
	UnseenCount  uint32
	UIDValidity  uint32
	UIDNext      uint32
}

// FetchCriteria defines criteria for fetching emails
type FetchCriteria struct {
	// UID range to fetch (nil means all)
	UIDRange *UIDRange
	
	// Fetch only headers
	HeadersOnly bool
	
	// Maximum number of emails to fetch
	Limit int
	
	// Fetch emails since this date
	Since *time.Time
	
	// Fetch emails before this date
	Before *time.Time
	
	// Fetch only unseen emails
	UnseenOnly bool
}

// UIDRange represents a range of UIDs
type UIDRange struct {
	Start uint32
	End   uint32 // 0 means no upper limit
}

// ConnectionInfo contains information about the IMAP connection
type ConnectionInfo struct {
	Server      string
	Port        int
	TLS         bool
	Connected   bool
	ConnectedAt *time.Time
	LastError   error
	Capabilities []string
}

// IMAPConnectionPool manages a pool of IMAP connections
type IMAPConnectionPool interface {
	// Get retrieves a connection from the pool
	Get(ctx context.Context) (IMAPClient, error)
	
	// Put returns a connection to the pool
	Put(client IMAPClient) error
	
	// Close closes all connections in the pool
	Close() error
	
	// Stats returns pool statistics
	Stats() *PoolStats
}

// PoolStats contains connection pool statistics
type PoolStats struct {
	Active   int
	Idle     int
	Total    int
	MaxSize  int
	Hits     int64
	Misses   int64
	Timeouts int64
}