package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"snail-cli/internal/models"
	"snail-cli/internal/repository"
	"snail-cli/internal/services"
)

// configCmd handles configuration management
var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration settings",
	Long: `Manage snail-cli configuration settings including Gmail integration and filters.

Available configuration keys:
  gmail.username     - Gmail username/email address
  gmail.imap_server  - IMAP server address (default: imap.gmail.com:993)
  gmail.smtp_server  - SMTP server address (default: smtp.gmail.com:587)
  gmail.auth_method  - Authentication method (oauth2, password)
  storage.path       - Local storage path (default: ~/.snail/emails)
  storage.encrypt    - Enable repository encryption (true/false)
  editor.command     - External editor command (default: $EDITOR)
  sync.auto          - Enable automatic sync (true/false)
  sync.interval      - Sync interval in minutes (default: 15)`,
	Example: `  # Set Gmail username
  snail config set gmail.username user@gmail.com
  
  # Get current storage path
  snail config get storage.path
  
  # Enable automatic sync
  snail config set sync.auto true`,
}

var configSetCmd = &cobra.Command{
	Use:   "set [key] [value]",
	Short: "Set a configuration value",
	Long: `Set a configuration value for snail-cli.

The key should be in dot notation (e.g., gmail.username).
The value will be validated based on the configuration key type.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateArgs(cmd, args, 2); err != nil {
			return err
		}
		
		key := args[0]
		if !isValidConfigKey(key) {
			return fmt.Errorf("invalid configuration key: %s\nRun 'snail config --help' to see available keys", key)
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		key, value := args[0], args[1]
		
		if IsVerbose(cmd) {
			fmt.Printf("Setting configuration: %s = %s\n", key, value)
		}
		
		// TODO: Implement config setting
		return fmt.Errorf("configuration setting not implemented yet")
	},
	Example: `  # Set Gmail username
  snail config set gmail.username user@gmail.com
  
  # Enable repository encryption
  snail config set storage.encrypt true`,
}

var configGetCmd = &cobra.Command{
	Use:   "get [key]",
	Short: "Get a configuration value",
	Long: `Get a configuration value from snail-cli.

The key should be in dot notation (e.g., gmail.username).`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateArgs(cmd, args, 1); err != nil {
			return err
		}
		
		key := args[0]
		if !isValidConfigKey(key) {
			return fmt.Errorf("invalid configuration key: %s\nRun 'snail config --help' to see available keys", key)
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		key := args[0]
		
		if IsVerbose(cmd) {
			fmt.Printf("Getting configuration value for: %s\n", key)
		}
		
		// TODO: Implement config getting
		return fmt.Errorf("configuration getting not implemented yet")
	},
	Example: `  # Get Gmail username
  snail config get gmail.username
  
  # Get storage path
  snail config get storage.path`,
}

// isValidConfigKey validates configuration keys
func isValidConfigKey(key string) bool {
	validKeys := []string{
		"gmail.username", "gmail.imap_server", "gmail.smtp_server", "gmail.auth_method",
		"storage.path", "storage.encrypt",
		"editor.command",
		"sync.auto", "sync.interval",
	}
	
	for _, validKey := range validKeys {
		if key == validKey {
			return true
		}
	}
	return false
}

// syncCmd handles email synchronization
var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Synchronize emails with remote server",
	Long: `Synchronize emails between local repository and remote email server.

This command performs bidirectional synchronization:
- Downloads new emails from the remote server
- Uploads queued outgoing emails
- Resolves conflicts between local and remote changes

Use flags to control sync behavior:
- --incoming: Only download new emails
- --outgoing: Only send queued emails
- --force: Force sync even if conflicts exist`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if IsVerbose(cmd) {
			fmt.Println("Starting email synchronization...")
		}
		
		// TODO: Implement sync functionality
		return fmt.Errorf("email synchronization not implemented yet")
	},
	Example: `  # Full bidirectional sync
  snail sync
  
  # Only download new emails
  snail sync --incoming
  
  # Only send queued emails
  snail sync --outgoing
  
  # Force sync ignoring conflicts
  snail sync --force`,
}

// listCmd handles email listing
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List emails",
	Long: `List emails with optional filtering and sorting options.

This command displays emails from the local repository with various filtering options:
- Filter by folder, status, sender, or subject
- Limit results and use pagination
- Choose output format (table, json, plain, csv)

The default output shows the most recent 20 emails in table format.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeListCommand(cmd)
	},
	Example: `  # List recent emails
  snail list
  
  # List emails from inbox folder
  snail list --folder inbox
  
  # List unread emails
  snail list --status unread
  
  # List emails from specific sender
  snail list --from user@example.com
  
  # List with JSON output
  snail list --format json
  
  # List with pagination
  snail list --limit 50 --offset 100`,
}

// readCmd handles reading individual emails
var readCmd = &cobra.Command{
	Use:   "read [email-id]",
	Short: "Read a specific email",
	Long: `Read a specific email by its ID.

The email ID can be obtained from the 'snail list' command.
This command displays the full email content including headers,
body, and attachment information.

The email will be marked as read after viewing.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateArgs(cmd, args, 1); err != nil {
			return err
		}
		
		emailID := strings.TrimSpace(args[0])
		if emailID == "" {
			return fmt.Errorf("email ID cannot be empty")
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		emailID := strings.TrimSpace(args[0])
		return executeReadCommand(cmd, emailID)
	},
	Example: `  # Read email by ID
  snail read msg-001
  
  # Read with verbose output
  snail read msg-001 --verbose`,
}

// composeCmd handles email composition
var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Compose a new email",
	Long: `Compose a new email using the configured external editor.

This command opens your configured editor to compose a new email.
You can pre-fill recipient and subject using command flags.
The email will be saved as a draft and queued for sending.

The editor will open with a template containing email headers
that you can modify before writing the email body.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return executeComposeCommand(cmd)
	},
	Example: `  # Compose new email
  snail compose
  
  # Compose with pre-filled recipient
  snail compose --to user@example.com
  
  # Compose with recipient and subject
  snail compose --to user@example.com --subject "Meeting tomorrow"
  
  # Use email template
  snail compose --template meeting-request`,
}

// replyCmd handles email replies
var replyCmd = &cobra.Command{
	Use:   "reply [email-id]",
	Short: "Reply to an email",
	Long: `Reply to an email by its ID.

This command opens your configured editor with a reply template
that includes the original email content and proper reply headers.
Use --all flag to reply to all recipients instead of just the sender.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateArgs(cmd, args, 1); err != nil {
			return err
		}
		
		emailID := strings.TrimSpace(args[0])
		if emailID == "" {
			return fmt.Errorf("email ID cannot be empty")
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		emailID := strings.TrimSpace(args[0])
		return executeReplyCommand(cmd, emailID)
	},
	Example: `  # Reply to sender only
  snail reply msg-001
  
  # Reply to all recipients
  snail reply msg-001 --all`,
}

// searchCmd handles email searching
var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search emails",
	Long: `Search emails using text-based queries.

This command searches across all email content and metadata including:
- Email body content
- Subject lines
- Sender and recipient addresses
- Email headers

Use additional flags to narrow your search scope or filter results.`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateArgs(cmd, args, 1); err != nil {
			return err
		}
		
		query := strings.TrimSpace(args[0])
		if query == "" {
			return fmt.Errorf("search query cannot be empty")
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.TrimSpace(args[0])
		return executeSearchCommand(cmd, query)
	},
	Example: `  # Search for emails containing "meeting"
  snail search "meeting"
  
  # Search within inbox folder
  snail search "project" --folder inbox
  
  # Search by sender
  snail search "report" --from boss@company.com
  
  # Search within date range
  snail search "invoice" --date-from 2024-01-01 --date-to 2024-01-31`,
}

// filterCmd handles filter management
var filterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Manage email filters",
	Long: `Manage email filters for automated email processing.

Filters are shell scripts that process emails automatically based on events.
Common use cases include:
- Spam filtering and email classification
- Automatic labeling and organization
- AI-powered email summarization
- Custom email processing workflows

Filters receive email content via stdin and can modify email metadata
through special output directives.`,
	Example: `  # Add a new filter
  snail filter add spam-filter.sh --name "Spam Filter"
  
  # List all filters
  snail filter list
  
  # Add filter with custom events
  snail filter add ai-summarize.sh --events email_received,email_updated`,
}

var filterAddCmd = &cobra.Command{
	Use:   "add [script]",
	Short: "Add a new filter",
	Long: `Add a new email filter script.

The script should be a shell script that processes emails.
It receives email content via stdin and can output metadata
directives to modify email properties.

Filter scripts should be executable and follow the filter API:
- Input: Email content via stdin
- Output: Modified email content via stdout
- Metadata: Special directives for email properties
- Exit code: 0 for success, non-zero for failure`,
	Args: func(cmd *cobra.Command, args []string) error {
		if err := ValidateArgs(cmd, args, 1); err != nil {
			return err
		}
		
		script := strings.TrimSpace(args[0])
		if script == "" {
			return fmt.Errorf("script path cannot be empty")
		}
		
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		script := strings.TrimSpace(args[0])
		
		if IsVerbose(cmd) {
			fmt.Printf("Adding filter script: %s\n", script)
		}
		
		// TODO: Implement filter addition
		return fmt.Errorf("filter addition not implemented yet")
	},
	Example: `  # Add a spam filter
  snail filter add spam-filter.sh --name "Spam Filter"
  
  # Add AI summarization filter
  snail filter add ai-summarize.sh --name "AI Summary" --events email_received
  
  # Add filter with timeout
  snail filter add slow-filter.sh --timeout 30s`,
}

var filterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all filters",
	Long: `List all configured email filters.

This command displays all filters with their configuration including:
- Filter name and script path
- Enabled/disabled status
- Trigger events
- Execution timeout settings`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if IsVerbose(cmd) {
			fmt.Println("Listing all configured filters...")
		}
		
		// TODO: Implement filter listing
		return fmt.Errorf("filter listing not implemented yet")
	},
	Example: `  # List all filters
  snail filter list
  
  # List with verbose output
  snail filter list --verbose`,
}

// statusCmd shows system status
var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show system status",
	Long: `Show current system status including sync state and repository information.

This command displays comprehensive system information including:
- Repository status and location
- Last sync time and results
- Network connectivity status
- Queued emails count
- Active filters and configuration`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if IsVerbose(cmd) {
			fmt.Println("Gathering system status information...")
		}
		
		// TODO: Implement status display
		return fmt.Errorf("system status display not implemented yet")
	},
	Example: `  # Show system status
  snail status
  
  # Show detailed status
  snail status --verbose`,
}

func init() {
	// Configure config subcommands
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)

	// Configure sync command flags
	syncCmd.Flags().Bool("incoming", false, "sync only incoming emails")
	syncCmd.Flags().Bool("outgoing", false, "sync only outgoing emails")
	syncCmd.Flags().Bool("force", false, "force sync even if conflicts exist")

	// Configure list command flags
	listCmd.Flags().String("folder", "", "filter by folder")
	listCmd.Flags().String("status", "", "filter by status (unread, read, draft, sent)")
	listCmd.Flags().String("from", "", "filter by sender")
	listCmd.Flags().String("subject", "", "filter by subject")
	listCmd.Flags().Int("limit", 20, "limit number of results")
	listCmd.Flags().Int("offset", 0, "offset for pagination")

	// Configure compose command flags
	composeCmd.Flags().String("to", "", "recipient email address")
	composeCmd.Flags().String("subject", "", "email subject")
	composeCmd.Flags().String("template", "", "use email template")

	// Configure reply command flags
	replyCmd.Flags().Bool("all", false, "reply to all recipients")

	// Configure search command flags
	searchCmd.Flags().String("folder", "", "search within specific folder")
	searchCmd.Flags().String("from", "", "search by sender")
	searchCmd.Flags().String("subject", "", "search by subject")
	searchCmd.Flags().String("date-from", "", "search from date (YYYY-MM-DD)")
	searchCmd.Flags().String("date-to", "", "search to date (YYYY-MM-DD)")

	// Configure filter subcommands
	filterCmd.AddCommand(filterAddCmd)
	filterCmd.AddCommand(filterListCmd)

	// Configure filter add command flags
	filterAddCmd.Flags().String("name", "", "filter name")
	filterAddCmd.Flags().StringSlice("events", []string{"email_received"}, "events to trigger filter")
	filterAddCmd.Flags().Duration("timeout", 0, "filter execution timeout")
}

// executeListCommand implements the email listing functionality
func executeListCommand(cmd *cobra.Command) error {
	ctx := context.Background()
	
	// Create mock repository and service for now
	// In a real implementation, this would be injected or configured
	repo := repository.NewMockEmailRepository()
	emailService := services.NewEmailService(repo)
	
	// Parse command flags
	folder, _ := cmd.Flags().GetString("folder")
	statusStr, _ := cmd.Flags().GetString("status")
	from, _ := cmd.Flags().GetString("from")
	subject, _ := cmd.Flags().GetString("subject")
	limit, _ := cmd.Flags().GetInt("limit")
	offset, _ := cmd.Flags().GetInt("offset")
	
	// Parse status
	var status models.EmailStatus
	if statusStr != "" {
		switch strings.ToLower(statusStr) {
		case "unread":
			status = models.StatusUnread
		case "read":
			status = models.StatusRead
		case "draft":
			status = models.StatusDraft
		case "sent":
			status = models.StatusSent
		default:
			return fmt.Errorf("invalid status: %s (valid values: unread, read, draft, sent)", statusStr)
		}
	}
	
	// Build criteria
	criteria := &services.ListCriteria{
		Folder:   folder,
		Status:   status,
		From:     from,
		Subject:  subject,
		Limit:    limit,
		Offset:   offset,
		SortBy:   "date",
		SortDesc: true,
	}
	
	if IsVerbose(cmd) {
		fmt.Printf("Listing emails with criteria: folder=%s, status=%s, from=%s, subject=%s, limit=%d, offset=%d\n",
			folder, statusStr, from, subject, limit, offset)
	}
	
	// Get emails
	emails, err := emailService.ListEmails(ctx, criteria)
	if err != nil {
		return fmt.Errorf("failed to list emails: %w", err)
	}
	
	// Format and display results
	formatter := NewOutputFormatter(cmd.OutOrStdout())
	outputFormat := GetOutputFormat(cmd)
	
	result, err := formatter.FormatEmailList(emails, outputFormat)
	if err != nil {
		return fmt.Errorf("failed to format email list: %w", err)
	}
	
	fmt.Fprint(cmd.OutOrStdout(), result)
	
	// Show summary if verbose
	if IsVerbose(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "\nShowing %d emails (offset: %d)\n", len(emails), offset)
	}
	
	return nil
}

// executeReadCommand implements the email reading functionality
func executeReadCommand(cmd *cobra.Command, emailID string) error {
	ctx := context.Background()
	
	// Create mock repository and service for now
	repo := repository.NewMockEmailRepository()
	emailService := services.NewEmailService(repo)
	
	if IsVerbose(cmd) {
		fmt.Printf("Reading email: %s\n", emailID)
	}
	
	// Get email and mark as read
	email, err := emailService.GetEmail(ctx, emailID, true)
	if err != nil {
		return fmt.Errorf("failed to read email: %w", err)
	}
	
	// Format and display email
	formatter := NewOutputFormatter(cmd.OutOrStdout())
	outputFormat := GetOutputFormat(cmd)
	
	result, err := formatter.FormatEmail(email, outputFormat)
	if err != nil {
		return fmt.Errorf("failed to format email: %w", err)
	}
	
	fmt.Fprint(cmd.OutOrStdout(), result)
	
	return nil
}

// executeSearchCommand implements the email search functionality
func executeSearchCommand(cmd *cobra.Command, query string) error {
	ctx := context.Background()
	
	// Create mock repository and service for now
	repo := repository.NewMockEmailRepository()
	emailService := services.NewEmailService(repo)
	
	// Parse command flags
	folder, _ := cmd.Flags().GetString("folder")
	from, _ := cmd.Flags().GetString("from")
	subject, _ := cmd.Flags().GetString("subject")
	dateFromStr, _ := cmd.Flags().GetString("date-from")
	dateToStr, _ := cmd.Flags().GetString("date-to")
	
	// Parse dates
	var dateFrom, dateTo *time.Time
	if dateFromStr != "" {
		if parsed, err := time.Parse("2006-01-02", dateFromStr); err == nil {
			dateFrom = &parsed
		} else {
			return fmt.Errorf("invalid date-from format: %s (use YYYY-MM-DD)", dateFromStr)
		}
	}
	if dateToStr != "" {
		if parsed, err := time.Parse("2006-01-02", dateToStr); err == nil {
			// Set to end of day
			endOfDay := parsed.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			dateTo = &endOfDay
		} else {
			return fmt.Errorf("invalid date-to format: %s (use YYYY-MM-DD)", dateToStr)
		}
	}
	
	// Build criteria
	criteria := &services.ListCriteria{
		Folder:   folder,
		From:     from,
		Subject:  subject,
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Limit:    20,
		Offset:   0,
		SortBy:   "date",
		SortDesc: true,
	}
	
	if IsVerbose(cmd) {
		fmt.Printf("Searching for: %s\n", query)
	}
	
	// Search emails
	emails, err := emailService.SearchEmails(ctx, query, criteria)
	if err != nil {
		return fmt.Errorf("failed to search emails: %w", err)
	}
	
	// Format and display results
	formatter := NewOutputFormatter(cmd.OutOrStdout())
	outputFormat := GetOutputFormat(cmd)
	
	result, err := formatter.FormatEmailList(emails, outputFormat)
	if err != nil {
		return fmt.Errorf("failed to format search results: %w", err)
	}
	
	fmt.Fprint(cmd.OutOrStdout(), result)
	
	// Show summary if verbose
	if IsVerbose(cmd) {
		fmt.Fprintf(cmd.OutOrStdout(), "\nFound %d emails matching '%s'\n", len(emails), query)
	}
	
	return nil
}

// executeComposeCommand implements the email composition functionality
func executeComposeCommand(cmd *cobra.Command) error {
	ctx := context.Background()
	
	// Create mock repository for now
	repo := repository.NewMockEmailRepository()
	
	// Parse command flags
	to, _ := cmd.Flags().GetString("to")
	subject, _ := cmd.Flags().GetString("subject")
	template, _ := cmd.Flags().GetString("template")
	
	if IsVerbose(cmd) {
		fmt.Println("Opening editor for email composition...")
	}
	
	// Create email template
	emailTemplate := &services.EmailTemplate{
		To:      to,
		Subject: subject,
	}
	
	// Handle template loading (placeholder for now)
	if template != "" {
		if IsVerbose(cmd) {
			fmt.Printf("Using template: %s\n", template)
		}
		// TODO: Load template content
	}
	
	// For now, we'll simulate the composition without actually opening an editor
	// In a real implementation, this would open the configured editor
	if IsOfflineMode(cmd) || template == "test" {
		// Create a mock composed email for testing
		email := createMockComposedEmail(emailTemplate)
		
		// Save as draft
		if err := repo.Store(ctx, email); err != nil {
			return fmt.Errorf("failed to save draft: %w", err)
		}
		
		fmt.Printf("Email composed and saved as draft: %s\n", email.ID)
		
		// Format and display the composed email
		formatter := NewOutputFormatter(cmd.OutOrStdout())
		outputFormat := GetOutputFormat(cmd)
		
		result, err := formatter.FormatEmail(email, outputFormat)
		if err != nil {
			return fmt.Errorf("failed to format composed email: %w", err)
		}
		
		fmt.Fprint(cmd.OutOrStdout(), result)
		
		return nil
	}
	
	// In a real implementation, this would call compositionService.ComposeEmail
	return fmt.Errorf("email composition with external editor not implemented in mock mode - use --offline flag for testing")
}

// executeReplyCommand implements the email reply functionality
func executeReplyCommand(cmd *cobra.Command, emailID string) error {
	ctx := context.Background()
	
	// Create mock repository and services
	repo := repository.NewMockEmailRepository()
	emailService := services.NewEmailService(repo)
	
	// Parse command flags
	replyAll, _ := cmd.Flags().GetBool("all")
	
	if IsVerbose(cmd) {
		if replyAll {
			fmt.Printf("Replying to all recipients of email: %s\n", emailID)
		} else {
			fmt.Printf("Replying to email: %s\n", emailID)
		}
	}
	
	// Get the original email
	originalEmail, err := emailService.GetEmail(ctx, emailID, false)
	if err != nil {
		return fmt.Errorf("failed to get original email: %w", err)
	}
	
	// For now, we'll simulate the reply composition without actually opening an editor
	if IsOfflineMode(cmd) || true { // Always use mock mode for now
		// Create a mock reply email
		replyEmail := createMockReplyEmail(originalEmail, replyAll)
		
		// Save as draft
		if err := repo.Store(ctx, replyEmail); err != nil {
			return fmt.Errorf("failed to save reply draft: %w", err)
		}
		
		fmt.Printf("Reply composed and saved as draft: %s\n", replyEmail.ID)
		
		// Format and display the reply email
		formatter := NewOutputFormatter(cmd.OutOrStdout())
		outputFormat := GetOutputFormat(cmd)
		
		result, err := formatter.FormatEmail(replyEmail, outputFormat)
		if err != nil {
			return fmt.Errorf("failed to format reply email: %w", err)
		}
		
		fmt.Fprint(cmd.OutOrStdout(), result)
		
		return nil
	}
	
	// In a real implementation, this would call compositionService.ReplyToEmail
	return fmt.Errorf("email reply with external editor not implemented in mock mode")
}

// Helper functions for creating mock emails

func createMockComposedEmail(template *services.EmailTemplate) *models.Email {
	email := &models.Email{
		ID:        fmt.Sprintf("draft-%d", time.Now().UnixNano()),
		MessageID: fmt.Sprintf("<%d@snail-cli.local>", time.Now().UnixNano()),
		Date:      time.Now(),
		Status:    models.StatusDraft,
		Headers:   make(map[string]string),
		Labels:    []string{"drafts"},
		Subject:   "New Email",
		Body:      "This is a new email composed using Snail CLI.\n\nBest regards,\nUser",
	}
	
	// Apply template if provided
	if template != nil {
		if template.To != "" {
			if addr, err := models.ParseAddress(template.To); err == nil {
				email.To = []models.Address{*addr}
			}
		}
		if template.Subject != "" {
			email.Subject = template.Subject
		}
		if template.Body != "" {
			email.Body = template.Body
		}
	}
	
	// Set default recipient if none provided
	if len(email.To) == 0 {
		email.To = []models.Address{{Email: "recipient@example.com"}}
	}
	
	return email
}

func createMockReplyEmail(originalEmail *models.Email, replyAll bool) *models.Email {
	replyEmail := &models.Email{
		ID:        fmt.Sprintf("reply-%d", time.Now().UnixNano()),
		MessageID: fmt.Sprintf("<%d@snail-cli.local>", time.Now().UnixNano()),
		To:        []models.Address{originalEmail.From},
		Subject:   addReplyPrefix(originalEmail.Subject),
		Date:      time.Now(),
		Status:    models.StatusDraft,
		Headers:   make(map[string]string),
		Labels:    []string{"drafts"},
	}
	
	// Add In-Reply-To header
	replyEmail.Headers["In-Reply-To"] = originalEmail.MessageID
	replyEmail.Headers["References"] = originalEmail.MessageID
	
	// If reply all, add CC recipients
	if replyAll && len(originalEmail.To) > 1 {
		replyEmail.CC = append(replyEmail.CC, originalEmail.To[1:]...)
		replyEmail.CC = append(replyEmail.CC, originalEmail.CC...)
	}
	
	// Create reply body with quoted original
	replyEmail.Body = fmt.Sprintf("Thank you for your email.\n\nOn %s, %s wrote:\n%s",
		originalEmail.Date.Format("2006-01-02 15:04"),
		originalEmail.From.String(),
		addQuotePrefix(originalEmail.Body))
	
	return replyEmail
}

func addReplyPrefix(subject string) string {
	if strings.HasPrefix(strings.ToLower(subject), "re:") {
		return subject
	}
	return "Re: " + subject
}

func addQuotePrefix(body string) string {
	lines := strings.Split(body, "\n")
	quotedLines := make([]string, len(lines))
	for i, line := range lines {
		quotedLines[i] = "> " + line
	}
	return strings.Join(quotedLines, "\n")
}