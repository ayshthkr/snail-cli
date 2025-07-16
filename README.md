# Git-Mail

A terminal-based, offline-first email client that stores emails as plain-text files in a Git repository.

## Overview

Git-Mail treats email management like code development - versioned, scriptable, and portable. Each email is stored as an individual file, and all operations (replies, filters, transformations) are performed using shell scripts and command-line tools, enabling users to leverage the full power of Unix tools and Git workflows for email management.

## Features

- **Git-based Storage**: Every email is a file, every change is a commit
- **Offline-First**: Read, compose, and manage emails without internet
- **Terminal Interface**: Full-featured TUI with keyboard navigation
- **Scriptable Filters**: Custom shell scripts for email processing
- **Unix Integration**: Leverage grep, awk, sed, and other tools
- **Multi-Account**: Support for multiple email accounts
- **Plugin System**: Extend functionality with custom commands
- **Search & Organization**: Powerful search and folder management

## Installation

### From Source

```bash
git clone https://github.com/your-org/git-mail.git
cd git-mail
cargo build --release
sudo cp target/release/git-mail /usr/local/bin/
```

### Using Cargo

```bash
cargo install git-mail
```

## Quick Start

### 1. Initialize Repository

```bash
# Initialize in default location (~/.git-mail)
git-mail init

# Or specify custom location
git-mail init /path/to/mail-repo
```

### 2. Add Email Account

```bash
git-mail account add personal
```

Follow the interactive prompts to configure your email account (IMAP/POP3 for incoming, SMTP for outgoing).

### 3. Sync Emails

```bash
# Sync all accounts
git-mail sync

# Sync specific account
git-mail sync personal
```

### 4. Start Using

```bash
# Launch terminal interface
git-mail tui

# Or use command-line interface
git-mail list
git-mail show <email-id>
git-mail compose
```

## Usage Examples

### Email Management

```bash
# List emails in inbox
git-mail list

# List emails in specific folder
git-mail list sent

# Show email content
git-mail show abc123def

# Compose new email
git-mail compose --to colleague@company.com --subject "Meeting"

# Reply to email
git-mail reply abc123def

# Forward email
git-mail forward abc123def
```

### Search and Organization

```bash
# Search for emails
git-mail search "project deadline"

# Search in specific folder
git-mail search "urgent" --folder work

# Search with regex
git-mail search "meeting.*tomorrow"
```

### Draft Management

```bash
# List drafts
git-mail draft list

# Edit draft
git-mail draft edit draft123

# Send draft
git-mail draft send draft123

# Delete draft
git-mail draft delete draft123
```

### External Tools Integration

```bash
# Pipe emails to grep
git-mail tool grep "urgent" --emails abc123,def456

# Process with awk
git-mail tool awk '{print NF}' --emails abc123,def456

# Count lines in emails
git-mail tool count --emails abc123,def456

# Pipe to external command
git-mail tool pipe sort --emails abc123,def456
```

### Plugin System

```bash
# List available plugins
git-mail plugin list

# Execute plugin
git-mail plugin execute backup --full

# Get plugin info
git-mail plugin info backup
```

## Configuration

### Main Configuration (`~/.git-mail/config.toml`)

```toml
[general]
editor = "vim"
default_account = "personal"
auto_sync = true
sync_interval = 300  # seconds

[ui]
theme = "default"
show_line_numbers = true
vim_bindings = true

[logging]
level = "info"
file = "~/.git-mail/logs/git-mail.log"
```

### Account Configuration

Accounts are configured interactively with `git-mail account add <name>`. Configuration is stored securely using the system keyring.

### Email Filters

Create shell scripts in `~/.git-mail/filters/` to automatically process incoming emails:

```bash
#!/bin/bash
# ~/.git-mail/filters/spam-filter.sh

# Check for spam indicators
if grep -qi "urgent.*money" <<< "$EMAIL_SUBJECT"; then
    echo "action:move:spam"
    echo "tag:spam"
elif grep -qi "meeting" <<< "$EMAIL_SUBJECT"; then
    echo "action:move:meetings"
    echo "tag:meeting"
fi
```

### Custom Plugins

Create executable scripts in `~/.git-mail/plugins/`:

```bash
#!/bin/bash
# ~/.git-mail/plugins/backup.sh

# Plugin metadata
if [[ "$1" == "--info" ]]; then
    echo "name: backup"
    echo "description: Backup email repository"
    echo "usage: backup [--full]"
    exit 0
fi

# Plugin logic
if [[ "$1" == "--full" ]]; then
    tar -czf ~/mail-backup-$(date +%Y%m%d).tar.gz ~/.git-mail/
    echo "Full backup created"
else
    git -C ~/.git-mail bundle create ~/mail-bundle-$(date +%Y%m%d).bundle --all
    echo "Git bundle backup created"
fi
```

## Repository Structure

```
~/.git-mail/
├── .git/                 # Git repository
├── config.toml          # Main configuration
├── accounts/            # Account configurations
├── filters/             # Email filter scripts
├── plugins/             # Custom plugin scripts
├── inbox/               # Incoming emails
│   └── 2024/01/15/     # Organized by date
├── sent/                # Sent emails
├── drafts/              # Draft emails
├── archive/             # Archived emails
└── [custom-folders]/    # User-created folders
```

Each email is stored as a plain-text file with RFC 2822 formatting:

```
From: sender@example.com
To: recipient@example.com
Subject: Meeting Tomorrow
Date: Mon, 15 Jan 2024 10:30:00 +0000
Message-ID: <abc123@example.com>

Hi there,

Let's meet tomorrow at 2 PM to discuss the project.

Best regards,
Sender
```

## Terminal Interface (TUI)

The TUI provides a full-screen interface with multiple views:

### Key Bindings

- **Navigation**: Arrow keys, `hjkl` (vim-style)
- **Actions**: `Enter` (select), `Space` (mark), `Tab` (switch panes)
- **Email**: `c` (compose), `r` (reply), `f` (forward), `d` (delete)
- **Folders**: `m` (move), `n` (new folder), `/` (search)
- **General**: `q` (quit), `?` (help), `:` (command mode)

### Views

1. **Inbox View**: List of emails with sender, subject, date
2. **Email View**: Full email content with headers
3. **Compose View**: Email composition with editor integration
4. **Folder View**: Folder tree and organization
5. **Search View**: Search results and filters

## Advanced Features

### Git Integration

Since emails are stored in Git, you can use standard Git commands:

```bash
# View email history
git -C ~/.git-mail log --oneline

# See changes to specific email
git -C ~/.git-mail log -p inbox/2024/01/15/msg-001.txt

# Restore deleted email
git -C ~/.git-mail checkout HEAD~1 -- inbox/2024/01/15/msg-001.txt

# Create branches for different workflows
git -C ~/.git-mail checkout -b archive-2023
```

### Scripting and Automation

Create custom workflows using shell scripts:

```bash
#!/bin/bash
# Daily email processing script

# Sync all accounts
git-mail sync

# Apply custom filters
for email in $(git-mail list --format=id); do
    # Custom processing logic
    if git-mail show "$email" | grep -q "urgent"; then
        git-mail tool pipe notify-send "Urgent email received"
    fi
done

# Backup repository
git-mail plugin execute backup
```

### Integration with Other Tools

```bash
# Export emails to mbox format
git-mail tool pipe cat --emails $(git-mail list --format=id) > backup.mbox

# Generate email statistics
git-mail tool awk 'BEGIN{count=0} /^From:/{count++} END{print count " emails"}' \
    --emails $(git-mail list --format=id)

# Search with ripgrep for better performance
rg "pattern" ~/.git-mail/inbox/

# Use fzf for interactive email selection
git-mail show $(git-mail list --format="id:subject" | fzf | cut -d: -f1)
```

## Troubleshooting

### Common Issues

1. **Sync Failures**
   ```bash
   # Check account configuration
   git-mail account test <account-name>
   
   # Enable verbose logging
   git-mail --verbose sync
   ```

2. **Editor Not Opening**
   ```bash
   # Set EDITOR environment variable
   export EDITOR=vim
   
   # Or configure in config.toml
   echo 'editor = "vim"' >> ~/.git-mail/config.toml
   ```

3. **Permission Errors**
   ```bash
   # Fix repository permissions
   chmod -R 700 ~/.git-mail
   ```

4. **Large Repository**
   ```bash
   # Clean up old emails
   git-mail tool exec find ~/.git-mail/inbox -name "*.txt" -mtime +365 -delete
   
   # Compress Git repository
   git -C ~/.git-mail gc --aggressive
   ```

### Debug Mode

Enable debug logging for troubleshooting:

```bash
# Enable verbose output
git-mail --verbose <command>

# Check logs
tail -f ~/.git-mail/logs/git-mail.log

# Enable debug in config
[logging]
level = "debug"
```

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Development Setup

```bash
git clone https://github.com/your-org/git-mail.git
cd git-mail
cargo build
cargo test
```

### Running Tests

```bash
# Run all tests
cargo test

# Run specific test suite
cargo test --test integration_tests

# Run with verbose output
cargo test -- --nocapture
```

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Inspired by the Unix philosophy of small, composable tools
- Built with Rust for performance and safety
- Uses Git for reliable, distributed storage
- Integrates with existing Unix tools and workflows

## Support

- **Documentation**: See `man git-mail` or `git-mail --help`
- **Issues**: Report bugs on GitHub
- **Discussions**: Join our community discussions
- **Email**: Contact the maintainers

---

**Git-Mail**: Where email meets version control. 📧 + 🔀 = 🚀