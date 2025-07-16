use clap::{Parser, Subcommand};

#[derive(Parser)]
#[command(name = "git-mail")]
#[command(
    about = "Terminal-based, offline-first email client that stores emails as plain-text files in a Git repository"
)]
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
    /// Reply to an email
    Reply {
        /// Email ID to reply to
        id: String,
        /// Account to reply from (optional)
        #[arg(short, long)]
        account: Option<String>,
    },
    /// Forward an email
    Forward {
        /// Email ID to forward
        id: String,
        /// Account to forward from (optional)
        #[arg(short, long)]
        account: Option<String>,
    },
    /// Draft management
    Draft {
        #[command(subcommand)]
        action: DraftAction,
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
    /// Plugin management and execution
    Plugin {
        #[command(subcommand)]
        action: PluginAction,
    },
    /// External tool integration and piping
    Tool {
        #[command(subcommand)]
        action: ToolAction,
    },
}

#[derive(Subcommand)]
pub enum DraftAction {
    /// List all drafts
    List,
    /// Show draft content
    Show {
        /// Draft ID
        id: String,
    },
    /// Edit a draft
    Edit {
        /// Draft ID
        id: String,
    },
    /// Delete a draft
    Delete {
        /// Draft ID
        id: String,
    },
    /// Send a draft
    Send {
        /// Draft ID
        id: String,
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

#[derive(Subcommand)]
pub enum PluginAction {
    /// List available plugins
    List,
    /// Execute a plugin command
    Execute {
        /// Plugin name
        name: String,
        /// Plugin arguments
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
    },
    /// Show plugin information
    Info {
        /// Plugin name
        name: String,
    },
    /// Reload plugins from directories
    Reload,
}

#[derive(Subcommand)]
pub enum ToolAction {
    /// Pipe emails to an external command
    Pipe {
        /// Command to execute
        command: String,
        /// Command arguments
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
        /// Email IDs to pipe (comma-separated)
        #[arg(short, long)]
        emails: String,
        /// Current folder context
        #[arg(short, long)]
        folder: Option<String>,
    },
    /// Execute external command without email data
    Exec {
        /// Command to execute
        command: String,
        /// Command arguments
        #[arg(trailing_var_arg = true, allow_hyphen_values = true)]
        args: Vec<String>,
        /// Current folder context
        #[arg(short, long)]
        folder: Option<String>,
    },
    /// Grep through emails
    Grep {
        /// Grep pattern
        pattern: String,
        /// Email IDs to search (comma-separated)
        #[arg(short, long)]
        emails: String,
        /// Additional grep options
        #[arg(short, long)]
        options: Option<String>,
    },
    /// Process emails with awk
    Awk {
        /// Awk script
        script: String,
        /// Email IDs to process (comma-separated)
        #[arg(short, long)]
        emails: String,
    },
    /// Count lines in emails
    Count {
        /// Email IDs to count (comma-separated)
        #[arg(short, long)]
        emails: String,
        /// Count type (lines, words, chars)
        #[arg(short, long, default_value = "lines")]
        count_type: String,
    },
}
