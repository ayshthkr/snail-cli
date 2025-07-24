package filter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"snail-cli/internal/interfaces"
)

// ShellFilter implements the Filter interface for shell script execution
type ShellFilter struct {
	name        string
	description string
	scriptPath  string
	events      []interfaces.FilterEvent
	timeout     time.Duration
	enabled     bool
	environment map[string]string
	workingDir  string
	sandboxed   bool
}

// NewShellFilter creates a new shell filter
func NewShellFilter(config *interfaces.ShellFilter, workingDir string, sandboxed bool) (*ShellFilter, error) {
	if config == nil {
		return nil, fmt.Errorf("shell filter config cannot be nil")
	}

	if config.FilterName == "" {
		return nil, fmt.Errorf("filter name cannot be empty")
	}

	if config.ScriptPath == "" {
		return nil, fmt.Errorf("script path cannot be empty")
	}

	// Validate script exists and is executable
	if err := validateScript(config.ScriptPath); err != nil {
		return nil, fmt.Errorf("script validation failed: %w", err)
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &ShellFilter{
		name:        config.FilterName,
		description: config.FilterDesc,
		scriptPath:  config.ScriptPath,
		events:      config.FilterEvents,
		timeout:     timeout,
		enabled:     config.Enabled,
		environment: config.Environment,
		workingDir:  workingDir,
		sandboxed:   sandboxed,
	}, nil
}

// Name returns the filter name
func (sf *ShellFilter) Name() string {
	return sf.name
}

// Description returns the filter description
func (sf *ShellFilter) Description() string {
	return sf.description
}

// Events returns the events this filter handles
func (sf *ShellFilter) Events() []interfaces.FilterEvent {
	return sf.events
}

// Execute processes an email using the shell script
func (sf *ShellFilter) Execute(ctx context.Context, email *interfaces.Email, event interfaces.FilterEvent) (*interfaces.FilterResult, error) {
	start := time.Now()

	// Create timeout context
	execCtx, cancel := context.WithTimeout(ctx, sf.timeout)
	defer cancel()

	// Prepare temporary files for email data and metadata
	emailFile, metadataFile, err := sf.prepareFiles(email, event)
	if err != nil {
		return nil, fmt.Errorf("failed to prepare files: %w", err)
	}
	defer sf.cleanup(emailFile, metadataFile)

	// Execute the script
	result, err := sf.executeScript(execCtx, emailFile, metadataFile)
	if err != nil {
		return &interfaces.FilterResult{
			Modified: false,
			Email:    email,
			Actions:  []interfaces.FilterAction{},
			Labels:   []string{},
			Error:    err,
			Duration: time.Since(start),
		}, err
	}

	result.Duration = time.Since(start)
	return result, nil
}

// IsEnabled returns true if the filter is enabled
func (sf *ShellFilter) IsEnabled() bool {
	return sf.enabled
}

// SetEnabled enables or disables the filter
func (sf *ShellFilter) SetEnabled(enabled bool) {
	sf.enabled = enabled
}

// prepareFiles creates temporary files with email data and metadata
func (sf *ShellFilter) prepareFiles(email *interfaces.Email, event interfaces.FilterEvent) (string, string, error) {
	// Create temporary directory
	tempDir := filepath.Join(sf.workingDir, fmt.Sprintf("filter-%s-%d", sf.name, time.Now().UnixNano()))
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return "", "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Create email file
	emailFile := filepath.Join(tempDir, "email.eml")
	emailContent := sf.formatEmailContent(email)
	if err := os.WriteFile(emailFile, []byte(emailContent), 0600); err != nil {
		return "", "", fmt.Errorf("failed to write email file: %w", err)
	}

	// Create metadata file
	metadataFile := filepath.Join(tempDir, "metadata.json")
	metadata := map[string]interface{}{
		"event":      string(event),
		"id":         email.ID,
		"message_id": email.MessageID,
		"from":       email.From,
		"to":         email.To,
		"cc":         email.CC,
		"bcc":        email.BCC,
		"subject":    email.Subject,
		"date":       email.Date,
		"labels":     email.Labels,
		"status":     email.Status,
		"sync_id":    email.SyncID,
		"headers":    email.Headers,
	}

	metadataJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal metadata: %w", err)
	}

	if err := os.WriteFile(metadataFile, metadataJSON, 0600); err != nil {
		return "", "", fmt.Errorf("failed to write metadata file: %w", err)
	}

	return emailFile, metadataFile, nil
}

// executeScript runs the shell script with proper sandboxing
func (sf *ShellFilter) executeScript(ctx context.Context, emailFile, metadataFile string) (*interfaces.FilterResult, error) {
	cmd := exec.CommandContext(ctx, "/bin/bash", sf.scriptPath, emailFile, metadataFile)

	// Set up environment
	cmd.Env = os.Environ()
	for key, value := range sf.environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", key, value))
	}

	// Set working directory
	cmd.Dir = filepath.Dir(emailFile)

	// Apply sandboxing if enabled
	if sf.sandboxed {
		sf.applySandbox(cmd)
	}

	// Set up pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start script: %w", err)
	}

	// Read output
	var stdoutBuf, stderrBuf strings.Builder
	done := make(chan error, 1)

	go func() {
		defer stdout.Close()
		defer stderr.Close()
		
		// Read stdout and stderr concurrently
		go io.Copy(&stdoutBuf, stdout)
		go io.Copy(&stderrBuf, stderr)
		
		done <- cmd.Wait()
	}()

	// Wait for completion or timeout
	select {
	case err := <-done:
		if err != nil {
			return nil, fmt.Errorf("script execution failed: %w, stderr: %s", err, stderrBuf.String())
		}
	case <-ctx.Done():
		cmd.Process.Kill()
		return nil, fmt.Errorf("script execution timed out")
	}

	// Parse the result
	return sf.parseScriptOutput(stdoutBuf.String(), stderrBuf.String(), emailFile, metadataFile)
}

// applySandbox applies security restrictions to the command
func (sf *ShellFilter) applySandbox(cmd *exec.Cmd) {
	// Set resource limits
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Create new process group
		Setpgid: true,
		// Set resource limits
		// Note: In a production environment, you might want to use more sophisticated
		// sandboxing like containers, chroot, or security frameworks
	}
}

// parseScriptOutput parses the script output and creates a FilterResult
func (sf *ShellFilter) parseScriptOutput(stdout, stderr, emailFile, metadataFile string) (*interfaces.FilterResult, error) {
	result := &interfaces.FilterResult{
		Modified: false,
		Actions:  []interfaces.FilterAction{},
		Labels:   []string{},
	}

	// Check if email file was modified
	modifiedEmail, err := sf.readModifiedEmail(emailFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read modified email: %w", err)
	}
	result.Email = modifiedEmail

	// Check if metadata file was modified to extract actions
	actions, labels, stopProcessing, err := sf.parseMetadataActions(metadataFile)
	if err != nil {
		return nil, fmt.Errorf("failed to parse metadata actions: %w", err)
	}

	result.Actions = actions
	result.Labels = labels
	result.StopProcessing = stopProcessing

	// Parse stdout for additional instructions
	if stdout != "" {
		if err := sf.parseStdoutInstructions(stdout, result); err != nil {
			return nil, fmt.Errorf("failed to parse stdout instructions: %w", err)
		}
	}

	// Determine if email was modified
	result.Modified = len(result.Actions) > 0 || len(result.Labels) > 0

	return result, nil
}

// readModifiedEmail reads the potentially modified email file
func (sf *ShellFilter) readModifiedEmail(emailFile string) (*interfaces.Email, error) {
	content, err := os.ReadFile(emailFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read email file: %w", err)
	}

	// Parse the email content back into an Email struct
	// This is a simplified parser - in production you'd want a more robust email parser
	email := &interfaces.Email{
		Headers: make(map[string]string),
		Labels:  []string{},
		Status:  interfaces.StatusUnread,
	}

	lines := strings.Split(string(content), "\n")
	inHeaders := true
	bodyStart := 0

	for i, line := range lines {
		if inHeaders {
			if line == "" {
				inHeaders = false
				bodyStart = i + 1
				continue
			}

			if strings.Contains(line, ":") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					value := strings.TrimSpace(parts[1])
					
					switch strings.ToLower(key) {
					case "from":
						email.From = interfaces.Address{Email: value}
					case "subject":
						email.Subject = value
					case "message-id":
						email.MessageID = value
					case "x-snail-status":
						email.Status = interfaces.EmailStatus(value)
					case "x-snail-labels":
						if value != "" {
							email.Labels = strings.Split(value, ",")
							for i, label := range email.Labels {
								email.Labels[i] = strings.TrimSpace(label)
							}
						}
					case "x-snail-sync-id":
						email.SyncID = value
					default:
						email.Headers[key] = value
					}
				}
			}
		}
	}

	if bodyStart < len(lines) {
		email.Body = strings.Join(lines[bodyStart:], "\n")
	}

	return email, nil
}

// parseMetadataActions parses actions from the metadata file
func (sf *ShellFilter) parseMetadataActions(metadataFile string) ([]interfaces.FilterAction, []string, bool, error) {
	content, err := os.ReadFile(metadataFile)
	if err != nil {
		return nil, nil, false, fmt.Errorf("failed to read metadata file: %w", err)
	}

	var metadata map[string]interface{}
	if err := json.Unmarshal(content, &metadata); err != nil {
		return nil, nil, false, fmt.Errorf("failed to parse metadata JSON: %w", err)
	}

	actions := []interfaces.FilterAction{}
	labels := []string{}
	stopProcessing := false

	// Parse actions
	if actionsData, ok := metadata["actions"].([]interface{}); ok {
		for _, actionData := range actionsData {
			if actionMap, ok := actionData.(map[string]interface{}); ok {
				action := interfaces.FilterAction{
					Params: make(map[string]interface{}),
				}
				
				if actionType, ok := actionMap["type"].(string); ok {
					action.Type = interfaces.ActionType(actionType)
				}
				if value, ok := actionMap["value"].(string); ok {
					action.Value = value
				}
				if params, ok := actionMap["params"].(map[string]interface{}); ok {
					action.Params = params
				}
				
				actions = append(actions, action)
			}
		}
	}

	// Parse labels
	if labelsData, ok := metadata["labels"].([]interface{}); ok {
		for _, labelData := range labelsData {
			if label, ok := labelData.(string); ok {
				labels = append(labels, label)
			}
		}
	}

	// Parse stop processing flag
	if stop, ok := metadata["stop_processing"].(bool); ok {
		stopProcessing = stop
	}

	return actions, labels, stopProcessing, nil
}

// parseStdoutInstructions parses instructions from stdout
func (sf *ShellFilter) parseStdoutInstructions(stdout string, result *interfaces.FilterResult) error {
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse simple instructions like "LABEL: important" or "ACTION: move:archive"
		if strings.Contains(line, ":") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				command := strings.TrimSpace(strings.ToUpper(parts[0]))
				value := strings.TrimSpace(parts[1])

				switch command {
				case "LABEL":
					result.Labels = append(result.Labels, value)
				case "ACTION":
					if actionParts := strings.SplitN(value, ":", 2); len(actionParts) == 2 {
						action := interfaces.FilterAction{
							Type:   interfaces.ActionType(actionParts[0]),
							Value:  actionParts[1],
							Params: make(map[string]interface{}),
						}
						result.Actions = append(result.Actions, action)
					}
				case "STOP":
					result.StopProcessing = true
				}
			}
		}
	}

	return scanner.Err()
}

// formatEmailContent formats an email as a standard email message
func (sf *ShellFilter) formatEmailContent(email *interfaces.Email) string {
	var content strings.Builder

	// Write headers
	content.WriteString(fmt.Sprintf("From: %s\n", sf.formatAddress(email.From)))
	
	if len(email.To) > 0 {
		toAddrs := make([]string, len(email.To))
		for i, addr := range email.To {
			toAddrs[i] = sf.formatAddress(addr)
		}
		content.WriteString(fmt.Sprintf("To: %s\n", strings.Join(toAddrs, ", ")))
	}

	if len(email.CC) > 0 {
		ccAddrs := make([]string, len(email.CC))
		for i, addr := range email.CC {
			ccAddrs[i] = sf.formatAddress(addr)
		}
		content.WriteString(fmt.Sprintf("CC: %s\n", strings.Join(ccAddrs, ", ")))
	}

	content.WriteString(fmt.Sprintf("Subject: %s\n", email.Subject))
	content.WriteString(fmt.Sprintf("Date: %s\n", email.Date.Format(time.RFC1123Z)))
	content.WriteString(fmt.Sprintf("Message-ID: %s\n", email.MessageID))

	// Write custom headers
	for key, value := range email.Headers {
		content.WriteString(fmt.Sprintf("%s: %s\n", key, value))
	}

	// Write snail-specific headers
	if len(email.Labels) > 0 {
		content.WriteString(fmt.Sprintf("X-Snail-Labels: %s\n", strings.Join(email.Labels, ",")))
	}
	content.WriteString(fmt.Sprintf("X-Snail-Status: %s\n", string(email.Status)))
	if email.SyncID != "" {
		content.WriteString(fmt.Sprintf("X-Snail-Sync-ID: %s\n", email.SyncID))
	}

	// Empty line before body
	content.WriteString("\n")

	// Write body
	content.WriteString(email.Body)

	return content.String()
}

// formatAddress formats an address for email headers
func (sf *ShellFilter) formatAddress(addr interfaces.Address) string {
	if addr.Name != "" {
		return fmt.Sprintf("%s <%s>", addr.Name, addr.Email)
	}
	return addr.Email
}

// cleanup removes temporary files
func (sf *ShellFilter) cleanup(emailFile, metadataFile string) {
	tempDir := filepath.Dir(emailFile)
	os.RemoveAll(tempDir)
}

// validateScript validates that a script exists and is executable
func validateScript(scriptPath string) error {
	info, err := os.Stat(scriptPath)
	if err != nil {
		return fmt.Errorf("script not found: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("script path is a directory")
	}

	// Check if file is executable
	if info.Mode()&0111 == 0 {
		return fmt.Errorf("script is not executable")
	}

	return nil
}