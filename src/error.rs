use thiserror::Error;

/// Main error type for Git-Mail application
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
}

pub type Result<T> = std::result::Result<T, GitMailError>;
