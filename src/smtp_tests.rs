//! SMTP client tests

#[cfg(test)]
mod tests {
    use crate::models::{Account, Email, EmailBody, IncomingConfig, OutgoingConfig};
    use crate::sync::{DefaultSyncEngine, QueuedEmail, SmtpClient, SyncEngine};
    use chrono::Utc;
    use std::collections::HashMap;
    use std::time::Duration;
    use tempfile::tempdir;
    use tokio::fs;

    fn create_test_account() -> Account {
        Account {
            name: "test_account".to_string(),
            email: "test@example.com".to_string(),
            display_name: "Test User".to_string(),
            incoming: IncomingConfig {
                protocol: "imap".to_string(),
                server: "imap.example.com".to_string(),
                port: 993,
                username: "test@example.com".to_string(),
                password: "test_password".to_string(),
                ssl: true,
            },
            outgoing: OutgoingConfig {
                server: "smtp.example.com".to_string(),
                port: 587,
                username: "test@example.com".to_string(),
                password: "test_password".to_string(),
                ssl: true,
            },
            filters: vec![],
        }
    }

    fn create_test_email() -> Email {
        let mut email = Email::new("test_account".to_string());
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        email
            .headers
            .insert("Subject".to_string(), "Test Email".to_string());
        email.body = EmailBody {
            content_type: "text/plain".to_string(),
            content: "This is a test email body.".to_string(),
            html_content: None,
        };
        email
    }

    #[test]
    fn test_smtp_client_creation() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file.clone());

        assert_eq!(client.queue_file, queue_file);
        assert_eq!(client.max_retries, 5);
        assert_eq!(client.base_retry_delay, 30);
        assert!(client.queue.is_empty());
    }

    #[tokio::test]
    async fn test_queue_email() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file.clone());

        let email = create_test_email();
        let account_name = "test_account".to_string();

        let result = client
            .queue_email(email.clone(), account_name.clone())
            .await;
        assert!(result.is_ok());

        // Check that email was added to queue
        assert_eq!(client.queue.len(), 1);
        let queued_email = &client.queue[0];
        assert_eq!(queued_email.email.id, email.id);
        assert_eq!(queued_email.account_name, account_name);
        assert_eq!(queued_email.retry_count, 0);

        // Check that queue file was created
        assert!(queue_file.exists());
    }

    #[tokio::test]
    async fn test_load_empty_queue() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("nonexistent.json");
        let mut client = SmtpClient::new(queue_file);

        let result = client.load_queue().await;
        assert!(result.is_ok());
        assert!(client.queue.is_empty());
    }

    #[tokio::test]
    async fn test_save_and_load_queue() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file.clone());

        // Queue an email
        let email = create_test_email();
        let account_name = "test_account".to_string();
        client
            .queue_email(email.clone(), account_name.clone())
            .await
            .unwrap();

        // Create a new client and load the queue
        let mut client2 = SmtpClient::new(queue_file);
        client2.load_queue().await.unwrap();

        assert_eq!(client2.queue.len(), 1);
        let queued_email = &client2.queue[0];
        assert_eq!(queued_email.email.id, email.id);
        assert_eq!(queued_email.account_name, account_name);
    }

    #[tokio::test]
    async fn test_clear_queue() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file.clone());

        // Queue an email
        let email = create_test_email();
        client
            .queue_email(email, "test_account".to_string())
            .await
            .unwrap();
        assert_eq!(client.queue.len(), 1);

        // Clear the queue
        client.clear_queue().await.unwrap();
        assert!(client.queue.is_empty());

        // Verify queue file is updated
        let content = fs::read_to_string(&queue_file).await.unwrap();
        let queued_emails: Vec<QueuedEmail> = serde_json::from_str(&content).unwrap();
        assert!(queued_emails.is_empty());
    }

    #[test]
    fn test_queue_status() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file);

        // Initially empty
        let (total, ready) = client.queue_status();
        assert_eq!(total, 0);
        assert_eq!(ready, 0);

        // Add a queued email that's ready now
        let email = create_test_email();
        let queued_email = QueuedEmail {
            email,
            account_name: "test_account".to_string(),
            retry_count: 0,
            next_retry: Utc::now() - chrono::Duration::seconds(1), // Ready now
            queued_at: Utc::now(),
        };
        client.queue.push_back(queued_email);

        let (total, ready) = client.queue_status();
        assert_eq!(total, 1);
        assert_eq!(ready, 1);

        // Add another email that's not ready yet
        let email2 = create_test_email();
        let queued_email2 = QueuedEmail {
            email: email2,
            account_name: "test_account".to_string(),
            retry_count: 1,
            next_retry: Utc::now() + chrono::Duration::hours(1), // Not ready yet
            queued_at: Utc::now(),
        };
        client.queue.push_back(queued_email2);

        let (total, ready) = client.queue_status();
        assert_eq!(total, 2);
        assert_eq!(ready, 1); // Only one is ready
    }

    #[test]
    fn test_email_to_message_basic() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let account = create_test_account();
        let email = create_test_email();

        let result = client.email_to_message(&email, &account);
        assert!(result.is_ok());
    }

    #[test]
    fn test_email_to_message_missing_to() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let account = create_test_account();
        let mut email = create_test_email();
        email.headers.remove("To"); // Remove To header

        let result = client.email_to_message(&email, &account);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("No To address found"));
    }

    #[test]
    fn test_email_to_message_with_cc_bcc() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let account = create_test_account();
        let mut email = create_test_email();

        email
            .headers
            .insert("Cc".to_string(), "cc@example.com".to_string());
        email
            .headers
            .insert("Bcc".to_string(), "bcc@example.com".to_string());

        let result = client.email_to_message(&email, &account);
        assert!(result.is_ok());
    }

    #[test]
    fn test_email_to_message_with_html() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let account = create_test_account();
        let mut email = create_test_email();

        email.body.html_content = Some("<html><body><p>HTML content</p></body></html>".to_string());

        let result = client.email_to_message(&email, &account);
        assert!(result.is_ok());
    }

    #[test]
    fn test_create_transport_with_ssl() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let account = create_test_account();

        // This should succeed in creating the transport configuration
        // The actual connection would fail, but transport creation should work
        let result = client.create_transport(&account);
        // Transport creation should succeed with valid configuration
        assert!(result.is_ok());
    }

    #[test]
    fn test_create_transport_without_ssl() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let mut account = create_test_account();
        account.outgoing.ssl = false;

        let result = client.create_transport(&account);
        // Transport creation should succeed with valid configuration, even without SSL
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_process_empty_queue() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file);
        let accounts = HashMap::new();

        let result = client.process_queue(&accounts).await;
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 0);
    }

    #[tokio::test]
    async fn test_process_queue_missing_account() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file);
        let accounts = HashMap::new();

        // Add an email to the queue
        let email = create_test_email();
        client
            .queue_email(email, "nonexistent_account".to_string())
            .await
            .unwrap();

        let result = client.process_queue(&accounts).await;
        assert!(result.is_ok());
        assert_eq!(result.unwrap(), 0); // No emails sent
        assert!(client.queue.is_empty()); // Email should be removed from queue
    }

    #[test]
    fn test_exponential_backoff_calculation() {
        let base_delay = 30u64;

        // Test exponential backoff calculation
        assert_eq!(base_delay * (2_u64.pow(0)), 30); // First retry: 30 seconds
        assert_eq!(base_delay * (2_u64.pow(1)), 60); // Second retry: 60 seconds
        assert_eq!(base_delay * (2_u64.pow(2)), 120); // Third retry: 120 seconds
        assert_eq!(base_delay * (2_u64.pow(3)), 240); // Fourth retry: 240 seconds
        assert_eq!(base_delay * (2_u64.pow(4)), 480); // Fifth retry: 480 seconds
    }

    #[test]
    fn test_queued_email_serialization() {
        let email = create_test_email();
        let queued_email = QueuedEmail {
            email,
            account_name: "test_account".to_string(),
            retry_count: 2,
            next_retry: Utc::now(),
            queued_at: Utc::now(),
        };

        // Test JSON serialization
        let json = serde_json::to_string(&queued_email).unwrap();
        let deserialized: QueuedEmail = serde_json::from_str(&json).unwrap();

        assert_eq!(queued_email.email.id, deserialized.email.id);
        assert_eq!(queued_email.account_name, deserialized.account_name);
        assert_eq!(queued_email.retry_count, deserialized.retry_count);
    }

    #[test]
    fn test_sync_engine_creation_with_queue() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file.clone());

        assert_eq!(engine.smtp_client.queue_file, queue_file);
        assert_eq!(engine.imap_client.timeout, Duration::from_secs(30));
    }

    #[test]
    fn test_sync_engine_default_queue() {
        let engine = DefaultSyncEngine::new_with_default_queue();

        // Should create engine without panicking
        assert_eq!(engine.imap_client.timeout, Duration::from_secs(30));

        // Queue file should be in home directory
        let expected_path = dirs::home_dir()
            .unwrap_or_else(|| std::path::PathBuf::from("."))
            .join(".git-mail")
            .join("queue.json");
        assert_eq!(engine.smtp_client.queue_file, expected_path);
    }

    #[tokio::test]
    async fn test_send_email_invalid_account() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file);

        let mut account = create_test_account();
        account.name = String::new(); // Invalid account
        let email = create_test_email();

        let result = engine.send_email(&email, &account).await;
        assert!(result.is_err());
    }

    #[test]
    fn test_smtp_client_queue_operations() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file);

        // Test queue operations
        let email1 = create_test_email();
        let email2 = create_test_email();

        let queued1 = QueuedEmail {
            email: email1,
            account_name: "account1".to_string(),
            retry_count: 0,
            next_retry: Utc::now(),
            queued_at: Utc::now(),
        };

        let queued2 = QueuedEmail {
            email: email2,
            account_name: "account2".to_string(),
            retry_count: 1,
            next_retry: Utc::now() + chrono::Duration::hours(1),
            queued_at: Utc::now(),
        };

        client.queue.push_back(queued1);
        client.queue.push_back(queued2);

        assert_eq!(client.queue.len(), 2);

        // Test queue status
        let (total, ready) = client.queue_status();
        assert_eq!(total, 2);
        assert_eq!(ready, 1); // Only one is ready for retry
    }

    #[tokio::test]
    async fn test_queue_persistence() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");

        // Create client and queue an email
        {
            let mut client = SmtpClient::new(queue_file.clone());
            let email = create_test_email();
            client
                .queue_email(email, "test_account".to_string())
                .await
                .unwrap();
        }

        // Create new client and verify email is loaded
        {
            let mut client = SmtpClient::new(queue_file);
            client.load_queue().await.unwrap();
            assert_eq!(client.queue.len(), 1);
        }
    }

    #[test]
    fn test_invalid_email_addresses() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let client = SmtpClient::new(queue_file);
        let account = create_test_account();
        let mut email = create_test_email();

        // Test invalid From address
        email
            .headers
            .insert("From".to_string(), "invalid-email".to_string());
        let result = client.email_to_message(&email, &account);
        assert!(result.is_err());

        // Test invalid To address
        email
            .headers
            .insert("From".to_string(), "valid@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "invalid-email".to_string());
        let result = client.email_to_message(&email, &account);
        assert!(result.is_err());
    }

    #[tokio::test]
    async fn test_queue_file_creation() {
        let temp_dir = tempdir().unwrap();
        let nested_path = temp_dir.path().join("nested").join("path");
        let queue_file = nested_path.join("queue.json");
        let mut client = SmtpClient::new(queue_file.clone());

        // Queue an email - this should create the nested directory structure
        let email = create_test_email();
        let result = client.queue_email(email, "test_account".to_string()).await;
        assert!(result.is_ok());

        // Verify the file was created
        assert!(queue_file.exists());
        assert!(nested_path.exists());
    }

    // Integration test placeholder for real SMTP server
    #[ignore] // Ignored by default since it requires external dependencies
    #[tokio::test]
    async fn test_real_smtp_sending() {
        // This test would send to a real SMTP server
        // It's ignored by default and should only be run with proper test credentials

        // Example of how you might set up integration tests:
        // 1. Set up a test SMTP server (like MailHog or similar)
        // 2. Create test emails
        // 3. Test the full send workflow
        // 4. Verify emails are sent correctly

        assert!(true);
    }

    // Performance test placeholder
    #[ignore]
    #[tokio::test]
    async fn test_large_queue_performance() {
        let temp_dir = tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut client = SmtpClient::new(queue_file);

        // Test queuing many emails
        let start = std::time::Instant::now();
        for i in 0..1000 {
            let email = create_test_email();
            client
                .queue_email(email, format!("account_{}", i))
                .await
                .unwrap();
        }
        let duration = start.elapsed();

        // Should be able to queue 1000 emails in reasonable time
        assert!(duration.as_secs() < 10);
        assert_eq!(client.queue.len(), 1000);
    }
}
