//! External text editor integration

use crate::error::{GitMailError, Result};
use crate::models::Email;
use std::env;
use std::fs;
use std::process::Command;
use tempfile::NamedTempFile;

/// Editor integration interface
pub trait EditorIntegration {
    /// Open external editor for email composition
    fn compose_email(&self, template: Option<&str>) -> Result<String>;
    
    /// Open external editor for email reply
    fn reply_email(&self, original: &Email) -> Result<String>;
    
    /// Open external editor for email forward
    fn forward_email(&self, original: &Email) -> Result<String>;
}

/// Default editor integration implementation
pub struct DefaultEditorIntegration {
    editor_command: String,
}

impl DefaultEditorIntegration {
    /// Create a new editor integration
    pub fn new() -> Self {
        let editor_command = env::var("EDITOR")
            .or_else(|_| env::var("VISUAL"))
            .unwrap_or_else(|_| "nano".to_string());
        
        Self { editor_command }
    }
    
    /// Launch external editor with content
    fn launch_editor(&self, content: &str) -> Result<String> {
        // Create temporary file
        let temp_file = NamedTempFile::new()
            .map_err(|e| GitMailError::Editor(format!("Failed to create temp file: {}", e)))?;
        
        // Write initial content
        fs::write(temp_file.path(), content)
            .map_err(|e| GitMailError::Editor(format!("Failed to write temp file: {}", e)))?;
        
        // Launch editor
        let status = Command::new(&self.editor_command)
            .arg(temp_file.path())
            .status()
            .map_err(|e| GitMailError::Editor(format!("Failed to launch editor: {}", e)))?;
        
        if !status.success() {
            return Err(GitMailError::Editor("Editor exited with error".to_string()));
        }
        
        // Read edited content
        let edited_content = fs::read_to_string(temp_file.path())
            .map_err(|e| GitMailError::Editor(format!("Failed to read edited content: {}", e)))?;
        
        Ok(edited_content)
    }
    
    /// Generate reply template
    fn generate_reply_template(&self, original: &Email) -> String {
        let subject = original.headers.get("Subject")
            .map(|s| if s.starts_with("Re: ") { s.clone() } else { format!("Re: {}", s) })
            .unwrap_or_else(|| "Re: ".to_string());
        
        let from = original.headers.get("From").unwrap_or(&original.account);
        let date = original.headers.get("Date").map_or("Unknown date", |v| v.as_str());
        
        format!(
            "To: {}\nSubject: {}\n\n\n\nOn {}, {} wrote:\n> {}\n",
            from,
            subject,
            date,
            from,
            original.body.content.lines().collect::<Vec<_>>().join("\n> ")
        )
    }
    
    /// Generate forward template
    fn generate_forward_template(&self, original: &Email) -> String {
        let subject = original.headers.get("Subject")
            .map(|s| if s.starts_with("Fwd: ") { s.clone() } else { format!("Fwd: {}", s) })
            .unwrap_or_else(|| "Fwd: ".to_string());
        
        let from = original.headers.get("From").unwrap_or(&original.account);
        let date = original.headers.get("Date").map_or("Unknown date", |v| v.as_str());
        let to = original.headers.get("To").map_or("Unknown recipient", |v| v.as_str());
        
        format!(
            "To: \nSubject: {}\n\n\n\n---------- Forwarded message ---------\nFrom: {}\nDate: {}\nTo: {}\nSubject: {}\n\n{}\n",
            subject,
            from,
            date,
            to,
            original.headers.get("Subject").map_or("", |v| v.as_str()),
            original.body.content
        )
    }
}

impl EditorIntegration for DefaultEditorIntegration {
    fn compose_email(&self, template: Option<&str>) -> Result<String> {
        let content = template.unwrap_or("To: \nSubject: \n\n");
        self.launch_editor(content)
    }
    
    fn reply_email(&self, original: &Email) -> Result<String> {
        let template = self.generate_reply_template(original);
        self.launch_editor(&template)
    }
    
    fn forward_email(&self, original: &Email) -> Result<String> {
        let template = self.generate_forward_template(original);
        self.launch_editor(&template)
    }
}