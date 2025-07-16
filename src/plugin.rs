//! Plugin system for custom commands
//!
//! This module provides a plugin system that allows users to extend Git-Mail
//! with custom commands. Plugins can access email data and Git repository
//! information to implement custom functionality.

use crate::error::{GitMailError, Result};
use crate::models::{Email, EmailMetadata};
use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::process::{Command, Stdio};
use tokio::fs;
use tracing::{debug, error, info, warn};

/// Context provided to plugins during execution
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PluginContext {
    /// Current repository path
    pub repo_path: String,
    /// Current working directory
    pub working_dir: String,
    /// Environment variables to pass to plugin
    pub env_vars: HashMap<String, String>,
    /// Selected email IDs (if any)
    pub selected_emails: Vec<String>,
    /// Current folder context
    pub current_folder: Option<String>,
}

/// Result of plugin execution
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PluginResult {
    /// Exit code from plugin execution
    pub exit_code: i32,
    /// Standard output from plugin
    pub stdout: String,
    /// Standard error from plugin
    pub stderr: String,
    /// Whether the plugin execution was successful
    pub success: bool,
}

/// Plugin metadata and configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PluginInfo {
    /// Plugin name (used as command name)
    pub name: String,
    /// Plugin description for help system
    pub description: String,
    /// Plugin version
    pub version: String,
    /// Plugin author
    pub author: Option<String>,
    /// Path to the plugin executable or script
    pub executable_path: PathBuf,
    /// Whether the plugin requires email context
    pub requires_email_context: bool,
    /// Whether the plugin can modify the repository
    pub can_modify_repo: bool,
    /// Plugin usage information for help
    pub usage: Option<String>,
    /// Plugin examples for help
    pub examples: Vec<String>,
}

/// Trait for plugin execution
#[async_trait]
pub trait Plugin: Send + Sync {
    /// Get plugin information
    fn info(&self) -> &PluginInfo;

    /// Execute the plugin with given context
    async fn execute(&self, context: &PluginContext, args: &[String]) -> Result<PluginResult>;

    /// Validate plugin before execution
    async fn validate(&self) -> Result<()>;
}

/// Script-based plugin implementation
#[derive(Debug)]
pub struct ScriptPlugin {
    info: PluginInfo,
}

impl ScriptPlugin {
    /// Create a new script plugin from a plugin info
    pub fn new(info: PluginInfo) -> Self {
        Self { info }
    }

    /// Create a script plugin from a plugin file
    pub async fn from_file<P: AsRef<Path>>(plugin_file: P) -> Result<Self> {
        let content = fs::read_to_string(&plugin_file).await.map_err(|e| {
            GitMailError::PluginError(format!(
                "Failed to read plugin file {}: {}",
                plugin_file.as_ref().display(),
                e
            ))
        })?;

        // Try to parse as TOML first, then as executable script
        if let Ok(info) = toml::from_str::<PluginInfo>(&content) {
            Ok(Self::new(info))
        } else {
            // Treat as executable script, extract metadata from comments
            let info = Self::extract_metadata_from_script(&plugin_file, &content)?;
            Ok(Self::new(info))
        }
    }

    /// Extract plugin metadata from script comments
    fn extract_metadata_from_script<P: AsRef<Path>>(
        script_path: P,
        content: &str,
    ) -> Result<PluginInfo> {
        let mut name = script_path
            .as_ref()
            .file_stem()
            .and_then(|s| s.to_str())
            .unwrap_or("unknown")
            .to_string();
        let mut description = "Custom plugin".to_string();
        let mut version = "1.0.0".to_string();
        let mut author = None;
        let mut usage = None;
        let mut examples = Vec::new();
        let mut requires_email_context = false;
        let mut can_modify_repo = false;

        // Parse metadata from comments
        for line in content.lines() {
            let line = line.trim();
            if line.starts_with('#') || line.starts_with("//") {
                let comment = line.trim_start_matches('#').trim_start_matches("//").trim();

                if let Some(value) = comment.strip_prefix("@name:") {
                    name = value.trim().to_string();
                } else if let Some(value) = comment.strip_prefix("@description:") {
                    description = value.trim().to_string();
                } else if let Some(value) = comment.strip_prefix("@version:") {
                    version = value.trim().to_string();
                } else if let Some(value) = comment.strip_prefix("@author:") {
                    author = Some(value.trim().to_string());
                } else if let Some(value) = comment.strip_prefix("@usage:") {
                    usage = Some(value.trim().to_string());
                } else if let Some(value) = comment.strip_prefix("@example:") {
                    examples.push(value.trim().to_string());
                } else if comment.contains("@requires-email-context") {
                    requires_email_context = true;
                } else if comment.contains("@can-modify-repo") {
                    can_modify_repo = true;
                }
            }
        }

        Ok(PluginInfo {
            name,
            description,
            version,
            author,
            executable_path: script_path.as_ref().to_path_buf(),
            requires_email_context,
            can_modify_repo,
            usage,
            examples,
        })
    }
}

#[async_trait]
impl Plugin for ScriptPlugin {
    fn info(&self) -> &PluginInfo {
        &self.info
    }

    async fn execute(&self, context: &PluginContext, args: &[String]) -> Result<PluginResult> {
        debug!("Executing plugin: {} with args: {:?}", self.info.name, args);

        // Prepare environment variables
        let mut env_vars = context.env_vars.clone();
        env_vars.insert("GITMAIL_REPO_PATH".to_string(), context.repo_path.clone());
        env_vars.insert(
            "GITMAIL_WORKING_DIR".to_string(),
            context.working_dir.clone(),
        );

        if let Some(folder) = &context.current_folder {
            env_vars.insert("GITMAIL_CURRENT_FOLDER".to_string(), folder.clone());
        }

        if !context.selected_emails.is_empty() {
            env_vars.insert(
                "GITMAIL_SELECTED_EMAILS".to_string(),
                context.selected_emails.join(","),
            );
        }

        // Execute the plugin
        let mut cmd = Command::new(&self.info.executable_path);
        cmd.args(args)
            .envs(&env_vars)
            .current_dir(&context.working_dir)
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        let output = cmd.output().map_err(|e| {
            GitMailError::PluginError(format!(
                "Failed to execute plugin {}: {}",
                self.info.name, e
            ))
        })?;

        let stdout = String::from_utf8_lossy(&output.stdout).to_string();
        let stderr = String::from_utf8_lossy(&output.stderr).to_string();
        let exit_code = output.status.code().unwrap_or(-1);
        let success = output.status.success();

        if !success {
            warn!(
                "Plugin {} exited with code {}: {}",
                self.info.name, exit_code, stderr
            );
        }

        Ok(PluginResult {
            exit_code,
            stdout,
            stderr,
            success,
        })
    }

    async fn validate(&self) -> Result<()> {
        // Check if executable exists and is executable
        if !self.info.executable_path.exists() {
            return Err(GitMailError::PluginError(format!(
                "Plugin executable not found: {}",
                self.info.executable_path.display()
            )));
        }

        // Check if file is executable (Unix-like systems)
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let metadata = std::fs::metadata(&self.info.executable_path).map_err(|e| {
                GitMailError::PluginError(format!("Failed to read plugin metadata: {}", e))
            })?;
            let permissions = metadata.permissions();
            if permissions.mode() & 0o111 == 0 {
                return Err(GitMailError::PluginError(format!(
                    "Plugin file is not executable: {}",
                    self.info.executable_path.display()
                )));
            }
        }

        Ok(())
    }
}

/// Plugin registry for managing and executing plugins
pub struct PluginRegistry {
    plugins: HashMap<String, Box<dyn Plugin>>,
    plugin_dirs: Vec<PathBuf>,
}

impl PluginRegistry {
    /// Create a new plugin registry
    pub fn new() -> Self {
        Self {
            plugins: HashMap::new(),
            plugin_dirs: Vec::new(),
        }
    }

    /// Add a plugin directory to search for plugins
    pub fn add_plugin_dir<P: AsRef<Path>>(&mut self, dir: P) {
        self.plugin_dirs.push(dir.as_ref().to_path_buf());
    }

    /// Register a plugin
    pub fn register_plugin(&mut self, plugin: Box<dyn Plugin>) {
        let name = plugin.info().name.clone();
        info!("Registering plugin: {}", name);
        self.plugins.insert(name, plugin);
    }

    /// Discover and load plugins from configured directories
    pub async fn discover_plugins(&mut self) -> Result<()> {
        for plugin_dir in &self.plugin_dirs.clone() {
            if !plugin_dir.exists() {
                debug!("Plugin directory does not exist: {}", plugin_dir.display());
                continue;
            }

            info!("Discovering plugins in: {}", plugin_dir.display());
            self.discover_plugins_in_dir(plugin_dir).await?;
        }
        Ok(())
    }

    /// Discover plugins in a specific directory
    async fn discover_plugins_in_dir(&mut self, dir: &Path) -> Result<()> {
        let mut entries = fs::read_dir(dir).await.map_err(|e| {
            GitMailError::PluginError(format!(
                "Failed to read plugin directory {}: {}",
                dir.display(),
                e
            ))
        })?;

        while let Some(entry) = entries.next_entry().await.map_err(|e| {
            GitMailError::PluginError(format!("Failed to read directory entry: {}", e))
        })? {
            let path = entry.path();

            // Skip directories and non-executable files
            if path.is_dir() {
                continue;
            }

            // Try to load as plugin
            match ScriptPlugin::from_file(&path).await {
                Ok(plugin) => {
                    if let Err(e) = plugin.validate().await {
                        warn!("Plugin validation failed for {}: {}", path.display(), e);
                        continue;
                    }
                    self.register_plugin(Box::new(plugin));
                }
                Err(e) => {
                    debug!("Failed to load plugin from {}: {}", path.display(), e);
                }
            }
        }

        Ok(())
    }

    /// Get a plugin by name
    pub fn get_plugin(&self, name: &str) -> Option<&dyn Plugin> {
        self.plugins.get(name).map(|p| p.as_ref())
    }

    /// List all registered plugins
    pub fn list_plugins(&self) -> Vec<&PluginInfo> {
        self.plugins.values().map(|p| p.info()).collect()
    }

    /// Execute a plugin command
    pub async fn execute_plugin(
        &self,
        name: &str,
        context: &PluginContext,
        args: &[String],
    ) -> Result<PluginResult> {
        let plugin = self
            .get_plugin(name)
            .ok_or_else(|| GitMailError::PluginError(format!("Plugin not found: {}", name)))?;

        // Validate plugin requirements
        if plugin.info().requires_email_context && context.selected_emails.is_empty() {
            return Err(GitMailError::PluginError(format!(
                "Plugin {} requires email context but no emails are selected",
                name
            )));
        }

        plugin.execute(context, args).await
    }

    /// Check if a plugin command exists
    pub fn has_plugin(&self, name: &str) -> bool {
        self.plugins.contains_key(name)
    }

    /// Get help information for a plugin
    pub fn get_plugin_help(&self, name: &str) -> Option<String> {
        let plugin = self.get_plugin(name)?;
        let info = plugin.info();

        let mut help = format!("{} - {}\n", info.name, info.description);
        help.push_str(&format!("Version: {}\n", info.version));

        if let Some(author) = &info.author {
            help.push_str(&format!("Author: {}\n", author));
        }

        if let Some(usage) = &info.usage {
            help.push_str(&format!("\nUsage: {}\n", usage));
        }

        if !info.examples.is_empty() {
            help.push_str("\nExamples:\n");
            for example in &info.examples {
                help.push_str(&format!("  {}\n", example));
            }
        }

        Some(help)
    }

    /// Get all plugin help information for the main help system
    pub fn get_all_plugin_help(&self) -> String {
        if self.plugins.is_empty() {
            return "No custom plugins available.\n".to_string();
        }

        let mut help = "Custom Commands:\n".to_string();

        let mut plugins: Vec<_> = self.plugins.values().collect();
        plugins.sort_by(|a, b| a.info().name.cmp(&b.info().name));

        for plugin in plugins {
            let info = plugin.info();
            help.push_str(&format!("  {:20} {}\n", info.name, info.description));
        }

        help.push_str("\nUse 'git-mail help <plugin-name>' for detailed plugin help.\n");
        help
    }
}

impl Default for PluginRegistry {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tempfile::TempDir;
    use tokio::fs::File;
    use tokio::io::AsyncWriteExt;

    #[tokio::test]
    async fn test_script_plugin_creation() {
        let temp_dir = TempDir::new().unwrap();
        let script_path = temp_dir.path().join("test-plugin.sh");

        let script_content = r#"#!/bin/bash
# @name: test-plugin
# @description: A test plugin
# @version: 1.0.0
# @author: Test Author
# @usage: test-plugin [options]
# @example: test-plugin --help
# @requires-email-context
# @can-modify-repo

echo "Hello from test plugin"
echo "Args: $@"
echo "Repo: $GITMAIL_REPO_PATH"
"#;

        let mut file = File::create(&script_path).await.unwrap();
        file.write_all(script_content.as_bytes()).await.unwrap();
        file.flush().await.unwrap();
        drop(file); // Ensure file is closed

        // Make executable on Unix
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perms = std::fs::metadata(&script_path).unwrap().permissions();
            perms.set_mode(0o755);
            std::fs::set_permissions(&script_path, perms).unwrap();
        }

        // Small delay to ensure file system operations complete
        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;

        let plugin = ScriptPlugin::from_file(&script_path).await.unwrap();
        let info = plugin.info();

        assert_eq!(info.name, "test-plugin");
        assert_eq!(info.description, "A test plugin");
        assert_eq!(info.version, "1.0.0");
        assert_eq!(info.author, Some("Test Author".to_string()));
        assert!(info.requires_email_context);
        assert!(info.can_modify_repo);
        assert_eq!(info.usage, Some("test-plugin [options]".to_string()));
        assert_eq!(info.examples, vec!["test-plugin --help"]);
    }

    #[tokio::test]
    async fn test_plugin_registry() {
        let temp_dir = TempDir::new().unwrap();
        let plugin_dir = temp_dir.path().join("plugins");
        tokio::fs::create_dir(&plugin_dir).await.unwrap();

        // Create a test plugin
        let script_path = plugin_dir.join("hello.sh");
        let script_content = r#"#!/bin/bash
# @name: hello
# @description: Says hello
echo "Hello, World!"
"#;

        let mut file = File::create(&script_path).await.unwrap();
        file.write_all(script_content.as_bytes()).await.unwrap();
        file.flush().await.unwrap();
        drop(file); // Ensure file is closed

        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let mut perms = std::fs::metadata(&script_path).unwrap().permissions();
            perms.set_mode(0o755);
            std::fs::set_permissions(&script_path, perms).unwrap();
        }

        // Small delay to ensure file system operations complete
        tokio::time::sleep(tokio::time::Duration::from_millis(10)).await;

        let mut registry = PluginRegistry::new();
        registry.add_plugin_dir(&plugin_dir);
        registry.discover_plugins().await.unwrap();

        assert!(registry.has_plugin("hello"));
        assert_eq!(registry.list_plugins().len(), 1);

        let context = PluginContext {
            repo_path: "/tmp/test".to_string(),
            working_dir: "/tmp".to_string(),
            env_vars: HashMap::new(),
            selected_emails: vec![],
            current_folder: None,
        };

        let result = registry
            .execute_plugin("hello", &context, &[])
            .await
            .unwrap();
        assert!(result.success);
        assert!(result.stdout.contains("Hello, World!"));
    }

    #[tokio::test]
    async fn test_plugin_help_system() {
        let info = PluginInfo {
            name: "test-cmd".to_string(),
            description: "Test command".to_string(),
            version: "1.0.0".to_string(),
            author: Some("Test Author".to_string()),
            executable_path: PathBuf::from("/tmp/test"),
            requires_email_context: false,
            can_modify_repo: false,
            usage: Some("test-cmd [options]".to_string()),
            examples: vec![
                "test-cmd --help".to_string(),
                "test-cmd --version".to_string(),
            ],
        };

        let plugin = ScriptPlugin::new(info);
        let mut registry = PluginRegistry::new();
        registry.register_plugin(Box::new(plugin));

        let help = registry.get_plugin_help("test-cmd").unwrap();
        assert!(help.contains("test-cmd - Test command"));
        assert!(help.contains("Version: 1.0.0"));
        assert!(help.contains("Author: Test Author"));
        assert!(help.contains("Usage: test-cmd [options]"));
        assert!(help.contains("test-cmd --help"));
        assert!(help.contains("test-cmd --version"));
    }
}
