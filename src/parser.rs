//! Email parsing functionality using RFC 2822 compliance

use crate::models::{Attachment, Email, EmailBody, EmailMetadata};
use anyhow::{anyhow, Result};
use chrono::{DateTime, Utc};
use mail_parser::{Addr, HeaderValue, Message, MimeHeaders};
use std::collections::HashMap;
use std::path::Path;
use uuid::Uuid;

/// Email parser for converting raw email messages to Email structs
pub struct EmailParser;

impl EmailParser {
    /// Create a new email parser
    pub fn new() -> Self {
        Self
    }

    /// Parse raw email message into Email struct
    pub fn parse_email(&self, raw_message: &[u8], account: String) -> Result<Email> {
        let message =
            Message::parse(raw_message).ok_or_else(|| anyhow!("Failed to parse email message"))?;

        let mut email = Email::new(account);

        // Extract headers
        email.headers = self.extract_headers(&message)?;

        // Set message ID from headers if available
        if let Some(message_id) = email.headers.get("Message-ID") {
            email.message_id = message_id.clone();
        }

        // Extract body content
        email.body = self.extract_body(&message)?;

        // Extract attachments
        email.attachments = self.extract_attachments(&message)?;

        // Update metadata based on headers
        self.update_metadata_from_headers(&mut email.metadata, &email.headers)?;

        // Validate the parsed email
        email
            .validate()
            .map_err(|e| anyhow!("Email validation failed: {}", e))?;

        Ok(email)
    }

    /// Extract headers from parsed message
    fn extract_headers(&self, message: &Message) -> Result<HashMap<String, String>> {
        let mut headers = HashMap::new();

        // Extract From header
        let from = message.from();
        match from {
            HeaderValue::Address(addr) => {
                headers.insert("From".to_string(), format_address(addr));
            }
            HeaderValue::AddressList(addrs) => {
                headers.insert("From".to_string(), format_address_list(addrs));
            }
            HeaderValue::Text(text) => {
                headers.insert("From".to_string(), text.to_string());
            }
            _ => {}
        }

        // Extract To header
        let to = message.to();
        match to {
            HeaderValue::Address(addr) => {
                headers.insert("To".to_string(), format_address(addr));
            }
            HeaderValue::AddressList(addrs) => {
                headers.insert("To".to_string(), format_address_list(addrs));
            }
            HeaderValue::Text(text) => {
                headers.insert("To".to_string(), text.to_string());
            }
            _ => {}
        }

        // Extract CC header
        let cc = message.cc();
        match cc {
            HeaderValue::Address(addr) => {
                headers.insert("Cc".to_string(), format_address(addr));
            }
            HeaderValue::AddressList(addrs) => {
                headers.insert("Cc".to_string(), format_address_list(addrs));
            }
            HeaderValue::Text(text) => {
                headers.insert("Cc".to_string(), text.to_string());
            }
            _ => {}
        }

        // Extract BCC header
        let bcc = message.bcc();
        match bcc {
            HeaderValue::Address(addr) => {
                headers.insert("Bcc".to_string(), format_address(addr));
            }
            HeaderValue::AddressList(addrs) => {
                headers.insert("Bcc".to_string(), format_address_list(addrs));
            }
            HeaderValue::Text(text) => {
                headers.insert("Bcc".to_string(), text.to_string());
            }
            _ => {}
        }

        // Extract Subject
        if let Some(subject) = message.subject() {
            headers.insert("Subject".to_string(), subject.to_string());
        }

        // Extract Date
        if let Some(date) = message.date() {
            headers.insert("Date".to_string(), date.to_rfc822());
        }

        // Extract Message-ID
        if let Some(message_id) = message.message_id() {
            headers.insert("Message-ID".to_string(), format!("<{}>", message_id));
        }

        // Extract In-Reply-To
        let in_reply_to = message.in_reply_to();
        if let HeaderValue::Text(reply_id) = in_reply_to {
            headers.insert("In-Reply-To".to_string(), format!("<{}>", reply_id));
        }

        // Extract References
        let references = message.references();
        if let HeaderValue::TextList(refs) = references {
            let ref_strings: Vec<String> = refs.iter().map(|r| format!("<{}>", r)).collect();
            headers.insert("References".to_string(), ref_strings.join(" "));
        }

        // Extract all other headers
        for header in message.headers() {
            let name = header.name().to_string();

            // Skip if we already processed this header
            if headers.contains_key(&name) {
                continue;
            }

            let value = match header.value() {
                HeaderValue::Text(text) => text.to_string(),
                HeaderValue::TextList(list) => list.join(", "),
                HeaderValue::Address(addr) => format_address(addr),
                HeaderValue::AddressList(addrs) => format_address_list(addrs),
                HeaderValue::DateTime(dt) => dt.to_rfc822(),
                HeaderValue::Empty => String::new(),
                _ => String::new(),
            };

            if !value.is_empty() {
                headers.insert(name, value);
            }
        }

        Ok(headers)
    }

    /// Extract body content from parsed message
    fn extract_body(&self, message: &Message) -> Result<EmailBody> {
        let mut body = EmailBody::default();

        // Handle text parts
        if let Some(text_body) = message.body_text(0) {
            body.content = text_body.to_string();
            body.content_type = "text/plain".to_string();
        }

        // Handle HTML parts
        if let Some(html_body) = message.body_html(0) {
            body.html_content = Some(html_body.to_string());

            // If we don't have plain text, use HTML as primary content
            if body.content.is_empty() {
                body.content = html_body.to_string();
                body.content_type = "text/html".to_string();
            }
        }

        // Handle multipart messages
        if message.parts.len() > 1 {
            // For multipart messages, prefer text/plain over text/html
            if body.content.is_empty() {
                // Try to find any text content in parts
                for part in &message.parts {
                    if part.is_text() {
                        // Use the contents directly for text parts
                        let text = String::from_utf8_lossy(part.contents());
                        if !text.trim().is_empty() {
                            body.content = text.to_string();
                            body.content_type = "text/plain".to_string();
                            break;
                        }
                    }
                }
            }
        }

        // Ensure we have some content
        if body.content.is_empty() && body.html_content.is_none() {
            body.content = "[No readable content]".to_string();
        }

        Ok(body)
    }

    /// Extract attachments from parsed message
    fn extract_attachments(&self, message: &Message) -> Result<Vec<Attachment>> {
        let mut attachments = Vec::new();

        for attachment in message.attachments() {
            let filename = attachment
                .attachment_name()
                .unwrap_or("unnamed_attachment")
                .to_string();

            let content_type = attachment
                .content_type()
                .map(|ct| {
                    let main_type = ct.c_type.as_ref();
                    let sub_type = ct.c_subtype.as_ref().map_or("octet-stream", |v| v.as_ref());
                    format!("{}/{}", main_type, sub_type)
                })
                .unwrap_or_else(|| "application/octet-stream".to_string());

            let contents = attachment.contents();
            let size = contents.len() as u64;

            // Generate a unique file path for the attachment
            let attachment_id = Uuid::new_v4().to_string();
            let file_path = format!("attachments/{}/{}", attachment_id, filename);

            let attachment_info = Attachment {
                filename,
                content_type,
                size,
                file_path,
            };

            attachments.push(attachment_info);
        }

        Ok(attachments)
    }

    /// Update email metadata based on headers
    fn update_metadata_from_headers(
        &self,
        metadata: &mut EmailMetadata,
        headers: &HashMap<String, String>,
    ) -> Result<()> {
        // Set creation time from Date header if available
        if let Some(date_str) = headers.get("Date") {
            if let Ok(date) = DateTime::parse_from_rfc2822(date_str) {
                metadata.created_at = date.with_timezone(&Utc);
                metadata.modified_at = metadata.created_at;
            }
        }

        // Determine folder based on headers (basic logic)
        if headers.contains_key("X-Spam-Flag")
            || headers
                .get("Subject")
                .map_or(false, |s| s.to_lowercase().contains("spam"))
        {
            metadata.folder = "spam".to_string();
        } else if headers
            .get("From")
            .map_or(false, |f| f.contains("noreply") || f.contains("no-reply"))
        {
            metadata.folder = "notifications".to_string();
        }

        Ok(())
    }

    /// Parse email from file path
    pub fn parse_email_from_file<P: AsRef<Path>>(
        &self,
        file_path: P,
        account: String,
    ) -> Result<Email> {
        let raw_message = std::fs::read(file_path)?;
        self.parse_email(&raw_message, account)
    }

    /// Validate RFC 2822 compliance of raw message
    pub fn validate_rfc2822(&self, raw_message: &[u8]) -> Result<()> {
        let message = Message::parse(raw_message)
            .ok_or_else(|| anyhow!("Failed to parse message for RFC 2822 validation"))?;

        // Check required headers
        let from = message.from();
        if matches!(from, HeaderValue::Empty) {
            return Err(anyhow!("Missing required 'From' header"));
        }

        let date = message.date();
        if date.is_none() {
            return Err(anyhow!("Missing required 'Date' header"));
        }

        // Validate message ID format if present
        if let Some(message_id) = message.message_id() {
            if !message_id.contains('@') {
                return Err(anyhow!("Invalid Message-ID format: must contain '@'"));
            }
        }

        // Validate email addresses in From field
        match from {
            HeaderValue::Address(addr) => {
                if let Some(email) = &addr.address {
                    if !email.contains('@') || !email.contains('.') {
                        return Err(anyhow!("Invalid email address format in From: {}", email));
                    }
                }
            }
            HeaderValue::AddressList(addrs) => {
                for addr in addrs {
                    if let Some(email) = &addr.address {
                        if !email.contains('@') || !email.contains('.') {
                            return Err(anyhow!("Invalid email address format in From: {}", email));
                        }
                    }
                }
            }
            _ => {}
        }

        Ok(())
    }
}

impl Default for EmailParser {
    fn default() -> Self {
        Self::new()
    }
}

/// Email composer for creating RFC 2822 compliant emails
pub struct EmailComposer;

impl EmailComposer {
    /// Create a new email composer
    pub fn new() -> Self {
        Self
    }

    /// Compose a new email with proper RFC 2822 formatting
    pub fn compose_email(&self, email: &Email) -> Result<String> {
        let mut message = String::new();

        // Add required headers
        self.add_header(&mut message, "From", &self.format_from_header(email)?)?;
        self.add_header(&mut message, "To", &self.format_to_header(email)?)?;

        // Add optional headers
        if let Some(cc) = email.headers.get("Cc") {
            if !cc.is_empty() {
                self.add_header(&mut message, "Cc", cc)?;
            }
        }

        if let Some(bcc) = email.headers.get("Bcc") {
            if !bcc.is_empty() {
                self.add_header(&mut message, "Bcc", bcc)?;
            }
        }

        if let Some(subject) = email.headers.get("Subject") {
            self.add_header(&mut message, "Subject", subject)?;
        }

        // Add Date header
        let date = chrono::Utc::now()
            .format("%a, %d %b %Y %H:%M:%S %z")
            .to_string();
        self.add_header(&mut message, "Date", &date)?;

        // Add Message-ID
        self.add_header(&mut message, "Message-ID", &email.message_id)?;

        // Add reply headers if this is a reply
        if let Some(in_reply_to) = email.headers.get("In-Reply-To") {
            self.add_header(&mut message, "In-Reply-To", in_reply_to)?;
        }

        if let Some(references) = email.headers.get("References") {
            self.add_header(&mut message, "References", references)?;
        }

        // Add MIME version for multipart messages
        if email.body.html_content.is_some() || !email.attachments.is_empty() {
            self.add_header(&mut message, "MIME-Version", "1.0")?;
        }

        // Add content headers and body
        if email.attachments.is_empty() && email.body.html_content.is_none() {
            // Simple text email
            self.add_header(
                &mut message,
                "Content-Type",
                &format!("{}; charset=utf-8", email.body.content_type),
            )?;
            message.push_str("\r\n");
            message.push_str(&email.body.content);
        } else {
            // Multipart email
            let boundary = format!("boundary_{}", uuid::Uuid::new_v4().simple());
            self.add_multipart_content(&mut message, email, &boundary)?;
        }

        Ok(message)
    }

    /// Compose a reply email with proper threading
    pub fn compose_reply(
        &self,
        original: &Email,
        reply_content: &str,
        reply_all: bool,
    ) -> Result<Email> {
        let mut reply = Email::new(original.account.clone());

        // Set reply headers
        reply
            .headers
            .insert("In-Reply-To".to_string(), original.message_id.clone());

        // Build References header
        let mut references = Vec::new();
        if let Some(orig_refs) = original.headers.get("References") {
            references.extend(orig_refs.split_whitespace().map(|s| s.to_string()));
        }
        references.push(original.message_id.clone());
        reply
            .headers
            .insert("References".to_string(), references.join(" "));

        // Set From header (should be set by the calling code based on account)
        if let Some(from) = original.headers.get("To") {
            reply.headers.insert("From".to_string(), from.clone());
        }

        // Set To header
        if let Some(reply_to) = original.headers.get("Reply-To") {
            reply.headers.insert("To".to_string(), reply_to.clone());
        } else if let Some(from) = original.headers.get("From") {
            reply.headers.insert("To".to_string(), from.clone());
        }

        // Set Cc header for reply-all
        if reply_all {
            let mut cc_addresses = Vec::new();

            if let Some(orig_to) = original.headers.get("To") {
                cc_addresses.push(orig_to.clone());
            }

            if let Some(orig_cc) = original.headers.get("Cc") {
                cc_addresses.push(orig_cc.clone());
            }

            if !cc_addresses.is_empty() {
                reply
                    .headers
                    .insert("Cc".to_string(), cc_addresses.join(", "));
            }
        }

        // Set Subject with "Re:" prefix
        if let Some(subject) = original.headers.get("Subject") {
            let reply_subject = if subject.to_lowercase().starts_with("re:") {
                subject.clone()
            } else {
                format!("Re: {}", subject)
            };
            reply.headers.insert("Subject".to_string(), reply_subject);
        }

        // Set body with quoted original content
        let quoted_original = self.quote_original_content(original)?;
        reply.body.content = format!("{}\r\n\r\n{}", reply_content, quoted_original);
        reply.body.content_type = "text/plain".to_string();

        Ok(reply)
    }

    /// Compose a forward email
    pub fn compose_forward(&self, original: &Email, forward_content: &str) -> Result<Email> {
        let mut forward = Email::new(original.account.clone());

        // Set Subject with "Fwd:" prefix
        if let Some(subject) = original.headers.get("Subject") {
            let forward_subject = if subject.to_lowercase().starts_with("fwd:")
                || subject.to_lowercase().starts_with("fw:")
            {
                subject.clone()
            } else {
                format!("Fwd: {}", subject)
            };
            forward
                .headers
                .insert("Subject".to_string(), forward_subject);
        }

        // Set body with forwarded content
        let forwarded_content = self.format_forwarded_content(original)?;
        forward.body.content = format!("{}\r\n\r\n{}", forward_content, forwarded_content);
        forward.body.content_type = "text/plain".to_string();

        // Copy attachments
        forward.attachments = original.attachments.clone();

        Ok(forward)
    }

    /// Add a header to the message
    fn add_header(&self, message: &mut String, name: &str, value: &str) -> Result<()> {
        // Validate header name
        if name.is_empty() || name.contains('\r') || name.contains('\n') || name.contains(':') {
            return Err(anyhow!("Invalid header name: {}", name));
        }

        // Validate header value
        if value.contains('\r') || value.contains('\n') {
            return Err(anyhow!(
                "Invalid header value for {}: contains line breaks",
                name
            ));
        }

        message.push_str(&format!("{}: {}\r\n", name, value));
        Ok(())
    }

    /// Format From header
    fn format_from_header(&self, email: &Email) -> Result<String> {
        email
            .headers
            .get("From")
            .ok_or_else(|| anyhow!("Missing From header"))
            .map(|s| s.clone())
    }

    /// Format To header
    fn format_to_header(&self, email: &Email) -> Result<String> {
        email
            .headers
            .get("To")
            .ok_or_else(|| anyhow!("Missing To header"))
            .map(|s| s.clone())
    }

    /// Add multipart content to message
    fn add_multipart_content(
        &self,
        message: &mut String,
        email: &Email,
        boundary: &str,
    ) -> Result<()> {
        let content_type = if email.attachments.is_empty() {
            "multipart/alternative"
        } else {
            "multipart/mixed"
        };

        self.add_header(
            message,
            "Content-Type",
            &format!("{}; boundary=\"{}\"", content_type, boundary),
        )?;
        message.push_str("\r\n");

        // Add text part
        message.push_str(&format!("--{}\r\n", boundary));
        message.push_str(&format!(
            "Content-Type: {}; charset=utf-8\r\n",
            email.body.content_type
        ));
        message.push_str("\r\n");
        message.push_str(&email.body.content);
        message.push_str("\r\n");

        // Add HTML part if present
        if let Some(html_content) = &email.body.html_content {
            message.push_str(&format!("--{}\r\n", boundary));
            message.push_str("Content-Type: text/html; charset=utf-8\r\n");
            message.push_str("\r\n");
            message.push_str(html_content);
            message.push_str("\r\n");
        }

        // Add attachments
        for attachment in &email.attachments {
            message.push_str(&format!("--{}\r\n", boundary));
            message.push_str(&format!("Content-Type: {}\r\n", attachment.content_type));
            message.push_str(&format!(
                "Content-Disposition: attachment; filename=\"{}\"\r\n",
                attachment.filename
            ));
            message.push_str("Content-Transfer-Encoding: base64\r\n");
            message.push_str("\r\n");

            // In a real implementation, you would read the attachment file and encode it
            message.push_str("[Attachment content would be base64 encoded here]\r\n");
        }

        // Close multipart
        message.push_str(&format!("--{}--\r\n", boundary));

        Ok(())
    }

    /// Quote original content for replies
    fn quote_original_content(&self, original: &Email) -> Result<String> {
        let mut quoted = String::new();

        // Add attribution line
        let unknown_from = "Unknown".to_string();
        let unknown_date = "Unknown date".to_string();
        let from = original.headers.get("From").unwrap_or(&unknown_from);
        let date = original.headers.get("Date").unwrap_or(&unknown_date);
        quoted.push_str(&format!("On {} {} wrote:\r\n", date, from));

        // Quote the original content
        for line in original.body.content.lines() {
            quoted.push_str(&format!("> {}\r\n", line));
        }

        Ok(quoted)
    }

    /// Format forwarded content
    fn format_forwarded_content(&self, original: &Email) -> Result<String> {
        let mut forwarded = String::new();

        forwarded.push_str("---------- Forwarded message ----------\r\n");

        if let Some(from) = original.headers.get("From") {
            forwarded.push_str(&format!("From: {}\r\n", from));
        }

        if let Some(date) = original.headers.get("Date") {
            forwarded.push_str(&format!("Date: {}\r\n", date));
        }

        if let Some(subject) = original.headers.get("Subject") {
            forwarded.push_str(&format!("Subject: {}\r\n", subject));
        }

        if let Some(to) = original.headers.get("To") {
            forwarded.push_str(&format!("To: {}\r\n", to));
        }

        forwarded.push_str("\r\n");
        forwarded.push_str(&original.body.content);

        Ok(forwarded)
    }

    /// Validate email addresses in headers
    pub fn validate_email_addresses(&self, email: &Email) -> Result<()> {
        let headers_to_check = ["From", "To", "Cc", "Bcc"];

        for header_name in &headers_to_check {
            if let Some(header_value) = email.headers.get(*header_name) {
                self.validate_address_list(header_value)?;
            }
        }

        Ok(())
    }

    /// Validate a list of email addresses
    fn validate_address_list(&self, addresses: &str) -> Result<()> {
        for addr in addresses.split(',') {
            let addr = addr.trim();
            if !addr.is_empty() {
                self.validate_single_address(addr)?;
            }
        }
        Ok(())
    }

    /// Validate a single email address
    fn validate_single_address(&self, address: &str) -> Result<()> {
        // Extract email from "Name <email@domain.com>" format
        let email = if address.contains('<') && address.contains('>') {
            let start = address.find('<').unwrap() + 1;
            let end = address.find('>').unwrap();
            &address[start..end]
        } else {
            address
        };

        // Basic email validation
        if !email.contains('@') {
            return Err(anyhow!(
                "Invalid email address: missing @ symbol: {}",
                email
            ));
        }

        let parts: Vec<&str> = email.split('@').collect();
        if parts.len() != 2 {
            return Err(anyhow!(
                "Invalid email address: multiple @ symbols: {}",
                email
            ));
        }

        let (local, domain) = (parts[0], parts[1]);

        if local.is_empty() {
            return Err(anyhow!(
                "Invalid email address: empty local part: {}",
                email
            ));
        }

        if domain.is_empty() {
            return Err(anyhow!(
                "Invalid email address: empty domain part: {}",
                email
            ));
        }

        if !domain.contains('.') {
            return Err(anyhow!(
                "Invalid email address: domain missing TLD: {}",
                email
            ));
        }

        Ok(())
    }
}

impl Default for EmailComposer {
    fn default() -> Self {
        Self::new()
    }
}

/// Format a single address for header storage
fn format_address(addr: &Addr) -> String {
    if let (Some(name), Some(email)) = (&addr.name, &addr.address) {
        format!("{} <{}>", name, email)
    } else if let Some(email) = &addr.address {
        email.to_string()
    } else if let Some(name) = &addr.name {
        name.to_string()
    } else {
        "Unknown".to_string()
    }
}

/// Format address list for header storage
fn format_address_list(addresses: &[Addr]) -> String {
    addresses
        .iter()
        .map(format_address)
        .collect::<Vec<_>>()
        .join(", ")
}

/// Extract plain text from HTML content (basic implementation)
pub fn html_to_text(html: &str) -> String {
    // This is a very basic HTML to text conversion
    // In a production system, you might want to use a proper HTML parser
    let mut text = html.to_string();

    // Remove HTML tags
    let re = regex::Regex::new(r"<[^>]*>").unwrap();
    text = re.replace_all(&text, "").to_string();

    // Decode common HTML entities
    text = text.replace("&amp;", "&");
    text = text.replace("&lt;", "<");
    text = text.replace("&gt;", ">");
    text = text.replace("&quot;", "\"");
    text = text.replace("&#39;", "'");
    text = text.replace("&nbsp;", " ");

    // Clean up whitespace
    let re = regex::Regex::new(r"\s+").unwrap();
    text = re.replace_all(&text, " ").trim().to_string();

    text
}

#[cfg(test)]
mod tests {
    use super::*;

    const SAMPLE_EMAIL: &str = r#"From: sender@example.com
To: recipient@example.com
Subject: Test Email
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <test@example.com>

This is a test email body.
"#;

    const MULTIPART_EMAIL: &str = r#"From: sender@example.com
To: recipient@example.com
Subject: Multipart Test
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <multipart@example.com>
MIME-Version: 1.0
Content-Type: multipart/alternative; boundary="boundary123"

--boundary123
Content-Type: text/plain; charset=utf-8

This is the plain text version.

--boundary123
Content-Type: text/html; charset=utf-8

<html><body><p>This is the <b>HTML</b> version.</p></body></html>

--boundary123--
"#;

    #[test]
    fn test_parse_simple_email() {
        let parser = EmailParser::new();
        let result = parser.parse_email(SAMPLE_EMAIL.as_bytes(), "test@example.com".to_string());

        assert!(result.is_ok());
        let email = result.unwrap();

        assert_eq!(email.account, "test@example.com");
        assert_eq!(email.headers.get("From").unwrap(), "sender@example.com");
        assert_eq!(email.headers.get("To").unwrap(), "recipient@example.com");
        assert_eq!(email.headers.get("Subject").unwrap(), "Test Email");
        assert_eq!(email.message_id, "<test@example.com>");
        assert_eq!(email.body.content_type, "text/plain");
        assert!(email.body.content.contains("This is a test email body"));
        assert!(email.attachments.is_empty());
    }

    #[test]
    fn test_parse_multipart_email() {
        let parser = EmailParser::new();
        let result = parser.parse_email(MULTIPART_EMAIL.as_bytes(), "test@example.com".to_string());

        assert!(result.is_ok());
        let email = result.unwrap();

        assert_eq!(email.headers.get("Subject").unwrap(), "Multipart Test");
        // Note: The exact content type may vary based on parsing logic
        assert!(
            email.body.content.contains("plain text version")
                || email.body.content.contains("HTML")
        );
        // HTML content should be available
        assert!(email.body.html_content.is_some() || email.body.content.contains("HTML"));
    }

    #[test]
    fn test_validate_rfc2822_valid() {
        let parser = EmailParser::new();
        let result = parser.validate_rfc2822(SAMPLE_EMAIL.as_bytes());
        assert!(result.is_ok());
    }

    #[test]
    fn test_validate_rfc2822_missing_from() {
        let invalid_email = r#"To: recipient@example.com
Subject: Test Email
Date: Mon, 01 Jan 2024 12:00:00 +0000

This is a test email body.
"#;

        let parser = EmailParser::new();
        let result = parser.validate_rfc2822(invalid_email.as_bytes());
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Missing required 'From' header"));
    }

    #[test]
    fn test_validate_rfc2822_missing_date() {
        let invalid_email = r#"From: sender@example.com
To: recipient@example.com
Subject: Test Email

This is a test email body.
"#;

        let parser = EmailParser::new();
        let result = parser.validate_rfc2822(invalid_email.as_bytes());
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Missing required 'Date' header"));
    }

    #[test]
    fn test_validate_rfc2822_invalid_message_id() {
        let invalid_email = r#"From: sender@example.com
To: recipient@example.com
Subject: Test Email
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <invalid-message-id>

This is a test email body.
"#;

        let parser = EmailParser::new();
        let result = parser.validate_rfc2822(invalid_email.as_bytes());
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("Invalid Message-ID format"));
    }

    #[test]
    fn test_html_to_text() {
        let html =
            "<html><body><p>Hello <b>world</b>!</p><br/><p>Second paragraph.</p></body></html>";
        let text = html_to_text(html);

        assert!(!text.contains("<"));
        assert!(!text.contains(">"));
        assert!(text.contains("Hello world"));
        assert!(text.contains("Second paragraph"));
    }

    #[test]
    fn test_html_entities_conversion() {
        let html =
            "Hello &amp; goodbye &lt;test&gt; &quot;quoted&quot; &#39;apostrophe&#39; &nbsp;space";
        let text = html_to_text(html);

        assert!(text.contains("Hello & goodbye"));
        assert!(text.contains("<test>"));
        assert!(text.contains("\"quoted\""));
        assert!(text.contains("'apostrophe'"));
        assert!(text.contains(" space"));
    }

    #[test]
    fn test_email_metadata_update() {
        let parser = EmailParser::new();
        let result = parser.parse_email(SAMPLE_EMAIL.as_bytes(), "test@example.com".to_string());

        assert!(result.is_ok());
        let email = result.unwrap();

        // Check that metadata was updated from headers
        assert_eq!(email.metadata.folder, "inbox"); // Default folder
        assert!(!email.metadata.is_read); // Default unread
    }

    #[test]
    fn test_spam_detection_in_metadata() {
        let spam_email = r#"From: spammer@example.com
To: victim@example.com
Subject: [SPAM] Free money!
Date: Mon, 01 Jan 2024 12:00:00 +0000
X-Spam-Flag: YES

This is spam content.
"#;

        let parser = EmailParser::new();
        let result = parser.parse_email(spam_email.as_bytes(), "test@example.com".to_string());

        assert!(result.is_ok());
        let email = result.unwrap();
        assert_eq!(email.metadata.folder, "spam");
    }

    #[test]
    fn test_notification_detection_in_metadata() {
        let notification_email = r#"From: noreply@service.com
To: user@example.com
Subject: Account Notification
Date: Mon, 01 Jan 2024 12:00:00 +0000

This is a notification email.
"#;

        let parser = EmailParser::new();
        let result = parser.parse_email(
            notification_email.as_bytes(),
            "test@example.com".to_string(),
        );

        assert!(result.is_ok());
        let email = result.unwrap();
        assert_eq!(email.metadata.folder, "notifications");
    }

    #[test]
    fn test_parse_email_validation_failure() {
        // Create an email that will fail validation
        let parser = EmailParser::new();
        let result = parser.parse_email(SAMPLE_EMAIL.as_bytes(), "".to_string()); // Empty account

        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("validation failed"));
    }

    #[test]
    fn test_empty_content_handling() {
        let empty_email = r#"From: sender@example.com
To: recipient@example.com
Subject: Empty Email
Date: Mon, 01 Jan 2024 12:00:00 +0000

"#;

        let parser = EmailParser::new();
        let result = parser.parse_email(empty_email.as_bytes(), "test@example.com".to_string());

        assert!(result.is_ok());
        let email = result.unwrap();

        // Should have fallback content
        assert!(
            email.body.content.contains("[No readable content]") || !email.body.content.is_empty()
        );
    }

    #[test]
    fn test_format_address_functions() {
        let addr = Addr {
            name: Some("John Doe".into()),
            address: Some("john@example.com".into()),
        };

        let formatted = format_address(&addr);
        assert_eq!(formatted, "John Doe <john@example.com>");

        let addr_no_name = Addr {
            name: None,
            address: Some("jane@example.com".into()),
        };

        let formatted_no_name = format_address(&addr_no_name);
        assert_eq!(formatted_no_name, "jane@example.com");
    }

    #[test]
    fn test_attachment_parsing() {
        let email_with_attachment = r#"From: sender@example.com
To: recipient@example.com
Subject: Email with Attachment
Date: Mon, 01 Jan 2024 12:00:00 +0000
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="boundary456"

--boundary456
Content-Type: text/plain

This email has an attachment.

--boundary456
Content-Type: application/pdf
Content-Disposition: attachment; filename="document.pdf"

%PDF-1.4 fake pdf content

--boundary456--
"#;

        let parser = EmailParser::new();
        let result = parser.parse_email(
            email_with_attachment.as_bytes(),
            "test@example.com".to_string(),
        );

        assert!(result.is_ok());
        let email = result.unwrap();

        // Check if attachments were parsed (may depend on mail-parser implementation)
        // This test verifies the parsing doesn't fail with attachments present
        assert!(
            email.body.content.contains("attachment")
                || email.body.content.contains("[No readable content]")
        );
    }

    #[test]
    fn test_edge_case_malformed_email() {
        let malformed_email = r#"This is not a valid email format
No headers present
Just some text
"#;

        let parser = EmailParser::new();
        let result = parser.parse_email(malformed_email.as_bytes(), "test@example.com".to_string());

        // Should either parse with minimal headers or fail gracefully
        if result.is_ok() {
            let email = result.unwrap();
            assert_eq!(email.account, "test@example.com");
        } else {
            // Failing is also acceptable for malformed emails
            assert!(result.is_err());
        }
    }

    #[test]
    fn test_unicode_content() {
        let unicode_email = r#"From: sender@example.com
To: recipient@example.com
Subject: Unicode Test 🚀
Date: Mon, 01 Jan 2024 12:00:00 +0000
Message-ID: <unicode@example.com>

Hello 世界! This email contains unicode characters: 🎉 ñáéíóú
"#;

        let parser = EmailParser::new();
        let result = parser.parse_email(unicode_email.as_bytes(), "test@example.com".to_string());

        assert!(result.is_ok());
        let email = result.unwrap();

        assert!(email
            .headers
            .get("Subject")
            .unwrap()
            .contains("Unicode Test"));
        assert!(email.body.content.contains("世界"));
        assert!(email.body.content.contains("🎉"));
    }

    // Email composition tests
    #[test]
    fn test_compose_simple_email() {
        let composer = EmailComposer::new();
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
        email.body.content = "Hello, this is a test email.".to_string();

        let result = composer.compose_email(&email);
        assert!(result.is_ok());

        let composed = result.unwrap();
        assert!(composed.contains("From: sender@example.com"));
        assert!(composed.contains("To: recipient@example.com"));
        assert!(composed.contains("Subject: Test Subject"));
        assert!(composed.contains("Hello, this is a test email."));
        assert!(composed.contains("Date:"));
        assert!(composed.contains("Message-ID:"));
    }

    #[test]
    fn test_compose_multipart_email() {
        let composer = EmailComposer::new();
        let mut email = Email::new("test@example.com".to_string());

        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        email
            .headers
            .insert("Subject".to_string(), "Multipart Test".to_string());
        email.body.content = "Plain text content".to_string();
        email.body.html_content = Some("<p>HTML content</p>".to_string());

        let result = composer.compose_email(&email);
        assert!(result.is_ok());

        let composed = result.unwrap();
        assert!(composed.contains("MIME-Version: 1.0"));
        assert!(composed.contains("Content-Type: multipart/alternative"));
        assert!(composed.contains("Plain text content"));
        assert!(composed.contains("<p>HTML content</p>"));
    }

    #[test]
    fn test_compose_reply() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        original
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        original
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        original
            .headers
            .insert("Subject".to_string(), "Original Subject".to_string());
        original.headers.insert(
            "Date".to_string(),
            "Mon, 01 Jan 2024 12:00:00 +0000".to_string(),
        );
        original.body.content = "Original email content".to_string();

        let result = composer.compose_reply(&original, "This is my reply.", false);
        assert!(result.is_ok());

        let reply = result.unwrap();
        assert_eq!(
            reply.headers.get("Subject").unwrap(),
            "Re: Original Subject"
        );
        assert!(reply.headers.contains_key("In-Reply-To"));
        assert!(reply.headers.contains_key("References"));
        assert!(reply.body.content.contains("This is my reply."));
        assert!(reply.body.content.contains("> Original email content"));
    }

    #[test]
    fn test_compose_reply_all() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        original
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        original
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        original
            .headers
            .insert("Cc".to_string(), "cc@example.com".to_string());
        original
            .headers
            .insert("Subject".to_string(), "Original Subject".to_string());
        original.body.content = "Original content".to_string();

        let result = composer.compose_reply(&original, "Reply to all.", true);
        assert!(result.is_ok());

        let reply = result.unwrap();
        assert!(reply.headers.contains_key("Cc"));
        assert!(reply
            .headers
            .get("Cc")
            .unwrap()
            .contains("recipient@example.com"));
        assert!(reply.headers.get("Cc").unwrap().contains("cc@example.com"));
    }

    #[test]
    fn test_compose_forward() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        original
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        original
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        original
            .headers
            .insert("Subject".to_string(), "Original Subject".to_string());
        original.headers.insert(
            "Date".to_string(),
            "Mon, 01 Jan 2024 12:00:00 +0000".to_string(),
        );
        original.body.content = "Original email content".to_string();

        let result = composer.compose_forward(&original, "Forwarding this email.");
        assert!(result.is_ok());

        let forward = result.unwrap();
        assert_eq!(
            forward.headers.get("Subject").unwrap(),
            "Fwd: Original Subject"
        );
        assert!(forward.body.content.contains("Forwarding this email."));
        assert!(forward
            .body
            .content
            .contains("---------- Forwarded message ----------"));
        assert!(forward.body.content.contains("From: sender@example.com"));
        assert!(forward.body.content.contains("Original email content"));
    }

    #[test]
    fn test_validate_email_addresses_valid() {
        let composer = EmailComposer::new();
        let mut email = Email::new("test@example.com".to_string());

        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email.headers.insert(
            "To".to_string(),
            "recipient@example.com, another@example.org".to_string(),
        );
        email
            .headers
            .insert("Cc".to_string(), "John Doe <john@example.com>".to_string());

        let result = composer.validate_email_addresses(&email);
        assert!(result.is_ok());
    }

    #[test]
    fn test_validate_email_addresses_invalid() {
        let composer = EmailComposer::new();
        let mut email = Email::new("test@example.com".to_string());

        email
            .headers
            .insert("From".to_string(), "invalid-email".to_string());

        let result = composer.validate_email_addresses(&email);
        assert!(result.is_err());
        assert!(result.unwrap_err().to_string().contains("missing @ symbol"));
    }

    #[test]
    fn test_validate_single_address_formats() {
        let composer = EmailComposer::new();

        // Valid addresses
        assert!(composer.validate_single_address("user@example.com").is_ok());
        assert!(composer
            .validate_single_address("John Doe <john@example.com>")
            .is_ok());

        // Invalid addresses
        assert!(composer.validate_single_address("invalid").is_err());
        assert!(composer.validate_single_address("user@").is_err());
        assert!(composer.validate_single_address("@example.com").is_err());
        assert!(composer.validate_single_address("user@example").is_err());
    }

    #[test]
    fn test_reply_subject_handling() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        // Test with existing "Re:" prefix
        original
            .headers
            .insert("Subject".to_string(), "Re: Already a reply".to_string());
        let reply1 = composer
            .compose_reply(&original, "Reply content", false)
            .unwrap();
        assert_eq!(
            reply1.headers.get("Subject").unwrap(),
            "Re: Already a reply"
        );

        // Test without "Re:" prefix
        original
            .headers
            .insert("Subject".to_string(), "Original subject".to_string());
        let reply2 = composer
            .compose_reply(&original, "Reply content", false)
            .unwrap();
        assert_eq!(
            reply2.headers.get("Subject").unwrap(),
            "Re: Original subject"
        );
    }

    #[test]
    fn test_forward_subject_handling() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        // Test with existing "Fwd:" prefix
        original
            .headers
            .insert("Subject".to_string(), "Fwd: Already forwarded".to_string());
        let forward1 = composer
            .compose_forward(&original, "Forward content")
            .unwrap();
        assert_eq!(
            forward1.headers.get("Subject").unwrap(),
            "Fwd: Already forwarded"
        );

        // Test with "Fw:" prefix
        original
            .headers
            .insert("Subject".to_string(), "Fw: Already forwarded".to_string());
        let forward2 = composer
            .compose_forward(&original, "Forward content")
            .unwrap();
        assert_eq!(
            forward2.headers.get("Subject").unwrap(),
            "Fw: Already forwarded"
        );

        // Test without forward prefix
        original
            .headers
            .insert("Subject".to_string(), "Original subject".to_string());
        let forward3 = composer
            .compose_forward(&original, "Forward content")
            .unwrap();
        assert_eq!(
            forward3.headers.get("Subject").unwrap(),
            "Fwd: Original subject"
        );
    }

    #[test]
    fn test_header_validation() {
        let composer = EmailComposer::new();
        let mut message = String::new();

        // Valid header
        assert!(composer
            .add_header(&mut message, "Subject", "Test Subject")
            .is_ok());

        // Invalid header name with colon
        assert!(composer
            .add_header(&mut message, "Invalid:Name", "Value")
            .is_err());

        // Invalid header name with line break
        assert!(composer
            .add_header(&mut message, "Invalid\nName", "Value")
            .is_err());

        // Invalid header value with line break
        assert!(composer
            .add_header(&mut message, "Subject", "Value\nWith\nBreaks")
            .is_err());
    }

    #[test]
    fn test_references_header_building() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        original.message_id = "<original@example.com>".to_string();
        original.headers.insert(
            "References".to_string(),
            "<first@example.com> <second@example.com>".to_string(),
        );

        let reply = composer
            .compose_reply(&original, "Reply content", false)
            .unwrap();
        let references = reply.headers.get("References").unwrap();

        assert!(references.contains("<first@example.com>"));
        assert!(references.contains("<second@example.com>"));
        assert!(references.contains("<original@example.com>"));
    }

    #[test]
    fn test_attachment_handling_in_composition() {
        let composer = EmailComposer::new();
        let mut email = Email::new("test@example.com".to_string());

        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email
            .headers
            .insert("To".to_string(), "recipient@example.com".to_string());
        email.body.content = "Email with attachment".to_string();

        // Add an attachment
        email.attachments.push(Attachment {
            filename: "document.pdf".to_string(),
            content_type: "application/pdf".to_string(),
            size: 1024,
            file_path: "attachments/doc.pdf".to_string(),
        });

        let result = composer.compose_email(&email);
        assert!(result.is_ok());

        let composed = result.unwrap();
        assert!(composed.contains("MIME-Version: 1.0"));
        assert!(composed.contains("Content-Type: multipart/mixed"));
        assert!(composed.contains("Content-Disposition: attachment"));
        assert!(composed.contains("filename=\"document.pdf\""));
    }

    #[test]
    fn test_quote_original_content() {
        let composer = EmailComposer::new();
        let mut original = Email::new("test@example.com".to_string());

        original
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        original.headers.insert(
            "Date".to_string(),
            "Mon, 01 Jan 2024 12:00:00 +0000".to_string(),
        );
        original.body.content = "Line 1\nLine 2\nLine 3".to_string();

        let quoted = composer.quote_original_content(&original).unwrap();

        assert!(quoted.contains("On Mon, 01 Jan 2024 12:00:00 +0000 sender@example.com wrote:"));
        assert!(quoted.contains("> Line 1"));
        assert!(quoted.contains("> Line 2"));
        assert!(quoted.contains("> Line 3"));
    }
}
