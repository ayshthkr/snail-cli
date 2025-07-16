use git_mail::error::*;
use git_mail::error_context;
use std::time::Duration;
use tokio::time::sleep;

#[tokio::test]
async fn test_error_context_creation() {
    let context = ErrorContext::new("test_operation".to_string(), "test_component".to_string())
        .with_detail("Test detail".to_string())
        .with_suggestion("Test suggestion".to_string())
        .recoverable(true)
        .with_severity(ErrorSeverity::Warning);

    assert_eq!(context.operation, "test_operation");
    assert_eq!(context.component, "test_component");
    assert_eq!(context.details.len(), 1);
    assert_eq!(context.recovery_suggestions.len(), 1);
    assert!(context.is_recoverable);
    assert_eq!(context.severity, ErrorSeverity::Warning);
}

#[tokio::test]
async fn test_error_with_context() {
    let base_error = GitMailError::Config("Invalid configuration".to_string());
    let context = ErrorContext::config_operation("load_config", "config.toml");
    let enhanced_error = base_error.with_context(context);

    match enhanced_error {
        GitMailError::WithContext {
            message,
            context,
            source,
        } => {
            assert!(message.contains("Invalid configuration"));
            assert_eq!(context.operation, "load_config");
            assert_eq!(context.component, "configuration");
            assert!(source.is_some());
        }
        _ => panic!("Expected WithContext error"),
    }
}

#[tokio::test]
async fn test_result_ext_trait() {
    let result: std::result::Result<(), std::io::Error> = Err(std::io::Error::new(
        std::io::ErrorKind::NotFound,
        "File not found",
    ));

    let enhanced_result = result.with_operation_context("read_file", "file_system");
    assert!(enhanced_result.is_err());

    if let Err(error) = enhanced_result {
        assert!(error.is_recoverable());
        assert_eq!(error.severity(), ErrorSeverity::Error);
    }
}

#[tokio::test]
async fn test_error_handler() {
    let handler = ErrorHandler::new(true);
    let error = GitMailError::Network("Connection timeout".to_string());

    let action = handler.handle_error(&error).await;
    match action {
        RecoveryAction::RetryWithBackoff {
            max_attempts,
            base_delay_ms,
        } => {
            assert_eq!(max_attempts, 3);
            assert_eq!(base_delay_ms, 1000);
        }
        _ => panic!("Expected RetryWithBackoff action"),
    }
}

#[tokio::test]
async fn test_retry_with_backoff_success() {
    let mut attempt_count = 0;

    let result = utils::retry_with_backoff(
        || {
            attempt_count += 1;
            async move {
                if attempt_count < 3 {
                    Err(GitMailError::Network("Temporary failure".to_string()))
                } else {
                    Ok("Success".to_string())
                }
            }
        },
        5,
        10, // 10ms base delay for fast test
    )
    .await;

    assert!(result.is_ok());
    assert_eq!(result.unwrap(), "Success");
    assert_eq!(attempt_count, 3);
}

#[tokio::test]
async fn test_retry_with_backoff_failure() {
    let mut attempt_count = 0;

    let result: Result<String> = utils::retry_with_backoff(
        || {
            attempt_count += 1;
            async move { Err(GitMailError::Network("Persistent failure".to_string())) }
        },
        3,
        10, // 10ms base delay for fast test
    )
    .await;

    assert!(result.is_err());
    assert_eq!(attempt_count, 3);
}

#[tokio::test]
async fn test_retry_with_non_recoverable_error() {
    let mut attempt_count = 0;

    let result: Result<String> = utils::retry_with_backoff(
        || {
            attempt_count += 1;
            async move {
                Err(GitMailError::Repository(
                    "Critical repository error".to_string(),
                ))
            }
        },
        5,
        10,
    )
    .await;

    assert!(result.is_err());
    assert_eq!(attempt_count, 1); // Should not retry non-recoverable errors
}

#[tokio::test]
async fn test_timeout_operation() {
    let context = ErrorContext::network_operation("slow_operation", "test.server.com");

    let result = utils::with_timeout_and_context(
        || async {
            sleep(Duration::from_millis(100)).await;
            Ok("Should timeout".to_string())
        },
        Duration::from_millis(50),
        context,
    )
    .await;

    assert!(result.is_err());
    if let Err(error) = result {
        assert!(error.to_string().contains("timed out"));
    }
}

#[tokio::test]
async fn test_timeout_operation_success() {
    let context = ErrorContext::network_operation("fast_operation", "test.server.com");

    let result = utils::with_timeout_and_context(
        || async {
            sleep(Duration::from_millis(10)).await;
            Ok("Success".to_string())
        },
        Duration::from_millis(50),
        context,
    )
    .await;

    assert!(result.is_ok());
    assert_eq!(result.unwrap(), "Success");
}

#[tokio::test]
async fn test_email_validation() {
    assert!(utils::validate_email_address("valid@example.com").is_ok());
    assert!(utils::validate_email_address("user.name+tag@domain.co.uk").is_ok());

    assert!(utils::validate_email_address("").is_err());
    assert!(utils::validate_email_address("invalid").is_err());
    assert!(utils::validate_email_address("@domain.com").is_err());
    assert!(utils::validate_email_address("user@").is_err());
    assert!(utils::validate_email_address("user@@domain.com").is_err());
}

#[tokio::test]
async fn test_file_path_sanitization() {
    assert!(utils::sanitize_file_path("valid/path.txt").is_ok());
    assert!(utils::sanitize_file_path("folder/subfolder/file.txt").is_ok());

    assert!(utils::sanitize_file_path("../parent/file.txt").is_err());
    assert!(utils::sanitize_file_path("/absolute/path").is_err());
    assert!(utils::sanitize_file_path("path//double//slash").is_err());
    assert!(utils::sanitize_file_path("./current/../parent").is_err());
}

#[tokio::test]
async fn test_file_size_formatting() {
    assert_eq!(utils::format_file_size(0), "0 B");
    assert_eq!(utils::format_file_size(512), "512 B");
    assert_eq!(utils::format_file_size(1024), "1.0 KB");
    assert_eq!(utils::format_file_size(1536), "1.5 KB");
    assert_eq!(utils::format_file_size(1048576), "1.0 MB");
    assert_eq!(utils::format_file_size(1073741824), "1.0 GB");
    assert_eq!(utils::format_file_size(1099511627776), "1.0 TB");
}

#[tokio::test]
async fn test_error_severity_ordering() {
    let critical = ErrorSeverity::Critical;
    let error = ErrorSeverity::Error;
    let warning = ErrorSeverity::Warning;
    let info = ErrorSeverity::Info;

    // Test display formatting
    assert_eq!(critical.to_string(), "CRITICAL");
    assert_eq!(error.to_string(), "ERROR");
    assert_eq!(warning.to_string(), "WARNING");
    assert_eq!(info.to_string(), "INFO");
}

#[tokio::test]
async fn test_error_context_helpers() {
    let git_ctx = ErrorContext::git_operation("commit");
    assert_eq!(git_ctx.component, "git_storage");
    assert_eq!(git_ctx.severity, ErrorSeverity::Critical);
    assert!(!git_ctx.recovery_suggestions.is_empty());

    let network_ctx = ErrorContext::network_operation("fetch", "imap.gmail.com");
    assert_eq!(network_ctx.component, "network");
    assert_eq!(network_ctx.severity, ErrorSeverity::Error);
    assert!(network_ctx.is_recoverable);

    let auth_ctx = ErrorContext::auth_operation("login", "user@example.com");
    assert_eq!(auth_ctx.component, "authentication");
    assert!(auth_ctx
        .details
        .iter()
        .any(|d| d.contains("user@example.com")));

    let file_ctx = ErrorContext::file_operation("read", "/path/to/file.txt");
    assert_eq!(file_ctx.component, "file_system");
    assert!(file_ctx
        .details
        .iter()
        .any(|d| d.contains("/path/to/file.txt")));
}

#[tokio::test]
async fn test_error_macros() {
    let ctx = error_context!("test_op", "test_comp");
    assert_eq!(ctx.operation, "test_op");
    assert_eq!(ctx.component, "test_comp");

    let ctx_with_severity = error_context!("test_op", "test_comp", ErrorSeverity::Warning);
    assert_eq!(ctx_with_severity.severity, ErrorSeverity::Warning);

    let ctx_recoverable =
        error_context!("test_op", "test_comp", ErrorSeverity::Error, recoverable: true);
    assert!(ctx_recoverable.is_recoverable);
}

#[tokio::test]
async fn test_user_friendly_messages() {
    let git_error = GitMailError::Git(git2::Error::from_str("Repository not found"));
    let message = git_error.user_message();
    assert!(message.contains("Git operation failed"));
    assert!(message.contains("repository permissions"));

    let network_error = GitMailError::Network("Connection refused".to_string());
    let message = network_error.user_message();
    assert!(message.contains("Network error"));
    assert!(message.contains("internet connection"));

    let auth_error = GitMailError::Auth("Invalid credentials".to_string());
    let message = auth_error.user_message();
    assert!(message.contains("Authentication failed"));
    assert!(message.contains("credentials"));
}

#[tokio::test]
async fn test_recovery_strategies() {
    let network_error = GitMailError::Network("Timeout".to_string());
    match network_error.recovery_strategy() {
        RecoveryStrategy::RetryWithBackoff { .. } => {}
        _ => panic!("Expected RetryWithBackoff for network error"),
    }

    let auth_error = GitMailError::Auth("Bad password".to_string());
    match auth_error.recovery_strategy() {
        RecoveryStrategy::PromptUser => {}
        _ => panic!("Expected PromptUser for auth error"),
    }

    let not_found_error = GitMailError::EmailNotFound("123".to_string());
    match not_found_error.recovery_strategy() {
        RecoveryStrategy::Skip => {}
        _ => panic!("Expected Skip for not found error"),
    }
}

#[tokio::test]
async fn test_error_handler_formatting() {
    let handler = ErrorHandler::new(true);
    let context = ErrorContext::new("test_op".to_string(), "test_comp".to_string())
        .with_detail("Test detail".to_string())
        .with_suggestion("Test suggestion".to_string())
        .with_severity(ErrorSeverity::Warning);

    let error = GitMailError::new_with_context("Test error".to_string(), context);
    let formatted = handler.format_error_for_user(&error);

    assert!(formatted.contains("[WARNING]"));
    assert!(formatted.contains("Test error"));
    assert!(formatted.contains("Operation: test_op"));
    assert!(formatted.contains("Component: test_comp"));
    assert!(formatted.contains("Test detail"));
    assert!(formatted.contains("Test suggestion"));
}
