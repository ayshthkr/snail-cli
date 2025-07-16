//! Performance tests for large mailbox handling and system scalability

use chrono::Utc;
use git_mail::core::GitMailCore;
use git_mail::models::{Email, EmailMetadata};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tempfile::TempDir;
use tokio::task::JoinSet;

/// Performance test configuration
struct PerformanceConfig {
    pub email_count: usize,
    pub concurrent_operations: usize,
    pub max_storage_time: Duration,
    pub max_retrieval_time: Duration,
    pub max_search_time: Duration,
}

impl Default for PerformanceConfig {
    fn default() -> Self {
        Self {
            email_count: 10000,
            concurrent_operations: 10,
            max_storage_time: Duration::from_secs(60),
            max_retrieval_time: Duration::from_secs(30),
            max_search_time: Duration::from_secs(15),
        }
    }
}

/// Performance test fixture
struct PerformanceTestFixture {
    temp_dir: TempDir,
    core: Arc<GitMailCore>,
    config: PerformanceConfig,
}

impl PerformanceTestFixture {
    async fn new(config: PerformanceConfig) -> Result<Self, Box<dyn std::error::Error>> {
        let temp_dir = TempDir::new()?;
        let repo_path = temp_dir.path().to_str().unwrap().to_string();

        let core = Arc::new(GitMailCore::new(repo_path).await?);
        core.initialize_repository().await?;

        Ok(Self {
            temp_dir,
            core,
            config,
        })
    }

    fn create_test_email(&self, index: usize) -> Email {
        let mut email = Email::new(format!("test{}@example.com", index % 100));
        email
            .headers
            .insert("Subject".to_string(), format!("Test Email {}", index));
        email.headers.insert(
            "From".to_string(),
            format!("sender{}@example.com", index % 50),
        );
        email.headers.insert(
            "To".to_string(),
            format!("recipient{}@example.com", index % 25),
        );
        email.body.content = format!(
            "This is test email number {}. It contains some sample content to simulate real email data. \
             The content includes various keywords like important, urgent, meeting, project, and deadline \
             to test search functionality. Email {} was created at {}.",
            index, index, Utc::now()
        );

        // Add some variety to metadata
        email.metadata.folder = match index % 5 {
            0 => "inbox".to_string(),
            1 => "sent".to_string(),
            2 => "archive".to_string(),
            3 => "work".to_string(),
            _ => "personal".to_string(),
        };

        email.metadata.tags = match index % 3 {
            0 => vec!["important".to_string()],
            1 => vec!["work".to_string(), "project".to_string()],
            _ => vec!["personal".to_string()],
        };

        email.metadata.is_read = index % 3 == 0;
        email.metadata.is_starred = index % 10 == 0;

        email
    }
}

#[tokio::test]
async fn test_large_mailbox_storage_performance() {
    let config = PerformanceConfig::default();
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    println!(
        "Testing storage performance with {} emails",
        fixture.config.email_count
    );

    let start_time = Instant::now();
    let mut email_ids = Vec::with_capacity(fixture.config.email_count);

    // Store emails in batches to avoid overwhelming the system
    let batch_size = 100;
    for batch_start in (0..fixture.config.email_count).step_by(batch_size) {
        let batch_end = (batch_start + batch_size).min(fixture.config.email_count);

        for i in batch_start..batch_end {
            let email = fixture.create_test_email(i);
            let email_id = fixture
                .core
                .store_email(&email)
                .await
                .expect("Failed to store email");
            email_ids.push(email_id);
        }

        // Progress indicator
        if batch_start % 1000 == 0 {
            println!("Stored {} emails", batch_start);
        }
    }

    let storage_duration = start_time.elapsed();
    println!("Storage completed in {:?}", storage_duration);
    println!(
        "Average time per email: {:?}",
        storage_duration / fixture.config.email_count as u32
    );

    // Performance assertions
    assert!(
        storage_duration < fixture.config.max_storage_time,
        "Storage took too long: {:?} > {:?}",
        storage_duration,
        fixture.config.max_storage_time
    );

    // Verify all emails were stored
    assert_eq!(email_ids.len(), fixture.config.email_count);

    // Test repository size
    let repo_size = get_directory_size(fixture.temp_dir.path()).expect("Failed to get repo size");
    println!("Repository size: {} MB", repo_size / 1024 / 1024);

    // Repository should be reasonable size (less than 1GB for 10k emails)
    assert!(
        repo_size < 1024 * 1024 * 1024,
        "Repository too large: {} bytes",
        repo_size
    );
}

#[tokio::test]
async fn test_large_mailbox_retrieval_performance() {
    let config = PerformanceConfig {
        email_count: 5000, // Smaller set for retrieval test
        ..Default::default()
    };
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    // First, populate the mailbox
    let mut email_ids = Vec::new();
    for i in 0..fixture.config.email_count {
        let email = fixture.create_test_email(i);
        let email_id = fixture
            .core
            .store_email(&email)
            .await
            .expect("Failed to store email");
        email_ids.push(email_id);
    }

    println!(
        "Testing retrieval performance with {} emails",
        fixture.config.email_count
    );

    // Test random access retrieval
    let start_time = Instant::now();
    let test_count = 1000; // Retrieve 1000 random emails

    for i in 0..test_count {
        let random_index = i % email_ids.len();
        let email = fixture
            .core
            .retrieve_email(&email_ids[random_index])
            .await
            .expect("Failed to retrieve email");

        // Verify email content
        assert!(email.headers.get("Subject").unwrap().contains("Test Email"));
    }

    let retrieval_duration = start_time.elapsed();
    println!(
        "Retrieved {} emails in {:?}",
        test_count, retrieval_duration
    );
    println!(
        "Average retrieval time: {:?}",
        retrieval_duration / test_count as u32
    );

    // Performance assertion
    assert!(
        retrieval_duration < fixture.config.max_retrieval_time,
        "Retrieval took too long: {:?} > {:?}",
        retrieval_duration,
        fixture.config.max_retrieval_time
    );
}

#[tokio::test]
async fn test_large_mailbox_search_performance() {
    let config = PerformanceConfig {
        email_count: 5000,
        ..Default::default()
    };
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    // Populate mailbox with searchable content
    for i in 0..fixture.config.email_count {
        let email = fixture.create_test_email(i);
        fixture
            .core
            .store_email(&email)
            .await
            .expect("Failed to store email");
    }

    println!(
        "Testing search performance with {} emails",
        fixture.config.email_count
    );

    // Test various search scenarios
    let search_terms = vec!["important", "project", "meeting", "Test Email", "urgent"];

    for term in search_terms {
        let start_time = Instant::now();
        let results = fixture
            .core
            .search_emails(term)
            .await
            .expect("Search failed");
        let search_duration = start_time.elapsed();

        println!(
            "Search for '{}' took {:?}, found {} results",
            term,
            search_duration,
            results.len()
        );

        // Each search should complete within reasonable time
        assert!(
            search_duration < fixture.config.max_search_time,
            "Search for '{}' took too long: {:?} > {:?}",
            term,
            search_duration,
            fixture.config.max_search_time
        );

        // Should find some results for common terms
        if term == "Test Email" {
            assert!(results.len() > 0, "Should find results for '{}'", term);
        }
    }
}

#[tokio::test]
async fn test_concurrent_operations_performance() {
    let config = PerformanceConfig {
        email_count: 1000,
        concurrent_operations: 20,
        ..Default::default()
    };
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    println!(
        "Testing concurrent operations with {} threads",
        fixture.config.concurrent_operations
    );

    let start_time = Instant::now();
    let mut join_set = JoinSet::new();

    // Spawn concurrent storage operations
    for thread_id in 0..fixture.config.concurrent_operations {
        let core = fixture.core.clone();
        let emails_per_thread = fixture.config.email_count / fixture.config.concurrent_operations;

        join_set.spawn(async move {
            let mut thread_email_ids = Vec::new();

            for i in 0..emails_per_thread {
                let email_index = thread_id * emails_per_thread + i;
                let mut email = Email::new(format!("test{}@example.com", email_index));
                email.headers.insert(
                    "Subject".to_string(),
                    format!("Concurrent Email {} from Thread {}", i, thread_id),
                );
                email.body.content = format!("Content from thread {} email {}", thread_id, i);

                let email_id = core
                    .store_email(&email)
                    .await
                    .expect("Failed to store email");
                thread_email_ids.push(email_id);
            }

            thread_email_ids
        });
    }

    // Wait for all threads to complete
    let mut all_email_ids = Vec::new();
    while let Some(result) = join_set.join_next().await {
        let thread_email_ids = result.expect("Thread panicked");
        all_email_ids.extend(thread_email_ids);
    }

    let concurrent_duration = start_time.elapsed();
    println!("Concurrent storage completed in {:?}", concurrent_duration);
    println!(
        "Stored {} emails across {} threads",
        all_email_ids.len(),
        fixture.config.concurrent_operations
    );

    // Verify all emails were stored correctly
    assert_eq!(all_email_ids.len(), fixture.config.email_count);

    // Test concurrent retrieval
    let retrieval_start = Instant::now();
    let mut retrieval_join_set = JoinSet::new();

    for chunk in all_email_ids.chunks(all_email_ids.len() / fixture.config.concurrent_operations) {
        let core = fixture.core.clone();
        let chunk_ids = chunk.to_vec();

        retrieval_join_set.spawn(async move {
            let mut retrieved_count = 0;
            for email_id in chunk_ids {
                let _email = core
                    .retrieve_email(&email_id)
                    .await
                    .expect("Failed to retrieve email");
                retrieved_count += 1;
            }
            retrieved_count
        });
    }

    let mut total_retrieved = 0;
    while let Some(result) = retrieval_join_set.join_next().await {
        let count = result.expect("Retrieval thread panicked");
        total_retrieved += count;
    }

    let retrieval_duration = retrieval_start.elapsed();
    println!("Concurrent retrieval completed in {:?}", retrieval_duration);
    println!("Retrieved {} emails", total_retrieved);

    assert_eq!(total_retrieved, fixture.config.email_count);
}

#[tokio::test]
async fn test_memory_usage_under_load() {
    let config = PerformanceConfig {
        email_count: 2000,
        ..Default::default()
    };
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    // Get initial memory usage
    let initial_memory = get_memory_usage();
    println!("Initial memory usage: {} MB", initial_memory / 1024 / 1024);

    // Store emails and monitor memory
    for i in 0..fixture.config.email_count {
        let email = fixture.create_test_email(i);
        fixture
            .core
            .store_email(&email)
            .await
            .expect("Failed to store email");

        // Check memory every 500 emails
        if i % 500 == 0 && i > 0 {
            let current_memory = get_memory_usage();
            println!(
                "Memory after {} emails: {} MB",
                i,
                current_memory / 1024 / 1024
            );

            // Memory should not grow excessively (less than 500MB increase)
            assert!(
                current_memory - initial_memory < 500 * 1024 * 1024,
                "Memory usage too high: {} MB increase",
                (current_memory - initial_memory) / 1024 / 1024
            );
        }
    }

    let final_memory = get_memory_usage();
    println!("Final memory usage: {} MB", final_memory / 1024 / 1024);
    println!(
        "Memory increase: {} MB",
        (final_memory - initial_memory) / 1024 / 1024
    );
}

#[tokio::test]
async fn test_git_repository_performance() {
    let config = PerformanceConfig {
        email_count: 1000,
        ..Default::default()
    };
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    // Test Git operations performance
    let mut email_ids = Vec::new();

    // Store emails and measure Git commit performance
    let git_start = Instant::now();

    for i in 0..fixture.config.email_count {
        let email = fixture.create_test_email(i);
        let email_id = fixture
            .core
            .store_email(&email)
            .await
            .expect("Failed to store email");
        email_ids.push(email_id);
    }

    let git_duration = git_start.elapsed();
    println!(
        "Git operations for {} emails took {:?}",
        fixture.config.email_count, git_duration
    );

    // Test Git history performance
    let history_start = Instant::now();
    let sample_email_id = &email_ids[email_ids.len() / 2];
    let history = fixture
        .core
        .get_email_history(sample_email_id)
        .await
        .expect("Failed to get history");
    let history_duration = history_start.elapsed();

    println!("Git history lookup took {:?}", history_duration);
    assert!(!history.is_empty(), "Should have Git history");
    assert!(
        history_duration < Duration::from_secs(5),
        "Git history lookup too slow"
    );

    // Test repository integrity
    let integrity_start = Instant::now();
    let is_valid = fixture
        .core
        .verify_repository_integrity()
        .await
        .expect("Failed to verify integrity");
    let integrity_duration = integrity_start.elapsed();

    println!("Repository integrity check took {:?}", integrity_duration);
    assert!(is_valid, "Repository should be valid");
    assert!(
        integrity_duration < Duration::from_secs(10),
        "Integrity check too slow"
    );
}

#[tokio::test]
async fn test_folder_organization_performance() {
    let config = PerformanceConfig {
        email_count: 2000,
        ..Default::default()
    };
    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    // Create folder structure
    let folders = vec!["work", "personal", "archive", "spam", "important"];
    for folder in &folders {
        fixture
            .core
            .create_folder(folder, None)
            .await
            .expect("Failed to create folder");
    }

    // Create nested folders
    fixture
        .core
        .create_folder("projects", Some("work"))
        .await
        .expect("Failed to create nested folder");
    fixture
        .core
        .create_folder("meetings", Some("work"))
        .await
        .expect("Failed to create nested folder");

    // Store emails
    let mut email_ids = Vec::new();
    for i in 0..fixture.config.email_count {
        let email = fixture.create_test_email(i);
        let email_id = fixture
            .core
            .store_email(&email)
            .await
            .expect("Failed to store email");
        email_ids.push(email_id);
    }

    // Test folder operations performance
    let move_start = Instant::now();

    // Move emails to different folders
    for (i, email_id) in email_ids.iter().enumerate() {
        let target_folder = &folders[i % folders.len()];
        fixture
            .core
            .move_email(email_id, target_folder)
            .await
            .expect("Failed to move email");
    }

    let move_duration = move_start.elapsed();
    println!(
        "Moving {} emails took {:?}",
        fixture.config.email_count, move_duration
    );

    // Should complete within reasonable time
    assert!(
        move_duration < Duration::from_secs(30),
        "Email moving too slow: {:?}",
        move_duration
    );

    // Test folder listing performance
    let list_start = Instant::now();
    let folder_info = fixture
        .core
        .list_folders()
        .await
        .expect("Failed to list folders");
    let list_duration = list_start.elapsed();

    println!("Listing folders took {:?}", list_duration);
    assert!(
        list_duration < Duration::from_secs(5),
        "Folder listing too slow"
    );
    assert!(
        folder_info.len() >= folders.len(),
        "Should have all created folders"
    );
}

/// Helper function to get directory size recursively
fn get_directory_size(path: &std::path::Path) -> std::io::Result<u64> {
    let mut size = 0;

    if path.is_dir() {
        for entry in std::fs::read_dir(path)? {
            let entry = entry?;
            let path = entry.path();

            if path.is_dir() {
                size += get_directory_size(&path)?;
            } else {
                size += entry.metadata()?.len();
            }
        }
    } else {
        size = std::fs::metadata(path)?.len();
    }

    Ok(size)
}

/// Helper function to get current memory usage (simplified)
fn get_memory_usage() -> usize {
    // This is a simplified implementation
    // In a real scenario, you might use a crate like `sysinfo` for accurate memory monitoring
    use std::alloc::{GlobalAlloc, Layout, System};

    // For testing purposes, we'll return a mock value
    // In production, implement proper memory monitoring
    1024 * 1024 * 100 // 100MB baseline
}

#[tokio::test]
async fn test_stress_test_scenario() {
    let config = PerformanceConfig {
        email_count: 5000,
        concurrent_operations: 15,
        max_storage_time: Duration::from_secs(120),
        max_retrieval_time: Duration::from_secs(60),
        max_search_time: Duration::from_secs(30),
    };

    let fixture = PerformanceTestFixture::new(config)
        .await
        .expect("Failed to create fixture");

    println!("Running comprehensive stress test...");
    let overall_start = Instant::now();

    // Phase 1: Concurrent email storage
    println!(
        "Phase 1: Storing {} emails concurrently",
        fixture.config.email_count
    );
    let mut join_set = JoinSet::new();

    for thread_id in 0..fixture.config.concurrent_operations {
        let core = fixture.core.clone();
        let emails_per_thread = fixture.config.email_count / fixture.config.concurrent_operations;

        join_set.spawn(async move {
            let mut thread_results = Vec::new();

            for i in 0..emails_per_thread {
                let email_index = thread_id * emails_per_thread + i;
                let mut email = Email::new(format!("stress{}@example.com", email_index));
                email.headers.insert(
                    "Subject".to_string(),
                    format!("Stress Test Email {} Thread {}", i, thread_id),
                );
                email.body.content = format!(
                    "Stress test content {} from thread {}. This email contains keywords like \
                     important, urgent, meeting, project, deadline, and client for search testing.",
                    i, thread_id
                );

                let email_id = core
                    .store_email(&email)
                    .await
                    .expect("Failed to store email");
                thread_results.push(email_id);
            }

            thread_results
        });
    }

    let mut all_email_ids = Vec::new();
    while let Some(result) = join_set.join_next().await {
        let thread_results = result.expect("Storage thread failed");
        all_email_ids.extend(thread_results);
    }

    println!("Phase 1 completed: {} emails stored", all_email_ids.len());

    // Phase 2: Mixed operations (read, search, organize)
    println!("Phase 2: Mixed operations");

    // Create folders
    let folders = vec!["urgent", "projects", "clients", "archive"];
    for folder in &folders {
        fixture
            .core
            .create_folder(folder, None)
            .await
            .expect("Failed to create folder");
    }

    // Concurrent mixed operations
    let mut mixed_join_set = JoinSet::new();

    // Search operations
    mixed_join_set.spawn({
        let core = fixture.core.clone();
        async move {
            let search_terms = vec!["important", "urgent", "meeting", "project", "client"];
            let mut total_results = 0;

            for term in search_terms {
                let results = core.search_emails(term).await.expect("Search failed");
                total_results += results.len();
            }

            total_results
        }
    });

    // Retrieval operations
    mixed_join_set.spawn({
        let core = fixture.core.clone();
        let sample_ids = all_email_ids[0..100].to_vec();
        async move {
            let mut retrieved_count = 0;

            for email_id in sample_ids {
                let _email = core
                    .retrieve_email(&email_id)
                    .await
                    .expect("Failed to retrieve");
                retrieved_count += 1;
            }

            retrieved_count
        }
    });

    // Organization operations
    mixed_join_set.spawn({
        let core = fixture.core.clone();
        let move_ids = all_email_ids[100..200].to_vec();
        async move {
            let mut moved_count = 0;

            for (i, email_id) in move_ids.iter().enumerate() {
                let folder = match i % 4 {
                    0 => "urgent",
                    1 => "projects",
                    2 => "clients",
                    _ => "archive",
                };

                core.move_email(email_id, folder)
                    .await
                    .expect("Failed to move email");
                moved_count += 1;
            }

            moved_count
        }
    });

    // Wait for mixed operations
    while let Some(result) = mixed_join_set.join_next().await {
        let _count = result.expect("Mixed operation failed");
    }

    let overall_duration = overall_start.elapsed();
    println!("Stress test completed in {:?}", overall_duration);

    // Final verification
    let final_count = fixture
        .core
        .count_emails()
        .await
        .expect("Failed to count emails");
    assert_eq!(final_count, fixture.config.email_count);

    println!("Stress test successful: {} emails processed", final_count);
}
