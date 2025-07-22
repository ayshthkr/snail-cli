package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"snail-cli/internal/interfaces"
	"snail-cli/internal/models"
)

// OutputFormatter implements the interfaces.OutputFormatter interface
type OutputFormatter struct {
	writer io.Writer
}

// NewOutputFormatter creates a new output formatter
func NewOutputFormatter(writer io.Writer) *OutputFormatter {
	return &OutputFormatter{
		writer: writer,
	}
}

// FormatEmail formats an email for display
func (f *OutputFormatter) FormatEmail(email *models.Email, format interfaces.OutputFormat) (string, error) {
	if email == nil {
		return "", fmt.Errorf("email cannot be nil")
	}

	switch format {
	case interfaces.FormatJSON:
		return f.formatEmailJSON(email)
	case interfaces.FormatPlain:
		return f.formatEmailPlain(email)
	case interfaces.FormatCSV:
		return f.formatEmailCSV(email)
	default:
		return f.formatEmailTable(email)
	}
}

// FormatEmailList formats a list of emails for display
func (f *OutputFormatter) FormatEmailList(emails []*models.Email, format interfaces.OutputFormat) (string, error) {
	if emails == nil {
		return "", fmt.Errorf("emails list cannot be nil")
	}

	switch format {
	case interfaces.FormatJSON:
		return f.formatEmailListJSON(emails)
	case interfaces.FormatPlain:
		return f.formatEmailListPlain(emails)
	case interfaces.FormatCSV:
		return f.formatEmailListCSV(emails)
	default:
		return f.formatEmailListTable(emails)
	}
}

// FormatSyncStatus formats sync status for display
func (f *OutputFormatter) FormatSyncStatus(status *interfaces.SyncStatus, format interfaces.OutputFormat) (string, error) {
	if status == nil {
		return "", fmt.Errorf("sync status cannot be nil")
	}

	switch format {
	case interfaces.FormatJSON:
		return f.formatSyncStatusJSON(status)
	case interfaces.FormatPlain:
		return f.formatSyncStatusPlain(status)
	case interfaces.FormatCSV:
		return f.formatSyncStatusCSV(status)
	default:
		return f.formatSyncStatusTable(status)
	}
}

// FormatError formats an error for display
func (f *OutputFormatter) FormatError(err error, format interfaces.OutputFormat) string {
	if err == nil {
		return ""
	}

	switch format {
	case interfaces.FormatJSON:
		errorObj := map[string]string{"error": err.Error()}
		if data, jsonErr := json.MarshalIndent(errorObj, "", "  "); jsonErr == nil {
			return string(data)
		}
		return fmt.Sprintf(`{"error": "%s"}`, err.Error())
	case interfaces.FormatPlain:
		return err.Error()
	case interfaces.FormatCSV:
		return fmt.Sprintf("error,%s", err.Error())
	default:
		return fmt.Sprintf("Error: %s", err.Error())
	}
}

// formatEmailJSON formats a single email as JSON
func (f *OutputFormatter) formatEmailJSON(email *models.Email) (string, error) {
	data, err := json.MarshalIndent(email, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal email to JSON: %w", err)
	}
	return string(data), nil
}

// formatEmailPlain formats a single email as plain text
func (f *OutputFormatter) formatEmailPlain(email *models.Email) (string, error) {
	var builder strings.Builder
	
	builder.WriteString(fmt.Sprintf("ID: %s\n", email.ID))
	builder.WriteString(fmt.Sprintf("From: %s\n", email.From.String()))
	
	if len(email.To) > 0 {
		toAddrs := make([]string, len(email.To))
		for i, addr := range email.To {
			toAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("To: %s\n", strings.Join(toAddrs, ", ")))
	}
	
	if len(email.CC) > 0 {
		ccAddrs := make([]string, len(email.CC))
		for i, addr := range email.CC {
			ccAddrs[i] = addr.String()
		}
		builder.WriteString(fmt.Sprintf("CC: %s\n", strings.Join(ccAddrs, ", ")))
	}
	
	builder.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))
	builder.WriteString(fmt.Sprintf("Date: %s\n", email.Date.Format(time.RFC3339)))
	builder.WriteString(fmt.Sprintf("Status: %s\n", email.Status))
	
	if len(email.Labels) > 0 {
		builder.WriteString(fmt.Sprintf("Labels: %s\n", strings.Join(email.Labels, ", ")))
	}
	
	if len(email.Attachments) > 0 {
		builder.WriteString(fmt.Sprintf("Attachments: %d\n", len(email.Attachments)))
	}
	
	builder.WriteString("\n")
	builder.WriteString(email.Body)
	
	return builder.String(), nil
}

// formatEmailCSV formats a single email as CSV
func (f *OutputFormatter) formatEmailCSV(email *models.Email) (string, error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	
	record := []string{
		email.ID,
		email.From.String(),
		formatAddressList(email.To),
		email.Subject,
		email.Date.Format(time.RFC3339),
		string(email.Status),
		strings.Join(email.Labels, ";"),
		fmt.Sprintf("%d", len(email.Attachments)),
	}
	
	if err := writer.Write(record); err != nil {
		return "", fmt.Errorf("failed to write CSV record: %w", err)
	}
	
	writer.Flush()
	return builder.String(), nil
}

// formatEmailTable formats a single email as a table
func (f *OutputFormatter) formatEmailTable(email *models.Email) (string, error) {
	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', 0)
	
	fmt.Fprintf(writer, "ID:\t%s\n", email.ID)
	fmt.Fprintf(writer, "From:\t%s\n", email.From.String())
	
	if len(email.To) > 0 {
		fmt.Fprintf(writer, "To:\t%s\n", formatAddressList(email.To))
	}
	
	if len(email.CC) > 0 {
		fmt.Fprintf(writer, "CC:\t%s\n", formatAddressList(email.CC))
	}
	
	fmt.Fprintf(writer, "Subject:\t%s\n", email.Subject)
	fmt.Fprintf(writer, "Date:\t%s\n", email.Date.Format("2006-01-02 15:04:05"))
	fmt.Fprintf(writer, "Status:\t%s\n", email.Status)
	
	if len(email.Labels) > 0 {
		fmt.Fprintf(writer, "Labels:\t%s\n", strings.Join(email.Labels, ", "))
	}
	
	if len(email.Attachments) > 0 {
		fmt.Fprintf(writer, "Attachments:\t%d\n", len(email.Attachments))
	}
	
	writer.Flush()
	
	builder.WriteString("\n")
	builder.WriteString(email.Body)
	
	return builder.String(), nil
}

// Helper function to format address lists
func formatAddressList(addresses []models.Address) string {
	if len(addresses) == 0 {
		return ""
	}
	
	addrs := make([]string, len(addresses))
	for i, addr := range addresses {
		addrs[i] = addr.String()
	}
	return strings.Join(addrs, ", ")
}

// formatEmailListJSON formats a list of emails as JSON
func (f *OutputFormatter) formatEmailListJSON(emails []*models.Email) (string, error) {
	data, err := json.MarshalIndent(emails, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal email list to JSON: %w", err)
	}
	return string(data), nil
}

// formatEmailListPlain formats a list of emails as plain text
func (f *OutputFormatter) formatEmailListPlain(emails []*models.Email) (string, error) {
	var builder strings.Builder
	
	for i, email := range emails {
		if i > 0 {
			builder.WriteString("\n---\n\n")
		}
		
		builder.WriteString(fmt.Sprintf("%s | %s | %s | %s\n",
			email.ID,
			email.From.String(),
			email.Subject,
			email.Date.Format("2006-01-02 15:04")))
	}
	
	return builder.String(), nil
}

// formatEmailListCSV formats a list of emails as CSV
func (f *OutputFormatter) formatEmailListCSV(emails []*models.Email) (string, error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	
	// Write header
	header := []string{"ID", "From", "To", "Subject", "Date", "Status", "Labels", "Attachments"}
	if err := writer.Write(header); err != nil {
		return "", fmt.Errorf("failed to write CSV header: %w", err)
	}
	
	// Write email records
	for _, email := range emails {
		record := []string{
			email.ID,
			email.From.String(),
			formatAddressList(email.To),
			email.Subject,
			email.Date.Format(time.RFC3339),
			string(email.Status),
			strings.Join(email.Labels, ";"),
			fmt.Sprintf("%d", len(email.Attachments)),
		}
		
		if err := writer.Write(record); err != nil {
			return "", fmt.Errorf("failed to write CSV record: %w", err)
		}
	}
	
	writer.Flush()
	return builder.String(), nil
}

// formatEmailListTable formats a list of emails as a table
func (f *OutputFormatter) formatEmailListTable(emails []*models.Email) (string, error) {
	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', 0)
	
	// Write header
	fmt.Fprintf(writer, "ID\tFROM\tSUBJECT\tDATE\tSTATUS\tLABELS\n")
	fmt.Fprintf(writer, "---\t----\t-------\t----\t------\t------\n")
	
	// Write email records
	for _, email := range emails {
		labels := strings.Join(email.Labels, ",")
		if len(labels) > 20 {
			labels = labels[:17] + "..."
		}
		
		subject := email.Subject
		if len(subject) > 40 {
			subject = subject[:37] + "..."
		}
		
		from := email.From.String()
		if len(from) > 25 {
			from = from[:22] + "..."
		}
		
		fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n",
			email.ID,
			from,
			subject,
			email.Date.Format("2006-01-02 15:04"),
			email.Status,
			labels)
	}
	
	writer.Flush()
	return builder.String(), nil
}

// formatSyncStatusJSON formats sync status as JSON
func (f *OutputFormatter) formatSyncStatusJSON(status *interfaces.SyncStatus) (string, error) {
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal sync status to JSON: %w", err)
	}
	return string(data), nil
}

// formatSyncStatusPlain formats sync status as plain text
func (f *OutputFormatter) formatSyncStatusPlain(status *interfaces.SyncStatus) (string, error) {
	var builder strings.Builder
	
	builder.WriteString("Sync Status:\n")
	
	if status.LastSync != nil {
		builder.WriteString(fmt.Sprintf("Last Sync: %s\n", status.LastSync.Format("2006-01-02 15:04:05")))
	} else {
		builder.WriteString("Last Sync: Never\n")
	}
	
	builder.WriteString(fmt.Sprintf("Online: %t\n", status.IsOnline))
	builder.WriteString(fmt.Sprintf("Queued Emails: %d\n", status.QueuedEmails))
	builder.WriteString(fmt.Sprintf("Pending Conflicts: %d\n", status.PendingConflicts))
	
	if status.NextSyncAt != nil {
		builder.WriteString(fmt.Sprintf("Next Sync: %s\n", status.NextSyncAt.Format("2006-01-02 15:04:05")))
	} else {
		builder.WriteString("Next Sync: Not scheduled\n")
	}
	
	return builder.String(), nil
}

// formatSyncStatusCSV formats sync status as CSV
func (f *OutputFormatter) formatSyncStatusCSV(status *interfaces.SyncStatus) (string, error) {
	var builder strings.Builder
	writer := csv.NewWriter(&builder)
	
	// Write header
	header := []string{"LastSync", "IsOnline", "QueuedEmails", "PendingConflicts", "NextSyncAt"}
	if err := writer.Write(header); err != nil {
		return "", fmt.Errorf("failed to write CSV header: %w", err)
	}
	
	// Write status record
	lastSync := ""
	if status.LastSync != nil {
		lastSync = status.LastSync.Format(time.RFC3339)
	}
	
	nextSync := ""
	if status.NextSyncAt != nil {
		nextSync = status.NextSyncAt.Format(time.RFC3339)
	}
	
	record := []string{
		lastSync,
		fmt.Sprintf("%t", status.IsOnline),
		fmt.Sprintf("%d", status.QueuedEmails),
		fmt.Sprintf("%d", status.PendingConflicts),
		nextSync,
	}
	
	if err := writer.Write(record); err != nil {
		return "", fmt.Errorf("failed to write CSV record: %w", err)
	}
	
	writer.Flush()
	return builder.String(), nil
}

// formatSyncStatusTable formats sync status as a table
func (f *OutputFormatter) formatSyncStatusTable(status *interfaces.SyncStatus) (string, error) {
	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', 0)
	
	fmt.Fprintf(writer, "Sync Status\n")
	fmt.Fprintf(writer, "-----------\n")
	
	if status.LastSync != nil {
		fmt.Fprintf(writer, "Last Sync:\t%s\n", status.LastSync.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Fprintf(writer, "Last Sync:\tNever\n")
	}
	
	fmt.Fprintf(writer, "Online:\t%t\n", status.IsOnline)
	fmt.Fprintf(writer, "Queued Emails:\t%d\n", status.QueuedEmails)
	fmt.Fprintf(writer, "Pending Conflicts:\t%d\n", status.PendingConflicts)
	
	if status.NextSyncAt != nil {
		fmt.Fprintf(writer, "Next Sync:\t%s\n", status.NextSyncAt.Format("2006-01-02 15:04:05"))
	} else {
		fmt.Fprintf(writer, "Next Sync:\tNot scheduled\n")
	}
	
	writer.Flush()
	return builder.String(), nil
}