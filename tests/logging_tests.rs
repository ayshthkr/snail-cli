use git_mail::error::GitMailError;
use git_mail::logging::*;
use std::collections::HashMap;
use std::path::PathBuf;
use std::time::Duration;
use tokio::time::sleep;

#[test]
fn test_logging_config_default() {
    let config = LoggingConfig::default();
    assert_eq!(config.level, "info");
    assert!(!config.debug_mode);
    assert!(!config.log_to_file);
    assert!(!config.enable_performance_monitoring);
    assert!(!config.json_format);
    assert_eq!(config.max_file_size_mb, 100);
    assert_eq!(config.max_files, 5);
    assert!(config.component_levels.is_empty());
}

#[test]
fn test_logging_config_custom() {
    let mut component_levels = HashMap::new();
    component_levels.insert("git_storage".to_string(), "debug".to_string());
    component_levels.insert("sync_engine".to_string(), "trace".to_string());

    let config = LoggingConfig {
        level: "debug".to_string(),
        debug_mode: true,
        log_to_file: true,
        log_file: Some(PathBuf::from("/tmp/git-mail.log")),
        enable_performance_monitoring: true,
        json_format: true,
        max_file_size_mb: 50,
        max_files: 10,
        component_levels,
    };

    assert_eq!(config.level, "debug");
    assert!(config.debug_mode);
    assert!(config.log_to_file);
    assert!(config.enable_performance_monitoring);
    assert!(config.json_format);
    assert_eq!(config.max_file_size_mb, 50);
    assert_eq!(config.max_files, 10);
    assert_eq!(config.component_levels.len(), 2);
}

#[test]
fn test_performance_metrics_creation() {
    let metrics =
        PerformanceMetrics::new("test_operation".to_string(), "test_component".to_string());

    assert_eq!(metrics.operation, "test_operation");
    assert_eq!(metrics.component, "test_component");
    assert_eq!(metrics.duration, Duration::default());
    assert!(metrics.success);
    assert!(metrics.error_message.is_none());
    assert!(metrics.metadata.is_empty());
}

#[test]
fn test_performance_metrics_completion() {
    let metrics = PerformanceMetrics::new("test_op".to_string(), "test_comp".to_string());

    // Simulate some work
    std::thread::sleep(Duration::from_millis(10));

    let completed_metrics = metrics.complete();
    assert!(completed_metrics.success);
    assert!(completed_metrics.duration_ms() >= 10);
    assert!(completed_metrics.error_message.is_none());
}

#[test]
fn test_performance_metrics_failure() {
    let metrics = PerformanceMetrics::new("test_op".to_string(), "test_comp".to_string());
    let error = GitMailError::Config("Test configuration error".to_string());

    let failed_metrics = metrics.fail(&error);
    assert!(!failed_metrics.success);
    assert!(failed_metrics.error_message.is_some());
    assert!(failed_metrics
        .error_message
        .as_ref()
        .unwrap()
        .contains("Test configuration error"));
}

#[test]
fn test_performance_metrics_with_metadata() {
    let metrics = PerformanceMetrics::new("test_op".to_string(), "test_comp".to_string())
        .with_metadata("email_count".to_string(), "42".to_string())
        .with_metadata("account".to_string(), "user@example.com".to_string());

    assert_eq!(metrics.metadata.get("email_count"), Some(&"42".to_string()));
    assert_eq!(
        metrics.metadata.get("account"),
        Some(&"user@example.com".to_string())
    );
}

#[test]
fn test_performance_metrics_slow_detection() {
    let mut metrics = PerformanceMetrics::new("slow_op".to_string(), "test_comp".to_string());

    // Simulate slow operation
    std::thread::sleep(Duration::from_millis(100));
    metrics = metrics.complete();

    assert!(!metrics.is_slow(200)); // Not slow with 200ms threshold
    assert!(metrics.is_slow(50)); // Slow with 50ms threshold
}

#[test]
fn test_performance_metrics_display() {
    let metrics =
        PerformanceMetrics::new("test_op".to_string(), "test_comp".to_string()).complete();

    let display_string = format!("{}", metrics);
    assert!(display_string.contains("test_comp.test_op"));
    assert!(display_string.contains("ms"));
    assert!(display_string.contains("success"));
}

#[tokio::test]
async fn test_performance_monitor() {
    let config = LoggingConfig {
        enable_performance_monitoring: true,
        ..Default::default()
    };
    let monitor = PerformanceMonitor::new(config);

    // Test operation tracking
    let tracker =
        monitor.start_operation("test_operation".to_string(), "test_component".to_string());
    sleep(Duration::from_millis(10)).await;
    tracker.complete().await;

    // Get statistics
    let stats = monitor.get_statistics().await;
    assert_eq!(stats.total_operations, 1);
    assert_eq!(stats.successful_operations, 1);
    assert_eq!(stats.failed_operations, 0);
    assert!(stats.average_duration_ms >= 10.0);
}

#[tokio::test]
async fn test_performance_monitor_with_failure() {
    let config = LoggingConfig {
        enable_performance_monitoring: true,
        ..Default::default()
    };
    let monitor = PerformanceMonitor::new(config);

    // Test failed operation tracking
    let tracker = monitor.start_operation(
        "failing_operation".to_string(),
        "test_component".to_string(),
    );
    let error = GitMailError::Network("Connection failed".to_string());
    tracker.fail(&error).await;

    // Get statistics
    let stats = monitor.get_statistics().await;
    assert_eq!(stats.total_operations, 1);
    assert_eq!(stats.successful_operations, 0);
    assert_eq!(stats.failed_operations, 1);
}

#[tokio::test]
async fn test_performance_monitor_disabled() {
    let config = LoggingConfig {
        enable_performance_monitoring: false,
        ..Default::default()
    };
    let monitor = PerformanceMonitor::new(config);

    // Operations should not be tracked when monitoring is disabled
    let tracker =
        monitor.start_operation("test_operation".to_string(), "test_component".to_string());
    tracker.complete().await;

    let stats = monitor.get_statistics().await;
    assert_eq!(stats.total_operations, 0);
}

#[tokio::test]
async fn test_operation_tracker_with_metadata() {
    let config = LoggingConfig {
        enable_performance_monitoring: true,
        ..Default::default()
    };
    let monitor = PerformanceMonitor::new(config);

    let tracker = monitor
        .start_operation("test_operation".to_string(), "test_component".to_string())
        .with_metadata("user_id".to_string(), "123".to_string())
        .with_metadata("action".to_string(), "sync".to_string());

    tracker.complete().await;

    let stats = monitor.get_statistics().await;
    assert_eq!(stats.total_operations, 1);
}

#[test]
fn test_performance_statistics_empty() {
    let stats = PerformanceStatistics::from_metrics(&[]);
    assert_eq!(stats.total_operations, 0);
    assert_eq!(stats.successful_operations, 0);
    assert_eq!(stats.failed_operations, 0);
    assert_eq!(stats.average_duration_ms, 0.0);
    assert_eq!(stats.median_duration_ms, 0);
    assert_eq!(stats.p95_duration_ms, 0);
    assert_eq!(stats.max_duration_ms, 0);
    assert_eq!(stats.min_duration_ms, 0);
    assert!(stats.operations_by_component.is_empty());
    assert!(stats.avg_duration_by_component.is_empty());
    assert_eq!(stats.slow_operations, 0);
}

#[test]
fn test_performance_statistics_with_data() {
    let metrics = vec![
        PerformanceMetrics::new("op1".to_string(), "comp1".to_string()).complete(),
        PerformanceMetrics::new("op2".to_string(), "comp1".to_string()).complete(),
        PerformanceMetrics::new("op3".to_string(), "comp2".to_string())
            .fail(&GitMailError::Config("test error".to_string())),
        // Create a slow operation
        {
            let mut slow_metrics =
                PerformanceMetrics::new("slow_op".to_string(), "comp1".to_string());
            std::thread::sleep(Duration::from_millis(1100)); // Over 1 second
            slow_metrics.complete()
        },
    ];

    let stats = PerformanceStatistics::from_metrics(&metrics);

    assert_eq!(stats.total_operations, 4);
    assert_eq!(stats.successful_operations, 3);
    assert_eq!(stats.failed_operations, 1);
    assert!(stats.average_duration_ms > 0.0);

    // Check component statistics
    assert_eq!(stats.operations_by_component.get("comp1"), Some(&3));
    assert_eq!(stats.operations_by_component.get("comp2"), Some(&1));

    // Check slow operations
    assert_eq!(stats.slow_operations, 1);
}

#[test]
fn test_debug_context_creation() {
    let ctx = DebugContext::new("test_component".to_string(), "test_operation".to_string());

    assert_eq!(ctx.component, "test_component");
    assert_eq!(ctx.operation, "test_operation");
    assert!(ctx.debug_info.is_empty());
    assert!(!ctx.trace_id.is_empty());

    // Trace ID should be a valid UUID format
    assert!(uuid::Uuid::parse_str(&ctx.trace_id).is_ok());
}

#[test]
fn test_debug_context_with_info() {
    let ctx = DebugContext::new("test_component".to_string(), "test_operation".to_string())
        .with_info("key1".to_string(), "value1".to_string())
        .with_info("key2".to_string(), "value2".to_string());

    assert_eq!(ctx.debug_info.len(), 2);
    assert_eq!(ctx.debug_info.get("key1"), Some(&"value1".to_string()));
    assert_eq!(ctx.debug_info.get("key2"), Some(&"value2".to_string()));
}

#[test]
fn test_performance_statistics_percentiles() {
    // Create metrics with known durations for testing percentiles
    let mut metrics = Vec::new();

    // Create 100 metrics with durations from 1ms to 100ms
    for i in 1..=100 {
        let mut metric = PerformanceMetrics::new(format!("op_{}", i), "test_component".to_string());

        // Simulate duration by setting the times manually
        let start = std::time::Instant::now();
        let end = start + Duration::from_millis(i);
        metric.start_time = start;
        metric.end_time = end;
        metric.duration = Duration::from_millis(i);
        metric.success = true;

        metrics.push(metric);
    }

    let stats = PerformanceStatistics::from_metrics(&metrics);

    assert_eq!(stats.total_operations, 100);
    assert_eq!(stats.successful_operations, 100);
    assert_eq!(stats.failed_operations, 0);
    assert_eq!(stats.min_duration_ms, 1);
    assert_eq!(stats.max_duration_ms, 100);
    assert_eq!(stats.median_duration_ms, 50);
    assert_eq!(stats.p95_duration_ms, 95);
    assert_eq!(stats.average_duration_ms, 50.5);
}

#[test]
fn test_performance_statistics_component_breakdown() {
    let metrics = vec![
        PerformanceMetrics::new("op1".to_string(), "git_storage".to_string()).complete(),
        PerformanceMetrics::new("op2".to_string(), "git_storage".to_string()).complete(),
        PerformanceMetrics::new("op3".to_string(), "sync_engine".to_string()).complete(),
        PerformanceMetrics::new("op4".to_string(), "filter_engine".to_string())
            .fail(&GitMailError::Config("test".to_string())),
    ];

    let stats = PerformanceStatistics::from_metrics(&metrics);

    assert_eq!(stats.operations_by_component.get("git_storage"), Some(&2));
    assert_eq!(stats.operations_by_component.get("sync_engine"), Some(&1));
    assert_eq!(stats.operations_by_component.get("filter_engine"), Some(&1));

    // Check that average durations are calculated for each component
    assert!(stats.avg_duration_by_component.contains_key("git_storage"));
    assert!(stats.avg_duration_by_component.contains_key("sync_engine"));
    assert!(stats
        .avg_duration_by_component
        .contains_key("filter_engine"));
}

#[tokio::test]
async fn test_performance_monitor_metrics_limit() {
    let config = LoggingConfig {
        enable_performance_monitoring: true,
        ..Default::default()
    };
    let monitor = PerformanceMonitor::new(config);

    // Add many operations to test the metrics limit
    for i in 0..1100 {
        let tracker =
            monitor.start_operation(format!("operation_{}", i), "test_component".to_string());
        tracker.complete().await;
    }

    let stats = monitor.get_statistics().await;
    // Should be limited to 1000 metrics
    assert_eq!(stats.total_operations, 1000);
}

#[tokio::test]
async fn test_performance_monitor_clear_metrics() {
    let config = LoggingConfig {
        enable_performance_monitoring: true,
        ..Default::default()
    };
    let monitor = PerformanceMonitor::new(config);

    // Add some operations
    let tracker = monitor.start_operation("test_op".to_string(), "test_comp".to_string());
    tracker.complete().await;

    let stats_before = monitor.get_statistics().await;
    assert_eq!(stats_before.total_operations, 1);

    // Clear metrics
    monitor.clear_metrics().await;

    let stats_after = monitor.get_statistics().await;
    assert_eq!(stats_after.total_operations, 0);
}

#[test]
fn test_performance_statistics_default() {
    let stats = PerformanceStatistics::default();

    assert_eq!(stats.total_operations, 0);
    assert_eq!(stats.successful_operations, 0);
    assert_eq!(stats.failed_operations, 0);
    assert_eq!(stats.average_duration_ms, 0.0);
    assert_eq!(stats.median_duration_ms, 0);
    assert_eq!(stats.p95_duration_ms, 0);
    assert_eq!(stats.max_duration_ms, 0);
    assert_eq!(stats.min_duration_ms, 0);
    assert!(stats.operations_by_component.is_empty());
    assert!(stats.avg_duration_by_component.is_empty());
    assert_eq!(stats.slow_operations, 0);
}
