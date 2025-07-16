//! Mock IMAP and SMTP servers for testing network operations

use std::collections::HashMap;
use std::sync::{Arc, Mutex};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::{TcpListener, TcpStream};
use tokio::sync::mpsc;

/// Mock IMAP server for testing
pub struct MockImapServer {
    port: u16,
    emails: Arc<Mutex<Vec<MockEmail>>>,
    shutdown_tx: Option<mpsc::Sender<()>>,
}

/// Mock SMTP server for testing
pub struct MockSmtpServer {
    port: u16,
    sent_emails: Arc<Mutex<Vec<MockEmail>>>,
    shutdown_tx: Option<mpsc::Sender<()>>,
}

#[derive(Debug, Clone)]
pub struct MockEmail {
    pub id: String,
    pub subject: String,
    pub from: String,
    pub to: String,
    pub body: String,
    pub headers: HashMap<String, String>,
}

impl MockImapServer {
    pub fn new(port: u16) -> Self {
        Self {
            port,
            emails: Arc::new(Mutex::new(Vec::new())),
            shutdown_tx: None,
        }
    }

    pub fn add_email(&self, email: MockEmail) {
        self.emails.lock().unwrap().push(email);
    }

    pub fn get_emails(&self) -> Vec<MockEmail> {
        self.emails.lock().unwrap().clone()
    }

    pub async fn start(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let listener = TcpListener::bind(format!("127.0.0.1:{}", self.port)).await?;
        let emails = self.emails.clone();
        let (shutdown_tx, mut shutdown_rx) = mpsc::channel(1);
        self.shutdown_tx = Some(shutdown_tx);

        tokio::spawn(async move {
            loop {
                tokio::select! {
                    result = listener.accept() => {
                        match result {
                            Ok((stream, _)) => {
                                let emails_clone = emails.clone();
                                tokio::spawn(async move {
                                    if let Err(e) = Self::handle_client(stream, emails_clone).await {
                                        eprintln!("IMAP client error: {}", e);
                                    }
                                });
                            }
                            Err(e) => eprintln!("IMAP accept error: {}", e),
                        }
                    }
                    _ = shutdown_rx.recv() => {
                        break;
                    }
                }
            }
        });

        Ok(())
    }

    pub async fn stop(&mut self) {
        if let Some(tx) = self.shutdown_tx.take() {
            let _ = tx.send(()).await;
        }
    }

    async fn handle_client(
        mut stream: TcpStream,
        emails: Arc<Mutex<Vec<MockEmail>>>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let (reader, mut writer) = stream.split();
        let mut reader = BufReader::new(reader);
        let mut line = String::new();

        // Send greeting
        writer.write_all(b"* OK Mock IMAP Server Ready\r\n").await?;

        let mut authenticated = false;
        let mut selected_mailbox = None;

        loop {
            line.clear();
            let bytes_read = reader.read_line(&mut line).await?;
            if bytes_read == 0 {
                break;
            }

            let line = line.trim();
            let parts: Vec<&str> = line.split_whitespace().collect();

            if parts.len() < 2 {
                continue;
            }

            let tag = parts[0];
            let command = parts[1].to_uppercase();

            match command.as_str() {
                "LOGIN" => {
                    if parts.len() >= 4 {
                        authenticated = true;
                        writer
                            .write_all(format!("{} OK LOGIN completed\r\n", tag).as_bytes())
                            .await?;
                    } else {
                        writer
                            .write_all(format!("{} BAD LOGIN failed\r\n", tag).as_bytes())
                            .await?;
                    }
                }
                "SELECT" => {
                    if authenticated && parts.len() >= 3 {
                        selected_mailbox = Some(parts[2].to_string());
                        let email_count = emails.lock().unwrap().len();
                        writer
                            .write_all(format!("* {} EXISTS\r\n", email_count).as_bytes())
                            .await?;
                        writer
                            .write_all(format!("* {} RECENT\r\n", email_count).as_bytes())
                            .await?;
                        writer
                            .write_all(format!("{} OK SELECT completed\r\n", tag).as_bytes())
                            .await?;
                    } else {
                        writer
                            .write_all(format!("{} BAD SELECT failed\r\n", tag).as_bytes())
                            .await?;
                    }
                }
                "FETCH" => {
                    if authenticated && selected_mailbox.is_some() {
                        let emails_guard = emails.lock().unwrap();
                        for (i, email) in emails_guard.iter().enumerate() {
                            let uid = i + 1;
                            writer.write_all(format!("* {} FETCH (UID {} RFC822.SIZE {} BODY.PEEK[HEADER] {{{}}})\r\n", 
                                uid, uid, email.body.len(), email.headers.len()).as_bytes()).await?;

                            // Send headers
                            for (key, value) in &email.headers {
                                writer
                                    .write_all(format!("{}: {}\r\n", key, value).as_bytes())
                                    .await?;
                            }
                            writer.write_all(b"\r\n").await?;
                        }
                        writer
                            .write_all(format!("{} OK FETCH completed\r\n", tag).as_bytes())
                            .await?;
                    } else {
                        writer
                            .write_all(format!("{} BAD FETCH failed\r\n", tag).as_bytes())
                            .await?;
                    }
                }
                "LOGOUT" => {
                    writer
                        .write_all(b"* BYE Mock IMAP Server logging out\r\n")
                        .await?;
                    writer
                        .write_all(format!("{} OK LOGOUT completed\r\n", tag).as_bytes())
                        .await?;
                    break;
                }
                _ => {
                    writer
                        .write_all(format!("{} BAD Command not recognized\r\n", tag).as_bytes())
                        .await?;
                }
            }
        }

        Ok(())
    }
}

impl MockSmtpServer {
    pub fn new(port: u16) -> Self {
        Self {
            port,
            sent_emails: Arc::new(Mutex::new(Vec::new())),
            shutdown_tx: None,
        }
    }

    pub fn get_sent_emails(&self) -> Vec<MockEmail> {
        self.sent_emails.lock().unwrap().clone()
    }

    pub async fn start(&mut self) -> Result<(), Box<dyn std::error::Error>> {
        let listener = TcpListener::bind(format!("127.0.0.1:{}", self.port)).await?;
        let sent_emails = self.sent_emails.clone();
        let (shutdown_tx, mut shutdown_rx) = mpsc::channel(1);
        self.shutdown_tx = Some(shutdown_tx);

        tokio::spawn(async move {
            loop {
                tokio::select! {
                    result = listener.accept() => {
                        match result {
                            Ok((stream, _)) => {
                                let sent_emails_clone = sent_emails.clone();
                                tokio::spawn(async move {
                                    if let Err(e) = Self::handle_client(stream, sent_emails_clone).await {
                                        eprintln!("SMTP client error: {}", e);
                                    }
                                });
                            }
                            Err(e) => eprintln!("SMTP accept error: {}", e),
                        }
                    }
                    _ = shutdown_rx.recv() => {
                        break;
                    }
                }
            }
        });

        Ok(())
    }

    pub async fn stop(&mut self) {
        if let Some(tx) = self.shutdown_tx.take() {
            let _ = tx.send(()).await;
        }
    }

    async fn handle_client(
        mut stream: TcpStream,
        sent_emails: Arc<Mutex<Vec<MockEmail>>>,
    ) -> Result<(), Box<dyn std::error::Error>> {
        let (reader, mut writer) = stream.split();
        let mut reader = BufReader::new(reader);
        let mut line = String::new();

        // Send greeting
        writer.write_all(b"220 Mock SMTP Server Ready\r\n").await?;

        let mut mail_from = None;
        let mut rcpt_to = Vec::new();
        let mut data_mode = false;
        let mut email_data = String::new();

        loop {
            line.clear();
            let bytes_read = reader.read_line(&mut line).await?;
            if bytes_read == 0 {
                break;
            }

            let line = line.trim();

            if data_mode {
                if line == "." {
                    // End of data
                    data_mode = false;

                    // Parse email data
                    let mut headers = HashMap::new();
                    let mut body = String::new();
                    let mut in_headers = true;

                    for email_line in email_data.lines() {
                        if in_headers {
                            if email_line.is_empty() {
                                in_headers = false;
                            } else if let Some(colon_pos) = email_line.find(':') {
                                let key = email_line[..colon_pos].trim().to_string();
                                let value = email_line[colon_pos + 1..].trim().to_string();
                                headers.insert(key, value);
                            }
                        } else {
                            body.push_str(email_line);
                            body.push('\n');
                        }
                    }

                    // Create mock email
                    let mock_email = MockEmail {
                        id: uuid::Uuid::new_v4().to_string(),
                        subject: headers.get("Subject").cloned().unwrap_or_default(),
                        from: mail_from.clone().unwrap_or_default(),
                        to: rcpt_to.join(", "),
                        body: body.trim().to_string(),
                        headers,
                    };

                    sent_emails.lock().unwrap().push(mock_email);

                    writer.write_all(b"250 OK Message accepted\r\n").await?;

                    // Reset state
                    mail_from = None;
                    rcpt_to.clear();
                    email_data.clear();
                } else {
                    email_data.push_str(line);
                    email_data.push('\n');
                }
                continue;
            }

            let parts: Vec<&str> = line.split_whitespace().collect();
            if parts.is_empty() {
                continue;
            }

            let command = parts[0].to_uppercase();

            match command.as_str() {
                "HELO" | "EHLO" => {
                    writer.write_all(b"250 Hello\r\n").await?;
                }
                "MAIL" => {
                    if parts.len() >= 2 && parts[1].starts_with("FROM:") {
                        mail_from = Some(
                            parts[1][5..]
                                .trim_matches('<')
                                .trim_matches('>')
                                .to_string(),
                        );
                        writer.write_all(b"250 OK\r\n").await?;
                    } else {
                        writer.write_all(b"501 Syntax error\r\n").await?;
                    }
                }
                "RCPT" => {
                    if parts.len() >= 2 && parts[1].starts_with("TO:") {
                        rcpt_to.push(
                            parts[1][3..]
                                .trim_matches('<')
                                .trim_matches('>')
                                .to_string(),
                        );
                        writer.write_all(b"250 OK\r\n").await?;
                    } else {
                        writer.write_all(b"501 Syntax error\r\n").await?;
                    }
                }
                "DATA" => {
                    writer
                        .write_all(b"354 Start mail input; end with <CRLF>.<CRLF>\r\n")
                        .await?;
                    data_mode = true;
                }
                "QUIT" => {
                    writer.write_all(b"221 Bye\r\n").await?;
                    break;
                }
                _ => {
                    writer.write_all(b"502 Command not implemented\r\n").await?;
                }
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::time::{sleep, Duration};

    #[tokio::test]
    async fn test_mock_imap_server() {
        let mut server = MockImapServer::new(0); // Use port 0 for automatic assignment

        // Add test email
        let test_email = MockEmail {
            id: "1".to_string(),
            subject: "Test Subject".to_string(),
            from: "sender@example.com".to_string(),
            to: "recipient@example.com".to_string(),
            body: "Test body".to_string(),
            headers: {
                let mut headers = HashMap::new();
                headers.insert("Subject".to_string(), "Test Subject".to_string());
                headers.insert("From".to_string(), "sender@example.com".to_string());
                headers
            },
        };

        server.add_email(test_email);

        // Start server
        server.start().await.expect("Failed to start IMAP server");

        // Give server time to start
        sleep(Duration::from_millis(100)).await;

        // Verify email was added
        let emails = server.get_emails();
        assert_eq!(emails.len(), 1);
        assert_eq!(emails[0].subject, "Test Subject");

        // Stop server
        server.stop().await;
    }

    #[tokio::test]
    async fn test_mock_smtp_server() {
        let mut server = MockSmtpServer::new(0); // Use port 0 for automatic assignment

        // Start server
        server.start().await.expect("Failed to start SMTP server");

        // Give server time to start
        sleep(Duration::from_millis(100)).await;

        // Initially no emails
        let sent_emails = server.get_sent_emails();
        assert_eq!(sent_emails.len(), 0);

        // Stop server
        server.stop().await;
    }
}
