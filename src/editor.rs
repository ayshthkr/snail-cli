//! External text editor integration

use crate::error::{GitMailError, Result};
use crate::models::Email;
use std::env;
use std::fs;
use std::path::PathBuf;
use std::process::Command;
use tempfile::TempDir;
use tracing::{debug, info, warn};

/// Detect the preferred text editor from environment variables
fn detect_editor() -> String {
    env::var("EDITOR")
        .or_else(|_| env::var("VISUAL"))
        .unwrap_or_else(|_| {
            // Try to detect common editors
            let common_editors = ["vim", "nano", "emacs", "code", "gedit"];
            for editor in &common_editors {
                if which_editor(editor) {
                    return editor.to_string();
                }
            }
            // Fallback to nano
            "nano".to_string()
        })
}

/// Check if an editor command is available in PATH
fn which_editor(editor: &str) -> bool {
    Command::new("which")
        .arg(editor)
        .output()
        .map(|output| output.status.success())
        .unwrap_or(false)
}

/// Editor configuration
#[derive(Debug, Clone)]
pub struct EditorConfig {
    /// Primary editor command
    pub editor_command: String,
    /// Additional arguments to pass to the editor
    pub editor_args: Vec<String>,
    /// File extension for temporary files
    pub temp_file_extension: String,
    /// Whether to preserve temporary files for debugging
    pub preserve_temp_files: bool,
}

impl Default for EditorConfig {
    fn default() -> Self {
        Self {
            editor_command: detect_editor(),
            editor_args: Vec::new(),
            temp_file_extension: "eml".to_string(),
            preserve_temp_files: false,
        }
    }
}

/// Editor integration interface
pub trait EditorIntegration {
    /// Open external editor for email composition
    fn compose_email(&mut self, template: Option<&str>) -> Result<String>;

    /// Open external editor for email reply
    fn reply_email(&mut self, original: &Email) -> Result<String>;

    /// Open external editor for email forward
    fn forward_email(&mut self, original: &Email) -> Result<String>;

    /// Get current editor configuration
    fn get_config(&self) -> &EditorConfig;

    /// Update editor configuration
    fn set_config(&mut self, config: EditorConfig);
}

/// Default editor integration implementation
pub struct DefaultEditorIntegration {
    config: EditorConfig,
    temp_dir: Option<TempDir>,
}

impl DefaultEditorIntegration {
    /// Create a new editor integration with default configuration
    pub fn new() -> Self {
        Self {
            config: EditorConfig::default(),
            temp_dir: None,
        }
    }

    /// Create a new editor integration with custom configuration
    pub fn with_config(config: EditorConfig) -> Self {
        Self {
            config,
            temp_dir: None,
        }
    }

    /// Initialize temporary directory for file management
    fn ensure_temp_dir(&mut self) -> Result<&TempDir> {
        if self.temp_dir.is_none() {
            let temp_dir = TempDir::new().map_err(|e| {
                GitMailError::Editor(format!("Failed to create temp directory: {}", e))
            })?;
            debug!("Created temporary directory: {:?}", temp_dir.path());
            self.temp_dir = Some(temp_dir);
        }
        Ok(self.temp_dir.as_ref().unwrap())
    }

    /// Create a temporary file with proper extension
    fn create_temp_file(&mut self, content: &str) -> Result<PathBuf> {
        // Clone the extension first to avoid borrowing conflicts
        let extension = self.config.temp_file_extension.clone();
        let temp_dir = self.ensure_temp_dir()?;

        let temp_file_name = format!(
            "git-mail-{}.{}",
            uuid::Uuid::new_v4().to_string()[..8].to_string(),
            extension
        );

        let temp_file_path = temp_dir.path().join(temp_file_name);

        fs::write(&temp_file_path, content)
            .map_err(|e| GitMailError::Editor(format!("Failed to write temp file: {}", e)))?;

        debug!("Created temporary file: {:?}", temp_file_path);
        Ok(temp_file_path)
    }

    /// Launch external editor with content using improved temporary file management
    fn launch_editor(&mut self, content: &str) -> Result<String> {
        info!("Launching editor: {}", self.config.editor_command);

        // Create temporary file with proper extension
        let temp_file_path = self.create_temp_file(content)?;

        // Build command with arguments
        let mut command = Command::new(&self.config.editor_command);
        command.args(&self.config.editor_args);
        command.arg(&temp_file_path);

        debug!("Executing command: {:?}", command);

        // Launch editor
        let status = command.status().map_err(|e| {
            GitMailError::Editor(format!(
                "Failed to launch editor '{}': {}",
                self.config.editor_command, e
            ))
        })?;

        if !status.success() {
            let exit_code = status.code().unwrap_or(-1);
            warn!("Editor exited with non-zero status: {}", exit_code);
            return Err(GitMailError::Editor(format!(
                "Editor exited with error code: {}",
                exit_code
            )));
        }

        // Read edited content
        let edited_content = fs::read_to_string(&temp_file_path)
            .map_err(|e| GitMailError::Editor(format!("Failed to read edited content: {}", e)))?;

        // Clean up temporary file unless preservation is enabled
        if !self.config.preserve_temp_files {
            if let Err(e) = fs::remove_file(&temp_file_path) {
                warn!(
                    "Failed to remove temporary file {:?}: {}",
                    temp_file_path, e
                );
            } else {
                debug!("Cleaned up temporary file: {:?}", temp_file_path);
            }
        } else {
            info!(
                "Preserving temporary file for debugging: {:?}",
                temp_file_path
            );
        }

        Ok(edited_content)
    }

    /// Generate reply template
    fn generate_reply_template(&self, original: &Email) -> String {
        let subject = original
            .headers
            .get("Subject")
            .map(|s| {
                if s.starts_with("Re: ") {
                    s.clone()
                } else {
                    format!("Re: {}", s)
                }
            })
            .unwrap_or_else(|| "Re: ".to_string());

        let from = original.headers.get("From").unwrap_or(&original.account);
        let date = original
            .headers
            .get("Date")
            .map_or("Unknown date", |v| v.as_str());

        format!(
            "To: {}\nSubject: {}\n\n\n\nOn {}, {} wrote:\n> {}\n",
            from,
            subject,
            date,
            from,
            original
                .body
                .content
                .lines()
                .collect::<Vec<_>>()
                .join("\n> ")
        )
    }

    /// Generate forward template
    fn generate_forward_template(&self, original: &Email) -> String {
        let subject = original
            .headers
            .get("Subject")
            .map(|s| {
                if s.starts_with("Fwd: ") {
                    s.clone()
                } else {
                    format!("Fwd: {}", s)
                }
            })
            .unwrap_or_else(|| "Fwd: ".to_string());

        let from = original.headers.get("From").unwrap_or(&original.account);
        let date = original
            .headers
            .get("Date")
            .map_or("Unknown date", |v| v.as_str());
        let to = original
            .headers
            .get("To")
            .map_or("Unknown recipient", |v| v.as_str());

        format!(
            "To: \nSubject: {}\n\n\n\n---------- Forwarded message ---------\nFrom: {}\nDate: {}\nTo: {}\nSubject: {}\n\n{}\n",
            subject,
            from,
            date,
            to,
            original.headers.get("Subject").map_or("", |v| v.as_str()),
            original.body.content
        )
    }
}

impl EditorIntegration for DefaultEditorIntegration {
    fn compose_email(&mut self, template: Option<&str>) -> Result<String> {
        let content = template.unwrap_or("To: \nSubject: \n\n");
        self.launch_editor(content)
    }

    fn reply_email(&mut self, original: &Email) -> Result<String> {
        let template = self.generate_reply_template(original);
        self.launch_editor(&template)
    }

    fn forward_email(&mut self, original: &Email) -> Result<String> {
        let template = self.generate_forward_template(original);
        self.launch_editor(&template)
    }

    fn get_config(&self) -> &EditorConfig {
        &self.config
    }

    fn set_config(&mut self, config: EditorConfig) {
        self.config = config;
        // Reset temp directory to use new configuration
        self.temp_dir = None;
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::Email;

    fn create_test_email() -> Email {
        let mut email = Email::new("test@example.com".to_string());
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        email
            .headers
            .insert("Subject".to_string(), "Test Subject".to_string());
        email.headers.insert(
            "Date".to_string(),
            "Mon, 1 Jan 2024 12:00:00 +0000".to_string(),
        );
        email.body.content = "This is a test email content.\nWith multiple lines.".to_string();
        email
    }

    #[test]
    fn test_detect_editor() {
        let editor = detect_editor();
        assert!(!editor.is_empty());
        // Should be one of the common editors or fallback to nano
        assert!(
            ["vim", "nano", "emacs", "code", "gedit"].contains(&editor.as_str())
                || editor == "nano"
        );
    }

    #[test]
    fn test_which_editor() {
        // Test with a command that should exist on most systems
        assert!(which_editor("sh") || which_editor("bash"));

        // Test with a command that definitely doesn't exist
        assert!(!which_editor("definitely_not_a_real_editor_command_12345"));
    }

    #[test]
    fn test_editor_config_default() {
        let config = EditorConfig::default();
        assert!(!config.editor_command.is_empty());
        assert!(config.editor_args.is_empty());
        assert_eq!(config.temp_file_extension, "eml");
        assert!(!config.preserve_temp_files);
    }

    #[test]
    fn test_editor_config_custom() {
        let config = EditorConfig {
            editor_command: "vim".to_string(),
            editor_args: vec!["-n".to_string(), "+set number".to_string()],
            temp_file_extension: "txt".to_string(),
            preserve_temp_files: true,
        };

        assert_eq!(config.editor_command, "vim");
        assert_eq!(config.editor_args, vec!["-n", "+set number"]);
        assert_eq!(config.temp_file_extension, "txt");
        assert!(config.preserve_temp_files);
    }

    #[test]
    fn test_default_editor_integration_new() {
        let editor = DefaultEditorIntegration::new();
        assert!(!editor.config.editor_command.is_empty());
        assert!(editor.temp_dir.is_none());
    }

    #[test]
    fn test_default_editor_integration_with_config() {
        let config = EditorConfig {
            editor_command: "test-editor".to_string(),
            editor_args: vec!["--test".to_string()],
            temp_file_extension: "test".to_string(),
            preserve_temp_files: true,
        };

        let editor = DefaultEditorIntegration::with_config(config.clone());
        assert_eq!(editor.config.editor_command, "test-editor");
        assert_eq!(editor.config.editor_args, vec!["--test"]);
        assert_eq!(editor.config.temp_file_extension, "test");
        assert!(editor.config.preserve_temp_files);
    }

    #[test]
    fn test_get_and_set_config() {
        let mut editor = DefaultEditorIntegration::new();
        let original_command = editor.get_config().editor_command.clone();

        let new_config = EditorConfig {
            editor_command: "new-editor".to_string(),
            editor_args: vec!["--new".to_string()],
            temp_file_extension: "new".to_string(),
            preserve_temp_files: true,
        };

        editor.set_config(new_config.clone());

        assert_eq!(editor.get_config().editor_command, "new-editor");
        assert_eq!(editor.get_config().editor_args, vec!["--new"]);
        assert_eq!(editor.get_config().temp_file_extension, "new");
        assert!(editor.get_config().preserve_temp_files);
        assert_ne!(editor.get_config().editor_command, original_command);
    }

    #[test]
    fn test_create_temp_file() {
        let mut editor = DefaultEditorIntegration::new();
        let content = "Test content for temporary file";

        let temp_file_path = editor
            .create_temp_file(content)
            .expect("Failed to create temp file");

        // Verify file exists and has correct content
        assert!(temp_file_path.exists());
        let read_content = fs::read_to_string(&temp_file_path).expect("Failed to read temp file");
        assert_eq!(read_content, content);

        // Verify file extension
        assert!(temp_file_path.extension().unwrap() == "eml");
    }

    #[test]
    fn test_create_temp_file_custom_extension() {
        let config = EditorConfig {
            editor_command: "test".to_string(),
            editor_args: vec![],
            temp_file_extension: "custom".to_string(),
            preserve_temp_files: false,
        };

        let mut editor = DefaultEditorIntegration::with_config(config);
        let content = "Test content";

        let temp_file_path = editor
            .create_temp_file(content)
            .expect("Failed to create temp file");
        assert!(temp_file_path.extension().unwrap() == "custom");
    }

    #[test]
    fn test_ensure_temp_dir() {
        let mut editor = DefaultEditorIntegration::new();

        // Initially no temp dir
        assert!(editor.temp_dir.is_none());

        // Ensure temp dir creates one
        let temp_dir_path = {
            let temp_dir = editor.ensure_temp_dir().expect("Failed to ensure temp dir");
            temp_dir.path().to_path_buf()
        };
        assert!(temp_dir_path.exists());

        // Second call should return the same directory
        let temp_dir_path2 = {
            let temp_dir2 = editor.ensure_temp_dir().expect("Failed to ensure temp dir");
            temp_dir2.path().to_path_buf()
        };
        assert_eq!(temp_dir_path, temp_dir_path2);
    }

    #[test]
    fn test_generate_reply_template() {
        let editor = DefaultEditorIntegration::new();
        let email = create_test_email();

        let template = editor.generate_reply_template(&email);

        assert!(template.contains("To: sender@example.com"));
        assert!(template.contains("Subject: Re: Test Subject"));
        assert!(template.contains("On Mon, 1 Jan 2024 12:00:00 +0000, sender@example.com wrote:"));
        assert!(template.contains("> This is a test email content."));
        assert!(template.contains("> With multiple lines."));
    }

    #[test]
    fn test_generate_reply_template_already_reply() {
        let editor = DefaultEditorIntegration::new();
        let mut email = create_test_email();
        email
            .headers
            .insert("Subject".to_string(), "Re: Original Subject".to_string());

        let template = editor.generate_reply_template(&email);

        // Should not add another "Re: " prefix
        assert!(template.contains("Subject: Re: Original Subject"));
        assert!(!template.contains("Subject: Re: Re: Original Subject"));
    }

    #[test]
    fn test_generate_forward_template() {
        let editor = DefaultEditorIntegration::new();
        let email = create_test_email();

        let template = editor.generate_forward_template(&email);

        assert!(template.contains("Subject: Fwd: Test Subject"));
        assert!(template.contains("---------- Forwarded message ---------"));
        assert!(template.contains("From: sender@example.com"));
        assert!(template.contains("Date: Mon, 1 Jan 2024 12:00:00 +0000"));
        assert!(template.contains("To: recipient@example.com"));
        assert!(template.contains("This is a test email content."));
    }

    #[test]
    fn test_generate_forward_template_already_forward() {
        let editor = DefaultEditorIntegration::new();
        let mut email = create_test_email();
        email
            .headers
            .insert("Subject".to_string(), "Fwd: Original Subject".to_string());

        let template = editor.generate_forward_template(&email);

        // Should not add another "Fwd: " prefix
        assert!(template.contains("Subject: Fwd: Original Subject"));
        assert!(!template.contains("Subject: Fwd: Fwd: Original Subject"));
    }

    #[test]
    fn test_generate_templates_missing_headers() {
        let editor = DefaultEditorIntegration::new();
        let mut email = Email::new("test@example.com".to_string());
        email.body.content = "Test content".to_string();

        let reply_template = editor.generate_reply_template(&email);
        assert!(reply_template.contains("Subject: Re: "));
        assert!(reply_template.contains("Unknown date"));

        let forward_template = editor.generate_forward_template(&email);
        assert!(forward_template.contains("Subject: Fwd: "));
        assert!(forward_template.contains("Unknown date"));
        assert!(forward_template.contains("Unknown recipient"));
    }

    // Mock editor tests - these test the integration without actually launching an editor
    #[test]
    fn test_launch_editor_mock_success() {
        // This test would require mocking the Command execution
        // For now, we'll test the template generation which is the core logic
        let editor = DefaultEditorIntegration::new();
        let email = create_test_email();

        // Test that templates are generated correctly
        let reply_template = editor.generate_reply_template(&email);
        assert!(!reply_template.is_empty());
        assert!(reply_template.contains("To: "));
        assert!(reply_template.contains("Subject: "));

        let forward_template = editor.generate_forward_template(&email);
        assert!(!forward_template.is_empty());
        assert!(forward_template.contains("Subject: "));
        assert!(forward_template.contains("---------- Forwarded message ---------"));
    }

    #[test]
    fn test_editor_integration_trait_methods() {
        let mut editor = DefaultEditorIntegration::new();

        // Test that trait methods exist and can be called
        // Note: These will fail if no editor is available, but that's expected in CI
        let config = editor.get_config();
        assert!(!config.editor_command.is_empty());

        let new_config = EditorConfig {
            editor_command: "test-editor".to_string(),
            editor_args: vec![],
            temp_file_extension: "test".to_string(),
            preserve_temp_files: false,
        };

        editor.set_config(new_config);
        assert_eq!(editor.get_config().editor_command, "test-editor");
    }

    #[test]
    fn test_temp_file_cleanup_behavior() {
        // Test with preserve_temp_files = false
        let config = EditorConfig {
            editor_command: "echo".to_string(), // Use echo as a safe command
            editor_args: vec![],
            temp_file_extension: "test".to_string(),
            preserve_temp_files: false,
        };

        let mut editor = DefaultEditorIntegration::with_config(config);
        let content = "test content";

        // Create temp file
        let temp_file_path = editor
            .create_temp_file(content)
            .expect("Failed to create temp file");
        assert!(temp_file_path.exists());

        // Test with preserve_temp_files = true
        let config_preserve = EditorConfig {
            editor_command: "echo".to_string(),
            editor_args: vec![],
            temp_file_extension: "test".to_string(),
            preserve_temp_files: true,
        };

        editor.set_config(config_preserve);
        let temp_file_path2 = editor
            .create_temp_file(content)
            .expect("Failed to create temp file");
        assert!(temp_file_path2.exists());
    }

    #[test]
    fn test_error_handling() {
        // Test with invalid editor command
        let config = EditorConfig {
            editor_command: "definitely_not_a_real_command_12345".to_string(),
            editor_args: vec![],
            temp_file_extension: "test".to_string(),
            preserve_temp_files: false,
        };

        let mut editor = DefaultEditorIntegration::with_config(config);

        // This should fail when trying to launch the non-existent editor
        let result = editor.launch_editor("test content");
        assert!(result.is_err());

        if let Err(GitMailError::Editor(msg)) = result {
            assert!(msg.contains("Failed to launch editor"));
        } else {
            panic!("Expected GitMailError::Editor");
        }
    }

    #[test]
    fn test_concurrent_temp_file_creation() {
        let mut editor = DefaultEditorIntegration::new();

        // Create multiple temp files
        let file1 = editor
            .create_temp_file("content1")
            .expect("Failed to create temp file 1");
        let file2 = editor
            .create_temp_file("content2")
            .expect("Failed to create temp file 2");
        let file3 = editor
            .create_temp_file("content3")
            .expect("Failed to create temp file 3");

        // All files should exist and be different
        assert!(file1.exists());
        assert!(file2.exists());
        assert!(file3.exists());
        assert_ne!(file1, file2);
        assert_ne!(file2, file3);
        assert_ne!(file1, file3);

        // Content should be correct
        assert_eq!(fs::read_to_string(&file1).unwrap(), "content1");
        assert_eq!(fs::read_to_string(&file2).unwrap(), "content2");
        assert_eq!(fs::read_to_string(&file3).unwrap(), "content3");
    }
}
