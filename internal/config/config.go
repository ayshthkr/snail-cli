package config

import (
	"time"
)

// Config represents the main application configuration
type Config struct {
	Gmail    GmailConfig    `json:"gmail" yaml:"gmail"`
	Storage  StorageConfig  `json:"storage" yaml:"storage"`
	Filters  []FilterConfig `json:"filters" yaml:"filters"`
	Security SecurityConfig `json:"security" yaml:"security"`
	CLI      CLIConfig      `json:"cli" yaml:"cli"`
	Sync     SyncConfig     `json:"sync" yaml:"sync"`
}

// GmailConfig contains Gmail-specific configuration
type GmailConfig struct {
	Username     string              `json:"username" yaml:"username"`
	IMAPServer   string              `json:"imap_server" yaml:"imap_server"`
	SMTPServer   string              `json:"smtp_server" yaml:"smtp_server"`
	IMAPPort     int                 `json:"imap_port" yaml:"imap_port"`
	SMTPPort     int                 `json:"smtp_port" yaml:"smtp_port"`
	AuthMethod   string              `json:"auth_method" yaml:"auth_method"`
	Credentials  EncryptedCredentials `json:"credentials" yaml:"credentials"`
	UseTLS       bool                `json:"use_tls" yaml:"use_tls"`
	UseStartTLS  bool                `json:"use_starttls" yaml:"use_starttls"`
}

// StorageConfig contains storage-related configuration
type StorageConfig struct {
	RepositoryPath string `json:"repository_path" yaml:"repository_path"`
	IndexPath      string `json:"index_path" yaml:"index_path"`
	MaxFileSize    int64  `json:"max_file_size" yaml:"max_file_size"`
	Compression    bool   `json:"compression" yaml:"compression"`
	Encryption     bool   `json:"encryption" yaml:"encryption"`
	BackupEnabled  bool   `json:"backup_enabled" yaml:"backup_enabled"`
	BackupPath     string `json:"backup_path" yaml:"backup_path"`
}

// FilterConfig contains filter configuration
type FilterConfig struct {
	Name        string            `json:"name" yaml:"name"`
	Type        string            `json:"type" yaml:"type"`
	Script      string            `json:"script" yaml:"script"`
	Events      []string          `json:"events" yaml:"events"`
	Enabled     bool              `json:"enabled" yaml:"enabled"`
	Timeout     time.Duration     `json:"timeout" yaml:"timeout"`
	Environment map[string]string `json:"environment" yaml:"environment"`
	Config      map[string]interface{} `json:"config" yaml:"config"`
}

// SecurityConfig contains security-related configuration
type SecurityConfig struct {
	EncryptionKey     string `json:"encryption_key" yaml:"encryption_key"`
	KeychainService   string `json:"keychain_service" yaml:"keychain_service"`
	CredentialStorage string `json:"credential_storage" yaml:"credential_storage"`
	TLSVerify         bool   `json:"tls_verify" yaml:"tls_verify"`
	CertificatePath   string `json:"certificate_path" yaml:"certificate_path"`
}

// CLIConfig contains CLI-specific configuration
type CLIConfig struct {
	Editor         string       `json:"editor" yaml:"editor"`
	Pager          string       `json:"pager" yaml:"pager"`
	DefaultFormat  string       `json:"default_format" yaml:"default_format"`
	ColorEnabled   bool         `json:"color_enabled" yaml:"color_enabled"`
	PageSize       int          `json:"page_size" yaml:"page_size"`
	DateFormat     string       `json:"date_format" yaml:"date_format"`
	TimeZone       string       `json:"timezone" yaml:"timezone"`
}

// SyncConfig contains synchronization configuration
type SyncConfig struct {
	AutoSync         bool          `json:"auto_sync" yaml:"auto_sync"`
	SyncInterval     time.Duration `json:"sync_interval" yaml:"sync_interval"`
	MaxRetries       int           `json:"max_retries" yaml:"max_retries"`
	RetryDelay       time.Duration `json:"retry_delay" yaml:"retry_delay"`
	BatchSize        int           `json:"batch_size" yaml:"batch_size"`
	ConflictStrategy string        `json:"conflict_strategy" yaml:"conflict_strategy"`
	OfflineMode      bool          `json:"offline_mode" yaml:"offline_mode"`
}

// EncryptedCredentials represents encrypted credential storage
type EncryptedCredentials struct {
	EncryptedData string `json:"encrypted_data" yaml:"encrypted_data"`
	KeyID         string `json:"key_id" yaml:"key_id"`
	Algorithm     string `json:"algorithm" yaml:"algorithm"`
}

// DefaultConfig returns a configuration with sensible defaults
func DefaultConfig() *Config {
	return &Config{
		Gmail: GmailConfig{
			IMAPServer:  "imap.gmail.com",
			SMTPServer:  "smtp.gmail.com",
			IMAPPort:    993,
			SMTPPort:    587,
			AuthMethod:  "oauth2",
			UseTLS:      true,
			UseStartTLS: true,
		},
		Storage: StorageConfig{
			RepositoryPath: "~/.snail/emails",
			IndexPath:      "~/.snail/indices",
			MaxFileSize:    50 * 1024 * 1024, // 50MB
			Compression:    true,
			Encryption:     false,
			BackupEnabled:  false,
		},
		Security: SecurityConfig{
			KeychainService:   "snail-cli",
			CredentialStorage: "keychain",
			TLSVerify:         true,
		},
		CLI: CLIConfig{
			Editor:        "vim",
			Pager:         "less",
			DefaultFormat: "table",
			ColorEnabled:  true,
			PageSize:      20,
			DateFormat:    "2006-01-02 15:04:05",
			TimeZone:      "Local",
		},
		Sync: SyncConfig{
			AutoSync:         true,
			SyncInterval:     15 * time.Minute,
			MaxRetries:       3,
			RetryDelay:       30 * time.Second,
			BatchSize:        100,
			ConflictStrategy: "remote_wins",
			OfflineMode:      false,
		},
		Filters: []FilterConfig{},
	}
}