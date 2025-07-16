//! Integration tests for organization functionality

use git_mail::git_storage::{DefaultGitStorage, GitStorage};
use git_mail::models::Email;
use git_mail::organization::{DefaultOrganizationManager, OrganizationManager};
use std::collections::HashMap;
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

fn create_test_email(id: &str, subject: &str, folder: &str) -> Email {
    let mut email = Email::new("test@example.com".to_string());
    email.id = id.to_string();
    email
        .headers
        .insert("Subject".to_string(), subject.to_string());
    email
        .headers
        .insert("From".to_string(), "sender@example.com".to_string());
    email.body.content = format!("Test email content for {}", subject);
    email.metadata.folder = folder.to_string();
    email
}

#[test]
fn test_create_and_list_folders() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Create root folders
    org_manager
        .create_folder("work", None)
        .expect("Failed to create work folder");
    org_manager
        .create_folder("personal", None)
        .expect("Failed to create personal folder");

    // Create nested folders
    org_manager
        .create_folder("projects", Some("work"))
        .expect("Failed to create projects folder");
    org_manager
        .create_folder("meetings", Some("work"))
        .expect("Failed to create meetings folder");
    org_manager
        .create_folder("family", Some("personal"))
        .expect("Failed to create family folder");

    // List all folders
    let folders = org_manager.list_folders().expect("Failed to list folders");
    let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();

    // Verify all folders exist
    assert!(folder_names.contains(&"work".to_string()));
    assert!(folder_names.contains(&"personal".to_string()));
    assert!(folder_names.contains(&"work/projects".to_string()));
    assert!(folder_names.contains(&"work/meetings".to_string()));
    assert!(folder_names.contains(&"personal/family".to_string()));

    // Check folder hierarchy
    let work_folder = folders.iter().find(|f| f.name == "work").unwrap();
    assert_eq!(work_folder.parent, None);
    assert!(work_folder.children.contains(&"work/projects".to_string()));
    assert!(work_folder.children.contains(&"work/meetings".to_string()));

    let projects_folder = folders.iter().find(|f| f.name == "work/projects").unwrap();
    assert_eq!(projects_folder.parent, Some("work".to_string()));
    assert!(projects_folder.children.is_empty());
}

#[test]
fn test_folder_validation() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Test empty folder name
    assert!(org_manager.create_folder("", None).is_err());

    // Test invalid characters (forward slash is now allowed for nested folders)
    assert!(org_manager
        .create_folder("folder\\with\\backslash", None)
        .is_err());
    assert!(org_manager
        .create_folder("folder:with:colon", None)
        .is_err());
    assert!(org_manager
        .create_folder("folder*with*asterisk", None)
        .is_err());
    assert!(org_manager
        .create_folder("folder?with?question", None)
        .is_err());
    assert!(org_manager
        .create_folder("folder\"with\"quote", None)
        .is_err());
    assert!(org_manager
        .create_folder("folder<with>brackets", None)
        .is_err());
    assert!(org_manager.create_folder("folder|with|pipe", None).is_err());

    // Test reserved names
    assert!(org_manager.create_folder(".git", None).is_err());
    assert!(org_manager.create_folder(".", None).is_err());
    assert!(org_manager.create_folder("..", None).is_err());

    // Test valid folder name
    assert!(org_manager
        .create_folder("valid_folder-name123", None)
        .is_ok());
}

#[test]
fn test_move_email_between_folders() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create test email in inbox
    let email = create_test_email("test_email", "Test Subject", "inbox");
    storage.store_email(&email).expect("Failed to store email");

    // Create target folders
    org_manager
        .create_folder("archive", None)
        .expect("Failed to create archive folder");
    org_manager
        .create_folder("work", None)
        .expect("Failed to create work folder");
    org_manager
        .create_folder("important", Some("work"))
        .expect("Failed to create important folder");

    // Move email to archive
    let result = org_manager
        .move_email("test_email", "archive")
        .expect("Failed to move email");
    assert!(result.success);
    assert_eq!(result.old_folder, "inbox");
    assert_eq!(result.new_folder, "archive");
    assert!(result.error.is_none());

    // Verify email is in archive folder
    let moved_email = storage
        .retrieve_email("test_email")
        .expect("Failed to retrieve moved email");
    assert_eq!(moved_email.metadata.folder, "archive");

    // Move email to nested folder
    let result = org_manager
        .move_email("test_email", "work/important")
        .expect("Failed to move email");
    assert!(result.success);
    assert_eq!(result.old_folder, "archive");
    assert_eq!(result.new_folder, "work/important");

    // Verify email is in nested folder
    let moved_email = storage
        .retrieve_email("test_email")
        .expect("Failed to retrieve moved email");
    assert_eq!(moved_email.metadata.folder, "work/important");
}

#[test]
fn test_move_nonexistent_email() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Create target folder
    org_manager
        .create_folder("archive", None)
        .expect("Failed to create folder");

    // Try to move non-existent email
    let result = org_manager
        .move_email("nonexistent", "archive")
        .expect("Move operation completed");
    assert!(!result.success);
    assert!(result.error.is_some());
    assert!(result.error.unwrap().contains("Failed to retrieve email"));
}

#[test]
fn test_move_email_to_same_folder() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create test email
    let email = create_test_email("test_email", "Test Subject", "inbox");
    storage.store_email(&email).expect("Failed to store email");

    // Move email to same folder (should succeed but do nothing)
    let result = org_manager
        .move_email("test_email", "inbox")
        .expect("Failed to move email");
    assert!(result.success);
    assert_eq!(result.old_folder, "inbox");
    assert_eq!(result.new_folder, "inbox");
    assert!(result.error.is_none());
}

#[test]
fn test_delete_empty_folder() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Create folder
    org_manager
        .create_folder("temp_folder", None)
        .expect("Failed to create folder");

    // Verify folder exists
    let folders = org_manager.list_folders().expect("Failed to list folders");
    let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();
    assert!(folder_names.contains(&"temp_folder".to_string()));

    // Delete folder
    org_manager
        .delete_folder("temp_folder")
        .expect("Failed to delete folder");

    // Verify folder is deleted
    let folders = org_manager.list_folders().expect("Failed to list folders");
    let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();
    assert!(!folder_names.contains(&"temp_folder".to_string()));
}

#[test]
fn test_delete_nonexistent_folder() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Try to delete non-existent folder
    let result = org_manager.delete_folder("nonexistent");
    assert!(result.is_err());
}

#[test]
fn test_delete_non_empty_folder() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create folder and add email to it
    org_manager
        .create_folder("archive", None)
        .expect("Failed to create folder");

    let email = create_test_email("test_email", "Test Subject", "inbox");
    storage.store_email(&email).expect("Failed to store email");

    // Move email to archive folder
    org_manager
        .move_email("test_email", "archive")
        .expect("Failed to move email");

    // Try to delete non-empty folder
    let result = org_manager.delete_folder("archive");
    assert!(result.is_err());
    assert!(result
        .unwrap_err()
        .to_string()
        .contains("Cannot delete non-empty folder"));
}

#[test]
fn test_rename_folder() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create folder with email
    org_manager
        .create_folder("old_name", None)
        .expect("Failed to create folder");

    let email = create_test_email("test_email", "Test Subject", "inbox");
    storage.store_email(&email).expect("Failed to store email");
    org_manager
        .move_email("test_email", "old_name")
        .expect("Failed to move email");

    // Rename folder
    org_manager
        .rename_folder("old_name", "new_name")
        .expect("Failed to rename folder");

    // Verify folder is renamed
    let folders = org_manager.list_folders().expect("Failed to list folders");
    let folder_names: Vec<String> = folders.iter().map(|f| f.name.clone()).collect();
    assert!(!folder_names.contains(&"old_name".to_string()));
    assert!(folder_names.contains(&"new_name".to_string()));

    // Verify email metadata is updated
    let email = storage
        .retrieve_email("test_email")
        .expect("Failed to retrieve email");
    assert_eq!(email.metadata.folder, "new_name");
}

#[test]
fn test_rename_nonexistent_folder() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Try to rename non-existent folder
    let result = org_manager.rename_folder("nonexistent", "new_name");
    assert!(result.is_err());
}

#[test]
fn test_rename_to_existing_folder() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Create two folders
    org_manager
        .create_folder("folder1", None)
        .expect("Failed to create folder1");
    org_manager
        .create_folder("folder2", None)
        .expect("Failed to create folder2");

    // Try to rename folder1 to folder2 (should fail)
    let result = org_manager.rename_folder("folder1", "folder2");
    assert!(result.is_err());
    assert!(result
        .unwrap_err()
        .to_string()
        .contains("Target folder already exists"));
}

#[test]
fn test_get_folder_info() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create folder structure
    org_manager
        .create_folder("work", None)
        .expect("Failed to create work folder");
    org_manager
        .create_folder("projects", Some("work"))
        .expect("Failed to create projects folder");
    org_manager
        .create_folder("meetings", Some("work"))
        .expect("Failed to create meetings folder");

    // Add emails to work folder
    let email1 = create_test_email("email1", "Work Email 1", "inbox");
    let email2 = create_test_email("email2", "Work Email 2", "inbox");
    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");

    org_manager
        .move_email("email1", "work")
        .expect("Failed to move email1");
    org_manager
        .move_email("email2", "work")
        .expect("Failed to move email2");

    // Get folder info
    let folder_info = org_manager
        .get_folder_info("work")
        .expect("Failed to get folder info");

    assert_eq!(folder_info.name, "work");
    assert_eq!(folder_info.parent, None);
    assert_eq!(folder_info.children.len(), 2);
    assert!(folder_info.children.contains(&"work/projects".to_string()));
    assert!(folder_info.children.contains(&"work/meetings".to_string()));
    assert_eq!(folder_info.email_count, 2);
}

#[test]
fn test_get_folder_tree() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Create complex folder structure
    org_manager
        .create_folder("work", None)
        .expect("Failed to create work");
    org_manager
        .create_folder("personal", None)
        .expect("Failed to create personal");
    org_manager
        .create_folder("projects", Some("work"))
        .expect("Failed to create projects");
    org_manager
        .create_folder("meetings", Some("work"))
        .expect("Failed to create meetings");
    org_manager
        .create_folder("urgent", Some("work/projects"))
        .expect("Failed to create urgent");
    org_manager
        .create_folder("family", Some("personal"))
        .expect("Failed to create family");

    // Get folder tree
    let tree = org_manager
        .get_folder_tree()
        .expect("Failed to get folder tree");

    // Verify root level
    let root_folders = tree.get("").expect("Root level should exist");
    assert!(root_folders.contains(&"work".to_string()));
    assert!(root_folders.contains(&"personal".to_string()));

    // Verify work children
    let work_children = tree.get("work").expect("Work children should exist");
    assert!(work_children.contains(&"work/projects".to_string()));
    assert!(work_children.contains(&"work/meetings".to_string()));

    // Verify projects children
    let projects_children = tree
        .get("work/projects")
        .expect("Projects children should exist");
    assert!(projects_children.contains(&"work/projects/urgent".to_string()));

    // Verify personal children
    let personal_children = tree
        .get("personal")
        .expect("Personal children should exist");
    assert!(personal_children.contains(&"personal/family".to_string()));
}

#[test]
fn test_folder_email_counts() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create folders
    org_manager
        .create_folder("archive", None)
        .expect("Failed to create archive");
    org_manager
        .create_folder("work", None)
        .expect("Failed to create work");

    // Create emails with different read states
    let mut email1 = create_test_email("email1", "Read Email", "inbox");
    email1.metadata.is_read = true;

    let mut email2 = create_test_email("email2", "Unread Email", "inbox");
    email2.metadata.is_read = false;

    let mut email3 = create_test_email("email3", "Another Unread", "inbox");
    email3.metadata.is_read = false;

    // Store emails
    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");
    storage
        .store_email(&email3)
        .expect("Failed to store email3");

    // Move emails to different folders
    org_manager
        .move_email("email1", "archive")
        .expect("Failed to move email1");
    org_manager
        .move_email("email2", "work")
        .expect("Failed to move email2");
    org_manager
        .move_email("email3", "work")
        .expect("Failed to move email3");

    // Check folder counts
    let archive_info = org_manager
        .get_folder_info("archive")
        .expect("Failed to get archive info");
    assert_eq!(archive_info.email_count, 1);
    assert_eq!(archive_info.unread_count, 0); // email1 is read

    let work_info = org_manager
        .get_folder_info("work")
        .expect("Failed to get work info");
    assert_eq!(work_info.email_count, 2);
    assert_eq!(work_info.unread_count, 2); // email2 and email3 are unread
}

#[test]
fn test_create_duplicate_folder() {
    let (_temp_dir, _storage, org_manager) = setup_test_organization();

    // Create folder
    org_manager
        .create_folder("test_folder", None)
        .expect("Failed to create folder");

    // Try to create same folder again
    let result = org_manager.create_folder("test_folder", None);
    assert!(result.is_err());
    assert!(result
        .unwrap_err()
        .to_string()
        .contains("Folder already exists"));
}

#[test]
fn test_nested_folder_operations() {
    let (_temp_dir, storage, org_manager) = setup_test_organization();

    // Create deep nested structure
    org_manager
        .create_folder("level1", None)
        .expect("Failed to create level1");
    org_manager
        .create_folder("level2", Some("level1"))
        .expect("Failed to create level2");
    org_manager
        .create_folder("level3", Some("level1/level2"))
        .expect("Failed to create level3");

    // Add email to deepest folder
    let email = create_test_email("deep_email", "Deep Email", "inbox");
    storage.store_email(&email).expect("Failed to store email");
    org_manager
        .move_email("deep_email", "level1/level2/level3")
        .expect("Failed to move email");

    // Verify email is in correct location
    let moved_email = storage
        .retrieve_email("deep_email")
        .expect("Failed to retrieve email");
    assert_eq!(moved_email.metadata.folder, "level1/level2/level3");

    // Verify folder hierarchy
    let level3_info = org_manager
        .get_folder_info("level1/level2/level3")
        .expect("Failed to get level3 info");
    assert_eq!(level3_info.parent, Some("level1/level2".to_string()));
    assert_eq!(level3_info.email_count, 1);

    let level2_info = org_manager
        .get_folder_info("level1/level2")
        .expect("Failed to get level2 info");
    assert_eq!(level2_info.parent, Some("level1".to_string()));
    assert!(level2_info
        .children
        .contains(&"level1/level2/level3".to_string()));

    let level1_info = org_manager
        .get_folder_info("level1")
        .expect("Failed to get level1 info");
    assert_eq!(level1_info.parent, None);
    assert!(level1_info.children.contains(&"level1/level2".to_string()));
}
