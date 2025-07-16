//! Email organization functionality for Git-Mail
//!
//! Provides folder-based organization with Git tracking for organizational changes

use crate::error::{GitMailError, Result};
use crate::git_storage::GitStorage;
use crate::models::{Email, EmailMetadata};
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};

/// Folder information
#[derive(Debug, Clone)]
pub struct Folder {
    /// Folder name
    pub name: String,
    /// Full path to folder
    pub path: PathBuf,
    /// Number of emails in folder
    pub email_count: usize,
    /// Number of unread emails
    pub unread_count: usize,
    /// Parent folder (None for root folders)
    pub parent: Option<String>,
    /// Child folders
    pub children: Vec<String>,
}

/// Email move operation result
#[derive(Debug)]
pub struct MoveResult {
    /// Whether the move was successful
    pub success: bool,
    /// Old folder path
    pub old_folder: String,
    /// New folder path
    pub new_folder: String,
    /// Error message if move failed
    pub error: Option<String>,
}

/// Organization manager interface
pub trait OrganizationManager {
    /// Move an email to a different folder
    fn move_email(&self, email_id: &str, target_folder: &str) -> Result<MoveResult>;

    /// Create a new folder
    fn create_folder(&self, folder_name: &str, parent_folder: Option<&str>) -> Result<()>;

    /// Delete a folder (must be empty)
    fn delete_folder(&self, folder_name: &str) -> Result<()>;

    /// List all folders with their metadata
    fn list_folders(&self) -> Result<Vec<Folder>>;

    /// Get folder information
    fn get_folder_info(&self, folder_name: &str) -> Result<Folder>;

    /// Rename a folder
    fn rename_folder(&self, old_name: &str, new_name: &str) -> Result<()>;

    /// Get folder hierarchy as a tree structure
    fn get_folder_tree(&self) -> Result<HashMap<String, Vec<String>>>;
}

/// Default organization manager implementation
pub struct DefaultOrganizationManager<T: GitStorage> {
    storage: T,
    repository_path: String,
}

impl<T: GitStorage> DefaultOrganizationManager<T> {
    /// Create a new organization manager
    pub fn new(storage: T, repository_path: String) -> Self {
        Self {
            storage,
            repository_path,
        }
    }

    /// Get the full path for a folder
    fn get_folder_path(&self, folder_name: &str) -> PathBuf {
        Path::new(&self.repository_path).join(folder_name)
    }

    /// Validate folder name
    fn validate_folder_name(&self, folder_name: &str) -> Result<()> {
        if folder_name.is_empty() {
            return Err(GitMailError::Validation(
                "Folder name cannot be empty".to_string(),
            ));
        }

        // Split by '/' to validate each part of nested folder names
        let parts: Vec<&str> = folder_name.split('/').collect();

        for part in &parts {
            if part.is_empty() {
                return Err(GitMailError::Validation(
                    "Folder name part cannot be empty".to_string(),
                ));
            }

            // Check for invalid characters (excluding '/' since we handle nested folders)
            let invalid_chars = ['\\', ':', '*', '?', '"', '<', '>', '|'];
            if part.chars().any(|c| invalid_chars.contains(&c)) {
                return Err(GitMailError::Validation(
                    "Folder name contains invalid characters".to_string(),
                ));
            }

            // Check for reserved names
            let reserved_names = [".git", ".gitignore", ".", ".."];
            if reserved_names.contains(part) {
                return Err(GitMailError::Validation(
                    "Folder name is reserved".to_string(),
                ));
            }
        }

        Ok(())
    }

    /// Count emails in a folder
    fn count_emails_in_folder(&self, folder_path: &Path) -> Result<(usize, usize)> {
        let mut total_count = 0;
        let mut unread_count = 0;

        if !folder_path.exists() {
            return Ok((0, 0));
        }

        self.count_emails_recursive(folder_path, &mut total_count, &mut unread_count)?;

        Ok((total_count, unread_count))
    }

    /// Recursively count emails in folder and subfolders
    fn count_emails_recursive(
        &self,
        dir: &Path,
        total: &mut usize,
        unread: &mut usize,
    ) -> Result<()> {
        let entries = fs::read_dir(dir).map_err(|e| GitMailError::Io(e))?;

        for entry in entries {
            let entry = entry.map_err(|e| GitMailError::Io(e))?;
            let path = entry.path();

            if path.is_dir() {
                self.count_emails_recursive(&path, total, unread)?;
            } else if path.is_file() {
                if let Some(filename) = path.file_name().and_then(|n| n.to_str()) {
                    if filename.ends_with(".txt") && filename.starts_with("msg-") {
                        *total += 1;

                        // Check if email is unread by parsing the file
                        if let Ok(content) = fs::read_to_string(&path) {
                            if self.is_email_unread(&content) {
                                *unread += 1;
                            }
                        }
                    }
                }
            }
        }

        Ok(())
    }

    /// Check if an email is unread based on its content
    fn is_email_unread(&self, content: &str) -> bool {
        for line in content.lines() {
            if line.starts_with("Read: ") {
                return line == "Read: false";
            }
        }
        false // Default to read if not specified
    }

    /// Get all folder names in the repository
    fn get_all_folder_names(&self) -> Result<Vec<String>> {
        let mut folders = Vec::new();
        let base_path = Path::new(&self.repository_path);

        self.collect_folder_names(base_path, "", &mut folders)?;

        // Remove empty entries and sort
        folders.retain(|f| !f.is_empty());
        folders.sort();

        Ok(folders)
    }

    /// Recursively collect folder names
    fn collect_folder_names(
        &self,
        dir: &Path,
        prefix: &str,
        folders: &mut Vec<String>,
    ) -> Result<()> {
        if !dir.exists() {
            return Ok(());
        }

        let entries = fs::read_dir(dir).map_err(|e| GitMailError::Io(e))?;

        for entry in entries {
            let entry = entry.map_err(|e| GitMailError::Io(e))?;
            let path = entry.path();

            if path.is_dir() {
                if let Some(dir_name) = path.file_name().and_then(|n| n.to_str()) {
                    // Skip hidden directories and git directory
                    if !dir_name.starts_with('.') {
                        let folder_name = if prefix.is_empty() {
                            dir_name.to_string()
                        } else {
                            format!("{}/{}", prefix, dir_name)
                        };

                        folders.push(folder_name.clone());
                        self.collect_folder_names(&path, &folder_name, folders)?;
                    }
                }
            }
        }

        Ok(())
    }

    /// Build folder hierarchy
    fn build_folder_hierarchy(&self, folder_names: &[String]) -> HashMap<String, Vec<String>> {
        let mut hierarchy = HashMap::new();

        for folder_name in folder_names {
            let parts: Vec<&str> = folder_name.split('/').collect();

            if parts.len() == 1 {
                // Root folder
                hierarchy
                    .entry("".to_string())
                    .or_insert_with(Vec::new)
                    .push(folder_name.clone());
            } else {
                // Nested folder
                let parent = parts[..parts.len() - 1].join("/");
                hierarchy
                    .entry(parent)
                    .or_insert_with(Vec::new)
                    .push(folder_name.clone());
            }
        }

        // Sort children for each parent
        for children in hierarchy.values_mut() {
            children.sort();
        }

        hierarchy
    }

    /// Move email file to new folder
    fn move_email_file(&self, email: &Email, target_folder: &str) -> Result<String> {
        let current_path = Path::new(&email.metadata.file_path);

        if !current_path.exists() {
            return Err(GitMailError::NotFound(format!(
                "Email file not found: {}",
                email.metadata.file_path
            )));
        }

        // Generate new file path
        let filename = current_path
            .file_name()
            .ok_or_else(|| GitMailError::Storage("Invalid email file path".to_string()))?;

        let target_folder_path = self.get_folder_path(target_folder);

        // Create target folder if it doesn't exist
        if !target_folder_path.exists() {
            fs::create_dir_all(&target_folder_path).map_err(|e| GitMailError::Io(e))?;
        }

        let new_path = target_folder_path.join(filename);

        // Move the file
        fs::rename(current_path, &new_path).map_err(|e| GitMailError::Io(e))?;

        Ok(new_path.to_string_lossy().to_string())
    }

    /// Update email metadata after move
    fn update_email_metadata(
        &self,
        email_id: &str,
        new_folder: &str,
        new_file_path: &str,
    ) -> Result<()> {
        // Retrieve the email
        let mut email = self.storage.retrieve_email(email_id)?;

        // Update metadata
        email.metadata.folder = new_folder.to_string();
        email.metadata.file_path = new_file_path.to_string();
        email.metadata.modified_at = chrono::Utc::now();

        // Store the updated email (this will overwrite the file at the new location)
        self.storage.store_email(&email)?;

        Ok(())
    }
}

impl<T: GitStorage> OrganizationManager for DefaultOrganizationManager<T> {
    fn move_email(&self, email_id: &str, target_folder: &str) -> Result<MoveResult> {
        // Validate target folder name
        self.validate_folder_name(target_folder)?;

        // Retrieve the email
        let email = match self.storage.retrieve_email(email_id) {
            Ok(email) => email,
            Err(e) => {
                return Ok(MoveResult {
                    success: false,
                    old_folder: "unknown".to_string(),
                    new_folder: target_folder.to_string(),
                    error: Some(format!("Failed to retrieve email: {}", e)),
                });
            }
        };

        let old_folder = email.metadata.folder.clone();

        // Check if already in target folder
        if old_folder == target_folder {
            return Ok(MoveResult {
                success: true,
                old_folder: old_folder.clone(),
                new_folder: target_folder.to_string(),
                error: None,
            });
        }

        // Move the email file
        match self.move_email_file(&email, target_folder) {
            Ok(new_file_path) => {
                // Update email metadata
                match self.update_email_metadata(email_id, target_folder, &new_file_path) {
                    Ok(()) => Ok(MoveResult {
                        success: true,
                        old_folder,
                        new_folder: target_folder.to_string(),
                        error: None,
                    }),
                    Err(e) => Ok(MoveResult {
                        success: false,
                        old_folder,
                        new_folder: target_folder.to_string(),
                        error: Some(format!("Failed to update metadata: {}", e)),
                    }),
                }
            }
            Err(e) => Ok(MoveResult {
                success: false,
                old_folder,
                new_folder: target_folder.to_string(),
                error: Some(format!("Failed to move file: {}", e)),
            }),
        }
    }

    fn create_folder(&self, folder_name: &str, parent_folder: Option<&str>) -> Result<()> {
        self.validate_folder_name(folder_name)?;

        let full_folder_name = if let Some(parent) = parent_folder {
            format!("{}/{}", parent, folder_name)
        } else {
            folder_name.to_string()
        };

        let folder_path = self.get_folder_path(&full_folder_name);

        if folder_path.exists() {
            return Err(GitMailError::Validation(
                "Folder already exists".to_string(),
            ));
        }

        // Create the folder
        fs::create_dir_all(&folder_path).map_err(|e| GitMailError::Io(e))?;

        // Create a .gitkeep file to ensure the folder is tracked by Git
        let gitkeep_path = folder_path.join(".gitkeep");
        fs::write(&gitkeep_path, "").map_err(|e| GitMailError::Io(e))?;

        Ok(())
    }

    fn delete_folder(&self, folder_name: &str) -> Result<()> {
        let folder_path = self.get_folder_path(folder_name);

        if !folder_path.exists() {
            return Err(GitMailError::NotFound("Folder does not exist".to_string()));
        }

        // Check if folder is empty (except for .gitkeep)
        let entries = fs::read_dir(&folder_path).map_err(|e| GitMailError::Io(e))?;
        let mut has_emails = false;

        for entry in entries {
            let entry = entry.map_err(|e| GitMailError::Io(e))?;
            let path = entry.path();

            if path.is_file() {
                if let Some(filename) = path.file_name().and_then(|n| n.to_str()) {
                    if filename != ".gitkeep" {
                        has_emails = true;
                        break;
                    }
                }
            } else if path.is_dir() {
                has_emails = true;
                break;
            }
        }

        if has_emails {
            return Err(GitMailError::Validation(
                "Cannot delete non-empty folder".to_string(),
            ));
        }

        // Remove the folder
        fs::remove_dir_all(&folder_path).map_err(|e| GitMailError::Io(e))?;

        Ok(())
    }

    fn list_folders(&self) -> Result<Vec<Folder>> {
        let folder_names = self.get_all_folder_names()?;
        let hierarchy = self.build_folder_hierarchy(&folder_names);
        let mut folders = Vec::new();

        for folder_name in &folder_names {
            let folder_path = self.get_folder_path(folder_name);
            let (email_count, unread_count) = self.count_emails_in_folder(&folder_path)?;

            // Determine parent folder
            let parent = if folder_name.contains('/') {
                let parts: Vec<&str> = folder_name.split('/').collect();
                Some(parts[..parts.len() - 1].join("/"))
            } else {
                None
            };

            // Get children
            let children = hierarchy.get(folder_name).cloned().unwrap_or_default();

            folders.push(Folder {
                name: folder_name.clone(),
                path: folder_path,
                email_count,
                unread_count,
                parent,
                children,
            });
        }

        Ok(folders)
    }

    fn get_folder_info(&self, folder_name: &str) -> Result<Folder> {
        let folder_path = self.get_folder_path(folder_name);

        if !folder_path.exists() {
            return Err(GitMailError::NotFound("Folder does not exist".to_string()));
        }

        let (email_count, unread_count) = self.count_emails_in_folder(&folder_path)?;

        // Determine parent folder
        let parent = if folder_name.contains('/') {
            let parts: Vec<&str> = folder_name.split('/').collect();
            Some(parts[..parts.len() - 1].join("/"))
        } else {
            None
        };

        // Get children by listing subdirectories
        let mut children = Vec::new();
        if folder_path.is_dir() {
            let entries = fs::read_dir(&folder_path).map_err(|e| GitMailError::Io(e))?;

            for entry in entries {
                let entry = entry.map_err(|e| GitMailError::Io(e))?;
                let path = entry.path();

                if path.is_dir() {
                    if let Some(dir_name) = path.file_name().and_then(|n| n.to_str()) {
                        if !dir_name.starts_with('.') {
                            let child_name = if folder_name.is_empty() {
                                dir_name.to_string()
                            } else {
                                format!("{}/{}", folder_name, dir_name)
                            };
                            children.push(child_name);
                        }
                    }
                }
            }
        }

        children.sort();

        Ok(Folder {
            name: folder_name.to_string(),
            path: folder_path,
            email_count,
            unread_count,
            parent,
            children,
        })
    }

    fn rename_folder(&self, old_name: &str, new_name: &str) -> Result<()> {
        self.validate_folder_name(new_name)?;

        let old_path = self.get_folder_path(old_name);
        let new_path = self.get_folder_path(new_name);

        if !old_path.exists() {
            return Err(GitMailError::NotFound(
                "Source folder does not exist".to_string(),
            ));
        }

        if new_path.exists() {
            return Err(GitMailError::Validation(
                "Target folder already exists".to_string(),
            ));
        }

        // Rename the folder
        fs::rename(&old_path, &new_path).map_err(|e| GitMailError::Io(e))?;

        // Update all email metadata in the renamed folder
        self.update_folder_metadata_after_rename(old_name, new_name)?;

        Ok(())
    }

    fn get_folder_tree(&self) -> Result<HashMap<String, Vec<String>>> {
        let folder_names = self.get_all_folder_names()?;
        Ok(self.build_folder_hierarchy(&folder_names))
    }
}

impl<T: GitStorage> DefaultOrganizationManager<T> {
    /// Update email metadata after folder rename
    fn update_folder_metadata_after_rename(
        &self,
        old_folder: &str,
        new_folder: &str,
    ) -> Result<()> {
        // Get all emails in the old folder
        let emails = self.storage.list_emails(Some(old_folder))?;

        for email_metadata in emails {
            // Update each email's folder metadata
            let mut email = self
                .storage
                .retrieve_email(&self.extract_email_id_from_path(&email_metadata.file_path)?)?;
            email.metadata.folder = new_folder.to_string();
            email.metadata.modified_at = chrono::Utc::now();

            // Update file path
            let old_path = Path::new(&email.metadata.file_path);
            let filename = old_path.file_name().unwrap();
            let new_file_path = self.get_folder_path(new_folder).join(filename);
            email.metadata.file_path = new_file_path.to_string_lossy().to_string();

            // Store the updated email
            self.storage.store_email(&email)?;
        }

        Ok(())
    }

    /// Extract email ID from file path
    fn extract_email_id_from_path(&self, path: &str) -> Result<String> {
        let path = Path::new(path);
        let filename = path
            .file_name()
            .and_then(|n| n.to_str())
            .ok_or_else(|| GitMailError::Storage("Invalid file path".to_string()))?;

        // Email files are named like: msg-{id_prefix}-{sender}-{subject}.txt
        if let Some(start) = filename.find("msg-") {
            let after_prefix = &filename[start + 4..];
            if let Some(end) = after_prefix.find('-') {
                return Ok(after_prefix[..end].to_string());
            }
        }

        Err(GitMailError::Storage(
            "Could not extract email ID from filename".to_string(),
        ))
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::git_storage::DefaultGitStorage;
    use crate::models::Email;
    use tempfile::TempDir;

    fn setup_test_organization() -> (
        TempDir,
        DefaultGitStorage,
        DefaultOrganizationManager<DefaultGitStorage>,
    ) {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap().to_string();

        let storage = DefaultGitStorage::new(repo_path.clone());
        storage
            .initialize_repository(&repo_path)
            .expect("Failed to initialize repository");

        let org_manager = DefaultOrganizationManager::new(storage.clone(), repo_path);

        (temp_dir, storage, org_manager)
    }

    #[test]
    fn test_create_folder() {
        let (_temp_dir, _storage, org_manager) = setup_test_organization();

        // Create a root folder
        org_manager
            .create_folder("work", None)
            .expect("Failed to create folder");

        // Create a nested folder
        org_manager
            .create_folder("projects", Some("work"))
            .expect("Failed to create nested folder");

        // Verify folders exist
        let folders = org_manager.list_folders().expect("Failed to list folders");
        let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();

        assert!(folder_names.contains(&"work".to_string()));
        assert!(folder_names.contains(&"work/projects".to_string()));
    }

    #[test]
    fn test_folder_validation() {
        let (_temp_dir, _storage, org_manager) = setup_test_organization();

        // Test invalid folder names
        assert!(org_manager.create_folder("", None).is_err());
        assert!(org_manager
            .create_folder("folder/with/slash", None)
            .is_err());
        assert!(org_manager
            .create_folder("folder:with:colon", None)
            .is_err());
        assert!(org_manager.create_folder(".git", None).is_err());
    }

    #[test]
    fn test_move_email() {
        let (_temp_dir, storage, org_manager) = setup_test_organization();

        // Create test email
        let mut email = Email::new("test@example.com".to_string());
        email.id = "test_email".to_string();
        email
            .headers
            .insert("Subject".to_string(), "Test Email".to_string());
        email.body.content = "Test content".to_string();

        // Store email
        storage.store_email(&email).expect("Failed to store email");

        // Create target folder
        org_manager
            .create_folder("archive", None)
            .expect("Failed to create folder");

        // Move email
        let result = org_manager
            .move_email("test_email", "archive")
            .expect("Failed to move email");

        assert!(result.success);
        assert_eq!(result.old_folder, "inbox");
        assert_eq!(result.new_folder, "archive");

        // Verify email is in new folder
        let moved_email = storage
            .retrieve_email("test_email")
            .expect("Failed to retrieve moved email");
        assert_eq!(moved_email.metadata.folder, "archive");
    }

    #[test]
    fn test_delete_empty_folder() {
        let (_temp_dir, _storage, org_manager) = setup_test_organization();

        // Create and delete empty folder
        org_manager
            .create_folder("temp", None)
            .expect("Failed to create folder");
        org_manager
            .delete_folder("temp")
            .expect("Failed to delete folder");

        // Verify folder is deleted
        let folders = org_manager.list_folders().expect("Failed to list folders");
        let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();

        assert!(!folder_names.contains(&"temp".to_string()));
    }

    #[test]
    fn test_rename_folder() {
        let (_temp_dir, _storage, org_manager) = setup_test_organization();

        // Create folder
        org_manager
            .create_folder("old_name", None)
            .expect("Failed to create folder");

        // Rename folder
        org_manager
            .rename_folder("old_name", "new_name")
            .expect("Failed to rename folder");

        // Verify rename
        let folders = org_manager.list_folders().expect("Failed to list folders");
        let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();

        assert!(!folder_names.contains(&"old_name".to_string()));
        assert!(folder_names.contains(&"new_name".to_string()));
    }

    #[test]
    fn test_folder_hierarchy() {
        let (_temp_dir, _storage, org_manager) = setup_test_organization();

        // Create nested folder structure
        org_manager
            .create_folder("work", None)
            .expect("Failed to create work folder");
        org_manager
            .create_folder("personal", None)
            .expect("Failed to create personal folder");
        org_manager
            .create_folder("projects", Some("work"))
            .expect("Failed to create projects folder");
        org_manager
            .create_folder("urgent", Some("work"))
            .expect("Failed to create urgent folder");

        // Get folder tree
        let tree = org_manager
            .get_folder_tree()
            .expect("Failed to get folder tree");

        // Verify hierarchy
        assert!(tree.contains_key(""));
        assert!(tree.contains_key("work"));

        let root_folders = tree.get("").unwrap();
        assert!(root_folders.contains(&"work".to_string()));
        assert!(root_folders.contains(&"personal".to_string()));

        let work_children = tree.get("work").unwrap();
        assert!(work_children.contains(&"work/projects".to_string()));
        assert!(work_children.contains(&"work/urgent".to_string()));
    }
}
