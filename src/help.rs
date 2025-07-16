//! Comprehensive help system for Git-Mail CLI

use crate::cli::{AccountAction, Commands, DraftAction, PluginAction, ToolAction};
use std::collections::HashMap;

/// Help system for providing detailed command information and examples
pub struct HelpSystem {
    command_help: HashMap<String, CommandHelp>,
}

/// Detailed help information for a command
#[derive(Debug, Clone)]
pub struct CommandHelp {
    pub description: String,
    pub usage: String,
    pub examples: Vec<Example>,
    pub related_commands: Vec<String>,
    pub notes: Vec<String>,
}

/// Command usage example
#[derive(Debug, Clone)]
pub struct Example {
    pub description: String,
    pub command: String,
}

impl HelpSystem {
    /// Create a new help system with all command documentation
    pub fn new() -> Self {
        let mut help_system = Self {
            command_help: HashMap::new(),
        };
        help_system.initialize_help();
        help_system
    }

    /// Get help for a specific command
    pub fn get_command_help(&self, command: &str) -> Option<&CommandHelp> {
        self.command_help.get(command)
    }

    /// Get all available commands
    pub fn get_all_commands(&self) -> Vec<String> {
        self.command_help.keys().cloned().collect()
    }

    /// Display comprehensive help for a command
    pub fn display_help(&self, command: &str) -> String {
        if let Some(help) = self.get_command_help(command) {
            self.format_help(command, help)
        } else {
            format!("No help available for command: {}", command)
        }
    }

    /// Display general help overview
    pub fn display_overview(&self) -> String {
        let mut output = String::new();
        output.push_str("Git-Mail - Terminal-based, offline-first email client\n\n");
        output.push_str("DESCRIPTION:\n");
        output.push_str("    Git-Mail stores emails as plain-text files in a Git repository,\n");
        output.push_str("    treating email management like code development - versioned,\n");
        output.push_str("    scriptable, and portable.\n\n");

        output.push_str("USAGE:\n");
        output.push_str("    git-mail [OPTIONS] <COMMAND>\n\n");

        output.push_str("COMMANDS:\n");
        let mut commands: Vec<_> = self.command_help.iter().collect();
        commands.sort_by_key(|(name, _)| *name);

        for (name, help) in commands {
            output.push_str(&format!(
                "    {:12} {}\n",
                name,
                help.description.lines().next().unwrap_or("")
            ));
        }

        output.push_str("\nFor detailed help on a specific command, use:\n");
        output.push_str("    git-mail help <COMMAND>\n");
        output.push_str("    git-mail <COMMAND> --help\n\n");

        output.push_str("GETTING STARTED:\n");
        output.push_str("    1. Initialize a repository:  git-mail init\n");
        output.push_str("    2. Add an email account:     git-mail account add myaccount\n");
        output.push_str("    3. Sync your emails:         git-mail sync\n");
        output.push_str("    4. Start the TUI:            git-mail tui\n\n");

        output
    }

    /// Format help information for display
    fn format_help(&self, command: &str, help: &CommandHelp) -> String {
        let mut output = String::new();

        output.push_str(&format!("git-mail {}\n", command));
        output.push_str(&format!("{}\n\n", help.description));

        output.push_str("USAGE:\n");
        output.push_str(&format!("    {}\n\n", help.usage));

        if !help.examples.is_empty() {
            output.push_str("EXAMPLES:\n");
            for example in &help.examples {
                output.push_str(&format!("    # {}\n", example.description));
                output.push_str(&format!("    {}\n\n", example.command));
            }
        }

        if !help.notes.is_empty() {
            output.push_str("NOTES:\n");
            for note in &help.notes {
                output.push_str(&format!("    • {}\n", note));
            }
            output.push('\n');
        }

        if !help.related_commands.is_empty() {
            output.push_str("SEE ALSO:\n");
            output.push_str(&format!("    {}\n", help.related_commands.join(", ")));
        }

        output
    }

    /// Initialize all command help documentation
    fn initialize_help(&mut self) {
        self.add_init_help();
        self.add_tui_help();
        self.add_sync_help();
        self.add_compose_help();
        self.add_reply_help();
        self.add_forward_help();
        self.add_draft_help();
        self.add_search_help();
        self.add_list_help();
        self.add_show_help();
        self.add_account_help();
        self.add_plugin_help();
        self.add_tool_help();
    }

    fn add_init_help(&mut self) {
        self.command_help.insert(
            "init".to_string(),
            CommandHelp {
                description: "Initialize a new Git-Mail repository for storing emails".to_string(),
                usage: "git-mail init [PATH]".to_string(),
                examples: vec![
                    Example {
                        description: "Initialize in default location (~/.git-mail)".to_string(),
                        command: "git-mail init".to_string(),
                    },
                    Example {
                        description: "Initialize in custom directory".to_string(),
                        command: "git-mail init /path/to/mail-repo".to_string(),
                    },
                ],
                related_commands: vec!["account".to_string(), "sync".to_string()],
                notes: vec![
                    "Creates a Git repository structure for email storage".to_string(),
                    "Sets up default folders (inbox, sent, drafts, etc.)".to_string(),
                    "Initializes configuration files".to_string(),
                ],
            },
        );
    }

    fn add_tui_help(&mut self) {
        self.command_help.insert(
            "tui".to_string(),
            CommandHelp {
                description: "Start the terminal user interface for interactive email management"
                    .to_string(),
                usage: "git-mail tui".to_string(),
                examples: vec![
                    Example {
                        description: "Start the TUI with default settings".to_string(),
                        command: "git-mail tui".to_string(),
                    },
                    Example {
                        description: "Start TUI with verbose logging".to_string(),
                        command: "git-mail --verbose tui".to_string(),
                    },
                ],
                related_commands: vec![
                    "list".to_string(),
                    "show".to_string(),
                    "compose".to_string(),
                ],
                notes: vec![
                    "Provides full-screen terminal interface".to_string(),
                    "Use arrow keys and vim-like bindings for navigation".to_string(),
                    "Press 'q' to quit, '?' for help within the TUI".to_string(),
                    "Supports mouse interaction in compatible terminals".to_string(),
                ],
            },
        );
    }

    fn add_sync_help(&mut self) {
        self.command_help.insert(
            "sync".to_string(),
            CommandHelp {
                description: "Synchronize emails with configured email accounts".to_string(),
                usage: "git-mail sync [ACCOUNT]".to_string(),
                examples: vec![
                    Example {
                        description: "Sync all configured accounts".to_string(),
                        command: "git-mail sync".to_string(),
                    },
                    Example {
                        description: "Sync only a specific account".to_string(),
                        command: "git-mail sync work".to_string(),
                    },
                    Example {
                        description: "Sync with verbose output".to_string(),
                        command: "git-mail --verbose sync".to_string(),
                    },
                ],
                related_commands: vec!["account".to_string(), "list".to_string()],
                notes: vec![
                    "Fetches new emails from IMAP/POP3 servers".to_string(),
                    "Sends queued outbound emails via SMTP".to_string(),
                    "Applies configured filters to new emails".to_string(),
                    "Creates Git commits for all changes".to_string(),
                    "Works offline - queues operations when disconnected".to_string(),
                ],
            },
        );
    }

    fn add_compose_help(&mut self) {
        self.command_help.insert(
            "compose".to_string(),
            CommandHelp {
                description: "Compose a new email using your configured text editor".to_string(),
                usage: "git-mail compose [--to EMAIL] [--subject SUBJECT]".to_string(),
                examples: vec![
                    Example {
                        description: "Compose a new email interactively".to_string(),
                        command: "git-mail compose".to_string(),
                    },
                    Example {
                        description: "Compose with pre-filled recipient".to_string(),
                        command: "git-mail compose --to colleague@company.com".to_string(),
                    },
                    Example {
                        description: "Compose with recipient and subject".to_string(),
                        command:
                            "git-mail compose --to team@company.com --subject \"Weekly Update\""
                                .to_string(),
                    },
                ],
                related_commands: vec![
                    "draft".to_string(),
                    "reply".to_string(),
                    "forward".to_string(),
                ],
                notes: vec![
                    "Opens your configured text editor (EDITOR environment variable)".to_string(),
                    "Email headers are editable in the compose window".to_string(),
                    "Supports attachments via file paths in the compose template".to_string(),
                    "Saves as draft if you exit without sending".to_string(),
                    "Use Ctrl+C to cancel composition".to_string(),
                ],
            },
        );
    }

    fn add_reply_help(&mut self) {
        self.command_help.insert(
            "reply".to_string(),
            CommandHelp {
                description: "Reply to an existing email with quoted content".to_string(),
                usage: "git-mail reply <EMAIL_ID> [--account ACCOUNT]".to_string(),
                examples: vec![
                    Example {
                        description: "Reply to an email".to_string(),
                        command: "git-mail reply abc123def".to_string(),
                    },
                    Example {
                        description: "Reply from specific account".to_string(),
                        command: "git-mail reply abc123def --account work".to_string(),
                    },
                ],
                related_commands: vec![
                    "compose".to_string(),
                    "forward".to_string(),
                    "show".to_string(),
                ],
                notes: vec![
                    "Automatically quotes the original message".to_string(),
                    "Sets appropriate Reply-To and References headers".to_string(),
                    "Preserves email threading information".to_string(),
                    "Uses the same account as the original recipient by default".to_string(),
                ],
            },
        );
    }

    fn add_forward_help(&mut self) {
        self.command_help.insert(
            "forward".to_string(),
            CommandHelp {
                description: "Forward an email to other recipients".to_string(),
                usage: "git-mail forward <EMAIL_ID> [--account ACCOUNT]".to_string(),
                examples: vec![
                    Example {
                        description: "Forward an email".to_string(),
                        command: "git-mail forward abc123def".to_string(),
                    },
                    Example {
                        description: "Forward from specific account".to_string(),
                        command: "git-mail forward abc123def --account personal".to_string(),
                    },
                ],
                related_commands: vec![
                    "compose".to_string(),
                    "reply".to_string(),
                    "show".to_string(),
                ],
                notes: vec![
                    "Includes original message as quoted content".to_string(),
                    "Preserves original attachments".to_string(),
                    "Adds 'Fwd:' prefix to subject line".to_string(),
                    "Clears recipient fields for you to fill in".to_string(),
                ],
            },
        );
    }

    fn add_draft_help(&mut self) {
        self.command_help.insert(
            "draft".to_string(),
            CommandHelp {
                description: "Manage draft emails (list, edit, send, delete)".to_string(),
                usage: "git-mail draft <ACTION>".to_string(),
                examples: vec![
                    Example {
                        description: "List all drafts".to_string(),
                        command: "git-mail draft list".to_string(),
                    },
                    Example {
                        description: "Edit a draft".to_string(),
                        command: "git-mail draft edit abc123def".to_string(),
                    },
                    Example {
                        description: "Send a draft".to_string(),
                        command: "git-mail draft send abc123def".to_string(),
                    },
                    Example {
                        description: "Delete a draft".to_string(),
                        command: "git-mail draft delete abc123def".to_string(),
                    },
                ],
                related_commands: vec!["compose".to_string(), "list".to_string()],
                notes: vec![
                    "Drafts are automatically saved when composing".to_string(),
                    "Editing reopens your configured text editor".to_string(),
                    "Sending moves the draft to sent folder".to_string(),
                    "All draft operations are tracked in Git history".to_string(),
                ],
            },
        );
    }

    fn add_search_help(&mut self) {
        self.command_help.insert(
            "search".to_string(),
            CommandHelp {
                description: "Search through emails using powerful query syntax".to_string(),
                usage: "git-mail search <QUERY> [--folder FOLDER]".to_string(),
                examples: vec![
                    Example {
                        description: "Search for emails containing 'meeting'".to_string(),
                        command: "git-mail search meeting".to_string(),
                    },
                    Example {
                        description: "Search in specific folder".to_string(),
                        command: "git-mail search project --folder work".to_string(),
                    },
                    Example {
                        description: "Search with regex pattern".to_string(),
                        command: "git-mail search \"urgent.*deadline\"".to_string(),
                    },
                    Example {
                        description: "Search in headers only".to_string(),
                        command: "git-mail search \"from:boss@company.com\"".to_string(),
                    },
                ],
                related_commands: vec!["list".to_string(), "show".to_string(), "tool".to_string()],
                notes: vec![
                    "Uses grep and awk for powerful text searching".to_string(),
                    "Supports regular expressions and complex patterns".to_string(),
                    "Search results include context and relevance scoring".to_string(),
                    "Can search headers, body, or both".to_string(),
                    "Results are sorted by relevance and date".to_string(),
                ],
            },
        );
    }

    fn add_list_help(&mut self) {
        self.command_help.insert(
            "list".to_string(),
            CommandHelp {
                description: "List emails in a folder with summary information".to_string(),
                usage: "git-mail list [FOLDER] [--count COUNT]".to_string(),
                examples: vec![
                    Example {
                        description: "List emails in inbox".to_string(),
                        command: "git-mail list".to_string(),
                    },
                    Example {
                        description: "List emails in specific folder".to_string(),
                        command: "git-mail list sent".to_string(),
                    },
                    Example {
                        description: "List only 10 most recent emails".to_string(),
                        command: "git-mail list --count 10".to_string(),
                    },
                    Example {
                        description: "List emails in work folder".to_string(),
                        command: "git-mail list work --count 50".to_string(),
                    },
                ],
                related_commands: vec!["show".to_string(), "search".to_string(), "tui".to_string()],
                notes: vec![
                    "Shows sender, subject, date, and read status".to_string(),
                    "Emails are sorted by date (newest first)".to_string(),
                    "Unread emails are highlighted".to_string(),
                    "Use email IDs from this list with other commands".to_string(),
                ],
            },
        );
    }

    fn add_show_help(&mut self) {
        self.command_help.insert(
            "show".to_string(),
            CommandHelp {
                description: "Display the full content of an email".to_string(),
                usage: "git-mail show <EMAIL_ID>".to_string(),
                examples: vec![
                    Example {
                        description: "Show email content".to_string(),
                        command: "git-mail show abc123def".to_string(),
                    },
                    Example {
                        description: "Show email and pipe to less".to_string(),
                        command: "git-mail show abc123def | less".to_string(),
                    },
                ],
                related_commands: vec![
                    "list".to_string(),
                    "reply".to_string(),
                    "forward".to_string(),
                ],
                notes: vec![
                    "Displays headers, body, and attachment information".to_string(),
                    "Marks email as read automatically".to_string(),
                    "HTML emails are converted to plain text".to_string(),
                    "Attachment content is not displayed (only metadata)".to_string(),
                ],
            },
        );
    }

    fn add_account_help(&mut self) {
        self.command_help.insert(
            "account".to_string(),
            CommandHelp {
                description: "Manage email accounts (add, remove, list, test)".to_string(),
                usage: "git-mail account <ACTION>".to_string(),
                examples: vec![
                    Example {
                        description: "List configured accounts".to_string(),
                        command: "git-mail account list".to_string(),
                    },
                    Example {
                        description: "Add a new account interactively".to_string(),
                        command: "git-mail account add work".to_string(),
                    },
                    Example {
                        description: "Test account connection".to_string(),
                        command: "git-mail account test work".to_string(),
                    },
                    Example {
                        description: "Remove an account".to_string(),
                        command: "git-mail account remove old-account".to_string(),
                    },
                ],
                related_commands: vec!["sync".to_string(), "compose".to_string()],
                notes: vec![
                    "Account setup is interactive and guides you through configuration".to_string(),
                    "Supports IMAP, POP3 for incoming mail".to_string(),
                    "Supports SMTP for outgoing mail".to_string(),
                    "Credentials are stored securely using system keyring".to_string(),
                    "Each account can have custom filters and settings".to_string(),
                ],
            },
        );
    }

    fn add_plugin_help(&mut self) {
        self.command_help.insert(
            "plugin".to_string(),
            CommandHelp {
                description: "Manage and execute custom plugins for extending functionality"
                    .to_string(),
                usage: "git-mail plugin <ACTION>".to_string(),
                examples: vec![
                    Example {
                        description: "List available plugins".to_string(),
                        command: "git-mail plugin list".to_string(),
                    },
                    Example {
                        description: "Execute a plugin".to_string(),
                        command: "git-mail plugin execute backup --full".to_string(),
                    },
                    Example {
                        description: "Show plugin information".to_string(),
                        command: "git-mail plugin info backup".to_string(),
                    },
                    Example {
                        description: "Reload plugins from directories".to_string(),
                        command: "git-mail plugin reload".to_string(),
                    },
                ],
                related_commands: vec!["tool".to_string()],
                notes: vec![
                    "Plugins are shell scripts in ~/.git-mail/plugins/".to_string(),
                    "Plugins have access to email data and Git repository".to_string(),
                    "Custom plugins can add new commands and functionality".to_string(),
                    "Plugin help is automatically integrated into the help system".to_string(),
                ],
            },
        );
    }

    fn add_tool_help(&mut self) {
        self.command_help.insert(
            "tool".to_string(),
            CommandHelp {
                description: "Integrate with external Unix tools for email processing".to_string(),
                usage: "git-mail tool <ACTION>".to_string(),
                examples: vec![
                    Example {
                        description: "Pipe emails to grep".to_string(),
                        command: "git-mail tool grep \"urgent\" --emails abc123,def456".to_string(),
                    },
                    Example {
                        description: "Process emails with awk".to_string(),
                        command: "git-mail tool awk '{print $1}' --emails abc123,def456"
                            .to_string(),
                    },
                    Example {
                        description: "Count lines in emails".to_string(),
                        command: "git-mail tool count --emails abc123,def456".to_string(),
                    },
                    Example {
                        description: "Pipe emails to external command".to_string(),
                        command: "git-mail tool pipe sort --emails abc123,def456".to_string(),
                    },
                ],
                related_commands: vec!["search".to_string(), "plugin".to_string()],
                notes: vec![
                    "Leverages the full power of Unix command-line tools".to_string(),
                    "Email content is provided via stdin to external commands".to_string(),
                    "Results can be piped to other commands or files".to_string(),
                    "Supports complex data processing workflows".to_string(),
                ],
            },
        );
    }
}

/// Display help for a specific command or general overview
pub fn display_help(command: Option<&str>) -> String {
    let help_system = HelpSystem::new();

    match command {
        Some(cmd) => help_system.display_help(cmd),
        None => help_system.display_overview(),
    }
}

/// Get command suggestions for typos or partial matches
pub fn get_command_suggestions(input: &str) -> Vec<String> {
    let help_system = HelpSystem::new();
    let all_commands = help_system.get_all_commands();

    // Simple fuzzy matching - find commands that contain the input
    let mut suggestions: Vec<String> = all_commands
        .into_iter()
        .filter(|cmd| cmd.contains(input) || input.contains(cmd))
        .collect();

    suggestions.sort();
    suggestions
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_help_system_creation() {
        let help_system = HelpSystem::new();
        assert!(!help_system.get_all_commands().is_empty());
    }

    #[test]
    fn test_command_help_retrieval() {
        let help_system = HelpSystem::new();
        let init_help = help_system.get_command_help("init");
        assert!(init_help.is_some());
        assert!(init_help.unwrap().description.contains("Initialize"));
    }

    #[test]
    fn test_help_display() {
        let help_output = display_help(Some("init"));
        assert!(help_output.contains("git-mail init"));
        assert!(help_output.contains("USAGE:"));
        assert!(help_output.contains("EXAMPLES:"));
    }

    #[test]
    fn test_overview_display() {
        let overview = display_help(None);
        assert!(overview.contains("Git-Mail"));
        assert!(overview.contains("COMMANDS:"));
        assert!(overview.contains("GETTING STARTED:"));
    }

    #[test]
    fn test_command_suggestions() {
        let suggestions = get_command_suggestions("acc");
        assert!(suggestions.contains(&"account".to_string()));
    }

    #[test]
    fn test_all_commands_have_help() {
        let help_system = HelpSystem::new();
        let expected_commands = vec![
            "init", "tui", "sync", "compose", "reply", "forward", "draft", "search", "list",
            "show", "account", "plugin", "tool",
        ];

        for cmd in expected_commands {
            assert!(
                help_system.get_command_help(cmd).is_some(),
                "Missing help for command: {}",
                cmd
            );
        }
    }
}
