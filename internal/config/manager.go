package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	ConfigVersion = "1.0.0"
	ConfigFileName = "config.yaml"
	ConfigDirName = ".snail"
)

// ConfigManager handles configuration file operations
type ConfigManager struct {
	configPath string
	config     *Config
}

// VersionedConfig wraps the config with version information
type VersionedConfig struct {
	Version string `json:"version" yaml:"version"`
	Config  Config `json:"config" yaml:"config"`
}

// NewConfigManager creates a new configuration manager
func NewConfigManager(configDir string) *ConfigManager {
	if configDir == "" {
		homeDir, _ := os.UserHomeDir()
		configDir = filepath.Join(homeDir, ConfigDirName)
	}
	
	configPath := filepath.Join(configDir, ConfigFileName)
	
	return &ConfigManager{
		configPath: configPath,
		config:     DefaultConfig(),
	}
}

// Load loads configuration from file
func (cm *ConfigManager) Load() error {
	// Create config directory if it doesn't exist
	configDir := filepath.Dir(cm.configPath)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Check if config file exists
	if _, err := os.Stat(cm.configPath); os.IsNotExist(err) {
		// Create default config file
		return cm.Save()
	}

	// Read config file
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse versioned config
	var versionedConfig VersionedConfig
	if err := yaml.Unmarshal(data, &versionedConfig); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	// Check version compatibility and migrate if needed
	if err := cm.migrateConfig(&versionedConfig); err != nil {
		return fmt.Errorf("failed to migrate config: %w", err)
	}

	// Validate configuration
	if err := cm.validateConfig(&versionedConfig.Config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	cm.config = &versionedConfig.Config
	return nil
}

// Save saves configuration to file
func (cm *ConfigManager) Save() error {
	versionedConfig := VersionedConfig{
		Version: ConfigVersion,
		Config:  *cm.config,
	}

	// Validate before saving
	if err := cm.validateConfig(cm.config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Marshal to YAML
	data, err := yaml.Marshal(&versionedConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file with secure permissions
	if err := os.WriteFile(cm.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// Get returns the current configuration
func (cm *ConfigManager) Get() *Config {
	return cm.config
}

// Set updates the configuration
func (cm *ConfigManager) Set(config *Config) error {
	if err := cm.validateConfig(config); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}
	
	cm.config = config
	return nil
}

// Update updates specific configuration values using dot notation
func (cm *ConfigManager) Update(key string, value interface{}) error {
	if err := cm.updateConfigValue(cm.config, key, value); err != nil {
		return fmt.Errorf("failed to update config: %w", err)
	}
	
	if err := cm.validateConfig(cm.config); err != nil {
		return fmt.Errorf("invalid configuration after update: %w", err)
	}
	
	return nil
}

// GetConfigPath returns the path to the configuration file
func (cm *ConfigManager) GetConfigPath() string {
	return cm.configPath
}

// validateConfig validates the configuration structure and values
func (cm *ConfigManager) validateConfig(config *Config) error {
	// Validate Gmail configuration
	if config.Gmail.Username != "" {
		if config.Gmail.IMAPServer == "" {
			return fmt.Errorf("IMAP server is required when username is set")
		}
		if config.Gmail.SMTPServer == "" {
			return fmt.Errorf("SMTP server is required when username is set")
		}
		if config.Gmail.IMAPPort <= 0 || config.Gmail.IMAPPort > 65535 {
			return fmt.Errorf("invalid IMAP port: %d", config.Gmail.IMAPPort)
		}
		if config.Gmail.SMTPPort <= 0 || config.Gmail.SMTPPort > 65535 {
			return fmt.Errorf("invalid SMTP port: %d", config.Gmail.SMTPPort)
		}
		if config.Gmail.AuthMethod != "oauth2" && config.Gmail.AuthMethod != "password" {
			return fmt.Errorf("invalid auth method: %s", config.Gmail.AuthMethod)
		}
	}

	// Validate storage configuration
	if config.Storage.RepositoryPath == "" {
		return fmt.Errorf("repository path is required")
	}
	if config.Storage.MaxFileSize <= 0 {
		return fmt.Errorf("max file size must be positive")
	}

	// Validate security configuration
	if config.Security.CredentialStorage != "keychain" && config.Security.CredentialStorage != "file" {
		return fmt.Errorf("invalid credential storage: %s", config.Security.CredentialStorage)
	}

	// Validate CLI configuration
	if config.CLI.PageSize <= 0 {
		return fmt.Errorf("page size must be positive")
	}

	// Validate sync configuration
	if config.Sync.MaxRetries < 0 {
		return fmt.Errorf("max retries cannot be negative")
	}
	if config.Sync.BatchSize <= 0 {
		return fmt.Errorf("batch size must be positive")
	}
	if config.Sync.ConflictStrategy != "remote_wins" && config.Sync.ConflictStrategy != "local_wins" && config.Sync.ConflictStrategy != "manual" {
		return fmt.Errorf("invalid conflict strategy: %s", config.Sync.ConflictStrategy)
	}

	// Validate filters
	for i, filter := range config.Filters {
		if filter.Name == "" {
			return fmt.Errorf("filter %d: name is required", i)
		}
		if filter.Type != "shell" && filter.Type != "ai" {
			return fmt.Errorf("filter %d: invalid type: %s", i, filter.Type)
		}
		if filter.Script == "" && filter.Type == "shell" {
			return fmt.Errorf("filter %d: script is required for shell filters", i)
		}
	}

	return nil
}

// migrateConfig handles configuration version migration
func (cm *ConfigManager) migrateConfig(versionedConfig *VersionedConfig) error {
	switch versionedConfig.Version {
	case "":
		// Legacy config without version, assume 1.0.0
		versionedConfig.Version = "1.0.0"
		fallthrough
	case "1.0.0":
		// Current version, no migration needed
		return nil
	default:
		return fmt.Errorf("unsupported config version: %s", versionedConfig.Version)
	}
}

// updateConfigValue updates a configuration value using dot notation
func (cm *ConfigManager) updateConfigValue(config *Config, key string, value interface{}) error {
	parts := strings.Split(key, ".")
	if len(parts) == 0 {
		return fmt.Errorf("empty key")
	}

	switch parts[0] {
	case "gmail":
		return cm.updateGmailConfig(&config.Gmail, parts[1:], value)
	case "storage":
		return cm.updateStorageConfig(&config.Storage, parts[1:], value)
	case "security":
		return cm.updateSecurityConfig(&config.Security, parts[1:], value)
	case "cli":
		return cm.updateCLIConfig(&config.CLI, parts[1:], value)
	case "sync":
		return cm.updateSyncConfig(&config.Sync, parts[1:], value)
	default:
		return fmt.Errorf("unknown config section: %s", parts[0])
	}
}

// updateGmailConfig updates Gmail configuration values
func (cm *ConfigManager) updateGmailConfig(gmail *GmailConfig, parts []string, value interface{}) error {
	if len(parts) == 0 {
		return fmt.Errorf("missing gmail config key")
	}

	switch parts[0] {
	case "username":
		if str, ok := value.(string); ok {
			gmail.Username = str
		} else {
			return fmt.Errorf("username must be a string")
		}
	case "imap_server":
		if str, ok := value.(string); ok {
			gmail.IMAPServer = str
		} else {
			return fmt.Errorf("imap_server must be a string")
		}
	case "smtp_server":
		if str, ok := value.(string); ok {
			gmail.SMTPServer = str
		} else {
			return fmt.Errorf("smtp_server must be a string")
		}
	case "imap_port":
		if port, ok := value.(int); ok {
			gmail.IMAPPort = port
		} else {
			return fmt.Errorf("imap_port must be an integer")
		}
	case "smtp_port":
		if port, ok := value.(int); ok {
			gmail.SMTPPort = port
		} else {
			return fmt.Errorf("smtp_port must be an integer")
		}
	case "auth_method":
		if str, ok := value.(string); ok {
			gmail.AuthMethod = str
		} else {
			return fmt.Errorf("auth_method must be a string")
		}
	case "use_tls":
		if b, ok := value.(bool); ok {
			gmail.UseTLS = b
		} else {
			return fmt.Errorf("use_tls must be a boolean")
		}
	case "use_starttls":
		if b, ok := value.(bool); ok {
			gmail.UseStartTLS = b
		} else {
			return fmt.Errorf("use_starttls must be a boolean")
		}
	default:
		return fmt.Errorf("unknown gmail config key: %s", parts[0])
	}

	return nil
}

// updateStorageConfig updates storage configuration values
func (cm *ConfigManager) updateStorageConfig(storage *StorageConfig, parts []string, value interface{}) error {
	if len(parts) == 0 {
		return fmt.Errorf("missing storage config key")
	}

	switch parts[0] {
	case "repository_path":
		if str, ok := value.(string); ok {
			storage.RepositoryPath = str
		} else {
			return fmt.Errorf("repository_path must be a string")
		}
	case "index_path":
		if str, ok := value.(string); ok {
			storage.IndexPath = str
		} else {
			return fmt.Errorf("index_path must be a string")
		}
	case "max_file_size":
		if size, ok := value.(int64); ok {
			storage.MaxFileSize = size
		} else {
			return fmt.Errorf("max_file_size must be an integer")
		}
	case "compression":
		if b, ok := value.(bool); ok {
			storage.Compression = b
		} else {
			return fmt.Errorf("compression must be a boolean")
		}
	case "encryption":
		if b, ok := value.(bool); ok {
			storage.Encryption = b
		} else {
			return fmt.Errorf("encryption must be a boolean")
		}
	case "backup_enabled":
		if b, ok := value.(bool); ok {
			storage.BackupEnabled = b
		} else {
			return fmt.Errorf("backup_enabled must be a boolean")
		}
	case "backup_path":
		if str, ok := value.(string); ok {
			storage.BackupPath = str
		} else {
			return fmt.Errorf("backup_path must be a string")
		}
	default:
		return fmt.Errorf("unknown storage config key: %s", parts[0])
	}

	return nil
}

// updateSecurityConfig updates security configuration values
func (cm *ConfigManager) updateSecurityConfig(security *SecurityConfig, parts []string, value interface{}) error {
	if len(parts) == 0 {
		return fmt.Errorf("missing security config key")
	}

	switch parts[0] {
	case "keychain_service":
		if str, ok := value.(string); ok {
			security.KeychainService = str
		} else {
			return fmt.Errorf("keychain_service must be a string")
		}
	case "credential_storage":
		if str, ok := value.(string); ok {
			security.CredentialStorage = str
		} else {
			return fmt.Errorf("credential_storage must be a string")
		}
	case "tls_verify":
		if b, ok := value.(bool); ok {
			security.TLSVerify = b
		} else {
			return fmt.Errorf("tls_verify must be a boolean")
		}
	case "certificate_path":
		if str, ok := value.(string); ok {
			security.CertificatePath = str
		} else {
			return fmt.Errorf("certificate_path must be a string")
		}
	default:
		return fmt.Errorf("unknown security config key: %s", parts[0])
	}

	return nil
}

// updateCLIConfig updates CLI configuration values
func (cm *ConfigManager) updateCLIConfig(cli *CLIConfig, parts []string, value interface{}) error {
	if len(parts) == 0 {
		return fmt.Errorf("missing cli config key")
	}

	switch parts[0] {
	case "editor":
		if str, ok := value.(string); ok {
			cli.Editor = str
		} else {
			return fmt.Errorf("editor must be a string")
		}
	case "pager":
		if str, ok := value.(string); ok {
			cli.Pager = str
		} else {
			return fmt.Errorf("pager must be a string")
		}
	case "default_format":
		if str, ok := value.(string); ok {
			cli.DefaultFormat = str
		} else {
			return fmt.Errorf("default_format must be a string")
		}
	case "color_enabled":
		if b, ok := value.(bool); ok {
			cli.ColorEnabled = b
		} else {
			return fmt.Errorf("color_enabled must be a boolean")
		}
	case "page_size":
		if size, ok := value.(int); ok {
			cli.PageSize = size
		} else {
			return fmt.Errorf("page_size must be an integer")
		}
	case "date_format":
		if str, ok := value.(string); ok {
			cli.DateFormat = str
		} else {
			return fmt.Errorf("date_format must be a string")
		}
	case "timezone":
		if str, ok := value.(string); ok {
			cli.TimeZone = str
		} else {
			return fmt.Errorf("timezone must be a string")
		}
	default:
		return fmt.Errorf("unknown cli config key: %s", parts[0])
	}

	return nil
}

// updateSyncConfig updates sync configuration values
func (cm *ConfigManager) updateSyncConfig(sync *SyncConfig, parts []string, value interface{}) error {
	if len(parts) == 0 {
		return fmt.Errorf("missing sync config key")
	}

	switch parts[0] {
	case "auto_sync":
		if b, ok := value.(bool); ok {
			sync.AutoSync = b
		} else {
			return fmt.Errorf("auto_sync must be a boolean")
		}
	case "max_retries":
		if retries, ok := value.(int); ok {
			sync.MaxRetries = retries
		} else {
			return fmt.Errorf("max_retries must be an integer")
		}
	case "batch_size":
		if size, ok := value.(int); ok {
			sync.BatchSize = size
		} else {
			return fmt.Errorf("batch_size must be an integer")
		}
	case "conflict_strategy":
		if str, ok := value.(string); ok {
			sync.ConflictStrategy = str
		} else {
			return fmt.Errorf("conflict_strategy must be a string")
		}
	case "offline_mode":
		if b, ok := value.(bool); ok {
			sync.OfflineMode = b
		} else {
			return fmt.Errorf("offline_mode must be a boolean")
		}
	default:
		return fmt.Errorf("unknown sync config key: %s", parts[0])
	}

	return nil
}