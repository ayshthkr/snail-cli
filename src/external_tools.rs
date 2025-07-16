//! External tool integration and piping support
//!
//! This module provides functionality for integrating with external command-line tools,
//! supporting stdin/stdout piping for email data and creating integration points for
//! external utilities.

use crate::error::{GitMailError, Result};
use crate::models::{Email, EmailMetadata};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::process::Stdio;
use tokio::io::AsyncWriteExt;
use tokio::process::Command as AsyncCommand;
use tracing::{debug, error, info, warn};

/// Configuration for external tool integration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExternalToolConfig {
    /// Tool name/identifier
    pub name: String,
    /// Command to execute
    pub command: String,
    /// Arguments to pass to the command
    pub args: Vec<String>,
    /// Whether to pipe email content to stdin
    pub pipe_stdin: bool,
    /// Whether to capture stdout
    pub capture_stdout: bool,
    /// Whether to capture stderr
    pub capture_stderr: bool,
    /// Environment variables to set
    pub env_vars: HashMap<String, String>,
    /// Working directory for the command
    pub working_dir: Option<String>,
    /// Timeout in seconds (None for no timeout)
    pub timeout_seconds: Option<u64>,
}

/// Result of external tool execution
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExternalToolResult {
    /// Exit code from the tool
    pub exit_code: i32,
    /// Standard output from the tool
    pub stdout: String,
    /// Standard error from the tool
    pub stderr: String,
    /// Whether the execution was successful
    pub success: bool,
    /// Execution time in milliseconds
    pub execution_time_ms: u64,
}

/// Context for external tool execution
#[derive(Debug, Clone)]
pub struct ExternalToolContext {
    /// Repository path
    pub repo_path: String,
    /// Current working directory
    pub working_dir: String,
    /// Selected emails for processing
    pub emails: Vec<Email>,
    /// Email metadata for selected emails
    pub email_metadata: Vec<EmailMetadata>,
    /// Additional environment variables
    pub env_vars: HashMap<String, String>,
    /// Current folder context
    pub current_folder: Option<String>,
}

/// External tool integration manager
pub struct ExternalToolManager {
    /// Repository path
    repo_path: String,
    /// Default working directory
    working_dir: String,
}

impl ExternalToolManager {
    /// Create a new external tool manager
    pub fn new<P: AsRef<str>>(repo_path: P, working_dir: P) -> Self {
        Self {
            repo_path: repo_path.as_ref().to_string(),
            working_dir: working_dir.as_ref().to_string(),
        }
    }

    /// Execute an external tool with the given configuration and context
    pub async fn execute_tool(
        &self,
        config: &ExternalToolConfig,
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        let start_time = std::time::Instant::now();

        info!(
            "Executing external tool: {} with command: {}",
            config.name, config.command
        );
        debug!("Tool config: {:?}", config);

        // Prepare the command
        let mut cmd = AsyncCommand::new(&config.command);
        cmd.args(&config.args);

        // Set working directory
        let work_dir = config.working_dir.as_ref().unwrap_or(&context.working_dir);
        cmd.current_dir(work_dir);

        // Set up environment variables
        let mut env_vars = context.env_vars.clone();
        env_vars.extend(config.env_vars.clone());

        // Add Git-Mail specific environment variables
        env_vars.insert("GITMAIL_REPO_PATH".to_string(), context.repo_path.clone());
        env_vars.insert(
            "GITMAIL_WORKING_DIR".to_string(),
            context.working_dir.clone(),
        );
        env_vars.insert(
            "GITMAIL_EMAIL_COUNT".to_string(),
            context.emails.len().to_string(),
        );

        if let Some(folder) = &context.current_folder {
            env_vars.insert("GITMAIL_CURRENT_FOLDER".to_string(), folder.clone());
        }

        // Add email IDs as environment variable
        let email_ids: Vec<String> = context.emails.iter().map(|e| e.id.clone()).collect();
        if !email_ids.is_empty() {
            env_vars.insert("GITMAIL_EMAIL_IDS".to_string(), email_ids.join(","));
        }

        cmd.envs(&env_vars);

        // Set up stdio
        if config.pipe_stdin {
            cmd.stdin(Stdio::piped());
        } else {
            cmd.stdin(Stdio::null());
        }

        if config.capture_stdout {
            cmd.stdout(Stdio::piped());
        } else {
            cmd.stdout(Stdio::inherit());
        }

        if config.capture_stderr {
            cmd.stderr(Stdio::piped());
        } else {
            cmd.stderr(Stdio::inherit());
        }

        // Spawn the process
        let mut child = cmd.spawn().map_err(|e| {
            GitMailError::ExternalToolError(format!(
                "Failed to spawn external tool '{}': {}",
                config.name, e
            ))
        })?;

        // Handle stdin piping if enabled
        if config.pipe_stdin {
            if let Some(mut stdin) = child.stdin.take() {
                let emails_data = self.prepare_email_data_for_piping(&context.emails)?;

                // Write email data to stdin in a separate task
                let write_task = tokio::spawn(async move {
                    if let Err(e) = stdin.write_all(emails_data.as_bytes()).await {
                        error!("Failed to write to stdin: {}", e);
                    }
                    if let Err(e) = stdin.shutdown().await {
                        error!("Failed to close stdin: {}", e);
                    }
                });

                // Don't wait for write task to complete, let it run in background
                tokio::spawn(async move {
                    if let Err(e) = write_task.await {
                        error!("Write task failed: {}", e);
                    }
                });
            }
        }

        // Wait for the process to complete with optional timeout
        let output = if let Some(timeout_secs) = config.timeout_seconds {
            let timeout_duration = tokio::time::Duration::from_secs(timeout_secs);

            // Use select to handle timeout without consuming child
            tokio::select! {
                result = child.wait_with_output() => {
                    result.map_err(|e| {
                        GitMailError::ExternalToolError(format!(
                            "External tool '{}' execution failed: {}",
                            config.name, e
                        ))
                    })?
                }
                _ = tokio::time::sleep(timeout_duration) => {
                    return Err(GitMailError::ExternalToolError(format!(
                        "External tool '{}' timed out after {} seconds",
                        config.name, timeout_secs
                    )));
                }
            }
        } else {
            child.wait_with_output().await.map_err(|e| {
                GitMailError::ExternalToolError(format!(
                    "External tool '{}' execution failed: {}",
                    config.name, e
                ))
            })?
        };

        let execution_time = start_time.elapsed();
        let exit_code = output.status.code().unwrap_or(-1);
        let success = output.status.success();

        let stdout = if config.capture_stdout {
            String::from_utf8_lossy(&output.stdout).to_string()
        } else {
            String::new()
        };

        let stderr = if config.capture_stderr {
            String::from_utf8_lossy(&output.stderr).to_string()
        } else {
            String::new()
        };

        if !success {
            warn!(
                "External tool '{}' exited with code {}: {}",
                config.name, exit_code, stderr
            );
        } else {
            info!("External tool '{}' completed successfully", config.name);
        }

        Ok(ExternalToolResult {
            exit_code,
            stdout,
            stderr,
            success,
            execution_time_ms: execution_time.as_millis() as u64,
        })
    }

    /// Execute a simple command with email data piped to stdin
    pub async fn pipe_emails_to_command(
        &self,
        command: &str,
        args: &[String],
        emails: &[Email],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        let config = ExternalToolConfig {
            name: format!("pipe-{}", command),
            command: command.to_string(),
            args: args.to_vec(),
            pipe_stdin: true,
            capture_stdout: true,
            capture_stderr: true,
            env_vars: HashMap::new(),
            working_dir: Some(context.working_dir.clone()),
            timeout_seconds: Some(300), // 5 minute default timeout
        };

        let pipe_context = ExternalToolContext {
            emails: emails.to_vec(),
            ..context.clone()
        };

        self.execute_tool(&config, &pipe_context).await
    }

    /// Execute a command and capture its output
    pub async fn execute_command_with_output(
        &self,
        command: &str,
        args: &[String],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        let config = ExternalToolConfig {
            name: format!("exec-{}", command),
            command: command.to_string(),
            args: args.to_vec(),
            pipe_stdin: false,
            capture_stdout: true,
            capture_stderr: true,
            env_vars: HashMap::new(),
            working_dir: Some(context.working_dir.clone()),
            timeout_seconds: Some(60), // 1 minute default timeout
        };

        self.execute_tool(&config, context).await
    }

    /// Prepare email data for piping to external tools
    fn prepare_email_data_for_piping(&self, emails: &[Email]) -> Result<String> {
        let mut output = String::new();

        for (i, email) in emails.iter().enumerate() {
            if i > 0 {
                output.push_str("\n---EMAIL-SEPARATOR---\n");
            }

            // Add email metadata as comments
            output.push_str(&format!("# Email ID: {}\n", email.id));
            output.push_str(&format!("# Account: {}\n", email.account));

            if let Some(message_id) = email.headers.get("Message-ID") {
                output.push_str(&format!("# Message-ID: {}\n", message_id));
            }

            // Add headers
            for (key, value) in &email.headers {
                output.push_str(&format!("{}: {}\n", key, value));
            }

            // Add separator between headers and body
            output.push('\n');

            // Add body content
            output.push_str(&email.body.content);
            output.push('\n');
        }

        Ok(output)
    }

    /// Create a context for external tool execution
    pub fn create_context(
        &self,
        emails: Vec<Email>,
        email_metadata: Vec<EmailMetadata>,
        current_folder: Option<String>,
        additional_env: HashMap<String, String>,
    ) -> ExternalToolContext {
        ExternalToolContext {
            repo_path: self.repo_path.clone(),
            working_dir: self.working_dir.clone(),
            emails,
            email_metadata,
            env_vars: additional_env,
            current_folder,
        }
    }
}

/// Utility functions for common external tool integrations
pub mod utils {
    use super::*;

    /// Pipe emails to grep for searching
    pub async fn grep_emails(
        manager: &ExternalToolManager,
        pattern: &str,
        emails: &[Email],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        // Split pattern into separate arguments if it contains spaces
        let args: Vec<String> = pattern.split_whitespace().map(|s| s.to_string()).collect();
        manager
            .pipe_emails_to_command("grep", &args, emails, context)
            .await
    }

    /// Pipe emails to awk for processing
    pub async fn awk_emails(
        manager: &ExternalToolManager,
        script: &str,
        emails: &[Email],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        let args = vec![script.to_string()];
        manager
            .pipe_emails_to_command("awk", &args, emails, context)
            .await
    }

    /// Pipe emails to sed for text processing
    pub async fn sed_emails(
        manager: &ExternalToolManager,
        expression: &str,
        emails: &[Email],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        let args = vec![expression.to_string()];
        manager
            .pipe_emails_to_command("sed", &args, emails, context)
            .await
    }

    /// Count lines in emails using wc
    pub async fn count_email_lines(
        manager: &ExternalToolManager,
        emails: &[Email],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        let args = vec!["-l".to_string()];
        manager
            .pipe_emails_to_command("wc", &args, emails, context)
            .await
    }

    /// Sort emails using sort command
    pub async fn sort_emails(
        manager: &ExternalToolManager,
        sort_options: &[String],
        emails: &[Email],
        context: &ExternalToolContext,
    ) -> Result<ExternalToolResult> {
        manager
            .pipe_emails_to_command("sort", sort_options, emails, context)
            .await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::{EmailBody, EmailMetadata};
    use chrono::Utc;
    use std::collections::HashMap;
    use tempfile::TempDir;

    fn create_test_email(id: &str, subject: &str, content: &str) -> Email {
        let mut headers = HashMap::new();
        headers.insert("Subject".to_string(), subject.to_string());
        headers.insert("From".to_string(), "test@example.com".to_string());
        headers.insert("To".to_string(), "recipient@example.com".to_string());

        Email {
            id: id.to_string(),
            message_id: format!("<{}@example.com>", id),
            account: "test@example.com".to_string(),
            headers,
            body: EmailBody {
                content_type: "text/plain".to_string(),
                content: content.to_string(),
                html_content: None,
            },
            attachments: vec![],
            metadata: EmailMetadata {
                file_path: format!("/tmp/{}.txt", id),
                folder: "inbox".to_string(),
                tags: vec![],
                is_read: false,
                is_starred: false,
                created_at: Utc::now(),
                modified_at: Utc::now(),
            },
        }
    }

    #[tokio::test]
    async fn test_external_tool_manager_creation() {
        let temp_dir = TempDir::new().unwrap();
        let repo_path = temp_dir.path().to_string_lossy();
        let working_dir = temp_dir.path().to_string_lossy();

        let manager = ExternalToolManager::new(&*repo_path, &*working_dir);
        assert_eq!(manager.repo_path, repo_path);
        assert_eq!(manager.working_dir, working_dir);
    }

    #[tokio::test]
    async fn test_prepare_email_data_for_piping() {
        let temp_dir = TempDir::new().unwrap();
        let repo_path = temp_dir.path().to_string_lossy();
        let working_dir = temp_dir.path().to_string_lossy();

        let manager = ExternalToolManager::new(&*repo_path, &*working_dir);

        let emails = vec![
            create_test_email("1", "Test Subject 1", "Test content 1"),
            create_test_email("2", "Test Subject 2", "Test content 2"),
        ];

        let data = manager.prepare_email_data_for_piping(&emails).unwrap();

        assert!(data.contains("# Email ID: 1"));
        assert!(data.contains("# Email ID: 2"));
        assert!(data.contains("Subject: Test Subject 1"));
        assert!(data.contains("Subject: Test Subject 2"));
        assert!(data.contains("Test content 1"));
        assert!(data.contains("Test content 2"));
        assert!(data.contains("---EMAIL-SEPARATOR---"));
    }

    #[tokio::test]
    async fn test_execute_simple_command() {
        let temp_dir = TempDir::new().unwrap();
        let repo_path = temp_dir.path().to_string_lossy();
        let working_dir = temp_dir.path().to_string_lossy();

        let manager = ExternalToolManager::new(&*repo_path, &*working_dir);

        let context = manager.create_context(vec![], vec![], None, HashMap::new());

        // Test a simple echo command
        let result = manager
            .execute_command_with_output("echo", &["Hello, World!".to_string()], &context)
            .await
            .unwrap();

        assert!(result.success);
        assert_eq!(result.exit_code, 0);
        assert!(result.stdout.contains("Hello, World!"));
    }

    #[tokio::test]
    async fn test_context_creation() {
        let temp_dir = TempDir::new().unwrap();
        let repo_path = temp_dir.path().to_string_lossy();
        let working_dir = temp_dir.path().to_string_lossy();

        let manager = ExternalToolManager::new(&*repo_path, &*working_dir);

        let emails = vec![create_test_email("1", "Test", "Content")];
        let metadata = vec![emails[0].metadata.clone()];
        let mut env_vars = HashMap::new();
        env_vars.insert("TEST_VAR".to_string(), "test_value".to_string());

        let context = manager.create_context(
            emails.clone(),
            metadata,
            Some("inbox".to_string()),
            env_vars.clone(),
        );

        assert_eq!(context.emails.len(), 1);
        assert_eq!(context.emails[0].id, "1");
        assert_eq!(context.current_folder, Some("inbox".to_string()));
        assert_eq!(
            context.env_vars.get("TEST_VAR"),
            Some(&"test_value".to_string())
        );
    }

    #[tokio::test]
    async fn test_grep_utility() {
        let temp_dir = TempDir::new().unwrap();
        let repo_path = temp_dir.path().to_string_lossy();
        let working_dir = temp_dir.path().to_string_lossy();

        let manager = ExternalToolManager::new(&*repo_path, &*working_dir);

        let emails = vec![
            create_test_email("1", "Important Message", "This is important content"),
            create_test_email("2", "Regular Message", "This is regular content"),
        ];

        let context = manager.create_context(emails.clone(), vec![], None, HashMap::new());

        // Test grep for "important" (case-insensitive)
        let result = utils::grep_emails(&manager, "-i important", &emails, &context)
            .await
            .unwrap();

        if !result.success {
            println!("Grep failed with exit code: {}", result.exit_code);
            println!("Stderr: {}", result.stderr);
            println!("Stdout: {}", result.stdout);
        }

        assert!(result.success, "Grep command failed: {}", result.stderr);
        assert!(
            result.stdout.contains("Important Message")
                || result.stdout.contains("important content")
        );
    }
}
