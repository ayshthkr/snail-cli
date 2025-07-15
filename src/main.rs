use anyhow::Result;
use clap::Parser;
use tracing::{info, Level};
use tracing_subscriber;

mod cli;
mod core;
mod editor;
mod error;
mod filter;
mod git_storage;
mod models;
mod sync;
mod tui;

use cli::{Cli, Commands};
use core::GitMailCore;
use git_storage::DefaultGitStorage;
use tui::TuiApp;

#[tokio::main]
async fn main() -> Result<()> {
    // Initialize logging
    tracing_subscriber::fmt().with_max_level(Level::INFO).init();

    info!("Starting Git-Mail");

    // Parse command line arguments
    let cli = Cli::parse();

    // Initialize and run the application
    match run(cli).await {
        Ok(_) => {
            info!("Git-Mail completed successfully");
            Ok(())
        }
        Err(e) => {
            eprintln!("Error: {}", e);
            std::process::exit(1);
        }
    }
}

async fn run(cli: Cli) -> Result<()> {
    // Determine repository path
    let repo_path = cli.repo.unwrap_or_else(|| {
        dirs::home_dir()
            .map(|home| home.join(".git-mail").to_string_lossy().to_string())
            .unwrap_or_else(|| ".git-mail".to_string())
    });

    // Initialize Git storage
    let storage = DefaultGitStorage::new(repo_path.clone());
    let core = GitMailCore::new(Box::new(storage));

    // Handle commands
    match cli.command {
        Some(Commands::Tui) => {
            info!("Starting TUI interface");
            let mut app = TuiApp::new(core)?;
            app.run().await?;
        }
        Some(Commands::Init { path }) => {
            let init_path = path.unwrap_or_else(|| repo_path.clone());
            info!("Initializing Git-Mail repository at: {}", init_path);
            let init_storage = DefaultGitStorage::new(init_path.clone());
            core.init_repository(&init_path)?;
            println!("Git-Mail repository initialized successfully");
        }
        Some(Commands::List { folder, count }) => {
            info!("Listing emails");
            let emails = core.list_emails(folder.as_deref())?;
            let display_count = std::cmp::min(count, emails.len());

            println!("Emails ({} of {}):", display_count, emails.len());
            for email_meta in emails.iter().take(display_count) {
                let status = if email_meta.is_read { " " } else { "●" };
                let star = if email_meta.is_starred { "★" } else { " " };
                println!(
                    "{}{} {} | {} | {}",
                    status,
                    star,
                    email_meta.created_at.format("%Y-%m-%d %H:%M"),
                    truncate_for_display(
                        &format!(
                            "File: {}",
                            email_meta.file_path.split('/').last().unwrap_or("unknown")
                        ),
                        20
                    ),
                    truncate_for_display(&email_meta.folder, 40)
                );
            }
        }
        Some(Commands::Show { id }) => {
            info!("Showing email: {}", id);
            match core.get_email(&id) {
                Ok(email) => {
                    let from = email.headers.get("From").map_or(&email.account, |v| v);
                    let to = email.headers.get("To").map_or("", |v| v);
                    let subject = email.headers.get("Subject").map_or("(no subject)", |v| v);
                    let date = email.metadata.created_at.format("%Y-%m-%d %H:%M:%S");

                    println!("From: {}", from);
                    println!("To: {}", to);
                    println!("Subject: {}", subject);
                    println!("Date: {}", date);
                    println!("---");
                    println!("{}", email.body.content);
                }
                Err(e) => {
                    eprintln!("Failed to load email: {}", e);
                    std::process::exit(1);
                }
            }
        }
        Some(Commands::Sync { account: _ }) => {
            info!("Sync command not yet implemented");
            println!("Sync functionality will be implemented in future tasks");
        }
        Some(Commands::Compose { to: _, subject: _ }) => {
            info!("Compose command not yet implemented");
            println!("Compose functionality will be implemented in future tasks");
        }
        Some(Commands::Search {
            query: _,
            folder: _,
        }) => {
            info!("Search command not yet implemented");
            println!("Search functionality will be implemented in future tasks");
        }
        Some(Commands::Account { action: _ }) => {
            info!("Account management not yet implemented");
            println!("Account management will be implemented in future tasks");
        }
        None => {
            // Default to TUI if no command specified
            info!("No command specified, starting TUI interface");
            let mut app = TuiApp::new(core)?;
            app.run().await?;
        }
    }

    Ok(())
}

/// Helper function to truncate strings for display
fn truncate_for_display(s: &str, max_len: usize) -> String {
    if s.len() <= max_len {
        s.to_string()
    } else {
        format!("{}...", &s[..max_len.saturating_sub(3)])
    }
}
