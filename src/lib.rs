//! Git-Mail library
//!
//! Terminal-based, offline-first email client that stores emails as plain-text files in a Git repository.

pub mod account_manager;
pub mod cli;
pub mod config;
pub mod core;
pub mod draft;
pub mod editor;
pub mod error;
pub mod external_tools;
pub mod filter;
pub mod git_storage;
pub mod help;
pub mod logging;
pub mod models;
pub mod organization;
pub mod parser;
pub mod plugin;
pub mod search;
pub mod smtp_tests;
pub mod sync;
pub mod tui;

pub use error::{GitMailError, Result};
pub use git_storage::{DefaultGitStorage, GitStorage};
pub use models::*;
