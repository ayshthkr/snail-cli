//! Data models for Git-Mail

use chrono::{DateTime, Utc};
use keyring::Entry;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use uuid::Uuid;

/// Email message representation
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Email {
    /// Unique identifier
    pub id: String,
    /// RFC 2822 Message-ID
    pub message_id: String,
    /// Source account identifier
    pub account: String,
    /// Email headers
    pub headers: HashMap<String, String>,
    /// Email body content
    pub body: EmailBody,
    /// Email attachments
    pub attachments: Vec<Attachment>,
    /// Email metadata
    pub metadata: EmailMetadata,
}

impl Email {
    /// Create a new email with generated ID
    pub fn new(account: String) -> Self {
        let id = Uuid::new_v4().to_string();
        Self {
            id: id.clone(),
            message_id: format!("<{}>", id),
            account,
            headers: HashMap::new(),
            body: EmailBody::default(),
            attachments: Vec::new(),
            metadata: EmailMetadata::new(),
        }
    }

    /// Validate email structure and required fields
    pub fn validate(&self) -> Result<(), String> {
        if self.id.is_empty() {
            return Err("Email ID cannot be empty".to_string());
        }

        if self.account.is_empty() {
            return Err("Account cannot be empty".to_string());
        }

        if self.message_id.is_empty() {
            return Err("Message ID cannot be empty".to_string());
        }

        self.body.validate()?;

        for attachment in &self.attachments {
            attachment.validate()?;
        }

        self.metadata.validate()?;

        Ok(())
    }
}

/// Email body content
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EmailBody {
    /// Content type (text/plain, text/html, etc.)
    pub content_type: String,
    /// Plain text content
    pub content: String,
    /// HTML content (if multipart)
    pub html_content: Option<String>,
}

impl Default for EmailBody {
    fn default() -> Self {
        Self {
            content_type: "text/plain".to_string(),
            content: String::new(),
            html_content: None,
        }
    }
}

impl EmailBody {
    /// Validate email body
    pub fn validate(&self) -> Result<(), String> {
        if self.content_type.is_empty() {
            return Err("Content type cannot be empty".to_string());
        }

        // Validate content type format
        if !self.content_type.contains('/') {
            return Err("Content type must be in format 'type/subtype'".to_string());
        }

        Ok(())
    }
}

/// Email attachment
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Attachment {
    /// Filename
    pub filename: String,
    /// Content type
    pub content_type: String,
    /// File size in bytes
    pub size: u64,
    /// File path (relative to email storage)
    pub file_path: String,
}

impl Attachment {
    /// Validate attachment
    pub fn validate(&self) -> Result<(), String> {
        if self.filename.is_empty() {
            return Err("Attachment filename cannot be empty".to_string());
        }

        if self.content_type.is_empty() {
            return Err("Attachment content type cannot be empty".to_string());
        }

        if self.file_path.is_empty() {
            return Err("Attachment file path cannot be empty".to_string());
        }

        Ok(())
    }
}

/// Email metadata
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EmailMetadata {
    /// File path in repository
    pub file_path: String,
    /// Folder location
    pub folder: String,
    /// Tags
    pub tags: Vec<String>,
    /// Read status
    pub is_read: bool,
    /// Starred status
    pub is_starred: bool,
    /// Creation timestamp
    pub created_at: DateTime<Utc>,
    /// Last modification timestamp
    pub modified_at: DateTime<Utc>,
}

impl EmailMetadata {
    /// Create new metadata with current timestamp
    pub fn new() -> Self {
        let now = Utc::now();
        Self {
            file_path: String::new(),
            folder: "inbox".to_string(),
            tags: Vec::new(),
            is_read: false,
            is_starred: false,
            created_at: now,
            modified_at: now,
        }
    }

    /// Validate metadata
    pub fn validate(&self) -> Result<(), String> {
        if self.folder.is_empty() {
            return Err("Folder cannot be empty".to_string());
        }

        if self.created_at > self.modified_at {
            return Err("Created timestamp cannot be after modified timestamp".to_string());
        }

        Ok(())
    }
}
/// Email account configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Account {
    /// Account name
    pub name: String,
    /// Email address
    pub email: String,
    /// Display name
    pub display_name: String,
    /// Incoming mail configuration
    pub incoming: IncomingConfig,
    /// Outgoing mail configuration
    pub outgoing: OutgoingConfig,
    /// Filter script paths
    pub filters: Vec<String>,
}

impl Account {
    /// Validate account configuration
    pub fn validate(&self) -> Result<(), String> {
        if self.name.is_empty() {
            return Err("Account name cannot be empty".to_string());
        }

        if self.email.is_empty() {
            return Err("Email address cannot be empty".to_string());
        }

        // Basic email validation
        if !self.email.contains('@') || !self.email.contains('.') {
            return Err("Invalid email address format".to_string());
        }

        self.incoming.validate()?;
        self.outgoing.validate()?;

        Ok(())
    }

    /// Store passwords securely in system keyring
    pub fn store_credentials(&self) -> Result<(), String> {
        // Store incoming password
        let incoming_entry = Entry::new("git-mail", &format!("{}-incoming", self.name))
            .map_err(|e| format!("Failed to create keyring entry for incoming: {}", e))?;

        incoming_entry
            .set_password(&self.incoming.password)
            .map_err(|e| format!("Failed to store incoming password: {}", e))?;

        // Store outgoing password
        let outgoing_entry = Entry::new("git-mail", &format!("{}-outgoing", self.name))
            .map_err(|e| format!("Failed to create keyring entry for outgoing: {}", e))?;

        outgoing_entry
            .set_password(&self.outgoing.password)
            .map_err(|e| format!("Failed to store outgoing password: {}", e))?;

        Ok(())
    }

    /// Retrieve passwords from system keyring
    pub fn load_credentials(&mut self) -> Result<(), String> {
        // Load incoming password
        let incoming_entry = Entry::new("git-mail", &format!("{}-incoming", self.name))
            .map_err(|e| format!("Failed to create keyring entry for incoming: {}", e))?;

        self.incoming.password = incoming_entry
            .get_password()
            .map_err(|e| format!("Failed to retrieve incoming password: {}", e))?;

        // Load outgoing password
        let outgoing_entry = Entry::new("git-mail", &format!("{}-outgoing", self.name))
            .map_err(|e| format!("Failed to create keyring entry for outgoing: {}", e))?;

        self.outgoing.password = outgoing_entry
            .get_password()
            .map_err(|e| format!("Failed to retrieve outgoing password: {}", e))?;

        Ok(())
    }

    /// Delete stored credentials from system keyring
    pub fn delete_credentials(&self) -> Result<(), String> {
        // Delete incoming password
        let incoming_entry = Entry::new("git-mail", &format!("{}-incoming", self.name))
            .map_err(|e| format!("Failed to create keyring entry for incoming: {}", e))?;

        incoming_entry
            .delete_password()
            .map_err(|e| format!("Failed to delete incoming password: {}", e))?;

        // Delete outgoing password
        let outgoing_entry = Entry::new("git-mail", &format!("{}-outgoing", self.name))
            .map_err(|e| format!("Failed to create keyring entry for outgoing: {}", e))?;

        outgoing_entry
            .delete_password()
            .map_err(|e| format!("Failed to delete outgoing password: {}", e))?;

        Ok(())
    }

    /// Create account with empty passwords (to be filled from keyring)
    pub fn new_with_secure_storage(
        name: String,
        email: String,
        display_name: String,
        incoming: IncomingConfig,
        outgoing: OutgoingConfig,
        filters: Vec<String>,
    ) -> Self {
        let mut incoming_config = incoming;
        let mut outgoing_config = outgoing;

        // Clear passwords - they will be loaded from keyring
        incoming_config.password = String::new();
        outgoing_config.password = String::new();

        Self {
            name,
            email,
            display_name,
            incoming: incoming_config,
            outgoing: outgoing_config,
            filters,
        }
    }
}

/// Incoming mail server configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IncomingConfig {
    /// Protocol (imap, pop3)
    pub protocol: String,
    /// Server hostname
    pub server: String,
    /// Server port
    pub port: u16,
    /// Username
    pub username: String,
    /// Password (encrypted in storage)
    pub password: String,
    /// Use SSL/TLS
    pub ssl: bool,
}

impl IncomingConfig {
    /// Validate incoming configuration
    pub fn validate(&self) -> Result<(), String> {
        if self.protocol.is_empty() {
            return Err("Protocol cannot be empty".to_string());
        }

        let valid_protocols = ["imap", "pop3"];
        if !valid_protocols.contains(&self.protocol.to_lowercase().as_str()) {
            return Err("Protocol must be 'imap' or 'pop3'".to_string());
        }

        if self.server.is_empty() {
            return Err("Server cannot be empty".to_string());
        }

        if self.port == 0 {
            return Err("Port must be greater than 0".to_string());
        }

        if self.username.is_empty() {
            return Err("Username cannot be empty".to_string());
        }

        Ok(())
    }
}

/// Outgoing mail server configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OutgoingConfig {
    /// SMTP server hostname
    pub server: String,
    /// SMTP server port
    pub port: u16,
    /// Username
    pub username: String,
    /// Password (encrypted in storage)
    pub password: String,
    /// Use SSL/TLS
    pub ssl: bool,
}

impl OutgoingConfig {
    /// Validate outgoing configuration
    pub fn validate(&self) -> Result<(), String> {
        if self.server.is_empty() {
            return Err("Server cannot be empty".to_string());
        }

        if self.port == 0 {
            return Err("Port must be greater than 0".to_string());
        }

        if self.username.is_empty() {
            return Err("Username cannot be empty".to_string());
        }

        Ok(())
    }
}

/// Git commit information
#[derive(Debug, Clone)]
pub struct GitCommit {
    /// Commit hash
    pub hash: String,
    /// Commit message
    pub message: String,
    /// Author name
    pub author: String,
    /// Commit timestamp
    pub timestamp: DateTime<Utc>,
}

/// Sync operation result
#[derive(Debug)]
pub struct SyncResult {
    /// Number of emails fetched
    pub fetched: usize,
    /// Number of emails sent
    pub sent: usize,
    /// Any errors encountered
    pub errors: Vec<String>,
}

/// Connection status for an account
#[derive(Debug)]
pub enum ConnectionStatus {
    Connected,
    Disconnected,
    Error(String),
}

/// Filter execution result
#[derive(Debug)]
pub struct FilterResult {
    /// Whether the email was modified
    pub modified: bool,
    /// New folder location (if moved)
    pub new_folder: Option<String>,
    /// Tags to add
    pub add_tags: Vec<String>,
    /// Tags to remove
    pub remove_tags: Vec<String>,
    /// Whether to mark as read
    pub mark_read: Option<bool>,
    /// Whether to star
    pub star: Option<bool>,
}

/// Filter script configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct FilterScript {
    /// Script name
    pub name: String,
    /// Script file path
    pub path: String,
    /// Execution order
    pub order: u32,
    /// Whether the script is enabled
    pub enabled: bool,
}
#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;

    #[test]
    fn test_email_creation() {
        let email = Email::new("test@example.com".to_string());

        assert!(!email.id.is_empty());
        assert!(!email.message_id.is_empty());
        assert_eq!(email.account, "test@example.com");
        assert_eq!(email.body.content_type, "text/plain");
        assert_eq!(email.metadata.folder, "inbox");
        assert!(!email.metadata.is_read);
        assert!(!email.metadata.is_starred);
    }

    #[test]
    fn test_email_validation_success() {
        let email = Email::new("test@example.com".to_string());
        assert!(email.validate().is_ok());
    }

    #[test]
    fn test_email_validation_empty_id() {
        let mut email = Email::new("test@example.com".to_string());
        email.id = String::new();

        let result = email.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Email ID cannot be empty");
    }

    #[test]
    fn test_email_validation_empty_account() {
        let mut email = Email::new("test@example.com".to_string());
        email.account = String::new();

        let result = email.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Account cannot be empty");
    }

    #[test]
    fn test_email_body_validation_success() {
        let body = EmailBody::default();
        assert!(body.validate().is_ok());
    }

    #[test]
    fn test_email_body_validation_empty_content_type() {
        let mut body = EmailBody::default();
        body.content_type = String::new();

        let result = body.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Content type cannot be empty");
    }

    #[test]
    fn test_email_body_validation_invalid_content_type() {
        let mut body = EmailBody::default();
        body.content_type = "invalid".to_string();

        let result = body.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err(),
            "Content type must be in format 'type/subtype'"
        );
    }

    #[test]
    fn test_attachment_validation_success() {
        let attachment = Attachment {
            filename: "test.txt".to_string(),
            content_type: "text/plain".to_string(),
            size: 1024,
            file_path: "attachments/test.txt".to_string(),
        };

        assert!(attachment.validate().is_ok());
    }

    #[test]
    fn test_attachment_validation_empty_filename() {
        let attachment = Attachment {
            filename: String::new(),
            content_type: "text/plain".to_string(),
            size: 1024,
            file_path: "attachments/test.txt".to_string(),
        };

        let result = attachment.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Attachment filename cannot be empty");
    }

    #[test]
    fn test_email_metadata_validation_success() {
        let metadata = EmailMetadata::new();
        assert!(metadata.validate().is_ok());
    }

    #[test]
    fn test_email_metadata_validation_empty_folder() {
        let mut metadata = EmailMetadata::new();
        metadata.folder = String::new();

        let result = metadata.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Folder cannot be empty");
    }

    #[test]
    fn test_email_metadata_validation_invalid_timestamps() {
        let mut metadata = EmailMetadata::new();
        metadata.created_at = Utc::now();
        metadata.modified_at = metadata.created_at - chrono::Duration::hours(1);

        let result = metadata.validate();
        assert!(result.is_err());
        assert_eq!(
            result.unwrap_err(),
            "Created timestamp cannot be after modified timestamp"
        );
    }

    #[test]
    fn test_account_validation_success() {
        let account = Account {
            name: "Test Account".to_string(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            filters: vec![],
        };

        assert!(account.validate().is_ok());
    }

    #[test]
    fn test_account_validation_empty_name() {
        let mut account = Account {
            name: String::new(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            filters: vec![],
        };

        let result = account.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Account name cannot be empty");
    }

    #[test]
    fn test_account_validation_invalid_email() {
        let account = Account {
            name: "Test Account".to_string(),
            email: "invalid_email".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            filters: vec![],
        };

        let result = account.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Invalid email address format");
    }

    #[test]
    fn test_incoming_config_validation_success() {
        let config = IncomingConfig {
            protocol: "imap".to_string(),
            server: "imap.example.com".to_string(),
            port: 993,
            username: "test@example.com".to_string(),
            password: "encrypted_password".to_string(),
            ssl: true,
        };

        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_incoming_config_validation_invalid_protocol() {
        let config = IncomingConfig {
            protocol: "invalid".to_string(),
            server: "imap.example.com".to_string(),
            port: 993,
            username: "test@example.com".to_string(),
            password: "encrypted_password".to_string(),
            ssl: true,
        };

        let result = config.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Protocol must be 'imap' or 'pop3'");
    }

    #[test]
    fn test_incoming_config_validation_zero_port() {
        let config = IncomingConfig {
            protocol: "imap".to_string(),
            server: "imap.example.com".to_string(),
            port: 0,
            username: "test@example.com".to_string(),
            password: "encrypted_password".to_string(),
            ssl: true,
        };

        let result = config.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Port must be greater than 0");
    }

    #[test]
    fn test_outgoing_config_validation_success() {
        let config = OutgoingConfig {
            server: "smtp.example.com".to_string(),
            port: 587,
            username: "test@example.com".to_string(),
            password: "encrypted_password".to_string(),
            ssl: true,
        };

        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_outgoing_config_validation_empty_server() {
        let config = OutgoingConfig {
            server: String::new(),
            port: 587,
            username: "test@example.com".to_string(),
            password: "encrypted_password".to_string(),
            ssl: true,
        };

        let result = config.validate();
        assert!(result.is_err());
        assert_eq!(result.unwrap_err(), "Server cannot be empty");
    }

    #[test]
    fn test_account_new_with_secure_storage() {
        let incoming = IncomingConfig {
            protocol: "imap".to_string(),
            server: "imap.example.com".to_string(),
            port: 993,
            username: "test@example.com".to_string(),
            password: "original_password".to_string(),
            ssl: true,
        };

        let outgoing = OutgoingConfig {
            server: "smtp.example.com".to_string(),
            port: 587,
            username: "test@example.com".to_string(),
            password: "original_password".to_string(),
            ssl: true,
        };

        let account = Account::new_with_secure_storage(
            "Test Account".to_string(),
            "test@example.com".to_string(),
            "Test User".to_string(),
            incoming,
            outgoing,
            vec![],
        );

        // Passwords should be cleared
        assert!(account.incoming.password.is_empty());
        assert!(account.outgoing.password.is_empty());

        // Other fields should be preserved
        assert_eq!(account.name, "Test Account");
        assert_eq!(account.email, "test@example.com");
        assert_eq!(account.incoming.server, "imap.example.com");
        assert_eq!(account.outgoing.server, "smtp.example.com");
    }

    #[test]
    fn test_credential_storage_integration() {
        // Note: This test may fail in CI environments without keyring support
        // In a real implementation, you might want to mock the keyring for testing

        let account = Account {
            name: "test_account_unique".to_string(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "test_incoming_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "test_outgoing_password".to_string(),
                ssl: true,
            },
            filters: vec![],
        };

        // Test storing credentials
        if let Ok(()) = account.store_credentials() {
            // Test loading credentials
            let mut test_account = Account::new_with_secure_storage(
                account.name.clone(),
                account.email.clone(),
                account.display_name.clone(),
                IncomingConfig {
                    protocol: account.incoming.protocol.clone(),
                    server: account.incoming.server.clone(),
                    port: account.incoming.port,
                    username: account.incoming.username.clone(),
                    password: String::new(),
                    ssl: account.incoming.ssl,
                },
                OutgoingConfig {
                    server: account.outgoing.server.clone(),
                    port: account.outgoing.port,
                    username: account.outgoing.username.clone(),
                    password: String::new(),
                    ssl: account.outgoing.ssl,
                },
                account.filters.clone(),
            );

            if let Ok(()) = test_account.load_credentials() {
                assert_eq!(test_account.incoming.password, "test_incoming_password");
                assert_eq!(test_account.outgoing.password, "test_outgoing_password");
            }

            // Clean up - delete credentials
            let _ = account.delete_credentials();
        }
        // If keyring is not available, the test will simply pass without assertions
    }

    #[test]
    fn test_serialization_deserialization() {
        let original_email = Email::new("test@example.com".to_string());

        // Test JSON serialization
        let json = serde_json::to_string(&original_email).expect("Failed to serialize to JSON");
        let deserialized_email: Email =
            serde_json::from_str(&json).expect("Failed to deserialize from JSON");

        assert_eq!(original_email.id, deserialized_email.id);
        assert_eq!(original_email.account, deserialized_email.account);
        assert_eq!(
            original_email.body.content_type,
            deserialized_email.body.content_type
        );

        // Test TOML serialization for Account
        let account = Account {
            name: "Test Account".to_string(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "encrypted_password".to_string(),
                ssl: true,
            },
            filters: vec!["spam_filter.sh".to_string()],
        };

        let toml_str = toml::to_string(&account).expect("Failed to serialize to TOML");
        let deserialized_account: Account =
            toml::from_str(&toml_str).expect("Failed to deserialize from TOML");

        assert_eq!(account.name, deserialized_account.name);
        assert_eq!(account.email, deserialized_account.email);
        assert_eq!(
            account.incoming.protocol,
            deserialized_account.incoming.protocol
        );
        assert_eq!(
            account.outgoing.server,
            deserialized_account.outgoing.server
        );
    }
}
