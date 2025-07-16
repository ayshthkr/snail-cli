//! Comprehensive integration tests for end-to-end workflows
//! Tests complete email workflows: compose → send → receive → filter → read

use chrono::Utc;
use git_mail::error::Result;
use git_mail::filter::{FilterEngine, MutableFilterEngine};
use git_mail::git_storage::{DefaultGitStorage, GitStorage};
use git_mail::models::FilterScript;
use git_mail::models::{Account, Email, IncomingConfig, OutgoingConfig};
use git_mail::organization::{DefaultOrganizationManager, OrganizationManager};
use git_mail::search::{CommandLineSearchEngine, SearchEngine, SearchQuery};
use tempfile::TempDir;

/// Test fixture for integration tests
struct IntegrationTestFixture {
    temp_dir: TempDir,
    storage: DefaultGitStorage,
    organization: DefaultOrganizationManager<DefaultGitStorage>,
    search_engine: CommandLineSearchEngine<DefaultGitStorage>,
    filter_engine: MutableFilterEngine,
    test_account: Account,
}

impl IntegrationTestFixture {
    fn new() -> Result<Self> {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let repo_path = temp_dir.path().to_str().unwrap().to_string();

        // Initialize storage
        let storage = DefaultGitStorage::new(repo_path.clone());
        storage.initialize_repository(&repo_path)?;

        // Initialize organization manager
        let organization = DefaultOrganizationManager::new(storage.clone(), repo_path.clone());

        // Initialize search engine
        let search_engine = CommandLineSearchEngine::new(storage.clone(), repo_path.clone());

        // Initialize filter engine
        let filter_engine = MutableFilterEngine::new();

        // Create test account configuration
        let test_account = Account {
            name: "test_account".to_string(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "localhost".to_string(),
                port: 1143,
                username: "test@example.com".to_string(),
                password: "test_password".to_string(),
                ssl: false,
            },
            outgoing: OutgoingConfig {
                server: "localhost".to_string(),
                port: 1025,
                username: "test@example.com".to_string(),
                password: "test_password".to_string(),
                ssl: false,
            },
            filters: vec!["spam_filter.sh".to_string()],
        };

        Ok(Self {
            temp_dir,
            storage,
            organization,
            search_engine,
            filter_engine,
            test_account,
        })
    }

    fn create_test_email(&self, subject: &str, body: &str) -> Email {
        let mut email = Email::new(self.test_account.email.clone());
        email
            .headers
            .insert("Subject".to_string(), subject.to_string());
        email
            .headers
            .insert("From".to_string(), self.test_account.email.clone());
        email
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        email.body.content = body.to_string();
        email.metadata.created_at = Utc::now();
        email
    }
}

#[tokio::test]
async fn test_complete_email_workflow() {
    let fixture = IntegrationTestFixture::new().expect("Failed to create fixture");

    // Step 1: Compose email
    let email = fixture.create_test_email(
        "Test Email Subject",
        "This is a test email body with some content.",
    );

    // Step 2: Store email (simulating composition)
    let email_id = fixture
        .storage
        .store_email(&email)
        .expect("Failed to store email");

    // Step 3: Retrieve email (simulating reading)
    let retrieved_email = fixture
        .storage
        .retrieve_email(&email_id)
        .expect("Failed to retrieve email");
    assert_eq!(
        retrieved_email.headers.get("Subject").unwrap(),
        "Test Email Subject"
    );
    assert_eq!(
        retrieved_email.body.content,
        "This is a test email body with some content."
    );

    // Step 4: Move email to folder
    fixture
        .organization
        .create_folder("archive", None)
        .expect("Failed to create folder");
    fixture
        .organization
        .move_email(&email_id, "archive")
        .expect("Failed to move email");
    let moved_email = fixture
        .storage
        .retrieve_email(&email_id)
        .expect("Failed to retrieve moved email");
    assert_eq!(moved_email.metadata.folder, "archive");

    // Step 5: Search for email
    let query = SearchQuery::new("test email".to_string());
    let search_results = fixture
        .search_engine
        .search(&query)
        .expect("Failed to search");
    assert_eq!(search_results.len(), 1);
    assert!(search_results[0]
        .email_metadata
        .file_path
        .contains(&email_id));
}

#[tokio::test]
async fn test_multi_account_workflow() {
    let fixture = IntegrationTestFixture::new().expect("Failed to create fixture");

    // Create emails for different accounts
    let personal_email = fixture.create_test_email("Personal Email", "Personal content");
    let mut work_email = Email::new("work@company.com".to_string());
    work_email
        .headers
        .insert("Subject".to_string(), "Work Email".to_string());
    work_email
        .headers
        .insert("From".to_string(), "work@company.com".to_string());
    work_email.body.content = "Work content".to_string();

    // Store emails
    let personal_id = fixture
        .storage
        .store_email(&personal_email)
        .expect("Failed to store personal email");
    let work_id = fixture
        .storage
        .store_email(&work_email)
        .expect("Failed to store work email");

    // Verify account separation
    let personal_retrieved = fixture
        .storage
        .retrieve_email(&personal_id)
        .expect("Failed to retrieve personal email");
    let work_retrieved = fixture
        .storage
        .retrieve_email(&work_id)
        .expect("Failed to retrieve work email");

    assert_eq!(personal_retrieved.account, fixture.test_account.email);
    assert_eq!(work_retrieved.account, "work@company.com");

    // List all emails and verify they exist
    let all_emails = fixture
        .storage
        .list_emails(None)
        .expect("Failed to list emails");
    assert_eq!(all_emails.len(), 2);
}

#[tokio::test]
async fn test_filter_workflow() {
    let mut fixture = IntegrationTestFixture::new().expect("Failed to create fixture");

    // Create filter script
    let filter_script = FilterScript {
        name: "spam_filter".to_string(),
        path: "spam_filter.sh".to_string(),
        order: 1,
        enabled: true,
    };

    // Register filter
    fixture
        .filter_engine
        .register_filter(filter_script)
        .expect("Failed to register filter");

    // Create test emails
    let spam_email = fixture.create_test_email("SPAM: Get rich quick!", "This is spam content");
    let urgent_email =
        fixture.create_test_email("Important Notice", "This is urgent please respond");
    let normal_email = fixture.create_test_email("Normal Email", "This is normal content");

    // Apply filters to emails
    let spam_result = fixture
        .filter_engine
        .execute_filters(&spam_email)
        .expect("Failed to filter spam email");
    let urgent_result = fixture
        .filter_engine
        .execute_filters(&urgent_email)
        .expect("Failed to filter urgent email");
    let normal_result = fixture
        .filter_engine
        .execute_filters(&normal_email)
        .expect("Failed to filter normal email");

    // Verify filter results
    assert!(spam_result.new_folder.is_some());
    assert!(spam_result.add_tags.contains(&"spam".to_string()));

    assert!(urgent_result.new_folder.is_some());
    assert!(urgent_result.add_tags.contains(&"urgent".to_string()));

    assert!(!normal_result.modified);
}

#[tokio::test]
async fn test_organization_and_search_workflow() {
    let fixture = IntegrationTestFixture::new().expect("Failed to create fixture");

    // Create multiple emails
    let email1 = fixture.create_test_email("Important Meeting", "Meeting about project deadline");
    let email2 = fixture.create_test_email("Lunch Plans", "Want to grab lunch tomorrow?");
    let email3 = fixture.create_test_email("Project Update", "The project is progressing well");

    // Store emails
    let id1 = fixture
        .storage
        .store_email(&email1)
        .expect("Failed to store email 1");
    let id2 = fixture
        .storage
        .store_email(&email2)
        .expect("Failed to store email 2");
    let id3 = fixture
        .storage
        .store_email(&email3)
        .expect("Failed to store email 3");

    // Create folders
    fixture
        .organization
        .create_folder("work", None)
        .expect("Failed to create work folder");
    fixture
        .organization
        .create_folder("personal", None)
        .expect("Failed to create personal folder");

    // Move emails to appropriate folders
    fixture
        .organization
        .move_email(&id1, "work")
        .expect("Failed to move email 1");
    fixture
        .organization
        .move_email(&id3, "work")
        .expect("Failed to move email 3");
    fixture
        .organization
        .move_email(&id2, "personal")
        .expect("Failed to move email 2");

    // Verify folder organization
    let work_info = fixture
        .organization
        .get_folder_info("work")
        .expect("Failed to get work folder info");
    let personal_info = fixture
        .organization
        .get_folder_info("personal")
        .expect("Failed to get personal folder info");

    assert_eq!(work_info.email_count, 2);
    assert_eq!(personal_info.email_count, 1);

    // Test search functionality
    let project_query = SearchQuery::new("project".to_string());
    let project_results = fixture
        .search_engine
        .search(&project_query)
        .expect("Failed to search for project");
    assert_eq!(project_results.len(), 2); // Should find both project-related emails

    let meeting_query = SearchQuery::new("meeting".to_string());
    let meeting_results = fixture
        .search_engine
        .search(&meeting_query)
        .expect("Failed to search for meeting");
    assert_eq!(meeting_results.len(), 1); // Should find the meeting email
}

#[tokio::test]
async fn test_git_storage_workflow() {
    let fixture = IntegrationTestFixture::new().expect("Failed to create fixture");

    // Create and store initial email
    let email = fixture.create_test_email("Version Test", "Initial content");
    let email_id = fixture
        .storage
        .store_email(&email)
        .expect("Failed to store email");

    // Retrieve and verify
    let retrieved = fixture
        .storage
        .retrieve_email(&email_id)
        .expect("Failed to retrieve email");
    assert_eq!(retrieved.body.content, "Initial content");

    // Test Git history functionality
    let history = fixture
        .storage
        .get_history(&email_id)
        .expect("Failed to get history");
    assert!(!history.is_empty()); // Should have at least one commit
}

#[tokio::test]
async fn test_attachment_workflow() {
    let fixture = IntegrationTestFixture::new().expect("Failed to create fixture");

    // Create email with attachments
    let mut email = fixture.create_test_email("Email with Attachments", "See attached files");

    // Add test attachments
    email.attachments.push(git_mail::models::Attachment {
        filename: "document.txt".to_string(),
        content_type: "text/plain".to_string(),
        size: 23,
        file_path: "attachments/document.txt".to_string(),
    });

    email.attachments.push(git_mail::models::Attachment {
        filename: "image.jpg".to_string(),
        content_type: "image/jpeg".to_string(),
        size: 4,
        file_path: "attachments/image.jpg".to_string(),
    });

    // Store email with attachments
    let email_id = fixture
        .storage
        .store_email(&email)
        .expect("Failed to store email with attachments");

    // Retrieve and verify attachments
    let retrieved = fixture
        .storage
        .retrieve_email(&email_id)
        .expect("Failed to retrieve email");
    assert_eq!(retrieved.attachments.len(), 2);

    let text_attachment = retrieved
        .attachments
        .iter()
        .find(|a| a.filename == "document.txt")
        .unwrap();
    assert_eq!(text_attachment.content_type, "text/plain");
    assert_eq!(text_attachment.size, 23);

    let image_attachment = retrieved
        .attachments
        .iter()
        .find(|a| a.filename == "image.jpg")
        .unwrap();
    assert_eq!(image_attachment.content_type, "image/jpeg");
    assert_eq!(image_attachment.size, 4);
}
