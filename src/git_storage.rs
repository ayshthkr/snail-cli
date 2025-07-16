//! Git storage layer for email management

use crate::error::{GitMailError, Result};
use crate::models::{Email, EmailMetadata, GitCommit};
use chrono::{DateTime, Utc};
use git2::{Repository, Signature};
use std::fs;
use std::path::{Path, PathBuf};

/// Conflict resolution strategies for email merges
#[derive(Debug, Clone, Copy)]
pub enum ConflictResolutionStrategy {
    /// Keep the local version (HEAD)
    KeepLocal,
    /// Keep the remote version (incoming)
    KeepRemote,
    /// Intelligently merge metadata (combine tags, use newer timestamps)
    MergeMetadata,
    /// Require manual resolution
    Manual,
}

/// Git storage interface for email operations
pub trait GitStorage {
    /// Initialize a new Git repository for email storage
    fn initialize_repository(&self, path: &str) -> Result<()>;

    /// Store an email as a file in the repository
    fn store_email(&self, email: &Email) -> Result<String>;

    /// Retrieve an email by ID
    fn retrieve_email(&self, id: &str) -> Result<Email>;

    /// List emails with optional folder filter
    fn list_emails(&self, folder: Option<&str>) -> Result<Vec<EmailMetadata>>;

    /// Commit changes to the repository
    fn commit_changes(&self, message: &str) -> Result<()>;

    /// Get Git history for an email
    fn get_history(&self, email_id: &str) -> Result<Vec<GitCommit>>;
}

/// Default Git storage implementation
#[derive(Clone)]
pub struct DefaultGitStorage {
    repository_path: String,
}

impl DefaultGitStorage {
    /// Create a new Git storage instance
    pub fn new(repository_path: String) -> Self {
        Self { repository_path }
    }

    /// Open the Git repository
    fn open_repository(&self) -> Result<Repository> {
        Repository::open(&self.repository_path)
            .map_err(|e| GitMailError::Repository(format!("Failed to open repository: {}", e)))
    }

    /// Generate file path for an email based on date and metadata
    fn generate_email_file_path(&self, email: &Email) -> PathBuf {
        let date = email.metadata.created_at;
        let year = date.format("%Y").to_string();
        let month = date.format("%m").to_string();
        let day = date.format("%d").to_string();

        // Extract sender from headers for filename
        let sender = email
            .headers
            .get("From")
            .unwrap_or(&email.account)
            .replace(['@', '.', ' ', '<', '>'], "_")
            .chars()
            .take(20)
            .collect::<String>();

        // Extract subject for filename
        let subject = email
            .headers
            .get("Subject")
            .map(|s| s.as_str())
            .unwrap_or("no_subject")
            .replace([' ', '/', '\\', ':', '*', '?', '"', '<', '>', '|'], "_")
            .chars()
            .take(30)
            .collect::<String>();

        let id_prefix = if email.id.len() >= 8 {
            &email.id[..8]
        } else {
            &email.id
        };
        let filename = format!("msg-{}-{}-{}.txt", id_prefix, sender, subject);

        Path::new(&self.repository_path)
            .join(&email.metadata.folder)
            .join(year)
            .join(month)
            .join(day)
            .join(filename)
    }

    /// Create directory structure for email storage
    fn create_directory_structure(&self) -> Result<()> {
        let base_path = Path::new(&self.repository_path);

        // Create standard email folders
        let folders = ["inbox", "sent", "drafts", "accounts", "filters"];
        for folder in &folders {
            let folder_path = base_path.join(folder);
            if !folder_path.exists() {
                fs::create_dir_all(&folder_path).map_err(|e| GitMailError::Io(e))?;
            }
        }

        Ok(())
    }

    /// Serialize email to plain text format
    fn serialize_email_to_text(&self, email: &Email) -> String {
        let mut content = String::new();

        // Add headers
        content.push_str("--- Email Headers ---\n");
        for (key, value) in &email.headers {
            content.push_str(&format!("{}: {}\n", key, value));
        }

        // Add metadata
        content.push_str("\n--- Metadata ---\n");
        content.push_str(&format!("ID: {}\n", email.id));
        content.push_str(&format!("Message-ID: {}\n", email.message_id));
        content.push_str(&format!("Account: {}\n", email.account));
        content.push_str(&format!("Folder: {}\n", email.metadata.folder));
        content.push_str(&format!("Tags: {}\n", email.metadata.tags.join(", ")));
        content.push_str(&format!("Read: {}\n", email.metadata.is_read));
        content.push_str(&format!("Starred: {}\n", email.metadata.is_starred));
        content.push_str(&format!(
            "Created: {}\n",
            email.metadata.created_at.to_rfc3339()
        ));
        content.push_str(&format!(
            "Modified: {}\n",
            email.metadata.modified_at.to_rfc3339()
        ));

        // Add body
        content.push_str("\n--- Body ---\n");
        content.push_str(&format!("Content-Type: {}\n", email.body.content_type));
        content.push_str("\n");
        content.push_str(&email.body.content);

        if let Some(html_content) = &email.body.html_content {
            content.push_str("\n\n--- HTML Body ---\n");
            content.push_str(html_content);
        }

        // Add attachments info
        if !email.attachments.is_empty() {
            content.push_str("\n\n--- Attachments ---\n");
            for attachment in &email.attachments {
                content.push_str(&format!("Filename: {}\n", attachment.filename));
                content.push_str(&format!("Content-Type: {}\n", attachment.content_type));
                content.push_str(&format!("Size: {} bytes\n", attachment.size));
                content.push_str(&format!("Path: {}\n", attachment.file_path));
                content.push_str("\n");
            }
        }

        content
    }

    /// Parse email from plain text format
    fn parse_email_from_text(&self, content: &str, file_path: &str) -> Result<Email> {
        let mut email = Email::new("unknown".to_string());
        let mut current_section = "";
        let mut body_content = String::new();
        let mut html_content: Option<String> = None;
        let mut in_body = false;
        let mut in_html_body = false;

        for line in content.lines() {
            if line.starts_with("--- ") && line.ends_with(" ---") {
                current_section = line;
                in_body = false;
                in_html_body = false;
                continue;
            }

            match current_section {
                "--- Email Headers ---" => {
                    if let Some((key, value)) = line.split_once(": ") {
                        email.headers.insert(key.to_string(), value.to_string());
                    }
                }
                "--- Metadata ---" => {
                    if let Some((key, value)) = line.split_once(": ") {
                        match key {
                            "ID" => email.id = value.to_string(),
                            "Message-ID" => email.message_id = value.to_string(),
                            "Account" => email.account = value.to_string(),
                            "Folder" => email.metadata.folder = value.to_string(),
                            "Tags" => {
                                email.metadata.tags = value
                                    .split(", ")
                                    .filter(|s| !s.is_empty())
                                    .map(|s| s.to_string())
                                    .collect();
                            }
                            "Read" => email.metadata.is_read = value.parse().unwrap_or(false),
                            "Starred" => email.metadata.is_starred = value.parse().unwrap_or(false),
                            "Created" => {
                                if let Ok(dt) = DateTime::parse_from_rfc3339(value) {
                                    email.metadata.created_at = dt.with_timezone(&Utc);
                                }
                            }
                            "Modified" => {
                                if let Ok(dt) = DateTime::parse_from_rfc3339(value) {
                                    email.metadata.modified_at = dt.with_timezone(&Utc);
                                }
                            }
                            _ => {}
                        }
                    }
                }
                "--- Body ---" => {
                    if line.starts_with("Content-Type: ") {
                        email.body.content_type = line
                            .strip_prefix("Content-Type: ")
                            .unwrap_or("text/plain")
                            .to_string();
                    } else if line.is_empty() && !in_body {
                        in_body = true;
                    } else if in_body {
                        if !body_content.is_empty() {
                            body_content.push('\n');
                        }
                        body_content.push_str(line);
                    }
                }
                "--- HTML Body ---" => {
                    if !in_html_body {
                        in_html_body = true;
                        html_content = Some(String::new());
                    }
                    if let Some(ref mut html) = html_content {
                        if !html.is_empty() {
                            html.push('\n');
                        }
                        html.push_str(line);
                    }
                }
                _ => {}
            }
        }

        email.body.content = body_content.trim_end().to_string();
        email.body.html_content = html_content.map(|s| s.trim_end().to_string());
        email.metadata.file_path = file_path.to_string();

        Ok(email)
    }

    /// Recursively collect email files from a folder
    fn collect_email_files(
        &self,
        folder_path: &Path,
        folder_name: &str,
        email_metadata: &mut Vec<EmailMetadata>,
    ) -> Result<()> {
        // Read directory entries
        let entries = fs::read_dir(folder_path).map_err(|e| GitMailError::Io(e))?;

        for entry in entries {
            let entry = entry.map_err(|e| GitMailError::Io(e))?;
            let path = entry.path();

            if path.is_dir() {
                // Recursively search subdirectories
                self.collect_email_files(&path, folder_name, email_metadata)?;
            } else if path.is_file() {
                // Check if it's an email file (ends with .txt and starts with msg-)
                if let Some(filename) = path.file_name().and_then(|n| n.to_str()) {
                    if filename.ends_with(".txt") && filename.starts_with("msg-") {
                        // Extract metadata from file
                        if let Ok(metadata) = self.extract_email_metadata(&path, folder_name) {
                            email_metadata.push(metadata);
                        }
                    }
                }
            }
        }

        Ok(())
    }

    /// Extract email metadata from file path and basic file info
    fn extract_email_metadata(
        &self,
        file_path: &Path,
        _folder_name: &str,
    ) -> Result<EmailMetadata> {
        let _file_metadata = fs::metadata(file_path).map_err(|e| GitMailError::Io(e))?;

        // Try to read the email file to get more detailed metadata
        let content = fs::read_to_string(file_path).map_err(|e| GitMailError::Io(e))?;
        let email = self.parse_email_from_text(&content, &file_path.to_string_lossy())?;

        Ok(email.metadata)
    }

    /// Check if a commit touches a specific file
    fn commit_touches_file(
        &self,
        repo: &Repository,
        commit: &git2::Commit,
        file_path: &Path,
    ) -> Result<bool> {
        // Get the tree for this commit
        let tree = commit.tree().map_err(|e| GitMailError::Git(e))?;

        // Check if the file exists in this commit's tree
        let file_path_str = file_path
            .to_str()
            .ok_or_else(|| GitMailError::Repository("Invalid file path encoding".to_string()))?;

        // Try to find the file in the tree
        match tree.get_path(Path::new(file_path_str)) {
            Ok(_) => {
                // File exists in this commit, now check if it was modified
                if commit.parent_count() == 0 {
                    // This is the initial commit, so the file was added
                    return Ok(true);
                }

                // Compare with parent commit(s)
                for i in 0..commit.parent_count() {
                    let parent = commit.parent(i).map_err(|e| GitMailError::Git(e))?;
                    let parent_tree = parent.tree().map_err(|e| GitMailError::Git(e))?;

                    // Create diff between parent and current commit
                    let diff = repo
                        .diff_tree_to_tree(Some(&parent_tree), Some(&tree), None)
                        .map_err(|e| GitMailError::Git(e))?;

                    let mut file_modified = false;
                    diff.foreach(
                        &mut |delta, _progress| {
                            if let Some(new_file) = delta.new_file().path() {
                                if new_file == Path::new(file_path_str) {
                                    file_modified = true;
                                }
                            }
                            if let Some(old_file) = delta.old_file().path() {
                                if old_file == Path::new(file_path_str) {
                                    file_modified = true;
                                }
                            }
                            true
                        },
                        None,
                        None,
                        None,
                    )
                    .map_err(|e| GitMailError::Git(e))?;

                    if file_modified {
                        return Ok(true);
                    }
                }
            }
            Err(_) => {
                // File doesn't exist in this commit, check if it was deleted
                if commit.parent_count() > 0 {
                    for i in 0..commit.parent_count() {
                        let parent = commit.parent(i).map_err(|e| GitMailError::Git(e))?;
                        let parent_tree = parent.tree().map_err(|e| GitMailError::Git(e))?;

                        // Check if file existed in parent
                        if parent_tree.get_path(Path::new(file_path_str)).is_ok() {
                            // File was deleted in this commit
                            return Ok(true);
                        }
                    }
                }
            }
        }

        Ok(false)
    }

    /// Generate commit message for email operations
    pub fn generate_commit_message(&self, operation: &str, email: &Email) -> String {
        let subject = email
            .headers
            .get("Subject")
            .map(|s| {
                if s.len() > 50 {
                    format!("{}...", &s[..47])
                } else {
                    s.clone()
                }
            })
            .unwrap_or_else(|| "(no subject)".to_string());

        let from = email
            .headers
            .get("From")
            .map(|f| {
                // Extract just the email address or name
                if let Some(start) = f.find('<') {
                    if let Some(end) = f.find('>') {
                        f[start + 1..end].to_string()
                    } else {
                        f.clone()
                    }
                } else {
                    f.clone()
                }
            })
            .unwrap_or_else(|| email.account.clone());

        match operation {
            "store" => format!("Add email: {} from {}", subject, from),
            "update" => format!("Update email: {} from {}", subject, from),
            "delete" => format!("Delete email: {} from {}", subject, from),
            "move" => format!("Move email: {} from {}", subject, from),
            "tag" => format!("Tag email: {} from {}", subject, from),
            _ => format!("{} email: {} from {}", operation, subject, from),
        }
    }

    /// Resolve merge conflicts for email files
    pub fn resolve_email_conflict(
        &self,
        repo: &Repository,
        file_path: &Path,
        strategy: ConflictResolutionStrategy,
    ) -> Result<()> {
        let file_path_str = file_path
            .to_str()
            .ok_or_else(|| GitMailError::Repository("Invalid file path encoding".to_string()))?;

        // Read the conflicted file
        let conflicted_content = fs::read_to_string(file_path).map_err(|e| GitMailError::Io(e))?;

        // Check if file has conflict markers
        if !conflicted_content.contains("<<<<<<< HEAD") {
            return Ok(()); // No conflicts to resolve
        }

        let resolved_content = match strategy {
            ConflictResolutionStrategy::KeepLocal => {
                self.resolve_conflict_keep_local(&conflicted_content)?
            }
            ConflictResolutionStrategy::KeepRemote => {
                self.resolve_conflict_keep_remote(&conflicted_content)?
            }
            ConflictResolutionStrategy::MergeMetadata => {
                self.resolve_conflict_merge_metadata(&conflicted_content)?
            }
            ConflictResolutionStrategy::Manual => {
                return Err(GitMailError::Repository(
                    "Manual conflict resolution required".to_string(),
                ));
            }
        };

        // Write resolved content back to file
        fs::write(file_path, resolved_content).map_err(|e| GitMailError::Io(e))?;

        // Add resolved file to index
        let mut index = repo.index().map_err(|e| GitMailError::Git(e))?;
        index
            .add_path(Path::new(file_path_str))
            .map_err(|e| GitMailError::Git(e))?;
        index.write().map_err(|e| GitMailError::Git(e))?;

        Ok(())
    }

    /// Resolve conflict by keeping local version
    fn resolve_conflict_keep_local(&self, content: &str) -> Result<String> {
        let mut resolved = String::new();
        let mut in_conflict = false;
        let mut skip_until_end = false;

        for line in content.lines() {
            if line.starts_with("<<<<<<< HEAD") {
                in_conflict = true;
                skip_until_end = false;
                continue;
            } else if line.starts_with("=======") && in_conflict {
                skip_until_end = true;
                continue;
            } else if line.starts_with(">>>>>>> ") && in_conflict {
                in_conflict = false;
                skip_until_end = false;
                continue;
            }

            if !skip_until_end {
                resolved.push_str(line);
                resolved.push('\n');
            }
        }

        Ok(resolved.trim_end().to_string())
    }

    /// Resolve conflict by keeping remote version
    fn resolve_conflict_keep_remote(&self, content: &str) -> Result<String> {
        let mut resolved = String::new();
        let mut in_conflict = false;
        let mut keep_section = false;

        for line in content.lines() {
            if line.starts_with("<<<<<<< HEAD") {
                in_conflict = true;
                keep_section = false;
                continue;
            } else if line.starts_with("=======") && in_conflict {
                keep_section = true;
                continue;
            } else if line.starts_with(">>>>>>> ") && in_conflict {
                in_conflict = false;
                keep_section = false;
                continue;
            }

            if !in_conflict || keep_section {
                resolved.push_str(line);
                resolved.push('\n');
            }
        }

        Ok(resolved.trim_end().to_string())
    }

    /// Resolve conflict by merging metadata intelligently
    fn resolve_conflict_merge_metadata(&self, content: &str) -> Result<String> {
        // Parse both versions of the email
        let (local_content, remote_content) = self.extract_conflict_versions(content)?;

        let local_email = self.parse_email_from_text(&local_content, "local")?;
        let remote_email = self.parse_email_from_text(&remote_content, "remote")?;

        // Create merged email with intelligent conflict resolution
        let mut merged_email = local_email.clone();

        // Merge metadata - prefer newer timestamps, combine tags
        if remote_email.metadata.modified_at > local_email.metadata.modified_at {
            merged_email.metadata.modified_at = remote_email.metadata.modified_at;
        }

        // Combine tags from both versions
        let mut all_tags = local_email.metadata.tags.clone();
        for tag in &remote_email.metadata.tags {
            if !all_tags.contains(tag) {
                all_tags.push(tag.clone());
            }
        }
        merged_email.metadata.tags = all_tags;

        // Use the most recent read/starred status
        if remote_email.metadata.modified_at > local_email.metadata.modified_at {
            merged_email.metadata.is_read = remote_email.metadata.is_read;
            merged_email.metadata.is_starred = remote_email.metadata.is_starred;
        }

        // Serialize merged email
        Ok(self.serialize_email_to_text(&merged_email))
    }

    /// Extract local and remote versions from conflict content
    fn extract_conflict_versions(&self, content: &str) -> Result<(String, String)> {
        let mut local_content = String::new();
        let mut remote_content = String::new();
        let mut in_conflict = false;
        let mut in_remote_section = false;

        for line in content.lines() {
            if line.starts_with("<<<<<<< HEAD") {
                in_conflict = true;
                in_remote_section = false;
                continue;
            } else if line.starts_with("=======") && in_conflict {
                in_remote_section = true;
                continue;
            } else if line.starts_with(">>>>>>> ") && in_conflict {
                in_conflict = false;
                in_remote_section = false;
                continue;
            }

            if in_conflict {
                if in_remote_section {
                    remote_content.push_str(line);
                    remote_content.push('\n');
                } else {
                    local_content.push_str(line);
                    local_content.push('\n');
                }
            } else {
                // Non-conflicted content goes to both versions
                local_content.push_str(line);
                local_content.push('\n');
                remote_content.push_str(line);
                remote_content.push('\n');
            }
        }

        Ok((
            local_content.trim_end().to_string(),
            remote_content.trim_end().to_string(),
        ))
    }
}

impl GitStorage for DefaultGitStorage {
    fn initialize_repository(&self, path: &str) -> Result<()> {
        // Initialize Git repository
        let repo = Repository::init(path).map_err(|e| {
            GitMailError::Repository(format!("Failed to initialize repository: {}", e))
        })?;

        // Update repository path
        let mut storage = DefaultGitStorage::new(path.to_string());
        storage.repository_path = path.to_string();

        // Create directory structure
        storage.create_directory_structure()?;

        // Create initial commit
        let signature =
            Signature::now("Git-Mail", "git-mail@localhost").map_err(|e| GitMailError::Git(e))?;

        let tree_id = {
            let mut index = repo.index().map_err(|e| GitMailError::Git(e))?;

            // Add .gitkeep files to maintain directory structure
            let folders = ["inbox", "sent", "drafts", "accounts", "filters"];
            for folder in &folders {
                let gitkeep_path = Path::new(path).join(folder).join(".gitkeep");
                fs::write(&gitkeep_path, "").map_err(|e| GitMailError::Io(e))?;

                let relative_path = format!("{}/.gitkeep", folder);
                index
                    .add_path(Path::new(&relative_path))
                    .map_err(|e| GitMailError::Git(e))?;
            }

            index.write().map_err(|e| GitMailError::Git(e))?;

            index.write_tree().map_err(|e| GitMailError::Git(e))?
        };

        let tree = repo.find_tree(tree_id).map_err(|e| GitMailError::Git(e))?;

        repo.commit(
            Some("HEAD"),
            &signature,
            &signature,
            "Initial Git-Mail repository setup",
            &tree,
            &[],
        )
        .map_err(|e| GitMailError::Git(e))?;

        Ok(())
    }

    fn store_email(&self, email: &Email) -> Result<String> {
        // Validate email before storing
        email.validate().map_err(|e| GitMailError::Validation(e))?;

        // Generate file path
        let file_path = self.generate_email_file_path(email);

        // Create directory structure if it doesn't exist
        if let Some(parent) = file_path.parent() {
            fs::create_dir_all(parent).map_err(|e| GitMailError::Io(e))?;
        }

        // Serialize email to text format
        let content = self.serialize_email_to_text(email);

        // Write email to file
        fs::write(&file_path, content).map_err(|e| GitMailError::Io(e))?;

        // Add file to Git index and commit
        let repo = self.open_repository()?;
        let mut index = repo.index().map_err(|e| GitMailError::Git(e))?;

        // Get relative path from repository root
        let repo_path = Path::new(&self.repository_path);
        let relative_path = file_path
            .strip_prefix(repo_path)
            .map_err(|e| GitMailError::Repository(format!("Invalid file path: {}", e)))?;

        index
            .add_path(relative_path)
            .map_err(|e| GitMailError::Git(e))?;
        index.write().map_err(|e| GitMailError::Git(e))?;

        // Commit the change using generated commit message
        let commit_message = self.generate_commit_message("store", email);
        self.commit_changes(&commit_message)?;

        Ok(file_path.to_string_lossy().to_string())
    }

    fn retrieve_email(&self, id: &str) -> Result<Email> {
        // Search for email file by ID
        let id_prefix = if id.len() >= 8 { &id[..8] } else { id };
        let email_metadata = self
            .list_emails(None)?
            .into_iter()
            .find(|meta| meta.file_path.contains(id_prefix))
            .ok_or_else(|| GitMailError::EmailNotFound(id.to_string()))?;

        // Read email file
        let content =
            fs::read_to_string(&email_metadata.file_path).map_err(|e| GitMailError::Io(e))?;

        // Parse email from text
        self.parse_email_from_text(&content, &email_metadata.file_path)
    }

    fn list_emails(&self, folder: Option<&str>) -> Result<Vec<EmailMetadata>> {
        let mut email_metadata = Vec::new();
        let base_path = Path::new(&self.repository_path);

        // Determine which folders to search
        let folders_to_search = if let Some(folder) = folder {
            vec![folder]
        } else {
            vec!["inbox", "sent", "drafts"]
        };

        for folder_name in folders_to_search {
            let folder_path = base_path.join(folder_name);
            if !folder_path.exists() {
                continue;
            }

            // Recursively walk through the folder structure
            self.collect_email_files(&folder_path, folder_name, &mut email_metadata)?;
        }

        // Sort by creation date (newest first)
        email_metadata.sort_by(|a, b| b.created_at.cmp(&a.created_at));

        Ok(email_metadata)
    }

    fn commit_changes(&self, message: &str) -> Result<()> {
        let repo = self.open_repository()?;
        let mut index = repo.index().map_err(|e| GitMailError::Git(e))?;
        let tree_id = index.write_tree().map_err(|e| GitMailError::Git(e))?;
        let tree = repo.find_tree(tree_id).map_err(|e| GitMailError::Git(e))?;

        let signature =
            Signature::now("Git-Mail", "git-mail@localhost").map_err(|e| GitMailError::Git(e))?;

        // Get parent commit if it exists
        let parent_commit = match repo.head() {
            Ok(head) => Some(head.peel_to_commit().map_err(|e| GitMailError::Git(e))?),
            Err(_) => None,
        };

        let parents: Vec<&git2::Commit> = parent_commit.iter().collect();

        repo.commit(
            Some("HEAD"),
            &signature,
            &signature,
            message,
            &tree,
            &parents,
        )
        .map_err(|e| GitMailError::Git(e))?;

        Ok(())
    }

    fn get_history(&self, email_id: &str) -> Result<Vec<GitCommit>> {
        let repo = self.open_repository()?;

        // Find the email file path first
        let id_prefix = if email_id.len() >= 8 {
            &email_id[..8]
        } else {
            email_id
        };
        let email_metadata = self
            .list_emails(None)?
            .into_iter()
            .find(|meta| meta.file_path.contains(id_prefix))
            .ok_or_else(|| GitMailError::EmailNotFound(email_id.to_string()))?;

        // Get relative path from repository root
        let repo_path = Path::new(&self.repository_path);
        let relative_path = Path::new(&email_metadata.file_path)
            .strip_prefix(repo_path)
            .map_err(|e| GitMailError::Repository(format!("Invalid file path: {}", e)))?;

        // Create revwalk to iterate through commits
        let mut revwalk = repo.revwalk().map_err(|e| GitMailError::Git(e))?;
        revwalk.push_head().map_err(|e| GitMailError::Git(e))?;

        let mut commits = Vec::new();

        // Iterate through commits and check if they modified the email file
        for commit_id in revwalk {
            let commit_id = commit_id.map_err(|e| GitMailError::Git(e))?;
            let commit = repo
                .find_commit(commit_id)
                .map_err(|e| GitMailError::Git(e))?;

            // Check if this commit modified the email file
            if self.commit_touches_file(&repo, &commit, relative_path)? {
                let git_commit = GitCommit {
                    hash: commit.id().to_string(),
                    message: commit.message().unwrap_or("").to_string(),
                    author: commit.author().name().unwrap_or("Unknown").to_string(),
                    timestamp: DateTime::from_timestamp(commit.time().seconds(), 0)
                        .unwrap_or_else(|| Utc::now()),
                };
                commits.push(git_commit);
            }
        }

        // Sort by timestamp (newest first)
        commits.sort_by(|a, b| b.timestamp.cmp(&a.timestamp));

        Ok(commits)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::Email;
    use tempfile::TempDir;

    fn create_test_email() -> Email {
        let mut email = Email::new("test@example.com".to_string());
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "test@example.com".to_string());
        email
            .headers
            .insert("Subject".to_string(), "Test Email".to_string());
        email.body.content = "This is a test email body.".to_string();
        email.body.content_type = "text/plain".to_string();
        email
    }

    #[test]
    fn test_new_git_storage() {
        let storage = DefaultGitStorage::new("/tmp/test-repo".to_string());
        assert_eq!(storage.repository_path, "/tmp/test-repo");
    }

    #[test]
    fn test_initialize_repository() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let result = storage.initialize_repository(repo_path);

        assert!(result.is_ok(), "Repository initialization should succeed");

        // Verify repository was created
        let repo = Repository::open(repo_path);
        assert!(repo.is_ok(), "Repository should be openable");

        // Verify directory structure was created
        let folders = ["inbox", "sent", "drafts", "accounts", "filters"];
        for folder in &folders {
            let folder_path = temp_dir.path().join(folder);
            assert!(folder_path.exists(), "Folder {} should exist", folder);

            let gitkeep_path = folder_path.join(".gitkeep");
            assert!(
                gitkeep_path.exists(),
                ".gitkeep file should exist in {}",
                folder
            );
        }

        // Verify initial commit was made
        let repo = repo.unwrap();
        let head = repo.head();
        assert!(head.is_ok(), "Repository should have HEAD");

        let commit = head.unwrap().peel_to_commit();
        assert!(commit.is_ok(), "HEAD should point to a commit");

        let commit = commit.unwrap();
        assert_eq!(
            commit.message().unwrap(),
            "Initial Git-Mail repository setup"
        );
    }

    #[test]
    fn test_initialize_repository_invalid_path() {
        let storage = DefaultGitStorage::new("/invalid/path/that/does/not/exist".to_string());
        let result = storage.initialize_repository("/invalid/path/that/does/not/exist");

        assert!(
            result.is_err(),
            "Repository initialization should fail for invalid path"
        );
    }

    #[test]
    fn test_generate_email_file_path() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let email = create_test_email();

        let file_path = storage.generate_email_file_path(&email);

        // Verify path structure
        let path_str = file_path.to_str().unwrap();
        assert!(
            path_str.contains("inbox"),
            "Path should contain inbox folder"
        );
        assert!(
            path_str.contains(&email.metadata.created_at.format("%Y").to_string()),
            "Path should contain year"
        );
        assert!(
            path_str.contains(&email.metadata.created_at.format("%m").to_string()),
            "Path should contain month"
        );
        assert!(
            path_str.contains(&email.metadata.created_at.format("%d").to_string()),
            "Path should contain day"
        );
        assert!(path_str.ends_with(".txt"), "Path should end with .txt");
        assert!(
            path_str.contains(&email.id[..8]),
            "Path should contain email ID prefix"
        );
    }

    #[test]
    fn test_generate_email_file_path_sanitizes_filename() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let mut email = create_test_email();

        // Add problematic characters to headers
        email
            .headers
            .insert("From".to_string(), "sender@domain.com <test>".to_string());
        email.headers.insert(
            "Subject".to_string(),
            "Test/Subject\\With:Bad*Chars?".to_string(),
        );

        let file_path = storage.generate_email_file_path(&email);
        let filename = file_path.file_name().unwrap().to_str().unwrap();

        // Verify problematic characters are replaced
        assert!(!filename.contains('@'), "Filename should not contain @");
        assert!(!filename.contains('<'), "Filename should not contain <");
        assert!(!filename.contains('>'), "Filename should not contain >");
        assert!(!filename.contains('/'), "Filename should not contain /");
        assert!(!filename.contains('\\'), "Filename should not contain \\");
        assert!(!filename.contains(':'), "Filename should not contain :");
        assert!(!filename.contains('*'), "Filename should not contain *");
        assert!(!filename.contains('?'), "Filename should not contain ?");
    }

    #[test]
    fn test_create_directory_structure() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let result = storage.create_directory_structure();

        assert!(
            result.is_ok(),
            "Directory structure creation should succeed"
        );

        // Verify all folders were created
        let folders = ["inbox", "sent", "drafts", "accounts", "filters"];
        for folder in &folders {
            let folder_path = temp_dir.path().join(folder);
            assert!(folder_path.exists(), "Folder {} should exist", folder);
            assert!(
                folder_path.is_dir(),
                "Path {} should be a directory",
                folder
            );
        }
    }

    #[test]
    fn test_create_directory_structure_existing_folders() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        // Pre-create one folder
        let inbox_path = temp_dir.path().join("inbox");
        fs::create_dir(&inbox_path).expect("Failed to create inbox folder");

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let result = storage.create_directory_structure();

        assert!(
            result.is_ok(),
            "Directory structure creation should succeed with existing folders"
        );

        // Verify all folders exist
        let folders = ["inbox", "sent", "drafts", "accounts", "filters"];
        for folder in &folders {
            let folder_path = temp_dir.path().join(folder);
            assert!(folder_path.exists(), "Folder {} should exist", folder);
        }
    }

    #[test]
    fn test_serialize_email_to_text() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let email = create_test_email();

        let text = storage.serialize_email_to_text(&email);

        // Verify sections are present
        assert!(
            text.contains("--- Email Headers ---"),
            "Should contain headers section"
        );
        assert!(
            text.contains("--- Metadata ---"),
            "Should contain metadata section"
        );
        assert!(text.contains("--- Body ---"), "Should contain body section");

        // Verify header content
        assert!(
            text.contains("From: sender@example.com"),
            "Should contain From header"
        );
        assert!(
            text.contains("Subject: Test Email"),
            "Should contain Subject header"
        );

        // Verify metadata content
        assert!(
            text.contains(&format!("ID: {}", email.id)),
            "Should contain email ID"
        );
        assert!(
            text.contains("Account: test@example.com"),
            "Should contain account"
        );
        assert!(text.contains("Folder: inbox"), "Should contain folder");

        // Verify body content
        assert!(
            text.contains("Content-Type: text/plain"),
            "Should contain content type"
        );
        assert!(
            text.contains("This is a test email body."),
            "Should contain body text"
        );
    }

    #[test]
    fn test_serialize_email_with_html_content() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let mut email = create_test_email();
        email.body.html_content = Some("<p>HTML content</p>".to_string());

        let text = storage.serialize_email_to_text(&email);

        assert!(
            text.contains("--- HTML Body ---"),
            "Should contain HTML body section"
        );
        assert!(
            text.contains("<p>HTML content</p>"),
            "Should contain HTML content"
        );
    }

    #[test]
    fn test_serialize_email_with_attachments() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let mut email = create_test_email();

        email.attachments.push(crate::models::Attachment {
            filename: "test.pdf".to_string(),
            content_type: "application/pdf".to_string(),
            size: 1024,
            file_path: "attachments/test.pdf".to_string(),
        });

        let text = storage.serialize_email_to_text(&email);

        assert!(
            text.contains("--- Attachments ---"),
            "Should contain attachments section"
        );
        assert!(
            text.contains("Filename: test.pdf"),
            "Should contain attachment filename"
        );
        assert!(
            text.contains("Content-Type: application/pdf"),
            "Should contain attachment content type"
        );
        assert!(
            text.contains("Size: 1024 bytes"),
            "Should contain attachment size"
        );
    }

    #[test]
    fn test_parse_email_from_text() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let original_email = create_test_email();

        // Serialize and then parse
        let text = storage.serialize_email_to_text(&original_email);
        let parsed_email = storage.parse_email_from_text(&text, "/test/path.txt");

        assert!(parsed_email.is_ok(), "Email parsing should succeed");
        let parsed_email = parsed_email.unwrap();

        // Verify parsed data matches original
        assert_eq!(parsed_email.id, original_email.id);
        assert_eq!(parsed_email.account, original_email.account);
        assert_eq!(parsed_email.message_id, original_email.message_id);
        assert_eq!(parsed_email.body.content, original_email.body.content);
        assert_eq!(
            parsed_email.body.content_type,
            original_email.body.content_type
        );
        assert_eq!(parsed_email.metadata.folder, original_email.metadata.folder);
        assert_eq!(parsed_email.metadata.file_path, "/test/path.txt");

        // Verify headers
        assert_eq!(
            parsed_email.headers.get("From"),
            original_email.headers.get("From")
        );
        assert_eq!(
            parsed_email.headers.get("Subject"),
            original_email.headers.get("Subject")
        );
    }

    #[test]
    fn test_generate_commit_message() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let mut email = create_test_email();

        email
            .headers
            .insert("Subject".to_string(), "Test Subject".to_string());
        email.headers.insert(
            "From".to_string(),
            "John Doe <john@example.com>".to_string(),
        );

        let message = storage.generate_commit_message("store", &email);
        assert_eq!(message, "Add email: Test Subject from john@example.com");

        let message = storage.generate_commit_message("update", &email);
        assert_eq!(message, "Update email: Test Subject from john@example.com");

        let message = storage.generate_commit_message("delete", &email);
        assert_eq!(message, "Delete email: Test Subject from john@example.com");
    }

    #[test]
    fn test_generate_commit_message_long_subject() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let mut email = create_test_email();

        let long_subject = "This is a very long email subject that should be truncated because it exceeds the maximum length";
        email
            .headers
            .insert("Subject".to_string(), long_subject.to_string());
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());

        let message = storage.generate_commit_message("store", &email);
        assert!(message.len() < long_subject.len() + 50); // Should be truncated
        assert!(message.contains("..."));
        assert!(message.contains("sender@example.com"));
    }

    #[test]
    fn test_generate_commit_message_no_subject() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());
        let mut email = create_test_email();

        email.headers.remove("Subject");
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());

        let message = storage.generate_commit_message("store", &email);
        assert_eq!(message, "Add email: (no subject) from sender@example.com");
    }

    #[test]
    fn test_resolve_conflict_keep_local() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        let conflicted_content = r#"--- Email Headers ---
From: sender@example.com
<<<<<<< HEAD
Subject: Local Subject
=======
Subject: Remote Subject
>>>>>>> branch
To: recipient@example.com

--- Body ---
Content-Type: text/plain

<<<<<<< HEAD
Local body content
=======
Remote body content
>>>>>>> branch"#;

        let resolved = storage
            .resolve_conflict_keep_local(conflicted_content)
            .unwrap();

        assert!(resolved.contains("Subject: Local Subject"));
        assert!(!resolved.contains("Subject: Remote Subject"));
        assert!(resolved.contains("Local body content"));
        assert!(!resolved.contains("Remote body content"));
        assert!(!resolved.contains("<<<<<<< HEAD"));
        assert!(!resolved.contains("======="));
        assert!(!resolved.contains(">>>>>>> branch"));
    }

    #[test]
    fn test_resolve_conflict_keep_remote() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        let conflicted_content = r#"--- Email Headers ---
From: sender@example.com
<<<<<<< HEAD
Subject: Local Subject
=======
Subject: Remote Subject
>>>>>>> branch
To: recipient@example.com

--- Body ---
Content-Type: text/plain

<<<<<<< HEAD
Local body content
=======
Remote body content
>>>>>>> branch"#;

        let resolved = storage
            .resolve_conflict_keep_remote(conflicted_content)
            .unwrap();

        assert!(!resolved.contains("Subject: Local Subject"));
        assert!(resolved.contains("Subject: Remote Subject"));
        assert!(!resolved.contains("Local body content"));
        assert!(resolved.contains("Remote body content"));
        assert!(!resolved.contains("<<<<<<< HEAD"));
        assert!(!resolved.contains("======="));
        assert!(!resolved.contains(">>>>>>> branch"));
    }

    #[test]
    fn test_extract_conflict_versions() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        let conflicted_content = r#"--- Email Headers ---
From: sender@example.com
<<<<<<< HEAD
Subject: Local Subject
=======
Subject: Remote Subject
>>>>>>> branch
To: recipient@example.com"#;

        let (local, remote) = storage
            .extract_conflict_versions(conflicted_content)
            .unwrap();

        assert!(local.contains("Subject: Local Subject"));
        assert!(!local.contains("Subject: Remote Subject"));
        assert!(remote.contains("Subject: Remote Subject"));
        assert!(!remote.contains("Subject: Local Subject"));

        // Both should contain non-conflicted content
        assert!(local.contains("From: sender@example.com"));
        assert!(local.contains("To: recipient@example.com"));
        assert!(remote.contains("From: sender@example.com"));
        assert!(remote.contains("To: recipient@example.com"));
    }

    #[test]
    fn test_get_history_integration() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        // Initialize repository
        storage
            .initialize_repository(repo_path)
            .expect("Failed to initialize repository");

        // Create and store an email
        let email = create_test_email();
        let email_id = email.id.clone();

        storage.store_email(&email).expect("Failed to store email");

        // Get history for the email
        let history = storage
            .get_history(&email_id)
            .expect("Failed to get history");

        // Should have at least one commit (the store operation)
        assert!(!history.is_empty(), "History should not be empty");

        // Check the most recent commit
        let latest_commit = &history[0];
        assert!(latest_commit.message.contains("Add email"));
        assert!(latest_commit.message.contains("Test Email"));
        assert!(!latest_commit.hash.is_empty());
        assert!(!latest_commit.author.is_empty());
    }

    #[test]
    fn test_get_history_multiple_commits() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        // Initialize repository
        storage
            .initialize_repository(repo_path)
            .expect("Failed to initialize repository");

        // Create and store an email
        let mut email = create_test_email();
        let email_id = email.id.clone();

        storage.store_email(&email).expect("Failed to store email");

        // Modify and store the email again (simulating an update)
        email.metadata.is_read = true;
        email.metadata.modified_at = chrono::Utc::now();

        // We need to manually update the file and commit to simulate an update
        let file_path = storage.generate_email_file_path(&email);
        let content = storage.serialize_email_to_text(&email);
        std::fs::write(&file_path, content).expect("Failed to write updated email");

        let repo = storage
            .open_repository()
            .expect("Failed to open repository");
        let mut index = repo.index().expect("Failed to get index");
        let repo_path_buf = std::path::Path::new(repo_path);
        let relative_path = file_path
            .strip_prefix(repo_path_buf)
            .expect("Failed to get relative path");
        index
            .add_path(relative_path)
            .expect("Failed to add to index");
        index.write().expect("Failed to write index");

        let commit_message = storage.generate_commit_message("update", &email);
        storage
            .commit_changes(&commit_message)
            .expect("Failed to commit changes");

        // Get history for the email
        let history = storage
            .get_history(&email_id)
            .expect("Failed to get history");

        // Should have two commits now
        assert!(
            history.len() >= 2,
            "History should have at least 2 commits, got {}",
            history.len()
        );

        // Check that commits are sorted by timestamp (newest first)
        if history.len() >= 2 {
            assert!(
                history[0].timestamp >= history[1].timestamp,
                "Commits should be sorted by timestamp"
            );
        }

        // Check commit messages
        let messages: Vec<&str> = history.iter().map(|c| c.message.as_str()).collect();
        assert!(
            messages.iter().any(|m| m.contains("Add email")),
            "Should have 'Add email' commit"
        );
        assert!(
            messages.iter().any(|m| m.contains("Update email")),
            "Should have 'Update email' commit"
        );
    }

    #[test]
    fn test_get_history_nonexistent_email() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        // Initialize repository
        storage
            .initialize_repository(repo_path)
            .expect("Failed to initialize repository");

        // Try to get history for non-existent email
        let result = storage.get_history("nonexistent-id");

        assert!(result.is_err(), "Should return error for nonexistent email");
        match result.unwrap_err() {
            GitMailError::EmailNotFound(id) => {
                assert_eq!(id, "nonexistent-id");
            }
            _ => panic!("Should return EmailNotFound error"),
        }
    }

    #[test]
    fn test_commit_touches_file() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        // Initialize repository
        storage
            .initialize_repository(repo_path)
            .expect("Failed to initialize repository");

        // Create and store an email
        let email = create_test_email();
        storage.store_email(&email).expect("Failed to store email");

        let repo = storage
            .open_repository()
            .expect("Failed to open repository");
        let head = repo.head().expect("Failed to get HEAD");
        let commit = head.peel_to_commit().expect("Failed to get commit");

        // Get the email file path
        let file_path = storage.generate_email_file_path(&email);
        let repo_path_buf = std::path::Path::new(repo_path);
        let relative_path = file_path
            .strip_prefix(repo_path_buf)
            .expect("Failed to get relative path");

        // Check if the commit touches the file
        let touches = storage
            .commit_touches_file(&repo, &commit, relative_path)
            .expect("Failed to check if commit touches file");
        assert!(touches, "Commit should touch the email file");

        // Check with a different file path
        let other_path = std::path::Path::new("nonexistent/file.txt");
        let touches_other = storage
            .commit_touches_file(&repo, &commit, other_path)
            .expect("Failed to check if commit touches other file");
        assert!(!touches_other, "Commit should not touch non-existent file");
    }

    #[test]
    fn test_store_and_retrieve_email_integration() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        // Initialize repository
        storage
            .initialize_repository(repo_path)
            .expect("Failed to initialize repository");

        // Create test email
        let email = create_test_email();
        let original_id = email.id.clone();

        // Store email
        let file_path = storage.store_email(&email).expect("Failed to store email");
        assert!(!file_path.is_empty(), "File path should not be empty");

        // Verify file was created
        assert!(
            std::path::Path::new(&file_path).exists(),
            "Email file should exist"
        );

        // Retrieve email
        let retrieved_email = storage
            .retrieve_email(&original_id)
            .expect("Failed to retrieve email");

        // Verify retrieved email matches original
        assert_eq!(retrieved_email.id, original_id);
        assert_eq!(retrieved_email.account, email.account);
        assert_eq!(retrieved_email.body.content, email.body.content);
        assert_eq!(
            retrieved_email.headers.get("Subject"),
            email.headers.get("Subject")
        );
        assert_eq!(
            retrieved_email.headers.get("From"),
            email.headers.get("From")
        );
    }

    #[test]
    fn test_list_emails_integration() {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap();

        let storage = DefaultGitStorage::new(repo_path.to_string());

        // Initialize repository
        storage
            .initialize_repository(repo_path)
            .expect("Failed to initialize repository");

        // Create and store multiple test emails
        let mut emails = Vec::new();
        for i in 0..3 {
            let mut email = create_test_email();
            email
                .headers
                .insert("Subject".to_string(), format!("Test Email {}", i));
            emails.push(email);
        }

        // Store all emails
        for email in &emails {
            storage.store_email(email).expect("Failed to store email");
        }

        // List all emails
        let email_list = storage.list_emails(None).expect("Failed to list emails");
        assert_eq!(email_list.len(), 3, "Should have 3 emails");

        // Test folder-specific listing
        let inbox_emails = storage
            .list_emails(Some("inbox"))
            .expect("Failed to list inbox emails");
        assert_eq!(inbox_emails.len(), 3, "Should have 3 emails in inbox");
    }
}
