# Git-Mail Configuration Guide

This guide covers all aspects of configuring Git-Mail for optimal use.

## Configuration Files

Git-Mail uses TOML configuration files organized in a hierarchical structure:

```
~/.git-mail/
├── config.toml          # Main configuration
├── accounts/
│   ├── personal.toml    # Account-specific config
│   └── work.toml
├── filters/
│   ├── spam-filter.sh   # Email filter scripts
│   └── priority.sh
└── plugins/
    ├── backup.sh        # Custom plugin scripts
    └── stats.sh
```

## Main Configuration (`config.toml`)

### General Settings

```toml
[general]
# Default text editor for composing emails
editor = "vim"

# Default account for sending emails
default_account = "personal"

# Automatically sync on startup
auto_sync = true

# Sync interval in seconds (0 to disable)
sync_interval = 300

# Maximum number of emails to sync per account
max_sync_emails = 1000

# Enable offline mode (queue operations when disconnected)
offline_mode = true
```

### User Interface Settings

```toml
[ui]
# Color theme (default, dark, light, custom)
theme = "default"

# Show line numbers in email content
show_line_numbers = true

# Enable vim-style key bindings
vim_bindings = true

# Mouse support in terminal
mouse_support = true

# Date format for email lists
date_format = "%Y-%m-%d %H:%M"

# Email list columns to display
list_columns = ["status", "sender", "subject", "date"]

# Maximum subject length in lists
max_subject_length = 50
```

### Search Settings

```toml
[search]
# Default search engine (grep, ripgrep, custom)
engine = "grep"

# Case sensitive search by default
case_sensitive = false

# Include email headers in search
search_headers = true

# Include email body in search
search_body = true

# Maximum search results to display
max_results = 100

# Search result context lines
context_lines = 2
```

### Logging Configuration

```toml
[logging]
# Log level (error, warn, info, debug, trace)
level = "info"

# Log to file
log_to_file = true

# Log file path
log_file = "~/.git-mail/logs/git-mail.log"

# Maximum log file size in MB
max_log_size = 10

# Number of log files to keep
max_log_files = 5

# Enable performance monitoring
enable_performance_monitoring = false
```

### Git Settings

```toml
[git]
# Git user name for commits
user_name = "Your Name"

# Git user email for commits
user_email = "your.email@example.com"

# Automatically commit changes
auto_commit = true

# Commit message template
commit_message_template = "Email operation: {operation} - {summary}"

# Enable Git hooks
enable_hooks = true

# Automatic garbage collection
auto_gc = true
```

## Account Configuration

Accounts are configured using the interactive `git-mail account add` command, but you can also manually edit account files.

### IMAP Account Example (`accounts/personal.toml`)

```toml
[account]
name = "personal"
email = "you@example.com"
display_name = "Your Name"
default = false

[incoming]
protocol = "imap"
server = "imap.example.com"
port = 993
username = "you@example.com"
# Password stored in system keyring
use_ssl = true
validate_certs = true

# IMAP-specific settings
folder_mapping = { "INBOX" = "inbox", "Sent" = "sent", "Drafts" = "drafts" }
sync_folders = ["INBOX", "Sent", "Drafts"]
idle_support = true

[outgoing]
server = "smtp.example.com"
port = 587
username = "you@example.com"
# Password stored in system keyring
use_ssl = true
use_starttls = true
validate_certs = true

# SMTP authentication method
auth_method = "plain"  # plain, login, cram-md5

[filters]
# Filter scripts to apply to this account
scripts = ["spam-filter.sh", "priority.sh"]

# Account-specific filter settings
apply_on_sync = true
apply_on_manual = false

[folders]
# Custom folder mappings
inbox = "inbox"
sent = "sent"
drafts = "drafts"
trash = "trash"
archive = "archive"

# Auto-archive settings
auto_archive_days = 365
archive_folder = "archive"
```

### Gmail Configuration Example

```toml
[account]
name = "gmail"
email = "you@gmail.com"
display_name = "Your Name"

[incoming]
protocol = "imap"
server = "imap.gmail.com"
port = 993
username = "you@gmail.com"
use_ssl = true

# Gmail-specific settings
folder_mapping = { 
    "INBOX" = "inbox", 
    "[Gmail]/Sent Mail" = "sent",
    "[Gmail]/Drafts" = "drafts",
    "[Gmail]/All Mail" = "all",
    "[Gmail]/Spam" = "spam",
    "[Gmail]/Trash" = "trash"
}

[outgoing]
server = "smtp.gmail.com"
port = 587
username = "you@gmail.com"
use_starttls = true
```

### Office 365 Configuration Example

```toml
[account]
name = "work"
email = "you@company.com"
display_name = "Your Name (Work)"

[incoming]
protocol = "imap"
server = "outlook.office365.com"
port = 993
username = "you@company.com"
use_ssl = true

[outgoing]
server = "smtp.office365.com"
port = 587
username = "you@company.com"
use_starttls = true
auth_method = "plain"
```

## Email Filters

Email filters are shell scripts that process incoming emails. They receive email metadata as environment variables and email content via stdin.

### Environment Variables Available to Filters

```bash
EMAIL_ID          # Unique email identifier
EMAIL_ACCOUNT     # Source account name
EMAIL_FROM        # Sender email address
EMAIL_TO          # Recipient email address
EMAIL_SUBJECT     # Email subject
EMAIL_DATE        # Email date
EMAIL_FOLDER      # Current folder
EMAIL_SIZE        # Email size in bytes
EMAIL_HEADERS     # All headers (JSON format)
```

### Basic Spam Filter (`filters/spam-filter.sh`)

```bash
#!/bin/bash
# Basic spam filter

# Check subject for spam indicators
if grep -qi "urgent.*money\|lottery\|winner\|congratulations" <<< "$EMAIL_SUBJECT"; then
    echo "action:move:spam"
    echo "tag:spam"
    echo "tag:auto-filtered"
    exit 0
fi

# Check sender reputation
if grep -qi "noreply@suspicious-domain.com" <<< "$EMAIL_FROM"; then
    echo "action:move:spam"
    echo "tag:spam"
    exit 0
fi

# Check for excessive caps
caps_ratio=$(echo "$EMAIL_SUBJECT" | tr -cd '[:upper:]' | wc -c)
total_chars=$(echo "$EMAIL_SUBJECT" | wc -c)
if [ $total_chars -gt 0 ] && [ $((caps_ratio * 100 / total_chars)) -gt 70 ]; then
    echo "action:move:spam"
    echo "tag:spam"
    echo "tag:excessive-caps"
fi
```

### Priority Filter (`filters/priority.sh`)

```bash
#!/bin/bash
# Priority-based email organization

# High priority keywords
if grep -qi "urgent\|asap\|emergency\|critical" <<< "$EMAIL_SUBJECT"; then
    echo "action:move:priority"
    echo "tag:urgent"
    echo "star:true"
    exit 0
fi

# Work-related emails
if grep -qi "meeting\|project\|deadline\|review" <<< "$EMAIL_SUBJECT"; then
    echo "action:move:work"
    echo "tag:work"
    exit 0
fi

# Personal emails
if grep -qi "family\|friend\|personal" <<< "$EMAIL_FROM"; then
    echo "action:move:personal"
    echo "tag:personal"
    exit 0
fi

# Newsletter detection
if grep -qi "newsletter\|unsubscribe\|mailing.list" <<< "$EMAIL_HEADERS"; then
    echo "action:move:newsletters"
    echo "tag:newsletter"
    echo "mark_read:true"
fi
```

### Advanced Filter with External Tools (`filters/advanced-filter.sh`)

```bash
#!/bin/bash
# Advanced filter using external tools

# Use SpamAssassin if available
if command -v spamassassin >/dev/null 2>&1; then
    spam_score=$(spamassassin -t < /dev/stdin | grep "X-Spam-Level" | wc -c)
    if [ "$spam_score" -gt 10 ]; then
        echo "action:move:spam"
        echo "tag:spamassassin"
        exit 0
    fi
fi

# Language detection
if command -v langdetect >/dev/null 2>&1; then
    language=$(echo "$EMAIL_BODY" | langdetect)
    echo "tag:lang-$language"
fi

# Attachment analysis
if echo "$EMAIL_HEADERS" | grep -qi "content-disposition.*attachment"; then
    echo "tag:has-attachments"
    
    # Check for suspicious attachments
    if echo "$EMAIL_HEADERS" | grep -qi "\.exe\|\.scr\|\.bat\|\.com"; then
        echo "action:move:suspicious"
        echo "tag:suspicious-attachment"
    fi
fi
```

## Custom Plugins

Plugins extend Git-Mail functionality with custom commands.

### Plugin Structure

```bash
#!/bin/bash
# Plugin template

# Plugin metadata (required)
if [[ "$1" == "--info" ]]; then
    cat << EOF
name: plugin-name
description: Brief description of what the plugin does
version: 1.0.0
author: Your Name
usage: plugin-name [options] [arguments]
options:
  --help    Show this help
  --verbose Enable verbose output
EOF
    exit 0
fi

# Plugin help
if [[ "$1" == "--help" ]]; then
    echo "Detailed help information..."
    exit 0
fi

# Plugin logic
case "$1" in
    "action1")
        # Implementation
        ;;
    "action2")
        # Implementation
        ;;
    *)
        echo "Unknown action: $1"
        exit 1
        ;;
esac
```

### Backup Plugin (`plugins/backup.sh`)

```bash
#!/bin/bash
# Email repository backup plugin

if [[ "$1" == "--info" ]]; then
    cat << EOF
name: backup
description: Create backups of the email repository
version: 1.0.0
author: Git-Mail Team
usage: backup [--full|--incremental] [--output PATH]
options:
  --full         Create full backup (default)
  --incremental  Create incremental backup
  --output PATH  Specify output location
  --compress     Compress backup
EOF
    exit 0
fi

# Default settings
BACKUP_TYPE="full"
OUTPUT_DIR="$HOME/git-mail-backups"
COMPRESS=false

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --full)
            BACKUP_TYPE="full"
            shift
            ;;
        --incremental)
            BACKUP_TYPE="incremental"
            shift
            ;;
        --output)
            OUTPUT_DIR="$2"
            shift 2
            ;;
        --compress)
            COMPRESS=true
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Create backup directory
mkdir -p "$OUTPUT_DIR"

# Generate backup filename
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="git-mail-backup-$TIMESTAMP"

if [[ "$BACKUP_TYPE" == "full" ]]; then
    echo "Creating full backup..."
    if [[ "$COMPRESS" == true ]]; then
        tar -czf "$OUTPUT_DIR/$BACKUP_NAME.tar.gz" -C "$HOME" .git-mail
    else
        cp -r "$HOME/.git-mail" "$OUTPUT_DIR/$BACKUP_NAME"
    fi
else
    echo "Creating incremental backup..."
    git -C "$HOME/.git-mail" bundle create "$OUTPUT_DIR/$BACKUP_NAME.bundle" --all
fi

echo "Backup created: $OUTPUT_DIR/$BACKUP_NAME"
```

### Statistics Plugin (`plugins/stats.sh`)

```bash
#!/bin/bash
# Email statistics plugin

if [[ "$1" == "--info" ]]; then
    cat << EOF
name: stats
description: Generate email statistics and reports
version: 1.0.0
author: Git-Mail Team
usage: stats [--period PERIOD] [--account ACCOUNT] [--format FORMAT]
options:
  --period   Time period (day, week, month, year, all)
  --account  Specific account to analyze
  --format   Output format (text, json, csv)
EOF
    exit 0
fi

# Default settings
PERIOD="month"
ACCOUNT=""
FORMAT="text"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --period)
            PERIOD="$2"
            shift 2
            ;;
        --account)
            ACCOUNT="$2"
            shift 2
            ;;
        --format)
            FORMAT="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Calculate date range
case "$PERIOD" in
    "day")
        SINCE="1 day ago"
        ;;
    "week")
        SINCE="1 week ago"
        ;;
    "month")
        SINCE="1 month ago"
        ;;
    "year")
        SINCE="1 year ago"
        ;;
    "all")
        SINCE=""
        ;;
esac

# Generate statistics
REPO_PATH="$HOME/.git-mail"

if [[ "$FORMAT" == "json" ]]; then
    echo "{"
    echo "  \"period\": \"$PERIOD\","
    echo "  \"total_emails\": $(find "$REPO_PATH" -name "*.txt" | wc -l),"
    echo "  \"folders\": {"
    for folder in "$REPO_PATH"/*/; do
        folder_name=$(basename "$folder")
        count=$(find "$folder" -name "*.txt" | wc -l)
        echo "    \"$folder_name\": $count,"
    done | sed '$ s/,$//'
    echo "  }"
    echo "}"
else
    echo "Git-Mail Statistics ($PERIOD)"
    echo "=========================="
    echo
    echo "Total emails: $(find "$REPO_PATH" -name "*.txt" | wc -l)"
    echo
    echo "By folder:"
    for folder in "$REPO_PATH"/*/; do
        folder_name=$(basename "$folder")
        count=$(find "$folder" -name "*.txt" | wc -l)
        printf "  %-15s %d\n" "$folder_name:" "$count"
    done
    echo
    echo "Recent activity:"
    git -C "$REPO_PATH" log --oneline --since="$SINCE" | head -10
fi
```

## Best Practices

### Security

1. **Credential Storage**: Always use system keyring for passwords
2. **File Permissions**: Ensure repository has restrictive permissions (700)
3. **SSL/TLS**: Always use encrypted connections for email servers
4. **Filter Security**: Validate and sanitize filter scripts

### Performance

1. **Sync Frequency**: Balance between freshness and performance
2. **Repository Size**: Regularly archive old emails
3. **Search Optimization**: Use ripgrep for better search performance
4. **Git Maintenance**: Run `git gc` periodically

### Organization

1. **Folder Structure**: Create logical folder hierarchies
2. **Tagging Strategy**: Use consistent tagging conventions
3. **Filter Order**: Order filters by specificity (most specific first)
4. **Backup Strategy**: Regular backups with rotation

### Workflow Integration

1. **Editor Configuration**: Configure your preferred editor properly
2. **Shell Integration**: Add Git-Mail to your shell's PATH
3. **Aliases**: Create useful shell aliases for common operations
4. **Automation**: Use cron jobs for regular sync and maintenance

## Troubleshooting Configuration

### Common Issues

1. **Permission Denied**
   ```bash
   chmod 700 ~/.git-mail
   chmod 600 ~/.git-mail/config.toml
   ```

2. **Editor Not Found**
   ```bash
   export EDITOR=vim
   # Or set in config.toml
   ```

3. **SSL Certificate Errors**
   ```toml
   [incoming]
   validate_certs = false  # Only for testing
   ```

4. **Filter Not Executing**
   ```bash
   chmod +x ~/.git-mail/filters/filter-name.sh
   ```

### Validation

Test your configuration:

```bash
# Test account connection
git-mail account test personal

# Validate configuration
git-mail --verbose sync

# Test filters
git-mail plugin execute test-filters
```

## Migration

### From Other Email Clients

1. **Export emails** from your current client (mbox format recommended)
2. **Import using tools** like `mb2md` or custom scripts
3. **Configure accounts** to match your current setup
4. **Set up filters** to replicate your current organization

### Upgrading Git-Mail

1. **Backup your repository** before upgrading
2. **Check configuration compatibility** with new version
3. **Update configuration files** if needed
4. **Test functionality** after upgrade

This configuration guide should help you set up Git-Mail according to your specific needs and workflow requirements.