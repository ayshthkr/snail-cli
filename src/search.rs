//! Search functionality for Git-Mail
//!
//! Provides full-text search capabilities leveraging command-line tools like grep, awk, etc.

use crate::error::{GitMailError, Result};
use crate::git_storage::GitStorage;
use crate::models::EmailMetadata;
use regex::Regex;
use std::collections::HashMap;
use std::fs;
use std::path::{Path, PathBuf};

/// Search query parameters
#[derive(Debug, Clone)]
pub struct SearchQuery {
    /// Search term or pattern
    pub term: String,
    /// Whether to use regex matching
    pub regex: bool,
    /// Case sensitive search
    pub case_sensitive: bool,
    /// Search in specific folder only
    pub folder: Option<String>,
    /// Search in specific fields only (headers, body, metadata)
    pub fields: Vec<SearchField>,
    /// Date range filter
    pub date_range: Option<DateRange>,
    /// Additional tags to filter by
    pub tags: Vec<String>,
    /// Maximum number of results
    pub limit: Option<usize>,
}

/// Search field specification
#[derive(Debug, Clone, PartialEq)]
pub enum SearchField {
    /// Search in email headers
    Headers,
    /// Search in email body content
    Body,
    /// Search in email metadata
    Metadata,
    /// Search in specific header field
    Header(String),
    /// Search in attachments
    Attachments,
}

/// Date range for filtering search results
#[derive(Debug, Clone)]
pub struct DateRange {
    /// Start date (inclusive)
    pub start: chrono::DateTime<chrono::Utc>,
    /// End date (inclusive)
    pub end: chrono::DateTime<chrono::Utc>,
}

/// Search result containing email metadata and match information
#[derive(Debug, Clone)]
pub struct SearchResult {
    /// Email metadata
    pub email_metadata: EmailMetadata,
    /// Matching lines with context
    pub matches: Vec<SearchMatch>,
    /// Relevance score (0.0 to 1.0)
    pub score: f64,
}

/// Individual search match with context
#[derive(Debug, Clone)]
pub struct SearchMatch {
    /// Line number in the email file
    pub line_number: usize,
    /// The matching line content
    pub line: String,
    /// Context lines before the match
    pub context_before: Vec<String>,
    /// Context lines after the match
    pub context_after: Vec<String>,
    /// Field where the match was found
    pub field: SearchField,
}

/// Search engine interface
pub trait SearchEngine {
    /// Perform a search query
    fn search(&self, query: &SearchQuery) -> Result<Vec<SearchResult>>;

    /// Get search suggestions based on partial input
    fn suggest(&self, partial: &str) -> Result<Vec<String>>;

    /// Index emails for faster searching (optional optimization)
    fn index_emails(&self) -> Result<()>;
}

/// Command-line tool based search engine
pub struct CommandLineSearchEngine<T: GitStorage> {
    storage: T,
    repository_path: String,
}

impl<T: GitStorage> CommandLineSearchEngine<T> {
    /// Create a new command-line search engine
    pub fn new(storage: T, repository_path: String) -> Self {
        Self {
            storage,
            repository_path,
        }
    }

    /// Manual search implementation
    fn manual_search(&self, query: &SearchQuery) -> Result<Vec<(PathBuf, Vec<String>)>> {
        let mut results = Vec::new();
        let mut email_files = Vec::new();

        // Collect email files
        let search_path = if let Some(folder) = &query.folder {
            Path::new(&self.repository_path).join(folder)
        } else {
            Path::new(&self.repository_path).to_path_buf()
        };

        self.collect_email_files(&search_path, &mut email_files)?;

        // Search each file
        for file_path in email_files {
            let content = fs::read_to_string(&file_path).map_err(|e| GitMailError::Io(e))?;
            let matching_lines = self.search_in_content(&content, query)?;

            if !matching_lines.is_empty() {
                results.push((file_path, matching_lines));
            }
        }

        Ok(results)
    }

    /// Search within file content
    fn search_in_content(&self, content: &str, query: &SearchQuery) -> Result<Vec<String>> {
        let mut matches = Vec::new();
        let search_term = if query.case_sensitive {
            query.term.clone()
        } else {
            query.term.to_lowercase()
        };

        for line in content.lines() {
            let search_line = if query.case_sensitive {
                line.to_string()
            } else {
                line.to_lowercase()
            };

            let is_match = if query.regex {
                // Use regex crate for proper regex support
                match Regex::new(&query.term) {
                    Ok(re) => re.is_match(line),
                    Err(_) => false, // Invalid regex, no match
                }
            } else {
                search_line.contains(&search_term)
            };

            if is_match {
                matches.push(line.to_string());
            }
        }

        Ok(matches)
    }

    /// Filter results by field specification
    fn filter_by_fields(&self, content: &str, fields: &[SearchField], query: &SearchQuery) -> bool {
        if fields.is_empty() {
            return true; // No field filter, include all
        }

        let lines: Vec<&str> = content.lines().collect();
        let mut current_section = "";
        let search_term = if query.case_sensitive {
            query.term.clone()
        } else {
            query.term.to_lowercase()
        };

        for line in &lines {
            if line.starts_with("--- ") && line.ends_with(" ---") {
                current_section = line;
                continue;
            }

            let search_line = if query.case_sensitive {
                line.to_string()
            } else {
                line.to_lowercase()
            };

            // Check if this line contains our search term
            let line_matches = if query.regex {
                match Regex::new(&query.term) {
                    Ok(re) => re.is_match(line),
                    Err(_) => false,
                }
            } else {
                search_line.contains(&search_term)
            };

            if line_matches {
                for field in fields {
                    match field {
                        SearchField::Headers => {
                            if current_section == "--- Email Headers ---" {
                                return true;
                            }
                        }
                        SearchField::Body => {
                            if current_section == "--- Body ---"
                                || current_section == "--- HTML Body ---"
                            {
                                return true;
                            }
                        }
                        SearchField::Metadata => {
                            if current_section == "--- Metadata ---" {
                                return true;
                            }
                        }
                        SearchField::Header(header_name) => {
                            if current_section == "--- Email Headers ---"
                                && line.starts_with(&format!("{}:", header_name))
                            {
                                return true;
                            }
                        }
                        SearchField::Attachments => {
                            if current_section == "--- Attachments ---" {
                                return true;
                            }
                        }
                    }
                }
            }
        }

        false
    }

    /// Calculate relevance score for search result
    fn calculate_score(&self, matches: &[SearchMatch], query: &SearchQuery) -> f64 {
        if matches.is_empty() {
            return 0.0;
        }

        let mut score = 0.0;
        let term_lower = query.term.to_lowercase();

        for search_match in matches {
            let line_lower = search_match.line.to_lowercase();

            // Base score for having a match
            score += 0.1;

            // Bonus for exact word matches
            if line_lower.contains(&format!(" {} ", term_lower)) {
                score += 0.3;
            }

            // Bonus for matches in important fields
            match search_match.field {
                SearchField::Header(ref name) if name == "Subject" => score += 0.4,
                SearchField::Header(ref name) if name == "From" => score += 0.3,
                SearchField::Headers => score += 0.2,
                SearchField::Body => score += 0.1,
                _ => {}
            }

            // Penalty for very long lines (likely less relevant)
            if search_match.line.len() > 200 {
                score -= 0.1;
            }
        }

        // Normalize score to 0.0-1.0 range
        (score / matches.len() as f64).min(1.0).max(0.0)
    }

    /// Convert file matches to search results
    fn convert_to_search_results(
        &self,
        file_matches: Vec<(PathBuf, Vec<String>)>,
        query: &SearchQuery,
    ) -> Result<Vec<SearchResult>> {
        let mut results = Vec::new();

        for (file_path, matching_lines) in file_matches {
            // Read the full email file
            let content = fs::read_to_string(&file_path).map_err(|e| GitMailError::Io(e))?;

            // Filter by fields if specified
            if !query.fields.is_empty() && !self.filter_by_fields(&content, &query.fields, query) {
                continue;
            }

            // Parse email to get metadata
            let email_id = self.extract_email_id_from_path(&file_path)?;
            let email = self.storage.retrieve_email(&email_id)?;

            // Apply date range filter
            if let Some(date_range) = &query.date_range {
                if email.metadata.created_at < date_range.start
                    || email.metadata.created_at > date_range.end
                {
                    continue;
                }
            }

            // Apply tag filter
            if !query.tags.is_empty() {
                let has_required_tags = query
                    .tags
                    .iter()
                    .all(|tag| email.metadata.tags.contains(tag));
                if !has_required_tags {
                    continue;
                }
            }

            // Create search matches
            let matches = self.create_search_matches(&content, &matching_lines, query)?;

            // Calculate relevance score
            let score = self.calculate_score(&matches, query);

            results.push(SearchResult {
                email_metadata: email.metadata,
                matches,
                score,
            });
        }

        // Sort by relevance score (highest first)
        results.sort_by(|a, b| {
            b.score
                .partial_cmp(&a.score)
                .unwrap_or(std::cmp::Ordering::Equal)
        });

        // Apply limit if specified
        if let Some(limit) = query.limit {
            results.truncate(limit);
        }

        Ok(results)
    }

    /// Extract email ID from file path
    fn extract_email_id_from_path(&self, path: &Path) -> Result<String> {
        let filename = path
            .file_name()
            .and_then(|n| n.to_str())
            .ok_or_else(|| GitMailError::Search("Invalid file path".to_string()))?;

        // Email files are named like: msg-{id_prefix}-{sender}-{subject}.txt
        if let Some(start) = filename.find("msg-") {
            let after_prefix = &filename[start + 4..];
            if let Some(end) = after_prefix.find('-') {
                return Ok(after_prefix[..end].to_string());
            }
        }

        Err(GitMailError::Search(
            "Could not extract email ID from filename".to_string(),
        ))
    }

    /// Create search matches from matching lines
    fn create_search_matches(
        &self,
        content: &str,
        matching_lines: &[String],
        _query: &SearchQuery,
    ) -> Result<Vec<SearchMatch>> {
        let lines: Vec<&str> = content.lines().collect();
        let mut matches = Vec::new();
        let mut current_section = SearchField::Body;

        for (line_num, line) in lines.iter().enumerate() {
            // Track current section
            if line.starts_with("--- ") && line.ends_with(" ---") {
                current_section = match *line {
                    "--- Email Headers ---" => SearchField::Headers,
                    "--- Body ---" | "--- HTML Body ---" => SearchField::Body,
                    "--- Metadata ---" => SearchField::Metadata,
                    "--- Attachments ---" => SearchField::Attachments,
                    _ => SearchField::Body,
                };
                continue;
            }

            // Check if this line matches any of our search results
            for matching_line in matching_lines {
                if line.contains(matching_line.trim()) || matching_line.contains(line.trim()) {
                    // Get context lines
                    let context_before = if line_num >= 2 {
                        lines[line_num.saturating_sub(2)..line_num]
                            .iter()
                            .map(|s| s.to_string())
                            .collect()
                    } else {
                        lines[0..line_num].iter().map(|s| s.to_string()).collect()
                    };

                    let context_after = if line_num + 3 < lines.len() {
                        lines[line_num + 1..line_num + 3]
                            .iter()
                            .map(|s| s.to_string())
                            .collect()
                    } else {
                        lines[line_num + 1..]
                            .iter()
                            .map(|s| s.to_string())
                            .collect()
                    };

                    matches.push(SearchMatch {
                        line_number: line_num + 1,
                        line: line.to_string(),
                        context_before,
                        context_after,
                        field: current_section.clone(),
                    });
                    break;
                }
            }
        }

        Ok(matches)
    }

    /// Get common search terms for suggestions
    fn get_common_terms(&self) -> Result<Vec<String>> {
        // Simple implementation - extract common words from email files
        let mut word_counts: HashMap<String, usize> = HashMap::new();
        let mut email_files = Vec::new();

        self.collect_email_files(Path::new(&self.repository_path), &mut email_files)?;

        for file_path in email_files {
            let content = fs::read_to_string(&file_path).map_err(|e| GitMailError::Io(e))?;

            for line in content.lines() {
                for word in line.split_whitespace() {
                    let clean_word = word
                        .trim_matches(|c: char| !c.is_alphanumeric())
                        .to_lowercase();
                    if clean_word.len() > 3 {
                        *word_counts.entry(clean_word).or_insert(0) += 1;
                    }
                }
            }
        }

        let mut terms: Vec<String> = word_counts
            .into_iter()
            .filter(|(_, count)| *count > 2)
            .map(|(word, _)| word)
            .collect();

        terms.sort();
        Ok(terms)
    }

    /// Recursively collect email files
    fn collect_email_files(&self, dir: &Path, files: &mut Vec<PathBuf>) -> Result<()> {
        if !dir.exists() {
            return Ok(());
        }

        let entries = fs::read_dir(dir).map_err(|e| GitMailError::Io(e))?;

        for entry in entries {
            let entry = entry.map_err(|e| GitMailError::Io(e))?;
            let path = entry.path();

            if path.is_dir() {
                self.collect_email_files(&path, files)?;
            } else if path.is_file() {
                if let Some(filename) = path.file_name().and_then(|n| n.to_str()) {
                    if filename.ends_with(".txt") && filename.starts_with("msg-") {
                        files.push(path);
                    }
                }
            }
        }

        Ok(())
    }
}

impl<T: GitStorage> SearchEngine for CommandLineSearchEngine<T> {
    fn search(&self, query: &SearchQuery) -> Result<Vec<SearchResult>> {
        // Validate search query
        if query.term.trim().is_empty() {
            return Err(GitMailError::Search(
                "Search term cannot be empty".to_string(),
            ));
        }

        // Execute manual search
        let file_matches = self.manual_search(query)?;

        // Convert to structured search results
        self.convert_to_search_results(file_matches, query)
    }

    fn suggest(&self, partial: &str) -> Result<Vec<String>> {
        if partial.len() < 2 {
            return Ok(Vec::new());
        }

        let common_terms = self.get_common_terms()?;
        let partial_lower = partial.to_lowercase();

        let mut suggestions: Vec<String> = common_terms
            .into_iter()
            .filter(|term| term.to_lowercase().starts_with(&partial_lower))
            .take(10)
            .collect();

        suggestions.sort();
        Ok(suggestions)
    }

    fn index_emails(&self) -> Result<()> {
        // For command-line tools, indexing is not necessary as grep is already fast
        // This could be implemented later with a dedicated search index if needed
        Ok(())
    }
}

impl Default for SearchQuery {
    fn default() -> Self {
        Self {
            term: String::new(),
            regex: false,
            case_sensitive: false,
            folder: None,
            fields: Vec::new(),
            date_range: None,
            tags: Vec::new(),
            limit: Some(100),
        }
    }
}

impl SearchQuery {
    /// Create a new search query with the given term
    pub fn new(term: String) -> Self {
        Self {
            term,
            ..Default::default()
        }
    }

    /// Set regex mode
    pub fn with_regex(mut self, regex: bool) -> Self {
        self.regex = regex;
        self
    }

    /// Set case sensitivity
    pub fn with_case_sensitive(mut self, case_sensitive: bool) -> Self {
        self.case_sensitive = case_sensitive;
        self
    }

    /// Set folder filter
    pub fn with_folder(mut self, folder: String) -> Self {
        self.folder = Some(folder);
        self
    }

    /// Add field filter
    pub fn with_field(mut self, field: SearchField) -> Self {
        self.fields.push(field);
        self
    }

    /// Set date range filter
    pub fn with_date_range(
        mut self,
        start: chrono::DateTime<chrono::Utc>,
        end: chrono::DateTime<chrono::Utc>,
    ) -> Self {
        self.date_range = Some(DateRange { start, end });
        self
    }

    /// Add tag filter
    pub fn with_tag(mut self, tag: String) -> Self {
        self.tags.push(tag);
        self
    }

    /// Set result limit
    pub fn with_limit(mut self, limit: usize) -> Self {
        self.limit = Some(limit);
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::git_storage::DefaultGitStorage;
    use crate::models::Email;
    use tempfile::TempDir;

    fn create_test_email(id: &str, subject: &str, body: &str) -> Email {
        let mut email = Email::new("test@example.com".to_string());
        email.id = id.to_string();
        email
            .headers
            .insert("Subject".to_string(), subject.to_string());
        email
            .headers
            .insert("From".to_string(), "sender@example.com".to_string());
        email.body.content = body.to_string();
        email
    }

    #[test]
    fn test_search_query_builder() {
        let query = SearchQuery::new("test term".to_string())
            .with_regex(true)
            .with_case_sensitive(true)
            .with_folder("inbox".to_string())
            .with_field(SearchField::Headers)
            .with_tag("important".to_string())
            .with_limit(50);

        assert_eq!(query.term, "test term");
        assert!(query.regex);
        assert!(query.case_sensitive);
        assert_eq!(query.folder, Some("inbox".to_string()));
        assert_eq!(query.fields.len(), 1);
        assert_eq!(query.tags.len(), 1);
        assert_eq!(query.limit, Some(50));
    }

    #[test]
    fn test_search_field_matching() {
        let fields = vec![SearchField::Headers, SearchField::Body];
        let engine = CommandLineSearchEngine::new(
            DefaultGitStorage::new("test".to_string()),
            "test".to_string(),
        );

        // Test header section with search term
        let header_content = "--- Email Headers ---\nSubject: Test\nFrom: test@example.com\n--- Body ---\nBody content";
        let query = SearchQuery::new("Test".to_string());
        assert!(engine.filter_by_fields(header_content, &fields, &query));

        // Test body section with search term
        let body_content = "--- Body ---\nThis is body content\n";
        let query = SearchQuery::new("body".to_string());
        assert!(engine.filter_by_fields(body_content, &fields, &query));

        // Test non-matching section
        let metadata_content = "--- Metadata ---\nID: 123\n";
        let query = SearchQuery::new("123".to_string());
        assert!(!engine.filter_by_fields(metadata_content, &fields, &query));
    }

    #[test]
    fn test_email_id_extraction() {
        let engine = CommandLineSearchEngine::new(
            DefaultGitStorage::new("test".to_string()),
            "test".to_string(),
        );

        let path = Path::new("/path/to/msg-12345678-sender-subject.txt");
        let id = engine.extract_email_id_from_path(path).unwrap();
        assert_eq!(id, "12345678");
    }

    #[test]
    fn test_score_calculation() {
        let engine = CommandLineSearchEngine::new(
            DefaultGitStorage::new("test".to_string()),
            "test".to_string(),
        );

        let matches = vec![
            SearchMatch {
                line_number: 1,
                line: "Subject: Important test message".to_string(),
                context_before: vec![],
                context_after: vec![],
                field: SearchField::Header("Subject".to_string()),
            },
            SearchMatch {
                line_number: 10,
                line: "This is a test in the body".to_string(),
                context_before: vec![],
                context_after: vec![],
                field: SearchField::Body,
            },
        ];

        let query = SearchQuery::new("test".to_string());
        let score = engine.calculate_score(&matches, &query);

        // Should have a positive score with bonus for subject match
        assert!(score > 0.0);
        assert!(score <= 1.0);
    }
}
