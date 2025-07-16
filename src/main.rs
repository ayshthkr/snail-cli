use anyhow::Result;
use clap::Parser;
use tracing::{info, Level};
use tracing_subscriber;

mod cli;
mod core;
mod draft;
mod editor;
mod error;
mod external_tools;
mod filter;
mod git_storage;
mod models;
mod parser;
mod plugin;
mod smtp_tests;
mod sync;
mod tui;

use cli::{Cli, Commands, DraftAction, PluginAction, ToolAction};
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

    // Initialize Git storage and core components
    let storage = DefaultGitStorage::new(repo_path.clone());
    let drafts_dir = format!("{}/drafts", repo_path);
    let mut core = GitMailCore::new_with_defaults(Box::new(storage), &drafts_dir)?;

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
            let _init_storage = DefaultGitStorage::new(init_path.clone());
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
        Some(Commands::Compose { to, subject }) => {
            info!("Composing new email");
            // For now, use a default account - this should be configurable
            let account = "default@example.com".to_string();
            match core.compose_email(account, to, subject) {
                Ok(draft) => {
                    println!("Draft created successfully: {}", draft.id);
                    println!("Use 'git-mail draft edit {}' to continue editing", draft.id);
                    println!("Use 'git-mail draft send {}' to send the email", draft.id);
                }
                Err(e) => {
                    eprintln!("Failed to compose email: {}", e);
                    std::process::exit(1);
                }
            }
        }
        Some(Commands::Reply { id, account }) => {
            info!("Replying to email: {}", id);
            let reply_account = account.unwrap_or_else(|| "default@example.com".to_string());
            match core.reply_email(reply_account, &id) {
                Ok(draft) => {
                    println!("Reply draft created successfully: {}", draft.id);
                    println!("Use 'git-mail draft edit {}' to continue editing", draft.id);
                    println!("Use 'git-mail draft send {}' to send the reply", draft.id);
                }
                Err(e) => {
                    eprintln!("Failed to create reply: {}", e);
                    std::process::exit(1);
                }
            }
        }
        Some(Commands::Forward { id, account }) => {
            info!("Forwarding email: {}", id);
            let forward_account = account.unwrap_or_else(|| "default@example.com".to_string());
            match core.forward_email(forward_account, &id) {
                Ok(draft) => {
                    println!("Forward draft created successfully: {}", draft.id);
                    println!("Use 'git-mail draft edit {}' to continue editing", draft.id);
                    println!("Use 'git-mail draft send {}' to send the forward", draft.id);
                }
                Err(e) => {
                    eprintln!("Failed to create forward: {}", e);
                    std::process::exit(1);
                }
            }
        }
        Some(Commands::Draft { action }) => match action {
            DraftAction::List => {
                info!("Listing drafts");
                match core.list_drafts() {
                    Ok(drafts) => {
                        if drafts.is_empty() {
                            println!("No drafts found");
                        } else {
                            println!("Drafts ({}):", drafts.len());
                            for draft in drafts {
                                let subject = draft
                                    .headers
                                    .get("Subject")
                                    .map_or("(no subject)", |s| s.as_str());
                                let to = draft
                                    .headers
                                    .get("To")
                                    .map_or("(no recipient)", |s| s.as_str());
                                let draft_type = match draft.draft_type {
                                    crate::models::DraftType::Compose => "Compose",
                                    crate::models::DraftType::Reply => "Reply",
                                    crate::models::DraftType::Forward => "Forward",
                                };
                                println!(
                                    "  {} | {} | {} | {} | {}",
                                    draft.id,
                                    draft_type,
                                    draft.metadata.created_at.format("%Y-%m-%d %H:%M"),
                                    truncate_for_display(to, 25),
                                    truncate_for_display(subject, 40)
                                );
                            }
                        }
                    }
                    Err(e) => {
                        eprintln!("Failed to list drafts: {}", e);
                        std::process::exit(1);
                    }
                }
            }
            DraftAction::Show { id } => {
                info!("Showing draft: {}", id);
                match core.get_draft(&id) {
                    Ok(draft) => {
                        let from = draft.headers.get("From").map_or(&draft.account, |v| v);
                        let to = draft.headers.get("To").map_or("", |v| v);
                        let subject = draft.headers.get("Subject").map_or("(no subject)", |v| v);
                        let date = draft.metadata.created_at.format("%Y-%m-%d %H:%M:%S");
                        let draft_type = match draft.draft_type {
                            crate::models::DraftType::Compose => "Compose",
                            crate::models::DraftType::Reply => "Reply",
                            crate::models::DraftType::Forward => "Forward",
                        };

                        println!("Draft ID: {}", draft.id);
                        println!("Type: {}", draft_type);
                        println!("From: {}", from);
                        println!("To: {}", to);
                        println!("Subject: {}", subject);
                        println!("Created: {}", date);
                        if let Some(original_id) = &draft.original_email_id {
                            println!("Original Email: {}", original_id);
                        }
                        println!("---");
                        println!("{}", draft.body.content);
                    }
                    Err(e) => {
                        eprintln!("Failed to load draft: {}", e);
                        std::process::exit(1);
                    }
                }
            }
            DraftAction::Edit { id } => {
                info!("Editing draft: {}", id);
                match core.edit_draft(&id) {
                    Ok(draft) => {
                        println!("Draft updated successfully: {}", draft.id);
                        println!("Use 'git-mail draft send {}' to send the email", draft.id);
                    }
                    Err(e) => {
                        eprintln!("Failed to edit draft: {}", e);
                        std::process::exit(1);
                    }
                }
            }
            DraftAction::Delete { id } => {
                info!("Deleting draft: {}", id);
                match core.delete_draft(&id) {
                    Ok(_) => {
                        println!("Draft deleted successfully: {}", id);
                    }
                    Err(e) => {
                        eprintln!("Failed to delete draft: {}", e);
                        std::process::exit(1);
                    }
                }
            }
            DraftAction::Send { id } => {
                info!("Sending draft: {}", id);
                match core.send_draft(&id) {
                    Ok(email) => {
                        let subject = email
                            .headers
                            .get("Subject")
                            .map_or("(no subject)", |s| s.as_str());
                        println!("Email sent successfully!");
                        println!("Subject: {}", subject);
                        println!("Email ID: {}", email.id);
                    }
                    Err(e) => {
                        eprintln!("Failed to send draft: {}", e);
                        std::process::exit(1);
                    }
                }
            }
        },
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
        Some(Commands::Plugin { action }) => {
            // Initialize plugins first
            core.init_plugins().await?;

            match action {
                PluginAction::List => {
                    info!("Listing available plugins");
                    let plugins = core.list_plugins();
                    if plugins.is_empty() {
                        println!("No plugins found.");
                        println!(
                            "Add plugins to ~/.git-mail/plugins/ or {}/plugins/",
                            repo_path
                        );
                    } else {
                        println!("Available plugins ({}):", plugins.len());
                        for plugin in plugins {
                            println!(
                                "  {:20} {} (v{})",
                                plugin.name, plugin.description, plugin.version
                            );
                        }
                    }
                }
                PluginAction::Execute { name, args } => {
                    info!("Executing plugin: {} with args: {:?}", name, args);
                    match core.execute_plugin(&name, &args, vec![], None).await {
                        Ok(result) => {
                            if !result.stdout.is_empty() {
                                print!("{}", result.stdout);
                            }
                            if !result.stderr.is_empty() {
                                eprint!("{}", result.stderr);
                            }
                            if !result.success {
                                std::process::exit(result.exit_code);
                            }
                        }
                        Err(e) => {
                            eprintln!("Failed to execute plugin: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
                PluginAction::Info { name } => {
                    info!("Showing plugin info: {}", name);
                    match core.get_plugin_help(&name) {
                        Some(help) => println!("{}", help),
                        None => {
                            eprintln!("Plugin not found: {}", name);
                            std::process::exit(1);
                        }
                    }
                }
                PluginAction::Reload => {
                    info!("Reloading plugins");
                    core.init_plugins().await?;
                    let plugins = core.list_plugins();
                    println!("Reloaded {} plugins", plugins.len());
                }
            }
        }
        Some(Commands::Tool { action }) => {
            info!("Executing external tool command");

            match action {
                ToolAction::Pipe {
                    command,
                    args,
                    emails,
                    folder,
                } => {
                    info!(
                        "Piping emails to command: {} with args: {:?}",
                        command, args
                    );
                    let email_ids: Vec<String> =
                        emails.split(',').map(|s| s.trim().to_string()).collect();

                    match core
                        .pipe_emails_to_tool(&command, &args, &email_ids, folder)
                        .await
                    {
                        Ok(result) => {
                            if !result.stdout.is_empty() {
                                print!("{}", result.stdout);
                            }
                            if !result.stderr.is_empty() {
                                eprint!("{}", result.stderr);
                            }
                            if !result.success {
                                std::process::exit(result.exit_code);
                            }
                        }
                        Err(e) => {
                            eprintln!("Failed to pipe emails to tool: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
                ToolAction::Exec {
                    command,
                    args,
                    folder,
                } => {
                    info!("Executing command: {} with args: {:?}", command, args);

                    match core.execute_external_tool(&command, &args, folder).await {
                        Ok(result) => {
                            if !result.stdout.is_empty() {
                                print!("{}", result.stdout);
                            }
                            if !result.stderr.is_empty() {
                                eprint!("{}", result.stderr);
                            }
                            if !result.success {
                                std::process::exit(result.exit_code);
                            }
                        }
                        Err(e) => {
                            eprintln!("Failed to execute external tool: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
                ToolAction::Grep {
                    pattern,
                    emails,
                    options,
                } => {
                    info!("Grepping emails with pattern: {}", pattern);
                    let email_ids: Vec<String> =
                        emails.split(',').map(|s| s.trim().to_string()).collect();

                    let mut grep_args = vec![];
                    if let Some(opts) = options {
                        grep_args.extend(opts.split_whitespace().map(|s| s.to_string()));
                    }
                    grep_args.push(pattern);

                    match core
                        .pipe_emails_to_tool("grep", &grep_args, &email_ids, None)
                        .await
                    {
                        Ok(result) => {
                            if !result.stdout.is_empty() {
                                print!("{}", result.stdout);
                            }
                            if !result.stderr.is_empty() {
                                eprint!("{}", result.stderr);
                            }
                            if !result.success {
                                std::process::exit(result.exit_code);
                            }
                        }
                        Err(e) => {
                            eprintln!("Failed to grep emails: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
                ToolAction::Awk { script, emails } => {
                    info!("Processing emails with awk script: {}", script);
                    let email_ids: Vec<String> =
                        emails.split(',').map(|s| s.trim().to_string()).collect();
                    let awk_args = vec![script];

                    match core
                        .pipe_emails_to_tool("awk", &awk_args, &email_ids, None)
                        .await
                    {
                        Ok(result) => {
                            if !result.stdout.is_empty() {
                                print!("{}", result.stdout);
                            }
                            if !result.stderr.is_empty() {
                                eprint!("{}", result.stderr);
                            }
                            if !result.success {
                                std::process::exit(result.exit_code);
                            }
                        }
                        Err(e) => {
                            eprintln!("Failed to process emails with awk: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
                ToolAction::Count { emails, count_type } => {
                    info!("Counting {} in emails", count_type);
                    let email_ids: Vec<String> =
                        emails.split(',').map(|s| s.trim().to_string()).collect();

                    let wc_args = match count_type.as_str() {
                        "lines" => vec!["-l".to_string()],
                        "words" => vec!["-w".to_string()],
                        "chars" => vec!["-c".to_string()],
                        _ => {
                            eprintln!(
                                "Invalid count type: {}. Use 'lines', 'words', or 'chars'",
                                count_type
                            );
                            std::process::exit(1);
                        }
                    };

                    match core
                        .pipe_emails_to_tool("wc", &wc_args, &email_ids, None)
                        .await
                    {
                        Ok(result) => {
                            if !result.stdout.is_empty() {
                                print!("{}", result.stdout);
                            }
                            if !result.stderr.is_empty() {
                                eprint!("{}", result.stderr);
                            }
                            if !result.success {
                                std::process::exit(result.exit_code);
                            }
                        }
                        Err(e) => {
                            eprintln!("Failed to count in emails: {}", e);
                            std::process::exit(1);
                        }
                    }
                }
            }
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
