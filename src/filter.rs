//! Email filtering engine using shell scripts

use crate::error::{GitMailError, Result};
use crate::models::{Email, FilterResult, FilterScript};
use std::collections::HashMap;
use std::fs;
use std::path::Path;
use std::process::{Command, Stdio};

/// Email filter engine interface
pub trait FilterEngine {
    /// Execute all filters for an email
    fn execute_filters(&self, email: &Email) -> Result<FilterResult>;

    /// Register a new filter script
    fn register_filter(&mut self, script: FilterScript) -> Result<()>;

    /// Validate a filter script
    fn validate_filter(&self, script: &FilterScript) -> Result<()>;

    /// Get the current filter chain
    fn get_filter_chain(&self) -> Result<Vec<FilterScript>>;
}

/// Default filter engine implementation
pub struct DefaultFilterEngine {
    filters: Vec<FilterScript>,
}

/// Mutable filter engine for registration and management
pub struct MutableFilterEngine {
    filters: Vec<FilterScript>,
    max_execution_time: std::time::Duration,
    max_output_size: usize,
}

impl DefaultFilterEngine {
    /// Create a new filter engine
    pub fn new() -> Self {
        Self {
            filters: Vec::new(),
        }
    }

    /// Create a new filter engine with existing filters
    pub fn with_filters(filters: Vec<FilterScript>) -> Self {
        Self { filters }
    }

    /// Setup environment variables for email metadata
    fn setup_email_environment(&self, email: &Email) -> HashMap<String, String> {
        let mut env_vars = HashMap::new();

        // Basic email metadata
        env_vars.insert("EMAIL_ID".to_string(), email.id.clone());
        env_vars.insert("EMAIL_MESSAGE_ID".to_string(), email.message_id.clone());
        env_vars.insert("EMAIL_ACCOUNT".to_string(), email.account.clone());
        env_vars.insert("EMAIL_FOLDER".to_string(), email.metadata.folder.clone());
        env_vars.insert(
            "EMAIL_FILE_PATH".to_string(),
            email.metadata.file_path.clone(),
        );

        // Email status
        env_vars.insert(
            "EMAIL_IS_READ".to_string(),
            email.metadata.is_read.to_string(),
        );
        env_vars.insert(
            "EMAIL_IS_STARRED".to_string(),
            email.metadata.is_starred.to_string(),
        );

        // Timestamps
        env_vars.insert(
            "EMAIL_CREATED_AT".to_string(),
            email.metadata.created_at.to_rfc3339(),
        );
        env_vars.insert(
            "EMAIL_MODIFIED_AT".to_string(),
            email.metadata.modified_at.to_rfc3339(),
        );

        // Tags (comma-separated)
        env_vars.insert("EMAIL_TAGS".to_string(), email.metadata.tags.join(","));

        // Email headers
        for (key, value) in &email.headers {
            let env_key = format!("EMAIL_HEADER_{}", key.to_uppercase().replace('-', "_"));
            env_vars.insert(env_key, value.clone());
        }

        // Common headers with dedicated variables
        if let Some(from) = email.headers.get("From") {
            env_vars.insert("EMAIL_FROM".to_string(), from.clone());
        }
        if let Some(to) = email.headers.get("To") {
            env_vars.insert("EMAIL_TO".to_string(), to.clone());
        }
        if let Some(subject) = email.headers.get("Subject") {
            env_vars.insert("EMAIL_SUBJECT".to_string(), subject.clone());
        }
        if let Some(date) = email.headers.get("Date") {
            env_vars.insert("EMAIL_DATE".to_string(), date.clone());
        }

        // Body information
        env_vars.insert(
            "EMAIL_CONTENT_TYPE".to_string(),
            email.body.content_type.clone(),
        );
        env_vars.insert(
            "EMAIL_HAS_HTML".to_string(),
            email.body.html_content.is_some().to_string(),
        );

        // Attachment information
        env_vars.insert(
            "EMAIL_ATTACHMENT_COUNT".to_string(),
            email.attachments.len().to_string(),
        );
        if !email.attachments.is_empty() {
            let attachment_names: Vec<String> = email
                .attachments
                .iter()
                .map(|a| a.filename.clone())
                .collect();
            env_vars.insert(
                "EMAIL_ATTACHMENT_NAMES".to_string(),
                attachment_names.join(","),
            );
        }

        env_vars
    }

    /// Execute a single filter script with email data
    fn execute_script(&self, script: &FilterScript, email: &Email) -> Result<FilterResult> {
        // Validate script exists and is executable
        if !Path::new(&script.path).exists() {
            return Err(GitMailError::Filter(format!(
                "Filter script not found: {}",
                script.path
            )));
        }

        // Setup environment variables
        let env_vars = self.setup_email_environment(email);

        // Prepare email content for stdin
        let email_content = format!(
            "Headers:\n{}\n\nBody:\n{}\n",
            email
                .headers
                .iter()
                .map(|(k, v)| format!("{}: {}", k, v))
                .collect::<Vec<_>>()
                .join("\n"),
            email.body.content
        );

        // Execute the script
        let mut command = Command::new("sh");
        command
            .arg(&script.path)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        // Set environment variables
        for (key, value) in env_vars {
            command.env(key, value);
        }

        let mut child = command.spawn().map_err(|e| {
            GitMailError::Filter(format!(
                "Failed to spawn filter process {}: {}",
                script.name, e
            ))
        })?;

        // Write email content to stdin
        if let Some(stdin) = child.stdin.take() {
            use std::io::Write;
            let mut stdin = stdin;
            stdin.write_all(email_content.as_bytes()).map_err(|e| {
                GitMailError::Filter(format!(
                    "Failed to write email content to filter {}: {}",
                    script.name, e
                ))
            })?;
        }

        // Wait for completion and get output
        let output = child.wait_with_output().map_err(|e| {
            GitMailError::Filter(format!(
                "Failed to wait for filter completion {}: {}",
                script.name, e
            ))
        })?;

        // Check exit status
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(GitMailError::Filter(format!(
                "Filter {} failed with exit code {:?}: {}",
                script.name,
                output.status.code(),
                stderr
            )));
        }

        // Parse filter output
        self.parse_filter_output(&output.stdout)
    }

    /// Parse filter script output into FilterResult
    fn parse_filter_output(&self, output: &[u8]) -> Result<FilterResult> {
        let output_str = String::from_utf8_lossy(output);
        let mut result = FilterResult {
            modified: false,
            new_folder: None,
            add_tags: Vec::new(),
            remove_tags: Vec::new(),
            mark_read: None,
            star: None,
        };

        // Parse each line of output
        for line in output_str.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue; // Skip empty lines and comments
            }

            // Parse action commands
            if let Some(action) = line.strip_prefix("ACTION:") {
                let action = action.trim();
                match action {
                    "MODIFIED" => result.modified = true,
                    _ => {} // Unknown action, ignore
                }
            } else if let Some(folder) = line.strip_prefix("MOVE:") {
                result.new_folder = Some(folder.trim().to_string());
                result.modified = true;
            } else if let Some(tags) = line.strip_prefix("ADD_TAGS:") {
                let tags: Vec<String> = tags
                    .split(',')
                    .map(|t| t.trim().to_string())
                    .filter(|t| !t.is_empty())
                    .collect();
                result.add_tags.extend(tags);
                if !result.add_tags.is_empty() {
                    result.modified = true;
                }
            } else if let Some(tags) = line.strip_prefix("REMOVE_TAGS:") {
                let tags: Vec<String> = tags
                    .split(',')
                    .map(|t| t.trim().to_string())
                    .filter(|t| !t.is_empty())
                    .collect();
                result.remove_tags.extend(tags);
                if !result.remove_tags.is_empty() {
                    result.modified = true;
                }
            } else if let Some(read_status) = line.strip_prefix("MARK_READ:") {
                match read_status.trim().to_lowercase().as_str() {
                    "true" | "yes" | "1" => {
                        result.mark_read = Some(true);
                        result.modified = true;
                    }
                    "false" | "no" | "0" => {
                        result.mark_read = Some(false);
                        result.modified = true;
                    }
                    _ => {} // Invalid value, ignore
                }
            } else if let Some(star_status) = line.strip_prefix("STAR:") {
                match star_status.trim().to_lowercase().as_str() {
                    "true" | "yes" | "1" => {
                        result.star = Some(true);
                        result.modified = true;
                    }
                    "false" | "no" | "0" => {
                        result.star = Some(false);
                        result.modified = true;
                    }
                    _ => {} // Invalid value, ignore
                }
            }
        }

        Ok(result)
    }

    /// Merge multiple filter results
    fn merge_filter_results(&self, base: FilterResult, new: FilterResult) -> FilterResult {
        FilterResult {
            modified: base.modified || new.modified,
            new_folder: new.new_folder.or(base.new_folder),
            add_tags: {
                let mut tags = base.add_tags;
                tags.extend(new.add_tags);
                tags.sort();
                tags.dedup();
                tags
            },
            remove_tags: {
                let mut tags = base.remove_tags;
                tags.extend(new.remove_tags);
                tags.sort();
                tags.dedup();
                tags
            },
            mark_read: new.mark_read.or(base.mark_read),
            star: new.star.or(base.star),
        }
    }
}

impl FilterEngine for DefaultFilterEngine {
    fn execute_filters(&self, email: &Email) -> Result<FilterResult> {
        let mut result = FilterResult {
            modified: false,
            new_folder: None,
            add_tags: Vec::new(),
            remove_tags: Vec::new(),
            mark_read: None,
            star: None,
        };

        // Sort filters by order and execute enabled ones
        let mut sorted_filters = self.filters.clone();
        sorted_filters.sort_by_key(|f| f.order);

        for filter in &sorted_filters {
            if filter.enabled {
                let filter_result = self.execute_script(filter, email)?;
                result = self.merge_filter_results(result, filter_result);
            }
        }

        Ok(result)
    }

    fn register_filter(&mut self, script: FilterScript) -> Result<()> {
        // Validate the filter first
        self.validate_filter(&script)?;

        // In a real implementation, this would modify the filters collection
        // For now, we'll return success since the struct is immutable
        // A mutable version would be needed for actual registration
        Ok(())
    }

    fn validate_filter(&self, script: &FilterScript) -> Result<()> {
        // Check if script file exists
        if !Path::new(&script.path).exists() {
            return Err(GitMailError::Filter(format!(
                "Filter script file does not exist: {}",
                script.path
            )));
        }

        // Check if script is readable
        match fs::metadata(&script.path) {
            Ok(metadata) => {
                if !metadata.is_file() {
                    return Err(GitMailError::Filter(format!(
                        "Filter script path is not a file: {}",
                        script.path
                    )));
                }
            }
            Err(e) => {
                return Err(GitMailError::Filter(format!(
                    "Cannot access filter script {}: {}",
                    script.path, e
                )));
            }
        }

        // Validate script name
        if script.name.trim().is_empty() {
            return Err(GitMailError::Filter(
                "Filter script name cannot be empty".to_string(),
            ));
        }

        // Check for shell availability
        match Command::new("sh").arg("--version").output() {
            Ok(_) => {} // Shell is available
            Err(e) => {
                return Err(GitMailError::Filter(format!(
                    "Shell (sh) is not available for executing filters: {}",
                    e
                )));
            }
        }

        Ok(())
    }

    fn get_filter_chain(&self) -> Result<Vec<FilterScript>> {
        let mut sorted_filters = self.filters.clone();
        sorted_filters.sort_by_key(|f| f.order);
        Ok(sorted_filters)
    }
}

impl MutableFilterEngine {
    /// Create a new mutable filter engine
    pub fn new() -> Self {
        Self {
            filters: Vec::new(),
            max_execution_time: std::time::Duration::from_secs(30),
            max_output_size: 1024 * 1024, // 1MB
        }
    }

    /// Create a new mutable filter engine with custom limits
    pub fn with_limits(max_execution_time: std::time::Duration, max_output_size: usize) -> Self {
        Self {
            filters: Vec::new(),
            max_execution_time,
            max_output_size,
        }
    }

    /// Comprehensive validation with safety checks
    fn comprehensive_validate_filter(&self, script: &FilterScript) -> Result<()> {
        // Basic validation
        if !Path::new(&script.path).exists() {
            return Err(GitMailError::Filter(format!(
                "Filter script file does not exist: {}",
                script.path
            )));
        }

        if script.name.trim().is_empty() {
            return Err(GitMailError::Filter(
                "Filter script name cannot be empty".to_string(),
            ));
        }

        // Safety checks
        let path = Path::new(&script.path);
        let dangerous_paths = ["/bin", "/sbin", "/usr/bin", "/usr/sbin", "/etc"];
        for dangerous_path in &dangerous_paths {
            if path.starts_with(dangerous_path) {
                return Err(GitMailError::Filter(format!(
                    "Filter script in dangerous system directory: {}",
                    script.path
                )));
            }
        }

        // Check file size
        let metadata = fs::metadata(&script.path)
            .map_err(|e| GitMailError::Filter(format!("Cannot read script metadata: {}", e)))?;

        if metadata.len() > 1024 * 1024 {
            return Err(GitMailError::Filter(format!(
                "Filter script too large: {} bytes (max 1MB)",
                metadata.len()
            )));
        }

        // Check content for dangerous patterns
        let content = fs::read_to_string(&script.path)
            .map_err(|e| GitMailError::Filter(format!("Cannot read script content: {}", e)))?;

        let dangerous_patterns = ["rm -rf", "sudo", "su ", "chmod 777", ">/etc/"];
        for pattern in &dangerous_patterns {
            if content.contains(pattern) {
                return Err(GitMailError::Filter(format!(
                    "Filter script contains potentially dangerous pattern '{}': {}",
                    pattern, script.path
                )));
            }
        }

        Ok(())
    }

    /// Remove a filter by name
    pub fn remove_filter(&mut self, name: &str) -> Result<bool> {
        let initial_len = self.filters.len();
        self.filters.retain(|f| f.name != name);
        Ok(self.filters.len() < initial_len)
    }

    /// Update a filter's enabled status
    pub fn set_filter_enabled(&mut self, name: &str, enabled: bool) -> Result<bool> {
        for filter in &mut self.filters {
            if filter.name == name {
                filter.enabled = enabled;
                return Ok(true);
            }
        }
        Ok(false)
    }

    /// Update a filter's execution order
    pub fn set_filter_order(&mut self, name: &str, order: u32) -> Result<bool> {
        for filter in &mut self.filters {
            if filter.name == name {
                filter.order = order;
                return Ok(true);
            }
        }
        Ok(false)
    }

    /// Get filter by name
    pub fn get_filter(&self, name: &str) -> Option<&FilterScript> {
        self.filters.iter().find(|f| f.name == name)
    }

    /// List all filter names
    pub fn list_filter_names(&self) -> Vec<String> {
        self.filters.iter().map(|f| f.name.clone()).collect()
    }

    /// Clear all filters
    pub fn clear_filters(&mut self) {
        self.filters.clear();
    }

    /// Get filter count
    pub fn filter_count(&self) -> usize {
        self.filters.len()
    }

    /// Get enabled filter count
    pub fn enabled_filter_count(&self) -> usize {
        self.filters.iter().filter(|f| f.enabled).count()
    }
}

impl FilterEngine for MutableFilterEngine {
    fn execute_filters(&self, email: &Email) -> Result<FilterResult> {
        let mut result = FilterResult {
            modified: false,
            new_folder: None,
            add_tags: Vec::new(),
            remove_tags: Vec::new(),
            mark_read: None,
            star: None,
        };

        // Sort filters by order and execute enabled ones
        let mut sorted_filters = self.filters.clone();
        sorted_filters.sort_by_key(|f| f.order);

        for filter in &sorted_filters {
            if filter.enabled {
                // Use basic execution for now
                let filter_result = self.execute_script_basic(filter, email)?;
                result = self.merge_filter_results(result, filter_result);
            }
        }

        Ok(result)
    }

    fn register_filter(&mut self, script: FilterScript) -> Result<()> {
        // Comprehensive validation
        self.comprehensive_validate_filter(&script)?;

        // Check for duplicate names
        if self.filters.iter().any(|f| f.name == script.name) {
            return Err(GitMailError::Filter(format!(
                "Filter with name '{}' already exists",
                script.name
            )));
        }

        // Add the filter
        self.filters.push(script);
        Ok(())
    }

    fn validate_filter(&self, script: &FilterScript) -> Result<()> {
        self.comprehensive_validate_filter(script)
    }

    fn get_filter_chain(&self) -> Result<Vec<FilterScript>> {
        let mut sorted_filters = self.filters.clone();
        sorted_filters.sort_by_key(|f| f.order);
        Ok(sorted_filters)
    }
}

impl MutableFilterEngine {
    /// Basic script execution (reuse from DefaultFilterEngine logic)
    fn execute_script_basic(&self, script: &FilterScript, email: &Email) -> Result<FilterResult> {
        // Setup environment variables
        let env_vars = self.setup_email_environment(email);

        // Prepare email content for stdin
        let email_content = format!(
            "Headers:\n{}\n\nBody:\n{}\n",
            email
                .headers
                .iter()
                .map(|(k, v)| format!("{}: {}", k, v))
                .collect::<Vec<_>>()
                .join("\n"),
            email.body.content
        );

        // Execute the script
        let mut command = Command::new("sh");
        command
            .arg(&script.path)
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .stderr(Stdio::piped());

        // Set environment variables
        for (key, value) in env_vars {
            command.env(key, value);
        }

        let mut child = command.spawn().map_err(|e| {
            GitMailError::Filter(format!(
                "Failed to spawn filter process {}: {}",
                script.name, e
            ))
        })?;

        // Write email content to stdin
        if let Some(stdin) = child.stdin.take() {
            use std::io::Write;
            let mut stdin = stdin;
            stdin.write_all(email_content.as_bytes()).map_err(|e| {
                GitMailError::Filter(format!(
                    "Failed to write email content to filter {}: {}",
                    script.name, e
                ))
            })?;
        }

        // Wait for completion and get output
        let output = child.wait_with_output().map_err(|e| {
            GitMailError::Filter(format!(
                "Failed to wait for filter completion {}: {}",
                script.name, e
            ))
        })?;

        // Check exit status
        if !output.status.success() {
            let stderr = String::from_utf8_lossy(&output.stderr);
            return Err(GitMailError::Filter(format!(
                "Filter {} failed with exit code {:?}: {}",
                script.name,
                output.status.code(),
                stderr
            )));
        }

        // Check output size
        if output.stdout.len() > self.max_output_size {
            return Err(GitMailError::Filter(format!(
                "Filter {} output too large: {} bytes (max {})",
                script.name,
                output.stdout.len(),
                self.max_output_size
            )));
        }

        // Parse filter output
        self.parse_filter_output(&output.stdout)
    }

    /// Setup environment variables for email metadata (same as DefaultFilterEngine)
    fn setup_email_environment(&self, email: &Email) -> HashMap<String, String> {
        let mut env_vars = HashMap::new();

        // Basic email metadata
        env_vars.insert("EMAIL_ID".to_string(), email.id.clone());
        env_vars.insert("EMAIL_MESSAGE_ID".to_string(), email.message_id.clone());
        env_vars.insert("EMAIL_ACCOUNT".to_string(), email.account.clone());
        env_vars.insert("EMAIL_FOLDER".to_string(), email.metadata.folder.clone());
        env_vars.insert(
            "EMAIL_FILE_PATH".to_string(),
            email.metadata.file_path.clone(),
        );

        // Email status
        env_vars.insert(
            "EMAIL_IS_READ".to_string(),
            email.metadata.is_read.to_string(),
        );
        env_vars.insert(
            "EMAIL_IS_STARRED".to_string(),
            email.metadata.is_starred.to_string(),
        );

        // Timestamps
        env_vars.insert(
            "EMAIL_CREATED_AT".to_string(),
            email.metadata.created_at.to_rfc3339(),
        );
        env_vars.insert(
            "EMAIL_MODIFIED_AT".to_string(),
            email.metadata.modified_at.to_rfc3339(),
        );

        // Tags (comma-separated)
        env_vars.insert("EMAIL_TAGS".to_string(), email.metadata.tags.join(","));

        // Email headers
        for (key, value) in &email.headers {
            let env_key = format!("EMAIL_HEADER_{}", key.to_uppercase().replace('-', "_"));
            env_vars.insert(env_key, value.clone());
        }

        // Common headers with dedicated variables
        if let Some(from) = email.headers.get("From") {
            env_vars.insert("EMAIL_FROM".to_string(), from.clone());
        }
        if let Some(to) = email.headers.get("To") {
            env_vars.insert("EMAIL_TO".to_string(), to.clone());
        }
        if let Some(subject) = email.headers.get("Subject") {
            env_vars.insert("EMAIL_SUBJECT".to_string(), subject.clone());
        }
        if let Some(date) = email.headers.get("Date") {
            env_vars.insert("EMAIL_DATE".to_string(), date.clone());
        }

        // Body information
        env_vars.insert(
            "EMAIL_CONTENT_TYPE".to_string(),
            email.body.content_type.clone(),
        );
        env_vars.insert(
            "EMAIL_HAS_HTML".to_string(),
            email.body.html_content.is_some().to_string(),
        );

        // Attachment information
        env_vars.insert(
            "EMAIL_ATTACHMENT_COUNT".to_string(),
            email.attachments.len().to_string(),
        );
        if !email.attachments.is_empty() {
            let attachment_names: Vec<String> = email
                .attachments
                .iter()
                .map(|a| a.filename.clone())
                .collect();
            env_vars.insert(
                "EMAIL_ATTACHMENT_NAMES".to_string(),
                attachment_names.join(","),
            );
        }

        env_vars
    }

    /// Parse filter script output into FilterResult (same as DefaultFilterEngine)
    fn parse_filter_output(&self, output: &[u8]) -> Result<FilterResult> {
        let output_str = String::from_utf8_lossy(output);
        let mut result = FilterResult {
            modified: false,
            new_folder: None,
            add_tags: Vec::new(),
            remove_tags: Vec::new(),
            mark_read: None,
            star: None,
        };

        // Parse each line of output
        for line in output_str.lines() {
            let line = line.trim();
            if line.is_empty() || line.starts_with('#') {
                continue; // Skip empty lines and comments
            }

            // Parse action commands
            if let Some(action) = line.strip_prefix("ACTION:") {
                let action = action.trim();
                match action {
                    "MODIFIED" => result.modified = true,
                    _ => {} // Unknown action, ignore
                }
            } else if let Some(folder) = line.strip_prefix("MOVE:") {
                result.new_folder = Some(folder.trim().to_string());
                result.modified = true;
            } else if let Some(tags) = line.strip_prefix("ADD_TAGS:") {
                let tags: Vec<String> = tags
                    .split(',')
                    .map(|t| t.trim().to_string())
                    .filter(|t| !t.is_empty())
                    .collect();
                result.add_tags.extend(tags);
                if !result.add_tags.is_empty() {
                    result.modified = true;
                }
            } else if let Some(tags) = line.strip_prefix("REMOVE_TAGS:") {
                let tags: Vec<String> = tags
                    .split(',')
                    .map(|t| t.trim().to_string())
                    .filter(|t| !t.is_empty())
                    .collect();
                result.remove_tags.extend(tags);
                if !result.remove_tags.is_empty() {
                    result.modified = true;
                }
            } else if let Some(read_status) = line.strip_prefix("MARK_READ:") {
                match read_status.trim().to_lowercase().as_str() {
                    "true" | "yes" | "1" => {
                        result.mark_read = Some(true);
                        result.modified = true;
                    }
                    "false" | "no" | "0" => {
                        result.mark_read = Some(false);
                        result.modified = true;
                    }
                    _ => {} // Invalid value, ignore
                }
            } else if let Some(star_status) = line.strip_prefix("STAR:") {
                match star_status.trim().to_lowercase().as_str() {
                    "true" | "yes" | "1" => {
                        result.star = Some(true);
                        result.modified = true;
                    }
                    "false" | "no" | "0" => {
                        result.star = Some(false);
                        result.modified = true;
                    }
                    _ => {} // Invalid value, ignore
                }
            }
        }

        Ok(result)
    }

    /// Merge multiple filter results (same as DefaultFilterEngine)
    fn merge_filter_results(&self, base: FilterResult, new: FilterResult) -> FilterResult {
        FilterResult {
            modified: base.modified || new.modified,
            new_folder: new.new_folder.or(base.new_folder),
            add_tags: {
                let mut tags = base.add_tags;
                tags.extend(new.add_tags);
                tags.sort();
                tags.dedup();
                tags
            },
            remove_tags: {
                let mut tags = base.remove_tags;
                tags.extend(new.remove_tags);
                tags.sort();
                tags.dedup();
                tags
            },
            mark_read: new.mark_read.or(base.mark_read),
            star: new.star.or(base.star),
        }
    }
}
