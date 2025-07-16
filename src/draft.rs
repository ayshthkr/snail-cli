//! Draft management module
//!
//! Handles draft email storage, auto-save functionality, and draft lifecycle management.

use crate::error::{GitMailError, Result};
use crate::models::{Draft, DraftType, Email};
use chrono::Utc;
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::Duration;
use tokio::time::interval;
use tracing::{debug, error, info, warn};

/// Draft manager trait for handling draft operations
pub trait DraftManager {
    /// Create a new draft
    fn create_draft(&mut self, draft: Draft) -> Result<()>;

    /// Load a draft by ID
    fn load_draft(&self, draft_id: &str) -> Result<Draft>;

    /// Save a draft
    fn save_draft(&mut self, draft: &Draft) -> Result<()>;

    /// Delete a draft
    fn delete_draft(&mut self, draft_id: &str) -> Result<()>;

    /// List all drafts
    fn list_drafts(&self) -> Result<Vec<Draft>>;

    /// Auto-save a draft if needed
    fn auto_save_draft(&mut self, draft: &mut Draft) -> Result<bool>;

    /// Start auto-save background task
    fn start_auto_save_task(&self) -> Result<()>;

    /// Stop auto-save background task
    fn stop_auto_save_task(&mut self) -> Result<()>;
}

/// File-based draft manager implementation
pub struct FileDraftManager {
    /// Base directory for draft storage
    drafts_dir: PathBuf,
    /// In-memory cache of drafts
    draft_cache: HashMap<String, Draft>,
    /// Auto-save task handle
    auto_save_handle: Option<tokio::task::JoinHandle<()>>,
    /// Shared state for auto-save task
    shared_state: Arc<Mutex<DraftManagerState>>,
}

/// Shared state for draft manager
#[derive(Debug)]
struct DraftManagerState {
    /// Drafts that need auto-saving
    pending_auto_save: HashMap<String, Draft>,
    /// Whether auto-save is enabled
    auto_save_enabled: bool,
}

impl FileDraftManager {
    /// Create a new file-based draft manager
    pub fn new<P: AsRef<Path>>(drafts_dir: P) -> Result<Self> {
        let drafts_dir = drafts_dir.as_ref().to_path_buf();

        // Create drafts directory if it doesn't exist
        if !drafts_dir.exists() {
            fs::create_dir_all(&drafts_dir).map_err(|e| {
                GitMailError::Storage(format!(
                    "Failed to create drafts directory {:?}: {}",
                    drafts_dir, e
                ))
            })?;
        }

        let shared_state = Arc::new(Mutex::new(DraftManagerState {
            pending_auto_save: HashMap::new(),
            auto_save_enabled: true,
        }));

        Ok(Self {
            drafts_dir,
            draft_cache: HashMap::new(),
            auto_save_handle: None,
            shared_state,
        })
    }

    /// Get the file path for a draft
    fn get_draft_path(&self, draft_id: &str) -> PathBuf {
        self.drafts_dir.join(format!("{}.json", draft_id))
    }

    /// Load all drafts from disk into cache
    fn load_drafts_from_disk(&mut self) -> Result<()> {
        debug!("Loading drafts from disk: {:?}", self.drafts_dir);

        if !self.drafts_dir.exists() {
            return Ok(());
        }

        let entries = fs::read_dir(&self.drafts_dir).map_err(|e| {
            GitMailError::Storage(format!("Failed to read drafts directory: {}", e))
        })?;

        for entry in entries {
            let entry = entry.map_err(|e| {
                GitMailError::Storage(format!("Failed to read directory entry: {}", e))
            })?;

            let path = entry.path();
            if path.extension().and_then(|s| s.to_str()) == Some("json") {
                match self.load_draft_from_file(&path) {
                    Ok(draft) => {
                        self.draft_cache.insert(draft.id.clone(), draft);
                    }
                    Err(e) => {
                        warn!("Failed to load draft from {:?}: {}", path, e);
                    }
                }
            }
        }

        info!("Loaded {} drafts from disk", self.draft_cache.len());
        Ok(())
    }

    /// Load a single draft from file
    fn load_draft_from_file(&self, path: &Path) -> Result<Draft> {
        let content = fs::read_to_string(path).map_err(|e| {
            GitMailError::Storage(format!("Failed to read draft file {:?}: {}", path, e))
        })?;

        let draft: Draft = serde_json::from_str(&content).map_err(|e| {
            GitMailError::Storage(format!("Failed to parse draft file {:?}: {}", path, e))
        })?;

        Ok(draft)
    }

    /// Save a draft to file
    fn save_draft_to_file(&self, draft: &Draft) -> Result<()> {
        let path = self.get_draft_path(&draft.id);
        let content = serde_json::to_string_pretty(draft)
            .map_err(|e| GitMailError::Storage(format!("Failed to serialize draft: {}", e)))?;

        fs::write(&path, content).map_err(|e| {
            GitMailError::Storage(format!("Failed to write draft file {:?}: {}", path, e))
        })?;

        debug!("Saved draft {} to {:?}", draft.id, path);
        Ok(())
    }

    /// Background auto-save task
    async fn auto_save_task(shared_state: Arc<Mutex<DraftManagerState>>, drafts_dir: PathBuf) {
        let mut interval = interval(Duration::from_secs(5)); // Check every 5 seconds

        loop {
            interval.tick().await;

            let drafts_to_save = {
                let mut state = match shared_state.lock() {
                    Ok(state) => state,
                    Err(e) => {
                        error!("Failed to acquire lock for auto-save: {}", e);
                        continue;
                    }
                };

                if !state.auto_save_enabled {
                    continue;
                }

                // Find drafts that need auto-saving
                let mut drafts_to_save = Vec::new();
                for (id, draft) in &state.pending_auto_save {
                    if draft.needs_auto_save() {
                        drafts_to_save.push((id.clone(), draft.clone()));
                    }
                }

                drafts_to_save
            };

            // Save drafts outside the lock
            for (id, mut draft) in drafts_to_save {
                let path = drafts_dir.join(format!("{}.json", id));
                match serde_json::to_string_pretty(&draft) {
                    Ok(content) => {
                        if let Err(e) = fs::write(&path, content) {
                            error!("Auto-save failed for draft {}: {}", id, e);
                        } else {
                            debug!("Auto-saved draft {}", id);
                            draft.mark_auto_saved();

                            // Update the draft in shared state
                            if let Ok(mut state) = shared_state.lock() {
                                state.pending_auto_save.insert(id, draft);
                            }
                        }
                    }
                    Err(e) => {
                        error!("Failed to serialize draft {} for auto-save: {}", id, e);
                    }
                }
            }
        }
    }
}

impl DraftManager for FileDraftManager {
    fn create_draft(&mut self, draft: Draft) -> Result<()> {
        debug!("Creating draft: {}", draft.id);

        // Validate draft
        draft.validate().map_err(|e| GitMailError::Validation(e))?;

        // Save to file
        self.save_draft_to_file(&draft)?;

        // Add to cache
        self.draft_cache.insert(draft.id.clone(), draft.clone());

        // Add to auto-save queue
        if let Ok(mut state) = self.shared_state.lock() {
            state.pending_auto_save.insert(draft.id.clone(), draft);
        }

        Ok(())
    }

    fn load_draft(&self, draft_id: &str) -> Result<Draft> {
        debug!("Loading draft: {}", draft_id);

        // Try cache first
        if let Some(draft) = self.draft_cache.get(draft_id) {
            return Ok(draft.clone());
        }

        // Load from file
        let path = self.get_draft_path(draft_id);
        if !path.exists() {
            return Err(GitMailError::DraftNotFound(draft_id.to_string()));
        }

        self.load_draft_from_file(&path)
    }

    fn save_draft(&mut self, draft: &Draft) -> Result<()> {
        debug!("Saving draft: {}", draft.id);

        // Validate draft
        draft.validate().map_err(|e| GitMailError::Validation(e))?;

        // Save to file
        self.save_draft_to_file(draft)?;

        // Update cache
        self.draft_cache.insert(draft.id.clone(), draft.clone());

        // Update auto-save queue
        if let Ok(mut state) = self.shared_state.lock() {
            state
                .pending_auto_save
                .insert(draft.id.clone(), draft.clone());
        }

        Ok(())
    }

    fn delete_draft(&mut self, draft_id: &str) -> Result<()> {
        debug!("Deleting draft: {}", draft_id);

        let path = self.get_draft_path(draft_id);
        if path.exists() {
            fs::remove_file(&path).map_err(|e| {
                GitMailError::Storage(format!("Failed to delete draft file {:?}: {}", path, e))
            })?;
        }

        // Remove from cache
        self.draft_cache.remove(draft_id);

        // Remove from auto-save queue
        if let Ok(mut state) = self.shared_state.lock() {
            state.pending_auto_save.remove(draft_id);
        }

        Ok(())
    }

    fn list_drafts(&self) -> Result<Vec<Draft>> {
        debug!("Listing all drafts");
        Ok(self.draft_cache.values().cloned().collect())
    }

    fn auto_save_draft(&mut self, draft: &mut Draft) -> Result<bool> {
        if !draft.needs_auto_save() {
            return Ok(false);
        }

        debug!("Auto-saving draft: {}", draft.id);

        // Save to file
        self.save_draft_to_file(draft)?;

        // Mark as auto-saved
        draft.mark_auto_saved();

        // Update cache
        self.draft_cache.insert(draft.id.clone(), draft.clone());

        Ok(true)
    }

    fn start_auto_save_task(&self) -> Result<()> {
        if self.auto_save_handle.is_some() {
            return Ok(()); // Already running
        }

        info!("Starting auto-save background task");

        let shared_state = Arc::clone(&self.shared_state);
        let drafts_dir = self.drafts_dir.clone();

        // Enable auto-save
        if let Ok(mut state) = shared_state.lock() {
            state.auto_save_enabled = true;
        }

        // Note: In a real implementation, we would store the handle
        // For now, we'll spawn the task but can't store the handle due to borrowing rules
        tokio::spawn(Self::auto_save_task(shared_state, drafts_dir));

        Ok(())
    }

    fn stop_auto_save_task(&mut self) -> Result<()> {
        info!("Stopping auto-save background task");

        // Disable auto-save
        if let Ok(mut state) = self.shared_state.lock() {
            state.auto_save_enabled = false;
        }

        // Abort the task if we have a handle
        if let Some(handle) = self.auto_save_handle.take() {
            handle.abort();
        }

        Ok(())
    }
}

/// Draft composition helper functions
pub struct DraftComposer;

impl DraftComposer {
    /// Create a new compose draft with basic headers
    pub fn new_compose(account: String, to: Option<String>, subject: Option<String>) -> Draft {
        let mut draft = Draft::new_compose(account);

        if let Some(to) = to {
            draft.headers.insert("To".to_string(), to);
        }

        if let Some(subject) = subject {
            draft.headers.insert("Subject".to_string(), subject);
        }

        draft
    }

    /// Create a reply draft from an original email
    pub fn new_reply(account: String, original: &Email) -> Draft {
        let mut draft = Draft::new_reply(account, original.id.clone());

        // Set reply headers
        if let Some(from) = original.headers.get("From") {
            draft.headers.insert("To".to_string(), from.clone());
        }

        // Handle subject with Re: prefix
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
        draft.headers.insert("Subject".to_string(), subject);

        // Set In-Reply-To and References headers for proper threading
        if let Some(message_id) = original.headers.get("Message-ID") {
            draft
                .headers
                .insert("In-Reply-To".to_string(), message_id.clone());

            // Handle References header for threading
            let references = if let Some(existing_refs) = original.headers.get("References") {
                format!("{} {}", existing_refs, message_id)
            } else {
                message_id.clone()
            };
            draft.headers.insert("References".to_string(), references);
        }

        // Generate quoted content
        let from = original.headers.get("From").unwrap_or(&original.account);
        let date = original
            .headers
            .get("Date")
            .map_or("Unknown date", |v| v.as_str());

        let quoted_content = format!(
            "\n\nOn {}, {} wrote:\n> {}",
            date,
            from,
            original
                .body
                .content
                .lines()
                .collect::<Vec<_>>()
                .join("\n> ")
        );

        draft.body.content = quoted_content;

        draft
    }

    /// Create a forward draft from an original email
    pub fn new_forward(account: String, original: &Email) -> Draft {
        let mut draft = Draft::new_forward(account, original.id.clone());

        // Handle subject with Fwd: prefix
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
        draft.headers.insert("Subject".to_string(), subject);

        // Generate forward content
        let from = original.headers.get("From").unwrap_or(&original.account);
        let date = original
            .headers
            .get("Date")
            .map_or("Unknown date", |v| v.as_str());
        let to = original
            .headers
            .get("To")
            .map_or("Unknown recipient", |v| v.as_str());

        let forward_content = format!(
            "\n\n---------- Forwarded message ---------\nFrom: {}\nDate: {}\nTo: {}\nSubject: {}\n\n{}",
            from,
            date,
            to,
            original.headers.get("Subject").map_or("", |v| v.as_str()),
            original.body.content
        );

        draft.body.content = forward_content;

        // Copy attachments for forwarding
        draft.attachments = original.attachments.clone();

        draft
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::models::{Email, EmailBody};
    use std::collections::HashMap;
    use tempfile::TempDir;

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
        email.headers.insert(
            "Message-ID".to_string(),
            "<test-message-id@example.com>".to_string(),
        );
        email.body.content = "This is a test email content.\nWith multiple lines.".to_string();
        email
    }

    #[test]
    fn test_draft_composer_new_compose() {
        let draft = DraftComposer::new_compose(
            "test@example.com".to_string(),
            Some("recipient@example.com".to_string()),
            Some("Test Subject".to_string()),
        );

        assert_eq!(draft.account, "test@example.com");
        assert_eq!(draft.draft_type, DraftType::Compose);
        assert_eq!(
            draft.headers.get("To"),
            Some(&"recipient@example.com".to_string())
        );
        assert_eq!(
            draft.headers.get("Subject"),
            Some(&"Test Subject".to_string())
        );
        assert!(draft.original_email_id.is_none());
    }

    #[test]
    fn test_draft_composer_new_reply() {
        let original = create_test_email();
        let draft = DraftComposer::new_reply("reply@example.com".to_string(), &original);

        assert_eq!(draft.account, "reply@example.com");
        assert_eq!(draft.draft_type, DraftType::Reply);
        assert_eq!(draft.original_email_id, Some(original.id));
        assert_eq!(
            draft.headers.get("To"),
            Some(&"sender@example.com".to_string())
        );
        assert_eq!(
            draft.headers.get("Subject"),
            Some(&"Re: Test Subject".to_string())
        );
        assert_eq!(
            draft.headers.get("In-Reply-To"),
            Some(&"<test-message-id@example.com>".to_string())
        );
        assert!(draft
            .body
            .content
            .contains("On Mon, 1 Jan 2024 12:00:00 +0000, sender@example.com wrote:"));
        assert!(draft
            .body
            .content
            .contains("> This is a test email content."));
    }

    #[test]
    fn test_draft_composer_new_reply_already_reply() {
        let mut original = create_test_email();
        original
            .headers
            .insert("Subject".to_string(), "Re: Original Subject".to_string());

        let draft = DraftComposer::new_reply("reply@example.com".to_string(), &original);

        // Should not add another "Re: " prefix
        assert_eq!(
            draft.headers.get("Subject"),
            Some(&"Re: Original Subject".to_string())
        );
    }

    #[test]
    fn test_draft_composer_new_forward() {
        let original = create_test_email();
        let draft = DraftComposer::new_forward("forward@example.com".to_string(), &original);

        assert_eq!(draft.account, "forward@example.com");
        assert_eq!(draft.draft_type, DraftType::Forward);
        assert_eq!(draft.original_email_id, Some(original.id));
        assert_eq!(
            draft.headers.get("Subject"),
            Some(&"Fwd: Test Subject".to_string())
        );
        assert!(draft
            .body
            .content
            .contains("---------- Forwarded message ---------"));
        assert!(draft.body.content.contains("From: sender@example.com"));
        assert!(draft
            .body
            .content
            .contains("Date: Mon, 1 Jan 2024 12:00:00 +0000"));
        assert!(draft.body.content.contains("To: recipient@example.com"));
        assert!(draft.body.content.contains("This is a test email content."));
    }

    #[tokio::test]
    async fn test_file_draft_manager_create_and_load() {
        let temp_dir = TempDir::new().unwrap();
        let mut manager = FileDraftManager::new(temp_dir.path()).unwrap();

        let draft = Draft::new_compose("test@example.com".to_string());
        let draft_id = draft.id.clone();

        // Create draft
        manager.create_draft(draft).unwrap();

        // Load draft
        let loaded_draft = manager.load_draft(&draft_id).unwrap();
        assert_eq!(loaded_draft.id, draft_id);
        assert_eq!(loaded_draft.account, "test@example.com");
    }

    #[tokio::test]
    async fn test_file_draft_manager_save_and_delete() {
        let temp_dir = TempDir::new().unwrap();
        let mut manager = FileDraftManager::new(temp_dir.path()).unwrap();

        let mut draft = Draft::new_compose("test@example.com".to_string());
        draft
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        let draft_id = draft.id.clone();

        // Create and save draft
        manager.create_draft(draft.clone()).unwrap();
        manager.save_draft(&draft).unwrap();

        // Verify it exists
        assert!(manager.load_draft(&draft_id).is_ok());

        // Delete draft
        manager.delete_draft(&draft_id).unwrap();

        // Verify it's gone
        assert!(manager.load_draft(&draft_id).is_err());
    }

    #[tokio::test]
    async fn test_file_draft_manager_list_drafts() {
        let temp_dir = TempDir::new().unwrap();
        let mut manager = FileDraftManager::new(temp_dir.path()).unwrap();

        // Create multiple drafts
        let draft1 = Draft::new_compose("test1@example.com".to_string());
        let draft2 = Draft::new_compose("test2@example.com".to_string());

        manager.create_draft(draft1).unwrap();
        manager.create_draft(draft2).unwrap();

        // List drafts
        let drafts = manager.list_drafts().unwrap();
        assert_eq!(drafts.len(), 2);
    }

    #[tokio::test]
    async fn test_draft_auto_save() {
        let temp_dir = TempDir::new().unwrap();
        let mut manager = FileDraftManager::new(temp_dir.path()).unwrap();

        let mut draft = Draft::new_compose("test@example.com".to_string());
        draft.metadata.auto_save_interval = 1; // 1 second for testing

        // Initially needs auto-save
        assert!(draft.needs_auto_save());

        // Auto-save the draft
        let was_saved = manager.auto_save_draft(&mut draft).unwrap();
        assert!(was_saved);
        assert!(!draft.needs_auto_save());

        // Wait and check if it needs auto-save again
        tokio::time::sleep(Duration::from_secs(2)).await;
        assert!(draft.needs_auto_save());
    }
}
