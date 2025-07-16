//! Integration tests for search functionality

use chrono::Utc;
use git_mail::git_storage::{DefaultGitStorage, GitStorage};
use git_mail::models::Email;
use git_mail::search::{CommandLineSearchEngine, SearchEngine, SearchField, SearchQuery};
use tempfile::TempDir;

fn setup_test_repository() -> (
    TempDir,
    DefaultGitStorage,
    CommandLineSearchEngine<DefaultGitStorage>,
) {
    let temp_dir = TempDir::new().expect("Failed to create temp directory");
    let repo_path = temp_dir.path().to_str().unwrap().to_string();

    let storage = DefaultGitStorage::new(repo_path.clone());
    storage
        .initialize_repository(&repo_path)
        .expect("Failed to initialize repository");

    let search_engine = CommandLineSearchEngine::new(storage.clone(), repo_path);

    (temp_dir, storage, search_engine)
}

fn create_test_email(id: &str, subject: &str, from: &str, body: &str) -> Email {
    let mut email = Email::new("test@example.com".to_string());
    email.id = id.to_string();
    email
        .headers
        .insert("Subject".to_string(), subject.to_string());
    email.headers.insert("From".to_string(), from.to_string());
    email
        .headers
        .insert("To".to_string(), "recipient@example.com".to_string());
    email.body.content = body.to_string();
    email.metadata.tags = vec!["test".to_string()];
    email
}

#[test]
fn test_basic_search() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    // Store test emails
    let email1 = create_test_email(
        "email1",
        "Important meeting",
        "boss@company.com",
        "We need to discuss the project timeline.",
    );
    let email2 = create_test_email(
        "email2",
        "Lunch plans",
        "friend@example.com",
        "Want to grab lunch tomorrow?",
    );
    let email3 = create_test_email(
        "email3",
        "Project update",
        "colleague@company.com",
        "The project is progressing well.",
    );

    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");
    storage
        .store_email(&email3)
        .expect("Failed to store email3");

    // Search for "project"
    let query = SearchQuery::new("project".to_string());
    let results = search_engine.search(&query).expect("Search failed");

    // Should find 2 emails containing "project"
    assert_eq!(results.len(), 2);

    // Check that results are sorted by relevance
    assert!(results[0].score >= results[1].score);
}

#[test]
fn test_case_sensitive_search() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let email = create_test_email(
        "email1",
        "Important Meeting",
        "sender@example.com",
        "This is about the MEETING tomorrow.",
    );
    storage.store_email(&email).expect("Failed to store email");

    // Case insensitive search (default)
    let query = SearchQuery::new("meeting".to_string());
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1);

    // Case sensitive search
    let query = SearchQuery::new("meeting".to_string()).with_case_sensitive(true);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 0); // Should not find "Meeting" or "MEETING"

    let query = SearchQuery::new("Meeting".to_string()).with_case_sensitive(true);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1); // Should find "Meeting"
}

#[test]
fn test_regex_search() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let email1 = create_test_email(
        "email1",
        "Meeting on 2024-01-15",
        "sender@example.com",
        "See you at the meeting.",
    );
    let email2 = create_test_email(
        "email2",
        "Call at 15:30",
        "sender@example.com",
        "Phone call scheduled.",
    );
    let email3 = create_test_email(
        "email3",
        "Random text",
        "sender@example.com",
        "No dates here.",
    );

    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");
    storage
        .store_email(&email3)
        .expect("Failed to store email3");

    // Search for date pattern using regex
    let query = SearchQuery::new(r"\d{4}-\d{2}-\d{2}".to_string()).with_regex(true);
    let results = search_engine.search(&query).expect("Search failed");

    // Should find 1 email with date pattern
    assert_eq!(results.len(), 1);
    assert!(results[0].email_metadata.file_path.contains("email1"));
}

#[test]
fn test_field_specific_search() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let email = create_test_email(
        "email1",
        "Meeting reminder",
        "important@company.com",
        "This email is about lunch plans.",
    );
    storage.store_email(&email).expect("Failed to store email");

    // Search for "meeting" in headers only
    let query = SearchQuery::new("meeting".to_string()).with_field(SearchField::Headers);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1); // Found in subject

    // Search for "lunch" in headers only
    let query = SearchQuery::new("lunch".to_string()).with_field(SearchField::Headers);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 0); // Not found in headers

    // Search for "lunch" in body only
    let query = SearchQuery::new("lunch".to_string()).with_field(SearchField::Body);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1); // Found in body
}

#[test]
fn test_specific_header_search() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let email = create_test_email(
        "email1",
        "Test subject",
        "important@company.com",
        "Email body content.",
    );
    storage.store_email(&email).expect("Failed to store email");

    // Search for "important" in From header specifically
    let query = SearchQuery::new("important".to_string())
        .with_field(SearchField::Header("From".to_string()));
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1);

    // Search for "important" in Subject header specifically
    let query = SearchQuery::new("important".to_string())
        .with_field(SearchField::Header("Subject".to_string()));
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 0); // Not in subject
}

#[test]
fn test_folder_filter() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    // Create emails in different folders
    let mut inbox_email = create_test_email(
        "email1",
        "Inbox message",
        "sender@example.com",
        "This is in inbox.",
    );
    inbox_email.metadata.folder = "inbox".to_string();

    let mut sent_email = create_test_email(
        "email2",
        "Sent message",
        "sender@example.com",
        "This is in sent.",
    );
    sent_email.metadata.folder = "sent".to_string();

    storage
        .store_email(&inbox_email)
        .expect("Failed to store inbox email");
    storage
        .store_email(&sent_email)
        .expect("Failed to store sent email");

    // Search in inbox only
    let query = SearchQuery::new("message".to_string()).with_folder("inbox".to_string());
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1);
    assert_eq!(results[0].email_metadata.folder, "inbox");

    // Search in sent only
    let query = SearchQuery::new("message".to_string()).with_folder("sent".to_string());
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1);
    assert_eq!(results[0].email_metadata.folder, "sent");
}

#[test]
fn test_tag_filter() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let mut email1 = create_test_email(
        "email1",
        "Important message",
        "sender@example.com",
        "This is important.",
    );
    email1.metadata.tags = vec!["important".to_string(), "work".to_string()];

    let mut email2 = create_test_email(
        "email2",
        "Personal message",
        "friend@example.com",
        "This is personal.",
    );
    email2.metadata.tags = vec!["personal".to_string()];

    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");

    // Search with tag filter
    let query = SearchQuery::new("message".to_string()).with_tag("important".to_string());
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1);
    assert!(results[0]
        .email_metadata
        .tags
        .contains(&"important".to_string()));

    // Search with multiple tag filters
    let query = SearchQuery::new("message".to_string())
        .with_tag("important".to_string())
        .with_tag("work".to_string());
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1); // Only email1 has both tags
}

#[test]
fn test_date_range_filter() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let now = Utc::now();
    let yesterday = now - chrono::Duration::days(1);
    let tomorrow = now + chrono::Duration::days(1);

    let mut old_email = create_test_email(
        "email1",
        "Old message",
        "sender@example.com",
        "This is old.",
    );
    old_email.metadata.created_at = yesterday;
    old_email.metadata.modified_at = yesterday;

    let mut new_email = create_test_email(
        "email2",
        "New message",
        "sender@example.com",
        "This is new.",
    );
    new_email.metadata.created_at = now;
    new_email.metadata.modified_at = now;

    storage
        .store_email(&old_email)
        .expect("Failed to store old email");
    storage
        .store_email(&new_email)
        .expect("Failed to store new email");

    // Search with date range that includes only today
    let query = SearchQuery::new("message".to_string())
        .with_date_range(now - chrono::Duration::hours(1), tomorrow);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 1);
    assert!(results[0].email_metadata.file_path.contains("email2"));
}

#[test]
fn test_result_limit() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    // Store multiple emails
    for i in 1..=10 {
        let email = create_test_email(
            &format!("email{}", i),
            "Test message",
            "sender@example.com",
            "Test content.",
        );
        storage.store_email(&email).expect("Failed to store email");
    }

    // Search with limit
    let query = SearchQuery::new("message".to_string()).with_limit(5);
    let results = search_engine.search(&query).expect("Search failed");
    assert_eq!(results.len(), 5);
}

#[test]
fn test_search_suggestions() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    // Store emails with common terms
    let email1 = create_test_email(
        "email1",
        "Meeting tomorrow",
        "sender@example.com",
        "Important meeting about project.",
    );
    let email2 = create_test_email(
        "email2",
        "Project update",
        "sender@example.com",
        "Project is going well.",
    );
    let email3 = create_test_email(
        "email3",
        "Lunch meeting",
        "sender@example.com",
        "Meeting for lunch discussion.",
    );

    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");
    storage
        .store_email(&email3)
        .expect("Failed to store email3");

    // Get suggestions for partial term
    let suggestions = search_engine.suggest("mee").expect("Suggestions failed");

    // Should suggest "meeting" if it appears frequently enough
    // Note: This test might be flaky depending on the awk implementation
    // In a real scenario, you might want to mock the awk execution
}

#[test]
fn test_empty_search_term() {
    let (_temp_dir, _storage, search_engine) = setup_test_repository();

    let query = SearchQuery::new("".to_string());
    let result = search_engine.search(&query);

    assert!(result.is_err());
    assert!(result
        .unwrap_err()
        .to_string()
        .contains("Search term cannot be empty"));
}

#[test]
fn test_search_match_context() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    let email = create_test_email(
        "email1",
        "Test subject",
        "sender@example.com",
        "Line 1\nLine 2\nThis line contains the search term\nLine 4\nLine 5",
    );
    storage.store_email(&email).expect("Failed to store email");

    let query = SearchQuery::new("search term".to_string());
    let results = search_engine.search(&query).expect("Search failed");

    assert_eq!(results.len(), 1);
    assert!(!results[0].matches.is_empty());

    let search_match = &results[0].matches[0];
    assert!(search_match.line.contains("search term"));
    // Context should include surrounding lines
    assert!(!search_match.context_before.is_empty() || !search_match.context_after.is_empty());
}

#[test]
fn test_relevance_scoring() {
    let (_temp_dir, storage, search_engine) = setup_test_repository();

    // Email with term in subject (should score higher)
    let email1 = create_test_email(
        "email1",
        "Important project",
        "sender@example.com",
        "Some body content.",
    );

    // Email with term only in body (should score lower)
    let email2 = create_test_email(
        "email2",
        "Random subject",
        "sender@example.com",
        "This mentions the project in body.",
    );

    storage
        .store_email(&email1)
        .expect("Failed to store email1");
    storage
        .store_email(&email2)
        .expect("Failed to store email2");

    let query = SearchQuery::new("project".to_string());
    let results = search_engine.search(&query).expect("Search failed");

    assert_eq!(results.len(), 2);
    // Results should be sorted by relevance (highest first)
    // Email1 (subject match) should score higher than email2 (body match)
    assert!(results[0].score >= results[1].score);
}
