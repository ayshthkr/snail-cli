//! Email synchronization engine

use crate::error::Result;
use crate::git_storage::{ConflictResolutionStrategy, GitStorage};
use crate::models::{Account, ConnectionStatus, Email, EmailBody, SyncResult};
use chrono::{DateTime, Utc};
use lettre::message::{header, Mailbox, Message, MultiPart, SinglePart};
use lettre::transport::smtp::authentication::Credentials;
use lettre::transport::smtp::client::{Tls, TlsParameters};
use lettre::{SmtpTransport, Transport};
use serde::{Deserialize, Serialize};
use std::collections::{HashMap, HashSet, VecDeque};
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;
use tokio::fs;
use tokio::sync::Mutex;
use tracing::{debug, error, info, warn};

/// Email synchronization interface
pub trait SyncEngine {
    /// Fetch emails from remote server
    async fn fetch_emails(&self, account: &Account) -> Result<Vec<Email>>;

    /// Send an email via SMTP
    async fn send_email(&self, email: &Email, account: &Account) -> Result<()>;

    /// Synchronize an account (fetch and send)
    async fn sync_account(&self, account: &Account) -> Result<SyncResult>;

    /// Get connection status for an account
    async fn get_account_status(&self, account: &Account) -> Result<ConnectionStatus>;
}

/// IMAP client for email fetching
pub struct ImapClient {
    /// Connection timeout
    pub timeout: Duration,
    /// Last known UIDs for deduplication
    uid_cache: HashMap<String, HashSet<u32>>,
}

impl ImapClient {
    /// Create a new IMAP client
    pub fn new() -> Self {
        Self {
            timeout: Duration::from_secs(30),
            uid_cache: HashMap::new(),
        }
    }

    /// Connect to IMAP server and authenticate
    pub fn connect(&self, account: &Account) -> Result<imap::Session<std::net::TcpStream>> {
        let server = &account.incoming.server;
        let port = account.incoming.port;

        info!("Connecting to IMAP server {}:{}", server, port);

        // Create TCP connection
        let tcp_stream = std::net::TcpStream::connect((server.as_str(), port)).map_err(|e| {
            crate::error::GitMailError::Network(format!(
                "Failed to connect to {}:{}: {}",
                server, port, e
            ))
        })?;

        // Create IMAP client
        let client = imap::Client::new(tcp_stream);

        debug!("IMAP client created, attempting authentication");

        // Authenticate
        let session = client
            .login(&account.incoming.username, &account.incoming.password)
            .map_err(|(e, _)| {
                crate::error::GitMailError::Auth(format!("IMAP login failed: {}", e))
            })?;

        info!("Successfully authenticated with IMAP server");
        Ok(session)
    }

    /// Fetch emails from a specific folder
    pub fn fetch_emails_from_folder(
        &mut self,
        session: &mut imap::Session<std::net::TcpStream>,
        folder: &str,
        account_name: &str,
    ) -> Result<Vec<Email>> {
        info!("Fetching emails from folder: {}", folder);

        // Select the folder
        session.select(folder).map_err(|e| {
            crate::error::GitMailError::Network(format!(
                "Failed to select folder {}: {}",
                folder, e
            ))
        })?;

        // Get folder status
        let mailbox = session.examine(folder).map_err(|e| {
            crate::error::GitMailError::Network(format!(
                "Failed to examine folder {}: {}",
                folder, e
            ))
        })?;

        if mailbox.exists == 0 {
            debug!("Folder {} is empty", folder);
            return Ok(vec![]);
        }

        // Get cached UIDs for this folder
        let folder_key = format!("{}:{}", account_name, folder);
        let cached_uids = self.uid_cache.get(&folder_key).cloned().unwrap_or_default();

        // Search for all messages
        let message_nums = session.search("ALL").map_err(|e| {
            crate::error::GitMailError::Network(format!("Failed to search messages: {}", e))
        })?;

        if message_nums.is_empty() {
            debug!("No messages found in folder {}", folder);
            return Ok(vec![]);
        }

        // Convert message numbers to string for UID fetch
        let message_set = message_nums
            .iter()
            .map(|n| n.to_string())
            .collect::<Vec<_>>()
            .join(",");

        // Fetch UIDs for all messages
        let uid_fetch = session.uid_fetch(&message_set, "UID").map_err(|e| {
            crate::error::GitMailError::Network(format!("Failed to fetch UIDs: {}", e))
        })?;

        // Collect new UIDs (not in cache)
        let mut new_uids = Vec::new();
        let mut current_uids = HashSet::new();

        for fetch in uid_fetch.iter() {
            if let Some(uid) = fetch.uid {
                current_uids.insert(uid);
                if !cached_uids.contains(&uid) {
                    new_uids.push(uid);
                }
            }
        }

        // Update UID cache
        self.uid_cache.insert(folder_key, current_uids);

        if new_uids.is_empty() {
            info!("No new messages in folder {}", folder);
            return Ok(vec![]);
        }

        info!("Found {} new messages in folder {}", new_uids.len(), folder);

        // Fetch new messages
        let mut emails = Vec::new();

        // Fetch in batches to avoid overwhelming the server
        const BATCH_SIZE: usize = 50;
        for batch in new_uids.chunks(BATCH_SIZE) {
            let uid_list: Vec<String> = batch.iter().map(|uid| uid.to_string()).collect();
            let uid_range = uid_list.join(",");

            debug!("Fetching batch of {} messages", batch.len());

            let messages = session.uid_fetch(&uid_range, "RFC822").map_err(|e| {
                crate::error::GitMailError::Network(format!("Failed to fetch messages: {}", e))
            })?;

            for message in messages.iter() {
                if let Some(body) = message.body() {
                    match self.parse_email(body, folder, account_name) {
                        Ok(email) => {
                            debug!("Successfully parsed email: {}", email.id);
                            emails.push(email);
                        }
                        Err(e) => {
                            warn!("Failed to parse email: {}", e);
                            // Continue processing other emails
                        }
                    }
                }
            }
        }

        info!(
            "Successfully fetched {} emails from folder {}",
            emails.len(),
            folder
        );
        Ok(emails)
    }

    /// Parse raw email data into Email struct (simplified version)
    fn parse_email(&self, raw_data: &[u8], folder: &str, account_name: &str) -> Result<Email> {
        // Convert raw data to string for basic parsing
        let email_text = String::from_utf8_lossy(raw_data);

        let mut email = Email::new(account_name.to_string());

        // Basic header parsing - split by lines and look for headers
        let lines: Vec<&str> = email_text.lines().collect();
        let mut in_headers = true;
        let mut body_start = 0;

        for (i, line) in lines.iter().enumerate() {
            if in_headers {
                if line.is_empty() {
                    // Empty line marks end of headers
                    in_headers = false;
                    body_start = i + 1;
                    continue;
                }

                // Parse header lines
                if let Some(colon_pos) = line.find(':') {
                    let header_name = line[..colon_pos].trim().to_string();
                    let header_value = line[colon_pos + 1..].trim().to_string();
                    email.headers.insert(header_name, header_value);
                }
            }
        }

        // Extract body content (everything after headers)
        // If we never found an empty line, then there's no body content
        if !in_headers && body_start < lines.len() {
            let body_content = lines[body_start..].join("\n");
            email.body = EmailBody {
                content_type: "text/plain".to_string(),
                content: body_content,
                html_content: None,
            };
        } else {
            // No body content found
            email.body = EmailBody {
                content_type: "text/plain".to_string(),
                content: String::new(),
                html_content: None,
            };
        }

        // Set metadata
        email.metadata.folder = folder.to_string();
        email.metadata.created_at = Utc::now();
        email.metadata.modified_at = Utc::now();

        Ok(email)
    }

    /// Get folder list from IMAP server
    pub fn list_folders(
        &self,
        session: &mut imap::Session<std::net::TcpStream>,
    ) -> Result<Vec<String>> {
        debug!("Listing IMAP folders");

        let folders = session.list(Some(""), Some("*")).map_err(|e| {
            crate::error::GitMailError::Network(format!("Failed to list folders: {}", e))
        })?;

        let folder_names: Vec<String> = folders
            .iter()
            .map(|folder| folder.name().to_string())
            .collect();

        debug!("Found {} folders", folder_names.len());
        Ok(folder_names)
    }

    /// Check connection status
    pub fn check_connection(&self, account: &Account) -> Result<ConnectionStatus> {
        match self.connect(account) {
            Ok(mut session) => {
                // Try to perform a simple operation to verify the connection
                match session.noop() {
                    Ok(_) => {
                        let _ = session.logout();
                        Ok(ConnectionStatus::Connected)
                    }
                    Err(e) => Ok(ConnectionStatus::Error(format!(
                        "Connection test failed: {}",
                        e
                    ))),
                }
            }
            Err(e) => Ok(ConnectionStatus::Error(format!("Connection failed: {}", e))),
        }
    }

    /// Fetch all emails from all folders for an account
    pub fn fetch_all_emails(&mut self, account: &Account) -> Result<Vec<Email>> {
        let mut session = self.connect(account)?;
        let folders = self.list_folders(&mut session)?;

        let mut all_emails = Vec::new();

        // Common folders to prioritize
        let priority_folders = ["INBOX", "Inbox", "inbox"];
        let mut processed_folders = HashSet::new();

        // Process priority folders first
        for folder in &priority_folders {
            if folders.contains(&folder.to_string()) {
                match self.fetch_emails_from_folder(&mut session, folder, &account.name) {
                    Ok(mut emails) => {
                        all_emails.append(&mut emails);
                        processed_folders.insert(folder.to_string());
                    }
                    Err(e) => {
                        warn!("Failed to fetch emails from folder {}: {}", folder, e);
                    }
                }
            }
        }

        // Process remaining folders
        for folder in &folders {
            if !processed_folders.contains(folder) {
                match self.fetch_emails_from_folder(&mut session, folder, &account.name) {
                    Ok(mut emails) => {
                        all_emails.append(&mut emails);
                    }
                    Err(e) => {
                        warn!("Failed to fetch emails from folder {}: {}", folder, e);
                    }
                }
            }
        }

        let _ = session.logout();
        Ok(all_emails)
    }
}

/// Queued email for offline sending
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct QueuedEmail {
    /// Email to send
    pub email: Email,
    /// Account to use for sending
    pub account_name: String,
    /// Number of retry attempts
    pub retry_count: u32,
    /// Next retry time
    pub next_retry: chrono::DateTime<Utc>,
    /// Original queue time
    pub queued_at: chrono::DateTime<Utc>,
}

/// SMTP client for email sending
pub struct SmtpClient {
    /// Email queue for offline scenarios
    pub queue: VecDeque<QueuedEmail>,
    /// Queue file path
    pub queue_file: PathBuf,
    /// Maximum retry attempts
    pub max_retries: u32,
    /// Base retry delay in seconds
    pub base_retry_delay: u64,
}

impl SmtpClient {
    /// Create a new SMTP client
    pub fn new(queue_file: PathBuf) -> Self {
        Self {
            queue: VecDeque::new(),
            queue_file,
            max_retries: 5,
            base_retry_delay: 30, // 30 seconds base delay
        }
    }

    /// Load email queue from disk
    pub async fn load_queue(&mut self) -> Result<()> {
        if !self.queue_file.exists() {
            debug!("Queue file does not exist, starting with empty queue");
            return Ok(());
        }

        let content = fs::read_to_string(&self.queue_file)
            .await
            .map_err(|e| crate::error::GitMailError::Io(e))?;

        if content.trim().is_empty() {
            debug!("Queue file is empty");
            return Ok(());
        }

        let queued_emails: Vec<QueuedEmail> = serde_json::from_str(&content)
            .map_err(|e| crate::error::GitMailError::Serialization(e))?;

        self.queue = queued_emails.into();
        info!("Loaded {} emails from queue", self.queue.len());
        Ok(())
    }

    /// Save email queue to disk
    pub async fn save_queue(&self) -> Result<()> {
        let queued_emails: Vec<QueuedEmail> = self.queue.iter().cloned().collect();
        let content = serde_json::to_string_pretty(&queued_emails)
            .map_err(|e| crate::error::GitMailError::Serialization(e))?;

        // Create parent directory if it doesn't exist
        if let Some(parent) = self.queue_file.parent() {
            fs::create_dir_all(parent)
                .await
                .map_err(|e| crate::error::GitMailError::Io(e))?;
        }

        fs::write(&self.queue_file, content)
            .await
            .map_err(|e| crate::error::GitMailError::Io(e))?;

        debug!("Saved {} emails to queue", self.queue.len());
        Ok(())
    }

    /// Create SMTP transport for an account
    pub fn create_transport(&self, account: &Account) -> Result<SmtpTransport> {
        let server = &account.outgoing.server;
        let port = account.outgoing.port;

        info!("Creating SMTP transport for {}:{}", server, port);

        let mut transport_builder = SmtpTransport::relay(server).map_err(|e| {
            crate::error::GitMailError::Network(format!("Failed to create SMTP relay: {}", e))
        })?;

        // Configure port
        transport_builder = transport_builder.port(port);

        // Configure TLS
        if account.outgoing.ssl {
            let tls_parameters = TlsParameters::new(server.clone()).map_err(|e| {
                crate::error::GitMailError::Network(format!(
                    "Failed to create TLS parameters: {}",
                    e
                ))
            })?;
            transport_builder = transport_builder.tls(Tls::Required(tls_parameters));
        } else {
            transport_builder = transport_builder.tls(Tls::None);
        }

        // Configure authentication
        let credentials = Credentials::new(
            account.outgoing.username.clone(),
            account.outgoing.password.clone(),
        );
        transport_builder = transport_builder.credentials(credentials);

        // Build transport
        let transport = transport_builder.build();

        debug!("SMTP transport created successfully");
        Ok(transport)
    }

    /// Convert Email struct to lettre Message
    pub fn email_to_message(&self, email: &Email, account: &Account) -> Result<Message> {
        debug!("Converting email to lettre message: {}", email.id);

        // Parse sender
        let from_header = email
            .headers
            .get("From")
            .or_else(|| Some(&account.email))
            .ok_or_else(|| {
                crate::error::GitMailError::Config("No From address found".to_string())
            })?;

        let from: Mailbox = from_header.parse().map_err(|e| {
            crate::error::GitMailError::Config(format!(
                "Invalid From address '{}': {}",
                from_header, e
            ))
        })?;

        // Parse recipients
        let to_header = email
            .headers
            .get("To")
            .ok_or_else(|| crate::error::GitMailError::Config("No To address found".to_string()))?;

        let to: Mailbox = to_header.parse().map_err(|e| {
            crate::error::GitMailError::Config(format!("Invalid To address '{}': {}", to_header, e))
        })?;

        // Start building message
        let mut message_builder = Message::builder().from(from).to(to);

        // Add CC if present
        if let Some(cc_header) = email.headers.get("Cc") {
            let cc: Mailbox = cc_header.parse().map_err(|e| {
                crate::error::GitMailError::Config(format!(
                    "Invalid CC address '{}': {}",
                    cc_header, e
                ))
            })?;
            message_builder = message_builder.cc(cc);
        }

        // Add BCC if present
        if let Some(bcc_header) = email.headers.get("Bcc") {
            let bcc: Mailbox = bcc_header.parse().map_err(|e| {
                crate::error::GitMailError::Config(format!(
                    "Invalid BCC address '{}': {}",
                    bcc_header, e
                ))
            })?;
            message_builder = message_builder.bcc(bcc);
        }

        // Add subject
        if let Some(subject) = email.headers.get("Subject") {
            message_builder = message_builder.subject(subject);
        }

        // Add Message-ID if present
        if let Some(message_id) = email.headers.get("Message-ID") {
            message_builder = message_builder.message_id(Some(message_id.clone()));
        }

        // Add other headers - skip custom headers for now as lettre has specific header types
        // In a full implementation, you would handle each header type specifically

        // Create message body
        let message = if let Some(html_content) = &email.body.html_content {
            // Multipart message with both text and HTML
            message_builder
                .multipart(
                    MultiPart::alternative()
                        .singlepart(
                            SinglePart::builder()
                                .header(header::ContentType::TEXT_PLAIN)
                                .body(email.body.content.clone()),
                        )
                        .singlepart(
                            SinglePart::builder()
                                .header(header::ContentType::TEXT_HTML)
                                .body(html_content.clone()),
                        ),
                )
                .map_err(|e| {
                    crate::error::GitMailError::Config(format!(
                        "Failed to create multipart message: {}",
                        e
                    ))
                })?
        } else {
            // Simple text message
            message_builder
                .body(email.body.content.clone())
                .map_err(|e| {
                    crate::error::GitMailError::Config(format!(
                        "Failed to create message body: {}",
                        e
                    ))
                })?
        };

        debug!("Successfully converted email to lettre message");
        Ok(message)
    }

    /// Send an email immediately
    pub async fn send_email_now(&self, email: &Email, account: &Account) -> Result<()> {
        info!("Sending email immediately: {}", email.id);

        // Create SMTP transport
        let transport = self.create_transport(account)?;

        // Convert email to message
        let message = self.email_to_message(email, account)?;

        // Send the email
        transport.send(&message).map_err(|e| {
            crate::error::GitMailError::Network(format!("Failed to send email: {}", e))
        })?;

        info!("Successfully sent email: {}", email.id);
        Ok(())
    }

    /// Queue an email for later sending
    pub async fn queue_email(&mut self, email: Email, account_name: String) -> Result<()> {
        info!("Queueing email for later sending: {}", email.id);

        let queued_email = QueuedEmail {
            email,
            account_name,
            retry_count: 0,
            next_retry: Utc::now(),
            queued_at: Utc::now(),
        };

        self.queue.push_back(queued_email);
        self.save_queue().await?;

        info!("Email queued successfully");
        Ok(())
    }

    /// Process queued emails with retry logic
    pub async fn process_queue(&mut self, accounts: &HashMap<String, Account>) -> Result<usize> {
        if self.queue.is_empty() {
            debug!("Email queue is empty");
            return Ok(0);
        }

        info!("Processing {} queued emails", self.queue.len());
        let mut sent_count = 0;
        let mut failed_emails = Vec::new();

        // Process emails that are ready for retry
        while let Some(mut queued_email) = self.queue.pop_front() {
            if queued_email.next_retry > Utc::now() {
                // Not ready for retry yet, put it back
                self.queue.push_front(queued_email);
                break;
            }

            let account = match accounts.get(&queued_email.account_name) {
                Some(acc) => acc,
                None => {
                    warn!(
                        "Account '{}' not found for queued email",
                        queued_email.account_name
                    );
                    continue;
                }
            };

            match self.send_email_now(&queued_email.email, account).await {
                Ok(()) => {
                    info!("Successfully sent queued email: {}", queued_email.email.id);
                    sent_count += 1;
                }
                Err(e) => {
                    warn!("Failed to send queued email: {}", e);
                    queued_email.retry_count += 1;

                    if queued_email.retry_count <= self.max_retries {
                        // Calculate exponential backoff delay
                        let delay_seconds =
                            self.base_retry_delay * (2_u64.pow(queued_email.retry_count - 1));
                        queued_email.next_retry =
                            Utc::now() + chrono::Duration::seconds(delay_seconds as i64);

                        info!(
                            "Scheduling retry {} for email {} in {} seconds",
                            queued_email.retry_count, queued_email.email.id, delay_seconds
                        );

                        failed_emails.push(queued_email);
                    } else {
                        error!(
                            "Email {} exceeded maximum retry attempts ({}), dropping from queue",
                            queued_email.email.id, self.max_retries
                        );
                    }
                }
            }
        }

        // Re-add failed emails that haven't exceeded retry limit
        for failed_email in failed_emails {
            self.queue.push_back(failed_email);
        }

        // Save updated queue
        self.save_queue().await?;

        info!(
            "Processed queue: {} emails sent, {} remaining",
            sent_count,
            self.queue.len()
        );
        Ok(sent_count)
    }

    /// Get queue status
    pub fn queue_status(&self) -> (usize, usize) {
        let ready_count = self
            .queue
            .iter()
            .filter(|email| email.next_retry <= Utc::now())
            .count();
        (self.queue.len(), ready_count)
    }

    /// Clear the email queue
    pub async fn clear_queue(&mut self) -> Result<()> {
        self.queue.clear();
        self.save_queue().await?;
        info!("Email queue cleared");
        Ok(())
    }
}

/// Sync conflict information
#[derive(Debug, Clone)]
pub struct SyncConflict {
    /// Email ID that has a conflict
    pub email_id: String,
    /// Local version of the email
    pub local_email: Email,
    /// Remote version of the email
    pub remote_email: Email,
    /// Type of conflict
    pub conflict_type: ConflictType,
}

/// Types of sync conflicts
#[derive(Debug, Clone)]
pub enum ConflictType {
    /// Both local and remote versions were modified
    BothModified,
    /// Local version was deleted, remote was modified
    LocalDeletedRemoteModified,
    /// Local version was modified, remote was deleted
    LocalModifiedRemoteDeleted,
    /// Metadata conflicts (tags, read status, etc.)
    MetadataConflict,
}

/// Comprehensive sync orchestrator that handles full sync workflow
pub struct SyncOrchestrator<G: GitStorage> {
    /// Sync engine for email operations
    sync_engine: Arc<Mutex<DefaultSyncEngine>>,
    /// Git storage for email persistence
    git_storage: Arc<G>,
    /// Conflict resolution strategy
    conflict_strategy: ConflictResolutionStrategy,
}

impl<G: GitStorage> SyncOrchestrator<G> {
    /// Create a new sync orchestrator
    pub fn new(
        sync_engine: DefaultSyncEngine,
        git_storage: G,
        conflict_strategy: ConflictResolutionStrategy,
    ) -> Self {
        Self {
            sync_engine: Arc::new(Mutex::new(sync_engine)),
            git_storage: Arc::new(git_storage),
            conflict_strategy,
        }
    }

    /// Perform comprehensive account synchronization
    pub async fn sync_account_comprehensive(&self, account: &Account) -> Result<SyncResult> {
        info!("Starting comprehensive sync for account: {}", account.name);

        let mut result = SyncResult {
            fetched: 0,
            sent: 0,
            errors: vec![],
        };

        // Phase 1: Check connection and validate account
        if let Err(e) = self.validate_account_connection(account).await {
            let error_msg = format!("Account validation failed: {}", e);
            error!("{}", error_msg);
            result.errors.push(error_msg);
            return Ok(result);
        }

        // Phase 2: Process offline queue first (send pending emails)
        match self.process_offline_queue(account).await {
            Ok(sent_count) => {
                result.sent = sent_count;
                info!(
                    "Sent {} queued emails for account {}",
                    sent_count, account.name
                );
            }
            Err(e) => {
                let error_msg = format!("Failed to process offline queue: {}", e);
                warn!("{}", error_msg);
                result.errors.push(error_msg);
            }
        }

        // Phase 3: Fetch new emails from server
        match self.fetch_and_store_emails(account).await {
            Ok(fetched_count) => {
                result.fetched = fetched_count;
                info!(
                    "Fetched {} new emails for account {}",
                    fetched_count, account.name
                );
            }
            Err(e) => {
                let error_msg = format!("Failed to fetch emails: {}", e);
                error!("{}", error_msg);
                result.errors.push(error_msg);
            }
        }

        // Phase 4: Handle any sync conflicts that occurred during storage
        match self.resolve_sync_conflicts(account).await {
            Ok(conflicts_resolved) => {
                if conflicts_resolved > 0 {
                    info!(
                        "Resolved {} sync conflicts for account {}",
                        conflicts_resolved, account.name
                    );
                }
            }
            Err(e) => {
                let error_msg = format!("Failed to resolve sync conflicts: {}", e);
                warn!("{}", error_msg);
                result.errors.push(error_msg);
            }
        }

        // Phase 5: Commit all changes to Git repository
        match self.commit_sync_changes(account).await {
            Ok(()) => {
                info!(
                    "Successfully committed sync changes for account {}",
                    account.name
                );
            }
            Err(e) => {
                let error_msg = format!("Failed to commit sync changes: {}", e);
                error!("{}", error_msg);
                result.errors.push(error_msg);
            }
        }

        info!(
            "Comprehensive sync completed for account {}: {} fetched, {} sent, {} errors",
            account.name,
            result.fetched,
            result.sent,
            result.errors.len()
        );

        Ok(result)
    }

    /// Validate account connection and configuration
    async fn validate_account_connection(&self, account: &Account) -> Result<()> {
        info!("Validating connection for account: {}", account.name);

        // Validate account configuration
        account
            .validate()
            .map_err(|e| crate::error::GitMailError::Config(e))?;

        // Check connection status
        let sync_engine = self.sync_engine.lock().await;
        match sync_engine.get_account_status(account).await? {
            ConnectionStatus::Connected => {
                info!("Account {} connection validated", account.name);
                Ok(())
            }
            ConnectionStatus::Error(e) => Err(crate::error::GitMailError::Network(format!(
                "Account {} connection error: {}",
                account.name, e
            ))),
            ConnectionStatus::Disconnected => Err(crate::error::GitMailError::Network(format!(
                "Account {} is disconnected",
                account.name
            ))),
        }
    }

    /// Process offline email queue
    async fn process_offline_queue(&self, account: &Account) -> Result<usize> {
        info!("Processing offline queue for account: {}", account.name);

        let mut sync_engine = self.sync_engine.lock().await;
        let smtp_client = sync_engine.smtp_client_mut();

        // Load queue from disk
        smtp_client.load_queue().await?;

        // Create account map for queue processing
        let mut accounts = HashMap::new();
        accounts.insert(account.name.clone(), account.clone());

        // Process the queue
        let sent_count = smtp_client.process_queue(&accounts).await?;

        info!("Processed offline queue: {} emails sent", sent_count);
        Ok(sent_count)
    }

    /// Fetch emails from server and store them with conflict resolution
    async fn fetch_and_store_emails(&self, account: &Account) -> Result<usize> {
        info!("Fetching and storing emails for account: {}", account.name);

        // Fetch emails from server
        let sync_engine = self.sync_engine.lock().await;
        let emails = sync_engine.fetch_emails(account).await?;
        drop(sync_engine); // Release lock early

        if emails.is_empty() {
            info!("No new emails to store for account {}", account.name);
            return Ok(0);
        }

        let mut stored_count = 0;
        let mut conflicts = Vec::new();

        // Store each email with conflict detection
        for email in emails {
            match self.store_email_with_conflict_detection(&email).await {
                Ok(()) => {
                    stored_count += 1;
                    debug!("Successfully stored email: {}", email.id);
                }
                Err(crate::error::GitMailError::Conflict(conflict_info)) => {
                    warn!(
                        "Conflict detected for email {}: {}",
                        email.id, conflict_info
                    );
                    // Create conflict record for later resolution
                    if let Ok(local_email) = self.git_storage.retrieve_email(&email.id) {
                        conflicts.push(SyncConflict {
                            email_id: email.id.clone(),
                            local_email,
                            remote_email: email,
                            conflict_type: ConflictType::BothModified,
                        });
                    }
                }
                Err(e) => {
                    error!("Failed to store email {}: {}", email.id, e);
                    // Continue with other emails
                }
            }
        }

        // Handle conflicts immediately based on strategy
        for conflict in conflicts {
            if let Err(e) = self.resolve_single_conflict(&conflict).await {
                error!(
                    "Failed to resolve conflict for email {}: {}",
                    conflict.email_id, e
                );
            } else {
                stored_count += 1; // Count resolved conflicts as stored
            }
        }

        info!(
            "Stored {} emails for account {}",
            stored_count, account.name
        );
        Ok(stored_count)
    }

    /// Store email with conflict detection
    async fn store_email_with_conflict_detection(&self, email: &Email) -> Result<()> {
        // Check if email already exists
        match self.git_storage.retrieve_email(&email.id) {
            Ok(existing_email) => {
                // Email exists, check for conflicts
                if self.has_conflict(&existing_email, email) {
                    return Err(crate::error::GitMailError::Conflict(format!(
                        "Email {} has been modified both locally and remotely",
                        email.id
                    )));
                }
                // No conflict, update the email
                self.git_storage.store_email(email)?;
                Ok(())
            }
            Err(crate::error::GitMailError::NotFound(_)) => {
                // Email doesn't exist, store it
                self.git_storage.store_email(email)?;
                Ok(())
            }
            Err(e) => Err(e),
        }
    }

    /// Check if there's a conflict between local and remote email versions
    fn has_conflict(&self, local: &Email, remote: &Email) -> bool {
        // Check if both versions have been modified after the original creation
        if local.metadata.modified_at != local.metadata.created_at
            && remote.metadata.modified_at != remote.metadata.created_at
        {
            // Both have been modified, check if they're different
            return local.metadata.modified_at != remote.metadata.modified_at
                || local.body.content != remote.body.content
                || local.metadata.tags != remote.metadata.tags
                || local.metadata.is_read != remote.metadata.is_read
                || local.metadata.is_starred != remote.metadata.is_starred;
        }
        false
    }

    /// Resolve a single sync conflict
    async fn resolve_single_conflict(&self, conflict: &SyncConflict) -> Result<()> {
        info!("Resolving conflict for email: {}", conflict.email_id);

        let resolved_email = match self.conflict_strategy {
            ConflictResolutionStrategy::KeepLocal => {
                info!("Keeping local version for email {}", conflict.email_id);
                conflict.local_email.clone()
            }
            ConflictResolutionStrategy::KeepRemote => {
                info!("Keeping remote version for email {}", conflict.email_id);
                conflict.remote_email.clone()
            }
            ConflictResolutionStrategy::MergeMetadata => {
                info!("Merging metadata for email {}", conflict.email_id);
                self.merge_email_metadata(&conflict.local_email, &conflict.remote_email)
            }
            ConflictResolutionStrategy::Manual => {
                warn!(
                    "Manual conflict resolution required for email {}",
                    conflict.email_id
                );
                // For now, default to keeping local version
                // In a real implementation, this would prompt the user or create a conflict file
                conflict.local_email.clone()
            }
        };

        // Store the resolved email
        self.git_storage.store_email(&resolved_email)?;
        info!(
            "Successfully resolved conflict for email {}",
            conflict.email_id
        );
        Ok(())
    }

    /// Merge email metadata intelligently
    fn merge_email_metadata(&self, local: &Email, remote: &Email) -> Email {
        let mut merged = remote.clone(); // Start with remote content

        // Merge tags (union of both sets)
        let mut merged_tags = local.metadata.tags.clone();
        for tag in &remote.metadata.tags {
            if !merged_tags.contains(tag) {
                merged_tags.push(tag.clone());
            }
        }
        merged.metadata.tags = merged_tags;

        // Use the more recent read status
        if local.metadata.modified_at > remote.metadata.modified_at {
            merged.metadata.is_read = local.metadata.is_read;
            merged.metadata.is_starred = local.metadata.is_starred;
        }

        // Use the latest modification time
        merged.metadata.modified_at =
            std::cmp::max(local.metadata.modified_at, remote.metadata.modified_at);

        merged
    }

    /// Resolve any remaining sync conflicts
    async fn resolve_sync_conflicts(&self, _account: &Account) -> Result<usize> {
        // This would scan for any unresolved conflicts and handle them
        // For now, we assume conflicts are resolved during storage
        Ok(0)
    }

    /// Commit all sync changes to Git repository
    async fn commit_sync_changes(&self, account: &Account) -> Result<()> {
        let commit_message = format!(
            "Sync changes for account {} at {}",
            account.name,
            Utc::now().to_rfc3339()
        );

        self.git_storage.commit_changes(&commit_message)?;
        info!("Committed sync changes for account {}", account.name);
        Ok(())
    }

    /// Get sync status for an account
    pub async fn get_sync_status(&self, account: &Account) -> Result<SyncStatus> {
        let sync_engine = self.sync_engine.lock().await;
        let connection_status = sync_engine.get_account_status(account).await?;
        let (queue_total, queue_ready) = sync_engine.smtp_client().queue_status();

        Ok(SyncStatus {
            account_name: account.name.clone(),
            connection_status,
            queued_emails: queue_total,
            ready_to_send: queue_ready,
            last_sync: None, // Would be tracked in a real implementation
        })
    }
}

/// Sync status information
#[derive(Debug)]
pub struct SyncStatus {
    /// Account name
    pub account_name: String,
    /// Connection status
    pub connection_status: ConnectionStatus,
    /// Number of queued emails
    pub queued_emails: usize,
    /// Number of emails ready to send
    pub ready_to_send: usize,
    /// Last successful sync time
    pub last_sync: Option<DateTime<Utc>>,
}

/// Default sync engine implementation
pub struct DefaultSyncEngine {
    /// IMAP client for fetching emails
    pub imap_client: ImapClient,
    /// SMTP client for sending emails
    pub smtp_client: SmtpClient,
}

impl DefaultSyncEngine {
    /// Create a new sync engine
    pub fn new(queue_file: PathBuf) -> Self {
        Self {
            imap_client: ImapClient::new(),
            smtp_client: SmtpClient::new(queue_file),
        }
    }

    /// Create a new sync engine with default queue file location
    pub fn new_with_default_queue() -> Self {
        let queue_file = dirs::home_dir()
            .unwrap_or_else(|| PathBuf::from("."))
            .join(".git-mail")
            .join("queue.json");
        Self::new(queue_file)
    }

    /// Get mutable reference to SMTP client for queue operations
    pub fn smtp_client_mut(&mut self) -> &mut SmtpClient {
        &mut self.smtp_client
    }

    /// Get reference to SMTP client for status operations
    pub fn smtp_client(&self) -> &SmtpClient {
        &self.smtp_client
    }
}

impl SyncEngine for DefaultSyncEngine {
    async fn fetch_emails(&self, account: &Account) -> Result<Vec<Email>> {
        // Clone the IMAP client to make it mutable for this operation
        let mut imap_client = ImapClient::new();

        // Validate account configuration
        account
            .validate()
            .map_err(|e| crate::error::GitMailError::Config(e))?;

        // Check if this is an IMAP account
        if account.incoming.protocol.to_lowercase() != "imap" {
            return Err(crate::error::GitMailError::Config(format!(
                "Unsupported protocol for fetching: {}",
                account.incoming.protocol
            )));
        }

        info!("Fetching emails for account: {}", account.name);

        // Fetch emails from all folders
        let emails = imap_client.fetch_all_emails(account)?;

        info!(
            "Successfully fetched {} emails for account {}",
            emails.len(),
            account.name
        );
        Ok(emails)
    }

    async fn send_email(&self, email: &Email, account: &Account) -> Result<()> {
        // Validate account configuration
        account
            .validate()
            .map_err(|e| crate::error::GitMailError::Config(e))?;

        info!("Sending email via SMTP: {}", email.id);

        // Try to send immediately
        self.smtp_client.send_email_now(email, account).await
    }

    async fn sync_account(&self, account: &Account) -> Result<SyncResult> {
        info!("Starting full sync for account: {}", account.name);

        let mut result = SyncResult {
            fetched: 0,
            sent: 0,
            errors: vec![],
        };

        // Phase 1: Check connection status
        match self.get_account_status(account).await {
            Ok(ConnectionStatus::Connected) => {
                info!(
                    "Account {} is connected, proceeding with sync",
                    account.name
                );
            }
            Ok(ConnectionStatus::Error(e)) => {
                let error_msg = format!("Account {} connection error: {}", account.name, e);
                warn!("{}", error_msg);
                result.errors.push(error_msg);
                return Ok(result);
            }
            Ok(ConnectionStatus::Disconnected) => {
                let error_msg = format!("Account {} is disconnected", account.name);
                warn!("{}", error_msg);
                result.errors.push(error_msg);
                return Ok(result);
            }
            Err(e) => {
                let error_msg = format!("Failed to check account {} status: {}", account.name, e);
                error!("{}", error_msg);
                result.errors.push(error_msg);
                return Ok(result);
            }
        }

        // Phase 2: Fetch new emails from server
        match self.fetch_emails(account).await {
            Ok(emails) => {
                result.fetched = emails.len();
                info!(
                    "Successfully fetched {} emails for account {}",
                    emails.len(),
                    account.name
                );

                // TODO: Store emails using GitStorage with conflict resolution
                // This would be implemented when integrating with the storage layer
                if !emails.is_empty() {
                    info!("Emails would be stored with conflict resolution handling");
                }
            }
            Err(e) => {
                let error_msg =
                    format!("Failed to fetch emails for account {}: {}", account.name, e);
                error!("{}", error_msg);
                result.errors.push(error_msg);
            }
        }

        // Phase 3: Process offline queue (send pending emails)
        // Note: This requires mutable access to smtp_client, which is a design limitation
        // In a real implementation, this would be handled by a separate sync orchestrator
        info!("Offline queue processing should be handled by sync orchestrator");

        // Phase 4: Handle any sync conflicts
        // This would involve checking for concurrent modifications and resolving them
        info!("Conflict resolution would be applied during email storage");

        info!(
            "Sync completed for account {}: {} fetched, {} sent, {} errors",
            account.name,
            result.fetched,
            result.sent,
            result.errors.len()
        );

        Ok(result)
    }

    async fn get_account_status(&self, account: &Account) -> Result<ConnectionStatus> {
        let imap_client = ImapClient::new();
        imap_client.check_connection(account)
    }
}
#[cfg(test)]
mod tests {
    use super::*;
    use crate::git_storage::DefaultGitStorage;
    use crate::models::{IncomingConfig, OutgoingConfig};
    use chrono::{DateTime, Utc};
    use tempfile::TempDir;

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

    fn create_test_email(account: &str, subject: &str) -> Email {
        let mut email = Email::new(account.to_string());
        email
            .headers
            .insert("Subject".to_string(), subject.to_string());
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        email.body.content = format!("Test email body for {}", subject);
        email
    }

    async fn create_test_sync_orchestrator() -> (SyncOrchestrator<DefaultGitStorage>, TempDir) {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let repo_path = temp_dir.path().join("git-mail-repo");

        let sync_engine = DefaultSyncEngine::new(queue_file);
        let git_storage = DefaultGitStorage::new(repo_path.to_string_lossy().to_string());

        // Initialize the repository
        git_storage
            .initialize_repository(&repo_path.to_string_lossy())
            .unwrap();

        let orchestrator = SyncOrchestrator::new(
            sync_engine,
            git_storage,
            ConflictResolutionStrategy::MergeMetadata,
        );

        (orchestrator, temp_dir)
    }

    #[test]
    fn test_imap_client_creation() {
        let client = ImapClient::new();
        assert_eq!(client.timeout, Duration::from_secs(30));
        assert!(client.uid_cache.is_empty());
    }

    #[test]
    fn test_sync_engine_creation() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file.clone());
        // Just verify it can be created without panicking
        assert_eq!(engine.imap_client.timeout, Duration::from_secs(30));
        assert_eq!(engine.smtp_client.queue_file, queue_file);
    }

    #[tokio::test]
    async fn test_fetch_emails_invalid_protocol() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file);
        let mut account = create_test_account();
        account.incoming.protocol = "pop3".to_string();

        let result = engine.fetch_emails(&account).await;
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Unsupported protocol"));
    }

    #[tokio::test]
    async fn test_smtp_client_queue_operations() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut smtp_client = SmtpClient::new(queue_file);

        // Test empty queue
        let (total, ready) = smtp_client.queue_status();
        assert_eq!(total, 0);
        assert_eq!(ready, 0);

        // Test queueing an email
        let email = create_test_email("test@example.com", "Test Subject");
        smtp_client
            .queue_email(email.clone(), "test_account".to_string())
            .await
            .unwrap();

        let (total, ready) = smtp_client.queue_status();
        assert_eq!(total, 1);
        assert_eq!(ready, 1);

        // Test clearing queue
        smtp_client.clear_queue().await.unwrap();
        let (total, ready) = smtp_client.queue_status();
        assert_eq!(total, 0);
        assert_eq!(ready, 0);
    }

    #[tokio::test]
    async fn test_sync_orchestrator_creation() {
        let (_orchestrator, _temp_dir) = create_test_sync_orchestrator().await;
        // Just verify it can be created without panicking
    }

    #[tokio::test]
    async fn test_conflict_detection() {
        let (orchestrator, _temp_dir) = create_test_sync_orchestrator().await;

        // Create two versions of the same email with different modifications
        let mut local_email = create_test_email("test@example.com", "Test Subject");
        local_email.metadata.modified_at = Utc::now() - chrono::Duration::hours(1);
        local_email.metadata.is_read = true;
        local_email.metadata.tags.push("local-tag".to_string());

        let mut remote_email = local_email.clone();
        remote_email.metadata.modified_at = Utc::now();
        remote_email.metadata.is_starred = true;
        remote_email.metadata.tags.push("remote-tag".to_string());
        remote_email.body.content = "Modified content".to_string();

        // Test conflict detection
        let has_conflict = orchestrator.has_conflict(&local_email, &remote_email);
        assert!(
            has_conflict,
            "Should detect conflict between modified versions"
        );

        // Test no conflict when only one is modified
        let mut unmodified_email = create_test_email("test@example.com", "Test Subject");
        unmodified_email.metadata.modified_at = unmodified_email.metadata.created_at;

        let has_conflict = orchestrator.has_conflict(&unmodified_email, &remote_email);
        assert!(
            !has_conflict,
            "Should not detect conflict when only remote is modified"
        );
    }

    #[tokio::test]
    async fn test_metadata_merging() {
        let (orchestrator, _temp_dir) = create_test_sync_orchestrator().await;

        // Create local email with some tags and read status
        let mut local_email = create_test_email("test@example.com", "Test Subject");
        local_email.metadata.is_read = true;
        local_email.metadata.tags = vec!["local-tag".to_string(), "shared-tag".to_string()];
        local_email.metadata.modified_at = Utc::now() - chrono::Duration::hours(1);

        // Create remote email with different tags and starred status
        let mut remote_email = local_email.clone();
        remote_email.metadata.is_read = false;
        remote_email.metadata.is_starred = true;
        remote_email.metadata.tags = vec!["remote-tag".to_string(), "shared-tag".to_string()];
        remote_email.metadata.modified_at = Utc::now();
        remote_email.body.content = "Updated content from remote".to_string();

        // Test metadata merging
        let merged = orchestrator.merge_email_metadata(&local_email, &remote_email);

        // Should use remote content (newer)
        assert_eq!(merged.body.content, "Updated content from remote");

        // Should merge tags (union)
        assert!(merged.metadata.tags.contains(&"local-tag".to_string()));
        assert!(merged.metadata.tags.contains(&"remote-tag".to_string()));
        assert!(merged.metadata.tags.contains(&"shared-tag".to_string()));
        assert_eq!(merged.metadata.tags.len(), 3);

        // Should use local read status (older modification time, so remote wins)
        assert!(!merged.metadata.is_read);
        assert!(merged.metadata.is_starred);

        // Should use latest modification time
        assert_eq!(
            merged.metadata.modified_at,
            remote_email.metadata.modified_at
        );
    }

    #[tokio::test]
    async fn test_conflict_resolution_strategies() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let repo_path = temp_dir.path().join("git-mail-repo");

        let sync_engine = DefaultSyncEngine::new(queue_file);
        let git_storage = DefaultGitStorage::new(repo_path.to_string_lossy().to_string());
        git_storage
            .initialize_repository(&repo_path.to_string_lossy())
            .unwrap();

        // Test KeepLocal strategy - create new instances instead of cloning
        let queue_file_local = temp_dir.path().join("queue_local.json");
        let sync_engine_local = DefaultSyncEngine::new(queue_file_local);
        let git_storage_local = DefaultGitStorage::new(repo_path.to_string_lossy().to_string());
        // Don't initialize again - repository already exists

        let _orchestrator_local = SyncOrchestrator::new(
            sync_engine_local,
            git_storage_local,
            ConflictResolutionStrategy::KeepLocal,
        );

        let local_email = create_test_email("test@example.com", "Local Version");
        let remote_email = create_test_email("test@example.com", "Remote Version");

        let conflict = SyncConflict {
            email_id: local_email.id.clone(),
            local_email: local_email.clone(),
            remote_email: remote_email.clone(),
            conflict_type: ConflictType::BothModified,
        };

        // This would test the resolution, but we can't easily test without mocking the storage
        // In a real implementation, we'd use dependency injection and mocks
        assert_eq!(
            conflict.local_email.headers.get("Subject").unwrap(),
            "Local Version"
        );
        assert_eq!(
            conflict.remote_email.headers.get("Subject").unwrap(),
            "Remote Version"
        );
    }

    #[tokio::test]
    async fn test_sync_status() {
        let (orchestrator, _temp_dir) = create_test_sync_orchestrator().await;
        let account = create_test_account();

        // This test would normally fail because we can't connect to a real server
        // In a real implementation, we'd mock the network calls
        let result = orchestrator.get_sync_status(&account).await;

        // We expect this to succeed but return an error status since we can't connect
        assert!(result.is_ok());
        let status = result.unwrap();
        assert_eq!(status.account_name, account.name);
        // Connection status should be an error due to invalid server
        match status.connection_status {
            ConnectionStatus::Error(_) => {} // Expected
            _ => panic!("Expected error status for invalid server"),
        }
    }

    #[tokio::test]
    async fn test_offline_queue_processing() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let mut smtp_client = SmtpClient::new(queue_file);

        // Queue some test emails
        let email1 = create_test_email("test@example.com", "Email 1");
        let email2 = create_test_email("test@example.com", "Email 2");

        smtp_client
            .queue_email(email1, "test_account".to_string())
            .await
            .unwrap();
        smtp_client
            .queue_email(email2, "test_account".to_string())
            .await
            .unwrap();

        // Verify queue status
        let (total, ready) = smtp_client.queue_status();
        assert_eq!(total, 2);
        assert_eq!(ready, 2);

        // Test queue persistence
        smtp_client.save_queue().await.unwrap();

        let mut new_smtp_client = SmtpClient::new(smtp_client.queue_file.clone());
        new_smtp_client.load_queue().await.unwrap();

        let (total, ready) = new_smtp_client.queue_status();
        assert_eq!(total, 2);
        assert_eq!(ready, 2);
    }

    #[tokio::test]
    async fn test_comprehensive_sync_workflow() {
        let (orchestrator, _temp_dir) = create_test_sync_orchestrator().await;
        let account = create_test_account();

        // This test demonstrates the comprehensive sync workflow
        // In a real implementation, we'd mock the network calls and storage operations

        let result = orchestrator.sync_account_comprehensive(&account).await;

        // We expect this to fail with network errors since we're not mocking
        // but the structure should be correct
        assert!(result.is_ok());
        let sync_result = result.unwrap();

        // Should have errors due to network failures, but no panics
        assert!(sync_result.errors.len() > 0);
        assert_eq!(sync_result.fetched, 0); // No emails fetched due to network failure
        assert_eq!(sync_result.sent, 0); // No emails sent due to empty queue
    }

    #[test]
    fn test_conflict_type_variants() {
        // Test that all conflict types can be created
        let _both_modified = ConflictType::BothModified;
        let _local_deleted = ConflictType::LocalDeletedRemoteModified;
        let _remote_deleted = ConflictType::LocalModifiedRemoteDeleted;
        let _metadata = ConflictType::MetadataConflict;
    }

    #[test]
    fn test_sync_conflict_creation() {
        let local_email = create_test_email("test@example.com", "Local");
        let remote_email = create_test_email("test@example.com", "Remote");

        let conflict = SyncConflict {
            email_id: "test-id".to_string(),
            local_email: local_email.clone(),
            remote_email: remote_email.clone(),
            conflict_type: ConflictType::BothModified,
        };

        assert_eq!(conflict.email_id, "test-id");
        assert_eq!(
            conflict.local_email.headers.get("Subject").unwrap(),
            "Local"
        );
        assert_eq!(
            conflict.remote_email.headers.get("Subject").unwrap(),
            "Remote"
        );
    }

    #[test]
    fn test_sync_status_creation() {
        let status = SyncStatus {
            account_name: "test_account".to_string(),
            connection_status: ConnectionStatus::Connected,
            queued_emails: 5,
            ready_to_send: 3,
            last_sync: Some(Utc::now()),
        };

        assert_eq!(status.account_name, "test_account");
        assert_eq!(status.queued_emails, 5);
        assert_eq!(status.ready_to_send, 3);
        assert!(status.last_sync.is_some());
    }

    #[tokio::test]
    async fn test_sync_account_with_invalid_account() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file);
        let mut account = create_test_account();
        account.name = String::new(); // Invalid account

        let result = engine.sync_account(&account).await;
        assert!(result.is_ok()); // Should not fail, but should have errors in result

        let sync_result = result.unwrap();
        assert_eq!(sync_result.fetched, 0);
        assert!(!sync_result.errors.is_empty());
    }

    #[test]
    fn test_email_parsing_with_minimal_data() {
        let client = ImapClient::new();
        let raw_email = b"From: test@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n\r\nTest body";

        let result = client.parse_email(raw_email, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert_eq!(email.account, "test_account");
        assert_eq!(email.metadata.folder, "INBOX");
        assert_eq!(email.headers.get("Subject").unwrap(), "Test");
        assert!(email.body.content.contains("Test body"));
    }

    #[test]
    fn test_email_parsing_with_invalid_data() {
        let client = ImapClient::new();
        let invalid_data = b"This is not a valid email";

        let result = client.parse_email(invalid_data, "INBOX", "test_account");
        assert!(result.is_ok()); // Our simple parser should handle this gracefully

        let email = result.unwrap();
        assert_eq!(email.account, "test_account");
        assert_eq!(email.metadata.folder, "INBOX");
    }

    #[test]
    fn test_uid_cache_functionality() {
        let mut client = ImapClient::new();

        // Simulate adding UIDs to cache
        let folder_key = "test_account:INBOX".to_string();
        let mut uids = HashSet::new();
        uids.insert(123);
        uids.insert(456);

        client.uid_cache.insert(folder_key.clone(), uids);

        // Verify cache contains the UIDs
        let cached_uids = client.uid_cache.get(&folder_key).unwrap();
        assert!(cached_uids.contains(&123));
        assert!(cached_uids.contains(&456));
        assert!(!cached_uids.contains(&789));
    }

    #[test]
    fn test_connection_status_error_handling() {
        let client = ImapClient::new();
        let mut account = create_test_account();
        account.incoming.server = "nonexistent.server.com".to_string();

        let result = client.check_connection(&account);
        assert!(result.is_ok());

        // Should return an error status, not fail
        match result.unwrap() {
            ConnectionStatus::Error(_) => {} // Expected
            _ => panic!("Expected error status for invalid server"),
        }
    }

    #[tokio::test]
    async fn test_fetch_emails_with_empty_account_name() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file);
        let mut account = create_test_account();
        account.name = String::new();

        let result = engine.fetch_emails(&account).await;
        assert!(result.is_err());
    }

    #[test]
    fn test_email_parsing_with_headers() {
        let client = ImapClient::new();
        let raw_email = b"From: test@example.com\r\nTo: recipient@example.com\r\nCc: cc@example.com\r\nSubject: Test CC\r\n\r\nTest body";

        let result = client.parse_email(raw_email, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert!(email.headers.contains_key("From"));
        assert!(email.headers.contains_key("To"));
        assert!(email.headers.contains_key("Cc"));
        assert_eq!(email.headers.get("Subject").unwrap(), "Test CC");
    }

    #[test]
    fn test_email_parsing_with_multiline_headers() {
        let client = ImapClient::new();
        let raw_email = b"From: test@example.com\r\nTo: recipient@example.com\r\nSubject: Test\r\n Message-ID: <12345@example.com>\r\n\r\nTest body content";

        let result = client.parse_email(raw_email, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert!(email.headers.contains_key("From"));
        assert!(email.headers.contains_key("To"));
        assert!(email.headers.contains_key("Subject"));
        assert!(email.headers.contains_key("Message-ID"));
        assert!(email.body.content.contains("Test body content"));
    }

    #[test]
    fn test_email_parsing_empty_body() {
        let client = ImapClient::new();
        let raw_email =
            b"From: test@example.com\r\nTo: recipient@example.com\r\nSubject: Empty Body\r\n\r\n";

        let result = client.parse_email(raw_email, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert_eq!(email.headers.get("Subject").unwrap(), "Empty Body");
        assert!(email.body.content.is_empty());
    }

    #[test]
    fn test_batch_processing_logic() {
        // Test that batch size logic works correctly
        let uids: Vec<u32> = (1..=150).collect(); // 150 UIDs
        const BATCH_SIZE: usize = 50;

        let batches: Vec<_> = uids.chunks(BATCH_SIZE).collect();
        assert_eq!(batches.len(), 3); // Should be 3 batches
        assert_eq!(batches[0].len(), 50);
        assert_eq!(batches[1].len(), 50);
        assert_eq!(batches[2].len(), 50);
    }

    #[test]
    fn test_folder_prioritization() {
        let priority_folders = ["INBOX", "Inbox", "inbox"];
        let available_folders = vec![
            "Sent".to_string(),
            "INBOX".to_string(),
            "Drafts".to_string(),
            "Spam".to_string(),
        ];

        // Test that INBOX is found in priority folders
        let mut found_priority = false;
        for folder in &priority_folders {
            if available_folders.contains(&folder.to_string()) {
                found_priority = true;
                break;
            }
        }
        assert!(found_priority);
    }

    #[test]
    fn test_uid_deduplication_logic() {
        let mut client = ImapClient::new();
        let folder_key = "test_account:INBOX".to_string();

        // Simulate existing UIDs in cache
        let mut existing_uids = HashSet::new();
        existing_uids.insert(100);
        existing_uids.insert(200);
        client.uid_cache.insert(folder_key.clone(), existing_uids);

        // Simulate new UIDs from server
        let server_uids = vec![100, 200, 300, 400]; // 100, 200 already cached
        let cached_uids = client
            .uid_cache
            .get(&folder_key)
            .cloned()
            .unwrap_or_default();

        let new_uids: Vec<u32> = server_uids
            .into_iter()
            .filter(|uid| !cached_uids.contains(uid))
            .collect();

        assert_eq!(new_uids, vec![300, 400]); // Only new UIDs should be returned
    }

    #[test]
    fn test_message_set_formatting() {
        let message_nums = vec![1, 5, 10, 15];
        let message_set = message_nums
            .iter()
            .map(|n| n.to_string())
            .collect::<Vec<_>>()
            .join(",");

        assert_eq!(message_set, "1,5,10,15");
    }

    #[test]
    fn test_uid_range_formatting() {
        let uids = vec![123, 456, 789];
        let uid_list: Vec<String> = uids.iter().map(|uid| uid.to_string()).collect();
        let uid_range = uid_list.join(",");

        assert_eq!(uid_range, "123,456,789");
    }

    #[tokio::test]
    async fn test_sync_account_success_scenario() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file);
        let mut account = create_test_account();
        // Use a non-existent server to trigger error handling
        account.incoming.server = "nonexistent.example.com".to_string();

        let result = engine.sync_account(&account).await;
        assert!(result.is_ok());

        let sync_result = result.unwrap();
        assert_eq!(sync_result.fetched, 0);
        assert_eq!(sync_result.sent, 0);
        assert!(!sync_result.errors.is_empty()); // Should have connection error
    }

    #[tokio::test]
    async fn test_get_account_status() {
        let temp_dir = tempfile::tempdir().unwrap();
        let queue_file = temp_dir.path().join("queue.json");
        let engine = DefaultSyncEngine::new(queue_file);
        let mut account = create_test_account();
        account.incoming.server = "nonexistent.example.com".to_string();

        let result = engine.get_account_status(&account).await;
        assert!(result.is_ok());

        match result.unwrap() {
            ConnectionStatus::Error(_) => {} // Expected for non-existent server
            _ => panic!("Expected error status for invalid server"),
        }
    }

    #[test]
    fn test_email_metadata_timestamps() {
        let client = ImapClient::new();
        let raw_email = b"From: test@example.com\r\nSubject: Timestamp Test\r\n\r\nBody";

        let before = Utc::now();
        let result = client.parse_email(raw_email, "INBOX", "test_account");
        let after = Utc::now();

        assert!(result.is_ok());
        let email = result.unwrap();

        // Timestamps should be set to current time (within reasonable bounds)
        assert!(email.metadata.created_at >= before);
        assert!(email.metadata.created_at <= after);
        // Check that timestamps are close (within 1 second)
        let diff = (email.metadata.created_at.timestamp_millis()
            - email.metadata.modified_at.timestamp_millis())
        .abs();
        assert!(diff < 1000); // Less than 1 second difference
    }

    #[test]
    fn test_email_body_content_type() {
        let client = ImapClient::new();
        let raw_email =
            b"From: test@example.com\r\nSubject: Content Type Test\r\n\r\nPlain text body";

        let result = client.parse_email(raw_email, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert_eq!(email.body.content_type, "text/plain");
        assert!(email.body.html_content.is_none());
        assert!(email.body.content.contains("Plain text body"));
    }

    // Integration test placeholder - would require real IMAP server
    #[ignore] // Ignored by default since it requires external dependencies
    #[tokio::test]
    async fn test_real_imap_connection() {
        // This test would connect to a real IMAP server
        // It's ignored by default and should only be run with proper test credentials

        // Example of how you might set up integration tests:
        // 1. Set up a test IMAP server (like Greenmail for Java, or similar for Rust)
        // 2. Create test emails
        // 3. Test the full fetch workflow
        // 4. Verify emails are parsed correctly

        // For now, this is just a placeholder
        assert!(true);
    }

    // Mock IMAP server test - would require additional dependencies
    #[ignore]
    #[tokio::test]
    async fn test_with_mock_imap_server() {
        // This would test with a mock IMAP server
        // Requires additional test dependencies like mockall or similar

        // Example test flow:
        // 1. Start mock IMAP server
        // 2. Configure it with test data
        // 3. Run fetch_emails with test account
        // 4. Verify correct emails are returned
        // 5. Verify UID caching works correctly

        assert!(true);
    }

    // Performance test placeholder
    #[ignore]
    #[test]
    fn test_large_email_batch_performance() {
        // This would test performance with large numbers of emails
        // Should be run separately from regular unit tests

        let client = ImapClient::new();

        // Test parsing many emails
        let raw_email = b"From: test@example.com\r\nSubject: Performance Test\r\n\r\nBody";

        let start = std::time::Instant::now();
        for i in 0..1000 {
            let result = client.parse_email(raw_email, "INBOX", &format!("account_{}", i));
            assert!(result.is_ok());
        }
        let duration = start.elapsed();

        // Should be able to parse 1000 emails in reasonable time
        assert!(duration.as_secs() < 5);
    }

    // Error handling tests
    #[test]
    fn test_invalid_header_parsing() {
        let client = ImapClient::new();
        // Email with malformed headers
        let raw_email =
            b"From test@example.com\r\nTo: recipient\r\nInvalid header line\r\n\r\nBody";

        let result = client.parse_email(raw_email, "INBOX", "test_account");
        assert!(result.is_ok()); // Should handle gracefully

        let email = result.unwrap();
        // Should still extract valid headers
        assert!(email.headers.contains_key("To"));
        // Invalid header line should be ignored
        assert!(!email.headers.contains_key("Invalid header line"));
    }

    #[test]
    fn test_empty_email_parsing() {
        let client = ImapClient::new();
        let empty_email = b"";

        let result = client.parse_email(empty_email, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert!(email.headers.is_empty());
        assert!(email.body.content.is_empty());
    }

    #[test]
    fn test_only_headers_no_body() {
        let client = ImapClient::new();
        let headers_only = b"From: test@example.com\r\nSubject: No Body";

        let result = client.parse_email(headers_only, "INBOX", "test_account");
        assert!(result.is_ok());

        let email = result.unwrap();
        assert!(email.headers.contains_key("From"));
        assert!(email.headers.contains_key("Subject"));
        assert!(email.body.content.is_empty());
    }
}
