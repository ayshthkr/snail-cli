//! Configuration management for Git-Mail

use crate::models::{Account, FilterScript};
use anyhow::{Context, Result};
use dirs::config_dir;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use tracing::{debug, info, warn};

/// Current configuration version for migration support
const CONFIG_VERSION: u32 = 1;

/// Main configuration structure
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Config {
    /// Configuration version for migration
    pub version: u32,
    /// Default account name
    pub default_account: Option<String>,
    /// Email accounts
    pub accounts: HashMap<String, Account>,
    /// Global filter scripts
    pub filters: Vec<FilterScript>,
    /// Application settings
    pub settings: AppSettings,
}

/// Application settings
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AppSettings {
    /// Git repository path for email storage
    pub repository_path: PathBuf,
    /// Default text editor command
    pub editor: String,
    /// Auto-sync interval in minutes (0 = disabled)
    pub auto_sync_interval: u32,
    /// Maximum number of emails to fetch per sync
    pub max_fetch_count: u32,
    /// Enable debug logging
    pub debug_mode: bool,
    /// Terminal interface settings
    pub ui: UiSettings,
}

/// Terminal UI settings
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct UiSettings {
    /// Show email preview in inbox
    pub show_preview: bool,
    /// Number of emails to display per page
    pub emails_per_page: u32,
    /// Date format for email display
    pub date_format: String,
    /// Color scheme
    pub color_scheme: String,
    /// Keyboard shortcuts
    pub shortcuts: HashMap<String, String>,
}

impl Default for Config {
    fn default() -> Self {
        Self {
            version: CONFIG_VERSION,
            default_account: None,
            accounts: HashMap::new(),
            filters: Vec::new(),
            settings: AppSettings::default(),
        }
    }
}

impl Default for AppSettings {
    fn default() -> Self {
        let home_dir = dirs::home_dir().unwrap_or_else(|| PathBuf::from("."));

        Self {
            repository_path: home_dir.join(".git-mail"),
            editor: std::env::var("EDITOR").unwrap_or_else(|_| "vim".to_string()),
            auto_sync_interval: 15, // 15 minutes
            max_fetch_count: 100,
            debug_mode: false,
            ui: UiSettings::default(),
        }
    }
}

impl Default for UiSettings {
    fn default() -> Self {
        let mut shortcuts = HashMap::new();
        shortcuts.insert("quit".to_string(), "q".to_string());
        shortcuts.insert("compose".to_string(), "c".to_string());
        shortcuts.insert("reply".to_string(), "r".to_string());
        shortcuts.insert("forward".to_string(), "f".to_string());
        shortcuts.insert("delete".to_string(), "d".to_string());
        shortcuts.insert("mark_read".to_string(), "m".to_string());
        shortcuts.insert("star".to_string(), "s".to_string());
        shortcuts.insert("sync".to_string(), "S".to_string());
        shortcuts.insert("search".to_string(), "/".to_string());
        shortcuts.insert("help".to_string(), "?".to_string());

        Self {
            show_preview: true,
            emails_per_page: 25,
            date_format: "%Y-%m-%d %H:%M".to_string(),
            color_scheme: "default".to_string(),
            shortcuts,
        }
    }
}

/// Configuration manager
pub struct ConfigManager {
    config_path: PathBuf,
    config: Config,
}

impl ConfigManager {
    /// Create a new configuration manager
    pub fn new() -> Result<Self> {
        let config_path = Self::get_config_path()?;
        let config = if config_path.exists() {
            Self::load_from_file(&config_path)?
        } else {
            Config::default()
        };

        Ok(Self {
            config_path,
            config,
        })
    }

    /// Get the configuration file path
    fn get_config_path() -> Result<PathBuf> {
        let config_dir = config_dir().context("Failed to get user config directory")?;

        let git_mail_dir = config_dir.join("git-mail");
        if !git_mail_dir.exists() {
            fs::create_dir_all(&git_mail_dir)
                .context("Failed to create git-mail config directory")?;
        }

        Ok(git_mail_dir.join("config.toml"))
    }

    /// Load configuration from file
    fn load_from_file(path: &Path) -> Result<Config> {
        debug!("Loading configuration from: {}", path.display());

        let content = fs::read_to_string(path)
            .with_context(|| format!("Failed to read config file: {}", path.display()))?;

        let mut config: Config = toml::from_str(&content)
            .with_context(|| format!("Failed to parse config file: {}", path.display()))?;

        // Perform migration if needed
        if config.version < CONFIG_VERSION {
            info!(
                "Migrating configuration from version {} to {}",
                config.version, CONFIG_VERSION
            );
            config = Self::migrate_config(config)?;
        }

        // Validate configuration
        config.validate()?;

        // Load credentials from keyring for all accounts
        for account in config.accounts.values_mut() {
            if let Err(e) = account.load_credentials() {
                warn!(
                    "Failed to load credentials for account '{}': {}",
                    account.name, e
                );
            }
        }

        Ok(config)
    }

    /// Migrate configuration to current version
    fn migrate_config(mut config: Config) -> Result<Config> {
        // Future migration logic would go here
        // For now, just update the version
        config.version = CONFIG_VERSION;
        Ok(config)
    }

    /// Save configuration to file
    pub fn save(&self) -> Result<()> {
        debug!("Saving configuration to: {}", self.config_path.display());

        // Create a copy of config without passwords for serialization
        let mut config_to_save = self.config.clone();

        // Clear passwords from accounts (they're stored in keyring)
        for account in config_to_save.accounts.values_mut() {
            account.incoming.password.clear();
            account.outgoing.password.clear();
        }

        let content =
            toml::to_string_pretty(&config_to_save).context("Failed to serialize configuration")?;

        fs::write(&self.config_path, content).with_context(|| {
            format!(
                "Failed to write config file: {}",
                self.config_path.display()
            )
        })?;

        info!("Configuration saved successfully");
        Ok(())
    }

    /// Get current configuration
    pub fn get_config(&self) -> &Config {
        &self.config
    }

    /// Get mutable reference to configuration
    pub fn get_config_mut(&mut self) -> &mut Config {
        &mut self.config
    }

    /// Add a new account
    pub fn add_account(&mut self, account: Account) -> Result<()> {
        // Validate account
        account
            .validate()
            .map_err(|e| anyhow::anyhow!("Account validation failed: {}", e))?;

        // Store credentials in keyring
        account
            .store_credentials()
            .map_err(|e| anyhow::anyhow!("Failed to store account credentials: {}", e))?;

        // Add to configuration
        self.config.accounts.insert(account.name.clone(), account);

        // Set as default if it's the first account
        if self.config.default_account.is_none() {
            self.config.default_account = Some(self.config.accounts.keys().next().unwrap().clone());
        }

        self.save()?;
        Ok(())
    }

    /// Remove an account
    pub fn remove_account(&mut self, account_name: &str) -> Result<()> {
        if let Some(account) = self.config.accounts.get(account_name) {
            // Delete credentials from keyring
            if let Err(e) = account.delete_credentials() {
                warn!(
                    "Failed to delete credentials for account '{}': {}",
                    account_name, e
                );
            }
        }

        // Remove from configuration
        self.config.accounts.remove(account_name);

        // Update default account if necessary
        if self.config.default_account.as_ref() == Some(&account_name.to_string()) {
            self.config.default_account = self.config.accounts.keys().next().cloned();
        }

        self.save()?;
        Ok(())
    }

    /// Update an existing account
    pub fn update_account(&mut self, account: Account) -> Result<()> {
        // Validate account
        account
            .validate()
            .map_err(|e| anyhow::anyhow!("Account validation failed: {}", e))?;

        // Store credentials in keyring
        account
            .store_credentials()
            .map_err(|e| anyhow::anyhow!("Failed to store account credentials: {}", e))?;

        // Update configuration
        self.config.accounts.insert(account.name.clone(), account);

        self.save()?;
        Ok(())
    }

    /// Get an account by name
    pub fn get_account(&self, name: &str) -> Option<&Account> {
        self.config.accounts.get(name)
    }

    /// Get all account names
    pub fn get_account_names(&self) -> Vec<String> {
        self.config.accounts.keys().cloned().collect()
    }

    /// Get default account
    pub fn get_default_account(&self) -> Option<&Account> {
        self.config
            .default_account
            .as_ref()
            .and_then(|name| self.config.accounts.get(name))
    }

    /// Set default account
    pub fn set_default_account(&mut self, account_name: &str) -> Result<()> {
        if !self.config.accounts.contains_key(account_name) {
            return Err(anyhow::anyhow!("Account '{}' does not exist", account_name));
        }

        self.config.default_account = Some(account_name.to_string());
        self.save()?;
        Ok(())
    }

    /// Add a filter script
    pub fn add_filter(&mut self, filter: FilterScript) -> Result<()> {
        // Validate filter script exists
        if !Path::new(&filter.path).exists() {
            return Err(anyhow::anyhow!(
                "Filter script does not exist: {}",
                filter.path
            ));
        }

        self.config.filters.push(filter);
        self.config.filters.sort_by_key(|f| f.order);
        self.save()?;
        Ok(())
    }

    /// Remove a filter script
    pub fn remove_filter(&mut self, filter_name: &str) -> Result<()> {
        self.config.filters.retain(|f| f.name != filter_name);
        self.save()?;
        Ok(())
    }

    /// Update application settings
    pub fn update_settings(&mut self, settings: AppSettings) -> Result<()> {
        self.config.settings = settings;
        self.save()?;
        Ok(())
    }

    /// Create default configuration file
    pub fn create_default_config() -> Result<()> {
        let config_path = Self::get_config_path()?;

        if config_path.exists() {
            return Err(anyhow::anyhow!(
                "Configuration file already exists: {}",
                config_path.display()
            ));
        }

        let config = Config::default();
        let content =
            toml::to_string_pretty(&config).context("Failed to serialize default configuration")?;

        fs::write(&config_path, content).with_context(|| {
            format!(
                "Failed to write default config file: {}",
                config_path.display()
            )
        })?;

        info!(
            "Default configuration created at: {}",
            config_path.display()
        );
        Ok(())
    }

    /// Validate configuration file exists and is readable
    pub fn validate_config_file() -> Result<()> {
        let config_path = Self::get_config_path()?;

        if !config_path.exists() {
            return Err(anyhow::anyhow!(
                "Configuration file does not exist: {}",
                config_path.display()
            ));
        }

        let _config = Self::load_from_file(&config_path)?;
        Ok(())
    }
}

impl Config {
    /// Validate the entire configuration
    pub fn validate(&self) -> Result<()> {
        // Validate version
        if self.version > CONFIG_VERSION {
            return Err(anyhow::anyhow!(
                "Configuration version {} is newer than supported version {}",
                self.version,
                CONFIG_VERSION
            ));
        }

        // Validate default account exists
        if let Some(default_account) = &self.default_account {
            if !self.accounts.contains_key(default_account) {
                return Err(anyhow::anyhow!(
                    "Default account '{}' does not exist in accounts list",
                    default_account
                ));
            }
        }

        // Validate all accounts
        for (name, account) in &self.accounts {
            if name != &account.name {
                return Err(anyhow::anyhow!(
                    "Account key '{}' does not match account name '{}'",
                    name,
                    account.name
                ));
            }

            account
                .validate()
                .map_err(|e| anyhow::anyhow!("Account '{}' validation failed: {}", name, e))?;
        }

        // Validate filter scripts exist
        for filter in &self.filters {
            if !Path::new(&filter.path).exists() {
                return Err(anyhow::anyhow!(
                    "Filter script '{}' does not exist: {}",
                    filter.name,
                    filter.path
                ));
            }
        }

        // Validate settings
        self.settings.validate()?;

        Ok(())
    }
}

impl AppSettings {
    /// Validate application settings
    pub fn validate(&self) -> Result<()> {
        // Validate repository path is absolute or can be made absolute
        if !self.repository_path.is_absolute() {
            return Err(anyhow::anyhow!(
                "Repository path must be absolute: {}",
                self.repository_path.display()
            ));
        }

        // Validate editor command exists
        if self.editor.is_empty() {
            return Err(anyhow::anyhow!("Editor command cannot be empty"));
        }

        // Validate UI settings
        self.ui.validate()?;

        Ok(())
    }
}

impl UiSettings {
    /// Validate UI settings
    pub fn validate(&self) -> Result<()> {
        if self.emails_per_page == 0 {
            return Err(anyhow::anyhow!("Emails per page must be greater than 0"));
        }

        if self.emails_per_page > 1000 {
            return Err(anyhow::anyhow!("Emails per page cannot exceed 1000"));
        }

        // Validate date format by trying to format current time
        let now = chrono::Utc::now();
        // Try to format - if it panics or fails, the format is invalid
        // For now, we'll do a basic validation that it's not empty
        if self.date_format.is_empty() {
            return Err(anyhow::anyhow!("Date format cannot be empty"));
        }

        // Try to format to validate the format string
        let _formatted = now.format(&self.date_format).to_string();

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::{IncomingConfig, OutgoingConfig};
    use tempfile::TempDir;

    fn create_test_account() -> Account {
        Account {
            name: "test_account".to_string(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "test_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "test_password".to_string(),
                ssl: true,
            },
            filters: vec![],
        }
    }

    #[test]
    fn test_default_config() {
        let config = Config::default();
        assert_eq!(config.version, CONFIG_VERSION);
        assert!(config.accounts.is_empty());
        assert!(config.filters.is_empty());
        assert!(config.default_account.is_none());
    }

    #[test]
    fn test_config_validation_success() {
        let config = Config::default();
        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_config_validation_invalid_default_account() {
        let mut config = Config::default();
        config.default_account = Some("nonexistent".to_string());

        let result = config.validate();
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Default account 'nonexistent' does not exist"));
    }

    #[test]
    fn test_config_validation_mismatched_account_key() {
        let mut config = Config::default();
        let account = create_test_account();
        config.accounts.insert("wrong_key".to_string(), account);

        let result = config.validate();
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Account key 'wrong_key' does not match"));
    }

    #[test]
    fn test_app_settings_validation_success() {
        let settings = AppSettings::default();
        assert!(settings.validate().is_ok());
    }

    #[test]
    fn test_app_settings_validation_empty_editor() {
        let mut settings = AppSettings::default();
        settings.editor = String::new();

        let result = settings.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "Editor command cannot be empty"
        );
    }

    #[test]
    fn test_ui_settings_validation_success() {
        let settings = UiSettings::default();
        assert!(settings.validate().is_ok());
    }

    #[test]
    fn test_ui_settings_validation_zero_emails_per_page() {
        let mut settings = UiSettings::default();
        settings.emails_per_page = 0;

        let result = settings.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "Emails per page must be greater than 0"
        );
    }

    #[test]
    fn test_ui_settings_validation_too_many_emails_per_page() {
        let mut settings = UiSettings::default();
        settings.emails_per_page = 1001;

        let result = settings.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err().to_string(),
            "Emails per page cannot exceed 1000"
        );
    }

    #[test]
    fn test_config_serialization() {
        let config = Config::default();
        let toml_str = toml::to_string(&config).unwrap();
        let deserialized: Config = toml::from_str(&toml_str).unwrap();

        assert_eq!(config.version, deserialized.version);
        assert_eq!(config.accounts.len(), deserialized.accounts.len());
        assert_eq!(config.filters.len(), deserialized.filters.len());
    }

    #[test]
    fn test_config_with_account_serialization() {
        let mut config = Config::default();
        let account = create_test_account();
        config.accounts.insert(account.name.clone(), account);
        config.default_account = Some("test_account".to_string());

        let toml_str = toml::to_string(&config).unwrap();
        let deserialized: Config = toml::from_str(&toml_str).unwrap();

        assert_eq!(config.accounts.len(), deserialized.accounts.len());
        assert_eq!(config.default_account, deserialized.default_account);

        let deserialized_account = deserialized.accounts.get("test_account").unwrap();
        assert_eq!(deserialized_account.email, "test@example.com");
    }
}
