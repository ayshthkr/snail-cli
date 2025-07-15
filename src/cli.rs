use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "git-mail")]
#[command(about = "Terminal-based, offline-first email client that stores emails as plain-text files in a Git repository")]
#[command(version)]
pub struct Cli {
    #[command(subcommand)]
    pub command: Option<Commands>,

    /// Enable verbose logging
    #[arg(short, long)]
    pub verbose: bool,

    /// Configuration file path
    #[arg(short, long)]
    pub config: Option<String>,

    /// Repository path (defaults to ~/.git-mail)
    #[arg(short, long)]
    pub repo: Option<String>,
}

#[derive(Subcommand)]
pub enum Commands {
    /// Initialize a new Git-Mail repository
    Init {
        /// Repository path
        path: Option<String>,
    },
    /// Start the terminal interface
    Tui,
    /// Sync emails with configured accounts
    Sync {
        /// Account name to sync (sync all if not specified)
        account: Option<String>,
    },
    /// Compose a new email
    Compose {
        /// Recipient email address
        to: Option<String>,
        /// Email subject
        subject: Option<String>,
    },
    /// Search emails
    Search {
        /// Search query
        query: String,
        /// Search in specific folder
        #[arg(short, long)]
        folder: Option<String>,
    },
    /// List emails
    List {
        /// Folder to list (defaults to inbox)
        folder: Option<String>,
        /// Number of emails to show
        #[arg(short, long, default_value = "20")]
        count: usize,
    },
    /// Show email content
    Show {
        /// Email ID
        id: String,
    },
    /// Account management
    Account {
        #[command(subcommand)]
        action: AccountAction,
    },
}

#[derive(Subcommand)]
pub enum AccountAction {
    /// Add a new account
    Add {
        /// Account name
        name: String,
    },
    /// List configured accounts
    List,
    /// Remove an account
    Remove {
        /// Account name
        name: String,
    },
    /// Test account connection
    Test {
        /// Account name
        name: String,
    },
}