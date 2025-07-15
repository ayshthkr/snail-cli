use anyhow::Result;
use clap::Parser;
use tracing::{info, Level};
use tracing_subscriber;

mod cli;
mod core;
mod error;
mod git_storage;
mod sync;
mod filter;
mod editor;
mod models;

use cli::Cli;


#[tokio::main]
async fn main() -> Result<()> {
    // Initialize logging
    tracing_subscriber::fmt()
        .with_max_level(Level::INFO)
        .init();

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

async fn run(_cli: Cli) -> Result<()> {
    // TODO: Implement main application logic
    println!("Git-Mail - Terminal-based email client");
    Ok(())
}
