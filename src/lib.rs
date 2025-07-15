//! Git-Mail library
//!
//! Terminal-based, offline-first email client that stores emails as plain-text files in a Git repository.

pub mod cli;
pub mod core;
pub mod editor;
pub mod error;
pub mod filter;
pub mod git_storage;
pub mod models;
pub mod parser;
pub mod sync;

pub use error::{GitMailError, Result};
pub use git_storage::{DefaultGitStorage, GitStorage};
pub use models::*;
