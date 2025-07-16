//! Core application logic and orchestration

use crate::draft::{DraftComposer, DraftManager, FileDraftManager};
use crate::editor::{DefaultEditorIntegration, EditorIntegration};
use crate::error::Result;
use crate::external_tools::{ExternalToolConfig, ExternalToolManager, ExternalToolResult};
use crate::git_storage::GitStorage;
use crate::models::{Account, Draft, Email, EmailMetadata};
use crate::plugin::{PluginContext, PluginRegistry, PluginResult};
use std::collections::HashMap;

/// Core application engine that orchestrates all components
pub struct GitMailCore {
    /// Git storage backend
    storage: Box<dyn GitStorage>,
    /// Draft manager
    draft_manager: Box<dyn DraftManager>,
    /// Editor integration
    editor: Box<dyn EditorIntegration>,
    /// Plugin registry
    plugin_registry: PluginRegistry,
    /// Repository path
    repo_path: String,
}

impl GitMailCore {
    /// Create a new core engine instance with storage backend
    pub fn new(
        storage: Box<dyn GitStorage>,
        draft_manager: Box<dyn DraftManager>,
        editor: Box<dyn EditorIntegration>,
        repo_path: String,
    ) -> Self {
        let mut plugin_registry = PluginRegistry::new();

        // Add default plugin directories
        let plugins_dir = std::path::Path::new(&repo_path).join("plugins");
        plugin_registry.add_plugin_dir(plugins_dir);

        // Add user-level plugins directory
        if let Some(home_dir) = dirs::home_dir() {
            let user_plugins_dir = home_dir.join(".git-mail").join("plugins");
            plugin_registry.add_plugin_dir(user_plugins_dir);
        }

        Self {
            storage,
            draft_manager,
            editor,
            plugin_registry,
            repo_path,
        }
    }

    /// Create a new core engine with default components
    pub fn new_with_defaults(storage: Box<dyn GitStorage>, drafts_dir: &str) -> Result<Self> {
        let draft_manager = Box::new(FileDraftManager::new(drafts_dir)?);
        let editor = Box::new(DefaultEditorIntegration::new());

        // Extract repo path from drafts_dir (remove /drafts suffix)
        let repo_path = std::path::Path::new(drafts_dir)
            .parent()
            .unwrap_or_else(|| std::path::Path::new("."))
            .to_string_lossy()
            .to_string();

        let mut plugin_registry = PluginRegistry::new();

        // Add default plugin directories
        let plugins_dir = std::path::Path::new(&repo_path).join("plugins");
        plugin_registry.add_plugin_dir(plugins_dir);

        // Add user-level plugins directory
        if let Some(home_dir) = dirs::home_dir() {
            let user_plugins_dir = home_dir.join(".git-mail").join("plugins");
            plugin_registry.add_plugin_dir(user_plugins_dir);
        }

        Ok(Self {
            storage,
            draft_manager,
            editor,
            plugin_registry,
            repo_path,
        })
    }

    /// Initialize the Git-Mail repository
    pub fn init_repository(&self, path: &str) -> Result<()> {
        self.storage.initialize_repository(path)
    }

    /// List emails in a folder
    pub fn list_emails(&self, folder: Option<&str>) -> Result<Vec<EmailMetadata>> {
        self.storage.list_emails(folder)
    }

    /// Get a specific email by ID
    pub fn get_email(&self, id: &str) -> Result<Email> {
        self.storage.retrieve_email(id)
    }

    /// Store an email
    pub fn store_email(&self, email: &Email) -> Result<String> {
        self.storage.store_email(email)
    }

    /// Commit changes to the repository
    pub fn commit_changes(&self, message: &str) -> Result<()> {
        self.storage.commit_changes(message)
    }

    /// Compose a new email using external editor
    pub fn compose_email(
        &mut self,
        account: String,
        to: Option<String>,
        subject: Option<String>,
    ) -> Result<Draft> {
        let draft = DraftComposer::new_compose(account, to, subject);
        let content = self.editor.compose_email(None)?;

        let mut updated_draft = self.parse_email_content_to_draft(draft, &content)?;
        self.draft_manager.create_draft(updated_draft.clone())?;

        Ok(updated_draft)
    }

    /// Reply to an email using external editor
    pub fn reply_email(&mut self, account: String, original_id: &str) -> Result<Draft> {
        let original = self.get_email(original_id)?;
        let draft = DraftComposer::new_reply(account, &original);
        let content = self.editor.reply_email(&original)?;

        let mut updated_draft = self.parse_email_content_to_draft(draft, &content)?;
        self.draft_manager.create_draft(updated_draft.clone())?;

        Ok(updated_draft)
    }

    /// Forward an email using external editor
    pub fn forward_email(&mut self, account: String, original_id: &str) -> Result<Draft> {
        let original = self.get_email(original_id)?;
        let draft = DraftComposer::new_forward(account, &original);
        let content = self.editor.forward_email(&original)?;

        let mut updated_draft = self.parse_email_content_to_draft(draft, &content)?;
        self.draft_manager.create_draft(updated_draft.clone())?;

        Ok(updated_draft)
    }

    /// Edit an existing draft
    pub fn edit_draft(&mut self, draft_id: &str) -> Result<Draft> {
        let mut draft = self.draft_manager.load_draft(draft_id)?;
        let current_content = self.draft_to_email_content(&draft);
        let edited_content = self.editor.compose_email(Some(&current_content))?;

        let updated_draft = self.parse_email_content_to_draft(draft, &edited_content)?;
        self.draft_manager.save_draft(&updated_draft)?;

        Ok(updated_draft)
    }

    /// Send a draft as an email
    pub fn send_draft(&mut self, draft_id: &str) -> Result<Email> {
        let draft = self.draft_manager.load_draft(draft_id)?;
        let email = draft.to_email();

        // Store the email
        self.store_email(&email)?;

        // Delete the draft
        self.draft_manager.delete_draft(draft_id)?;

        // Commit changes
        self.commit_changes(&format!(
            "Send email: {}",
            email
                .headers
                .get("Subject")
                .unwrap_or(&"(no subject)".to_string())
        ))?;

        Ok(email)
    }

    /// List all drafts
    pub fn list_drafts(&self) -> Result<Vec<Draft>> {
        self.draft_manager.list_drafts()
    }

    /// Get a specific draft by ID
    pub fn get_draft(&self, draft_id: &str) -> Result<Draft> {
        self.draft_manager.load_draft(draft_id)
    }

    /// Delete a draft
    pub fn delete_draft(&mut self, draft_id: &str) -> Result<()> {
        self.draft_manager.delete_draft(draft_id)
    }

    /// Auto-save a draft if needed
    pub fn auto_save_draft(&mut self, draft: &mut Draft) -> Result<bool> {
        self.draft_manager.auto_save_draft(draft)
    }

    /// Initialize and discover plugins
    pub async fn init_plugins(&mut self) -> Result<()> {
        self.plugin_registry.discover_plugins().await?;
        Ok(())
    }

    /// Execute a plugin command
    pub async fn execute_plugin(
        &self,
        plugin_name: &str,
        args: &[String],
        selected_emails: Vec<String>,
        current_folder: Option<String>,
    ) -> Result<PluginResult> {
        let context = PluginContext {
            repo_path: self.repo_path.clone(),
            working_dir: std::env::current_dir()
                .unwrap_or_else(|_| std::path::PathBuf::from("."))
                .to_string_lossy()
                .to_string(),
            env_vars: HashMap::new(),
            selected_emails,
            current_folder,
        };

        self.plugin_registry
            .execute_plugin(plugin_name, &context, args)
            .await
    }

    /// Check if a plugin command exists
    pub fn has_plugin(&self, plugin_name: &str) -> bool {
        self.plugin_registry.has_plugin(plugin_name)
    }

    /// List all available plugins
    pub fn list_plugins(&self) -> Vec<&crate::plugin::PluginInfo> {
        self.plugin_registry.list_plugins()
    }

    /// Get help for a specific plugin
    pub fn get_plugin_help(&self, plugin_name: &str) -> Option<String> {
        self.plugin_registry.get_plugin_help(plugin_name)
    }

    /// Get help for all plugins
    pub fn get_all_plugin_help(&self) -> String {
        self.plugin_registry.get_all_plugin_help()
    }

    /// Create an external tool manager for this core instance
    pub fn create_external_tool_manager(&self) -> ExternalToolManager {
        let working_dir = std::env::current_dir()
            .unwrap_or_else(|_| std::path::PathBuf::from("."))
            .to_string_lossy()
            .to_string();

        ExternalToolManager::new(&self.repo_path, &working_dir)
    }

    /// Execute an external tool with email data piping
    pub async fn pipe_emails_to_tool(
        &self,
        command: &str,
        args: &[String],
        email_ids: &[String],
        current_folder: Option<String>,
    ) -> Result<ExternalToolResult> {
        let manager = self.create_external_tool_manager();

        // Retrieve emails by ID
        let mut emails = Vec::new();
        let mut email_metadata = Vec::new();

        for email_id in email_ids {
            match self.get_email(email_id) {
                Ok(email) => {
                    email_metadata.push(email.metadata.clone());
                    emails.push(email);
                }
                Err(e) => {
                    tracing::warn!("Failed to retrieve email {}: {}", email_id, e);
                }
            }
        }

        let context = manager.create_context(
            emails.clone(),
            email_metadata,
            current_folder,
            HashMap::new(),
        );

        manager
            .pipe_emails_to_command(command, args, &emails, &context)
            .await
    }

    /// Execute an external tool without email data
    pub async fn execute_external_tool(
        &self,
        command: &str,
        args: &[String],
        current_folder: Option<String>,
    ) -> Result<ExternalToolResult> {
        let manager = self.create_external_tool_manager();

        let context = manager.create_context(vec![], vec![], current_folder, HashMap::new());

        manager
            .execute_command_with_output(command, args, &context)
            .await
    }

    /// Execute an external tool with custom configuration
    pub async fn execute_external_tool_with_config(
        &self,
        config: &ExternalToolConfig,
        email_ids: &[String],
        current_folder: Option<String>,
    ) -> Result<ExternalToolResult> {
        let manager = self.create_external_tool_manager();

        // Retrieve emails by ID if needed
        let mut emails = Vec::new();
        let mut email_metadata = Vec::new();

        if config.pipe_stdin {
            for email_id in email_ids {
                match self.get_email(email_id) {
                    Ok(email) => {
                        email_metadata.push(email.metadata.clone());
                        emails.push(email);
                    }
                    Err(e) => {
                        tracing::warn!("Failed to retrieve email {}: {}", email_id, e);
                    }
                }
            }
        }

        let context =
            manager.create_context(emails, email_metadata, current_folder, HashMap::new());

        manager.execute_tool(config, &context).await
    }

    /// Parse email content from editor into draft
    fn parse_email_content_to_draft(&self, mut draft: Draft, content: &str) -> Result<Draft> {
        let lines: Vec<&str> = content.lines().collect();
        let mut body_start = 0;

        // Parse headers
        for (i, line) in lines.iter().enumerate() {
            if line.trim().is_empty() {
                body_start = i + 1;
                break;
            }

            if let Some(colon_pos) = line.find(':') {
                let header_name = line[..colon_pos].trim().to_string();
                let header_value = line[colon_pos + 1..].trim().to_string();
                draft.headers.insert(header_name, header_value);
            }
        }

        // Parse body
        if body_start < lines.len() {
            draft.body.content = lines[body_start..].join("\n");
        }

        Ok(draft)
    }

    /// Convert draft to email content format for editor
    fn draft_to_email_content(&self, draft: &Draft) -> String {
        let mut content = String::new();

        // Add headers
        for (key, value) in &draft.headers {
            content.push_str(&format!("{}: {}\n", key, value));
        }

        // Add empty line separator
        content.push('\n');

        // Add body
        content.push_str(&draft.body.content);

        content
    }
}

/// Legacy core engine for backward compatibility
pub struct CoreEngine {
    // TODO: Add fields for storage, sync, filter engines
}

impl CoreEngine {
    /// Create a new core engine instance
    pub fn new() -> Result<Self> {
        Ok(Self {
            // TODO: Initialize components
        })
    }

    /// Initialize the Git-Mail repository
    pub async fn init_repository(&self, _path: &str) -> Result<()> {
        // TODO: Implement repository initialization
        Ok(())
    }

    /// Start the terminal user interface
    pub async fn start_tui(&self) -> Result<()> {
        // TODO: Implement TUI startup
        Ok(())
    }

    /// Sync emails for the specified account
    pub async fn sync_account(&self, _account_name: Option<&str>) -> Result<()> {
        // TODO: Implement account synchronization
        Ok(())
    }

    /// Compose a new email
    pub async fn compose_email(&self, _to: Option<&str>, _subject: Option<&str>) -> Result<()> {
        // TODO: Implement email composition
        Ok(())
    }

    /// Search emails
    pub async fn search_emails(&self, _query: &str, _folder: Option<&str>) -> Result<Vec<Email>> {
        // TODO: Implement email search
        Ok(vec![])
    }

    /// List emails in a folder
    pub async fn list_emails(&self, _folder: Option<&str>, _count: usize) -> Result<Vec<Email>> {
        // TODO: Implement email listing
        Ok(vec![])
    }

    /// Show email content
    pub async fn show_email(&self, _id: &str) -> Result<Email> {
        // TODO: Implement email retrieval
        Err(crate::error::GitMailError::EmailNotFound(_id.to_string()))
    }

    /// Add a new account
    pub async fn add_account(&self, _name: &str) -> Result<()> {
        // TODO: Implement account addition
        Ok(())
    }

    /// List configured accounts
    pub async fn list_accounts(&self) -> Result<Vec<Account>> {
        // TODO: Implement account listing
        Ok(vec![])
    }

    /// Remove an account
    pub async fn remove_account(&self, _name: &str) -> Result<()> {
        // TODO: Implement account removal
        Ok(())
    }

    /// Test account connection
    pub async fn test_account(&self, _name: &str) -> Result<()> {
        // TODO: Implement account testing
        Ok(())
    }
}
