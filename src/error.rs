use std::fmt;
use thiserror::Error;

/// Error context information for better debugging and user experience
#[derive(Debug, Clone)]
pub struct ErrorContext {
    /// Operation that was being performed when error occurred
    pub operation: String,
    /// Component where the error originated
    pub component: String,
    /// Additional context information
    pub details: Vec<String>,
    /// Suggested recovery actions
    pub recovery_suggestions: Vec<String>,
    /// Whether this error is recoverable
    pub is_recoverable: bool,
    /// Error severity level
    pub severity: ErrorSeverity,
}

/// Error severity levels
#[derive(Debug, Clone, PartialEq)]
pub enum ErrorSeverity {
    /// Critical errors that prevent the application from functioning
    Critical,
    /// Errors that prevent specific operations but don't crash the app
    Error,
    /// Warnings that indicate potential issues
    Warning,
    /// Informational messages about recoverable issues
    Info,
}

/// Recovery strategy for different types of errors
#[derive(Debug, Clone)]
pub enum RecoveryStrategy {
    /// Retry the operation with the same parameters
    Retry,
    /// Retry with exponential backoff
    RetryWithBackoff {
        max_attempts: u32,
        base_delay_ms: u64,
    },
    /// Prompt user for input/confirmation
    PromptUser,
    /// Use fallback mechanism
    Fallback,
    /// Skip the operation and continue
    Skip,
    /// Abort the current operation
    Abort,
    /// Manual intervention required
    Manual,
}

/// Main error type for Git-Mail application with enhanced context
#[derive(Error, Debug)]
pub enum GitMailError {
    #[error("Git operation failed: {0}")]
    Git(#[from] git2::Error),

    #[error("IO error: {0}")]
    Io(#[from] std::io::Error),

    #[error("Email parsing error: {0}")]
    EmailParsing(String),

    #[error("IMAP error: {0}")]
    Imap(#[from] imap::Error),

    #[error("SMTP error: {0}")]
    Smtp(String),

    #[error("Serialization error: {0}")]
    Serialization(#[from] serde_json::Error),

    #[error("Configuration error: {0}")]
    Config(String),

    #[error("Filter execution error: {0}")]
    Filter(String),

    #[error("Editor integration error: {0}")]
    Editor(String),

    #[error("Terminal interface error: {0}")]
    Terminal(String),

    #[error("Authentication error: {0}")]
    Auth(String),

    #[error("Network error: {0}")]
    Network(String),

    #[error("Repository not found or invalid: {0}")]
    Repository(String),

    #[error("Email not found: {0}")]
    EmailNotFound(String),

    #[error("Account configuration error: {0}")]
    Account(String),

    #[error("Validation error: {0}")]
    Validation(String),

    #[error("Sync conflict: {0}")]
    Conflict(String),

    #[error("Not found: {0}")]
    NotFound(String),

    #[error("Storage error: {0}")]
    Storage(String),

    #[error("Draft not found: {0}")]
    DraftNotFound(String),

    #[error("Search error: {0}")]
    Search(String),

    #[error("Plugin error: {0}")]
    PluginError(String),

    #[error("External tool error: {0}")]
    ExternalToolError(String),

    /// Enhanced error with context information
    #[error("{message}")]
    WithContext {
        message: String,
        context: ErrorContext,
        #[source]
        source: Option<Box<GitMailError>>,
    },
}

/// Enhanced result type with error context
pub type Result<T> = std::result::Result<T, GitMailError>;

impl GitMailError {
    /// Add context to an error
    pub fn with_context(self, context: ErrorContext) -> Self {
        GitMailError::WithContext {
            message: self.to_string(),
            context,
            source: Some(Box::new(self)),
        }
    }

    /// Create a new error with context
    pub fn new_with_context(message: String, context: ErrorContext) -> Self {
        GitMailError::WithContext {
            message,
            context,
            source: None,
        }
    }

    /// Get error context if available
    pub fn context(&self) -> Option<&ErrorContext> {
        match self {
            GitMailError::WithContext { context, .. } => Some(context),
            _ => None,
        }
    }

    /// Get user-friendly error message
    pub fn user_message(&self) -> String {
        match self {
            GitMailError::WithContext {
                message, context, ..
            } => {
                let mut msg = message.clone();

                if !context.recovery_suggestions.is_empty() {
                    msg.push_str("\n\nSuggested actions:");
                    for suggestion in &context.recovery_suggestions {
                        msg.push_str(&format!("\n  • {}", suggestion));
                    }
                }

                msg
            }
            GitMailError::Git(e) => format!(
                "Git operation failed: {}. Check repository permissions and disk space.",
                e
            ),
            GitMailError::Io(e) => format!(
                "File system error: {}. Check file permissions and available disk space.",
                e
            ),
            GitMailError::Network(e) => format!(
                "Network error: {}. Check your internet connection and server settings.",
                e
            ),
            GitMailError::Auth(e) => format!(
                "Authentication failed: {}. Verify your credentials and account settings.",
                e
            ),
            GitMailError::Config(e) => format!(
                "Configuration error: {}. Check your configuration file and account settings.",
                e
            ),
            GitMailError::EmailNotFound(id) => format!(
                "Email '{}' not found. It may have been moved or deleted.",
                id
            ),
            GitMailError::DraftNotFound(id) => format!(
                "Draft '{}' not found. It may have been deleted or corrupted.",
                id
            ),
            _ => self.to_string(),
        }
    }

    /// Get recovery strategy for this error
    pub fn recovery_strategy(&self) -> RecoveryStrategy {
        match self {
            GitMailError::WithContext { context, .. } => {
                // Use context-specific recovery strategy if available
                match context.severity {
                    ErrorSeverity::Critical => RecoveryStrategy::Abort,
                    ErrorSeverity::Error => RecoveryStrategy::PromptUser,
                    ErrorSeverity::Warning => RecoveryStrategy::Skip,
                    ErrorSeverity::Info => RecoveryStrategy::Skip,
                }
            }
            GitMailError::Network(_) => RecoveryStrategy::RetryWithBackoff {
                max_attempts: 3,
                base_delay_ms: 1000,
            },
            GitMailError::Auth(_) => RecoveryStrategy::PromptUser,
            GitMailError::Config(_) => RecoveryStrategy::PromptUser,
            GitMailError::Git(_) => RecoveryStrategy::Manual,
            GitMailError::Io(_) => RecoveryStrategy::Retry,
            GitMailError::EmailNotFound(_) | GitMailError::DraftNotFound(_) => {
                RecoveryStrategy::Skip
            }
            _ => RecoveryStrategy::Abort,
        }
    }

    /// Check if error is recoverable
    pub fn is_recoverable(&self) -> bool {
        match self {
            GitMailError::WithContext { context, .. } => context.is_recoverable,
            GitMailError::Network(_) | GitMailError::Auth(_) | GitMailError::Config(_) => true,
            GitMailError::EmailNotFound(_) | GitMailError::DraftNotFound(_) => true,
            GitMailError::Io(_) => true,
            _ => false,
        }
    }

    /// Get error severity
    pub fn severity(&self) -> ErrorSeverity {
        match self {
            GitMailError::WithContext { context, .. } => context.severity.clone(),
            GitMailError::Git(_) => ErrorSeverity::Critical,
            GitMailError::Repository(_) => ErrorSeverity::Critical,
            GitMailError::Network(_) | GitMailError::Auth(_) => ErrorSeverity::Error,
            GitMailError::Config(_) | GitMailError::Account(_) => ErrorSeverity::Error,
            GitMailError::EmailNotFound(_) | GitMailError::DraftNotFound(_) => {
                ErrorSeverity::Warning
            }
            _ => ErrorSeverity::Error,
        }
    }
}

impl ErrorContext {
    /// Create a new error context
    pub fn new(operation: String, component: String) -> Self {
        Self {
            operation,
            component,
            details: Vec::new(),
            recovery_suggestions: Vec::new(),
            is_recoverable: false,
            severity: ErrorSeverity::Error,
        }
    }

    /// Add detail information
    pub fn with_detail(mut self, detail: String) -> Self {
        self.details.push(detail);
        self
    }

    /// Add recovery suggestion
    pub fn with_suggestion(mut self, suggestion: String) -> Self {
        self.recovery_suggestions.push(suggestion);
        self
    }

    /// Set recoverable flag
    pub fn recoverable(mut self, recoverable: bool) -> Self {
        self.is_recoverable = recoverable;
        self
    }

    /// Set severity level
    pub fn with_severity(mut self, severity: ErrorSeverity) -> Self {
        self.severity = severity;
        self
    }
}

impl fmt::Display for ErrorSeverity {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            ErrorSeverity::Critical => write!(f, "CRITICAL"),
            ErrorSeverity::Error => write!(f, "ERROR"),
            ErrorSeverity::Warning => write!(f, "WARNING"),
            ErrorSeverity::Info => write!(f, "INFO"),
        }
    }
}

/// Helper trait for adding context to results
pub trait ResultExt<T> {
    /// Add context to a result
    fn with_context<F>(self, f: F) -> Result<T>
    where
        F: FnOnce() -> ErrorContext;

    /// Add simple context with operation and component
    fn with_operation_context(self, operation: &str, component: &str) -> Result<T>;
}

impl<T, E> ResultExt<T> for std::result::Result<T, E>
where
    E: Into<GitMailError>,
{
    fn with_context<F>(self, f: F) -> Result<T>
    where
        F: FnOnce() -> ErrorContext,
    {
        self.map_err(|e| e.into().with_context(f()))
    }

    fn with_operation_context(self, operation: &str, component: &str) -> Result<T> {
        self.with_context(|| {
            ErrorContext::new(operation.to_string(), component.to_string())
                .recoverable(true)
                .with_severity(ErrorSeverity::Error)
        })
    }
}

/// Error handler for managing error recovery and user interaction
pub struct ErrorHandler {
    /// Whether to show detailed error information
    pub verbose: bool,
    /// Maximum retry attempts for recoverable errors
    pub max_retries: u32,
}

impl ErrorHandler {
    /// Create a new error handler
    pub fn new(verbose: bool) -> Self {
        Self {
            verbose,
            max_retries: 3,
        }
    }

    /// Handle an error with appropriate recovery strategy
    pub async fn handle_error(&self, error: &GitMailError) -> RecoveryAction {
        let strategy = error.recovery_strategy();
        let severity = error.severity();

        // Log the error
        match severity {
            ErrorSeverity::Critical => tracing::error!("{}", error.user_message()),
            ErrorSeverity::Error => tracing::error!("{}", error.user_message()),
            ErrorSeverity::Warning => tracing::warn!("{}", error.user_message()),
            ErrorSeverity::Info => tracing::info!("{}", error.user_message()),
        }

        // Show detailed information in verbose mode
        if self.verbose {
            if let Some(context) = error.context() {
                tracing::debug!(
                    "Error context: operation={}, component={}",
                    context.operation,
                    context.component
                );
                for detail in &context.details {
                    tracing::debug!("  Detail: {}", detail);
                }
            }
        }

        // Determine recovery action based on strategy
        match strategy {
            RecoveryStrategy::Retry => RecoveryAction::Retry,
            RecoveryStrategy::RetryWithBackoff {
                max_attempts,
                base_delay_ms,
            } => RecoveryAction::RetryWithBackoff {
                max_attempts,
                base_delay_ms,
            },
            RecoveryStrategy::PromptUser => RecoveryAction::PromptUser,
            RecoveryStrategy::Fallback => RecoveryAction::Fallback,
            RecoveryStrategy::Skip => RecoveryAction::Skip,
            RecoveryStrategy::Abort => RecoveryAction::Abort,
            RecoveryStrategy::Manual => RecoveryAction::Manual,
        }
    }

    /// Format error for display to user
    pub fn format_error_for_user(&self, error: &GitMailError) -> String {
        let mut output = String::new();

        // Add severity indicator
        let severity = error.severity();
        output.push_str(&format!("[{}] ", severity));

        // Add user-friendly message
        output.push_str(&error.user_message());

        // Add context details in verbose mode
        if self.verbose {
            if let Some(context) = error.context() {
                output.push_str(&format!("\n\nOperation: {}", context.operation));
                output.push_str(&format!("\nComponent: {}", context.component));

                if !context.details.is_empty() {
                    output.push_str("\nDetails:");
                    for detail in &context.details {
                        output.push_str(&format!("\n  • {}", detail));
                    }
                }
            }
        }

        output
    }
}

/// Actions that can be taken in response to an error
#[derive(Debug, Clone)]
pub enum RecoveryAction {
    /// Retry the operation immediately
    Retry,
    /// Retry with exponential backoff
    RetryWithBackoff {
        max_attempts: u32,
        base_delay_ms: u64,
    },
    /// Prompt user for input or confirmation
    PromptUser,
    /// Use fallback mechanism
    Fallback,
    /// Skip the operation and continue
    Skip,
    /// Abort the current operation
    Abort,
    /// Manual intervention required
    Manual,
}
/// Helper functions for creating common error contexts
impl ErrorContext {
    /// Create context for Git operations
    pub fn git_operation(operation: &str) -> Self {
        Self::new(operation.to_string(), "git_storage".to_string())
            .with_severity(ErrorSeverity::Critical)
            .with_suggestion("Check repository permissions and disk space".to_string())
            .with_suggestion("Ensure Git repository is properly initialized".to_string())
    }

    /// Create context for network operations
    pub fn network_operation(operation: &str, server: &str) -> Self {
        Self::new(operation.to_string(), "network".to_string())
            .with_detail(format!("Server: {}", server))
            .with_severity(ErrorSeverity::Error)
            .recoverable(true)
            .with_suggestion("Check your internet connection".to_string())
            .with_suggestion("Verify server settings and credentials".to_string())
            .with_suggestion("Try again in a few moments".to_string())
    }

    /// Create context for authentication operations
    pub fn auth_operation(operation: &str, account: &str) -> Self {
        Self::new(operation.to_string(), "authentication".to_string())
            .with_detail(format!("Account: {}", account))
            .with_severity(ErrorSeverity::Error)
            .recoverable(true)
            .with_suggestion("Verify your username and password".to_string())
            .with_suggestion("Check if two-factor authentication is required".to_string())
            .with_suggestion("Update your account credentials".to_string())
    }

    /// Create context for file operations
    pub fn file_operation(operation: &str, path: &str) -> Self {
        Self::new(operation.to_string(), "file_system".to_string())
            .with_detail(format!("Path: {}", path))
            .with_severity(ErrorSeverity::Error)
            .recoverable(true)
            .with_suggestion("Check file permissions".to_string())
            .with_suggestion("Ensure sufficient disk space".to_string())
            .with_suggestion("Verify the file path exists".to_string())
    }

    /// Create context for configuration operations
    pub fn config_operation(operation: &str, config_file: &str) -> Self {
        Self::new(operation.to_string(), "configuration".to_string())
            .with_detail(format!("Config file: {}", config_file))
            .with_severity(ErrorSeverity::Error)
            .recoverable(true)
            .with_suggestion("Check configuration file syntax".to_string())
            .with_suggestion("Verify all required fields are present".to_string())
            .with_suggestion("Run 'git-mail config validate' to check configuration".to_string())
    }

    /// Create context for email operations
    pub fn email_operation(operation: &str, email_id: &str) -> Self {
        Self::new(operation.to_string(), "email_processing".to_string())
            .with_detail(format!("Email ID: {}", email_id))
            .with_severity(ErrorSeverity::Warning)
            .recoverable(true)
            .with_suggestion("Check if the email still exists".to_string())
            .with_suggestion("Try refreshing the email list".to_string())
    }

    /// Create context for sync operations
    pub fn sync_operation(operation: &str, account: &str) -> Self {
        Self::new(operation.to_string(), "sync_engine".to_string())
            .with_detail(format!("Account: {}", account))
            .with_severity(ErrorSeverity::Error)
            .recoverable(true)
            .with_suggestion("Check network connectivity".to_string())
            .with_suggestion("Verify account credentials".to_string())
            .with_suggestion("Try syncing individual folders".to_string())
    }

    /// Create context for filter operations
    pub fn filter_operation(operation: &str, filter_name: &str) -> Self {
        Self::new(operation.to_string(), "filter_engine".to_string())
            .with_detail(format!("Filter: {}", filter_name))
            .with_severity(ErrorSeverity::Warning)
            .recoverable(true)
            .with_suggestion("Check filter script syntax".to_string())
            .with_suggestion("Verify script permissions".to_string())
            .with_suggestion("Test filter script manually".to_string())
    }

    /// Create context for plugin operations
    pub fn plugin_operation(operation: &str, plugin_name: &str) -> Self {
        Self::new(operation.to_string(), "plugin_system".to_string())
            .with_detail(format!("Plugin: {}", plugin_name))
            .with_severity(ErrorSeverity::Warning)
            .recoverable(true)
            .with_suggestion("Check plugin installation".to_string())
            .with_suggestion("Verify plugin permissions".to_string())
            .with_suggestion("Update plugin to latest version".to_string())
    }
}

/// Utility functions for error handling
pub mod utils {
    use super::*;
    use std::time::Duration;
    use tokio::time::sleep;

    /// Retry an operation with exponential backoff
    pub async fn retry_with_backoff<T, F, Fut>(
        mut operation: F,
        max_attempts: u32,
        base_delay_ms: u64,
    ) -> Result<T>
    where
        F: FnMut() -> Fut,
        Fut: std::future::Future<Output = Result<T>>,
    {
        let mut attempts = 0;
        let mut delay = base_delay_ms;

        loop {
            attempts += 1;

            match operation().await {
                Ok(result) => return Ok(result),
                Err(error) => {
                    if attempts >= max_attempts || !error.is_recoverable() {
                        return Err(error);
                    }

                    tracing::warn!(
                        "Operation failed (attempt {}/{}), retrying in {}ms: {}",
                        attempts,
                        max_attempts,
                        delay,
                        error
                    );

                    sleep(Duration::from_millis(delay)).await;
                    delay = std::cmp::min(delay * 2, 30000); // Cap at 30 seconds
                }
            }
        }
    }

    /// Execute operation with timeout and error context
    pub async fn with_timeout_and_context<T, F, Fut>(
        operation: F,
        timeout: Duration,
        context: ErrorContext,
    ) -> Result<T>
    where
        F: FnOnce() -> Fut,
        Fut: std::future::Future<Output = Result<T>>,
    {
        match tokio::time::timeout(timeout, operation()).await {
            Ok(result) => result,
            Err(_) => Err(GitMailError::new_with_context(
                format!("Operation timed out after {:?}", timeout),
                context
                    .with_detail(format!("Timeout: {:?}", timeout))
                    .with_suggestion("Try increasing the timeout value".to_string())
                    .with_suggestion("Check network connectivity".to_string()),
            )),
        }
    }

    /// Validate and sanitize user input
    pub fn validate_email_address(email: &str) -> Result<()> {
        if email.is_empty() {
            return Err(GitMailError::Validation(
                "Email address cannot be empty".to_string(),
            ));
        }

        if !email.contains('@') {
            return Err(GitMailError::Validation(
                "Invalid email address format".to_string(),
            ));
        }

        // Basic email validation - in production, use a proper email validation library
        let parts: Vec<&str> = email.split('@').collect();
        if parts.len() != 2 || parts[0].is_empty() || parts[1].is_empty() {
            return Err(GitMailError::Validation(
                "Invalid email address format".to_string(),
            ));
        }

        Ok(())
    }

    /// Sanitize file path to prevent directory traversal
    pub fn sanitize_file_path(path: &str) -> Result<String> {
        if path.contains("..") || path.contains("//") {
            return Err(GitMailError::Validation(
                "Invalid file path: contains unsafe characters".to_string(),
            ));
        }

        if path.starts_with('/') {
            return Err(GitMailError::Validation(
                "Absolute paths are not allowed".to_string(),
            ));
        }

        Ok(path.to_string())
    }

    /// Check if a directory is writable
    pub async fn check_directory_writable(path: &std::path::Path) -> Result<()> {
        use tokio::fs;

        if !path.exists() {
            return Err(GitMailError::Io(std::io::Error::new(
                std::io::ErrorKind::NotFound,
                format!("Directory does not exist: {}", path.display()),
            )));
        }

        // Try to create a temporary file to test write permissions
        let test_file = path.join(".git-mail-write-test");
        match fs::write(&test_file, "test").await {
            Ok(()) => {
                // Clean up test file
                let _ = fs::remove_file(&test_file).await;
                Ok(())
            }
            Err(e) => Err(GitMailError::Io(e).with_context(
                ErrorContext::file_operation("write_test", &path.to_string_lossy())
                    .with_suggestion("Check directory permissions".to_string())
                    .with_suggestion("Ensure you have write access to the directory".to_string()),
            )),
        }
    }

    /// Format file size for user display
    pub fn format_file_size(bytes: u64) -> String {
        const UNITS: &[&str] = &["B", "KB", "MB", "GB", "TB"];
        let mut size = bytes as f64;
        let mut unit_index = 0;

        while size >= 1024.0 && unit_index < UNITS.len() - 1 {
            size /= 1024.0;
            unit_index += 1;
        }

        if unit_index == 0 {
            format!("{} {}", bytes, UNITS[unit_index])
        } else {
            format!("{:.1} {}", size, UNITS[unit_index])
        }
    }

    /// Check system resources and warn if low
    pub async fn check_system_resources() -> Vec<String> {
        let mut warnings = Vec::new();

        // Check available disk space
        if let Ok(metadata) = tokio::fs::metadata(".").await {
            // This is a simplified check - in production, you'd use a proper system info crate
            if metadata.len() == 0 {
                warnings.push("Unable to determine disk space".to_string());
            }
        }

        // Check memory usage (simplified)
        // In production, use a system info crate like `sysinfo`

        warnings
    }
}

/// Macro for creating error contexts with less boilerplate
#[macro_export]
macro_rules! error_context {
    ($operation:expr, $component:expr) => {
        $crate::error::ErrorContext::new($operation.to_string(), $component.to_string())
    };

    ($operation:expr, $component:expr, $severity:expr) => {
        $crate::error::ErrorContext::new($operation.to_string(), $component.to_string())
            .with_severity($severity)
    };

    ($operation:expr, $component:expr, $severity:expr, recoverable: $recoverable:expr) => {
        $crate::error::ErrorContext::new($operation.to_string(), $component.to_string())
            .with_severity($severity)
            .recoverable($recoverable)
    };
}

/// Macro for adding context to results with less boilerplate
#[macro_export]
macro_rules! with_context {
    ($result:expr, $operation:expr, $component:expr) => {
        $result.with_operation_context($operation, $component)
    };
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_error_context_creation() {
        let context = ErrorContext::git_operation("commit");
        assert_eq!(context.operation, "commit");
        assert_eq!(context.component, "git_storage");
        assert_eq!(context.severity, ErrorSeverity::Critical);
        assert!(!context.recovery_suggestions.is_empty());
    }

    #[test]
    fn test_error_severity_display() {
        assert_eq!(ErrorSeverity::Critical.to_string(), "CRITICAL");
        assert_eq!(ErrorSeverity::Error.to_string(), "ERROR");
        assert_eq!(ErrorSeverity::Warning.to_string(), "WARNING");
        assert_eq!(ErrorSeverity::Info.to_string(), "INFO");
    }

    #[test]
    fn test_error_recovery_strategy() {
        let network_error = GitMailError::Network("Connection failed".to_string());
        match network_error.recovery_strategy() {
            RecoveryStrategy::RetryWithBackoff {
                max_attempts,
                base_delay_ms,
            } => {
                assert_eq!(max_attempts, 3);
                assert_eq!(base_delay_ms, 1000);
            }
            _ => panic!("Expected RetryWithBackoff strategy"),
        }
    }

    #[test]
    fn test_error_user_message() {
        let error = GitMailError::EmailNotFound("test123".to_string());
        let message = error.user_message();
        assert!(message.contains("Email 'test123' not found"));
        assert!(message.contains("moved or deleted"));
    }

    #[tokio::test]
    async fn test_validate_email_address() {
        use utils::validate_email_address;

        assert!(validate_email_address("test@example.com").is_ok());
        assert!(validate_email_address("").is_err());
        assert!(validate_email_address("invalid").is_err());
        assert!(validate_email_address("@example.com").is_err());
        assert!(validate_email_address("test@").is_err());
    }

    #[test]
    fn test_sanitize_file_path() {
        use utils::sanitize_file_path;

        assert!(sanitize_file_path("valid/path.txt").is_ok());
        assert!(sanitize_file_path("../invalid").is_err());
        assert!(sanitize_file_path("/absolute/path").is_err());
        assert!(sanitize_file_path("path//with//double//slash").is_err());
    }

    #[test]
    fn test_format_file_size() {
        use utils::format_file_size;

        assert_eq!(format_file_size(512), "512 B");
        assert_eq!(format_file_size(1024), "1.0 KB");
        assert_eq!(format_file_size(1536), "1.5 KB");
        assert_eq!(format_file_size(1048576), "1.0 MB");
    }
}
