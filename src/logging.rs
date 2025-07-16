//! Logging and debugging infrastructure for Git-Mail

use crate::error::{GitMailError, Result};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::fmt;
use std::path::PathBuf;
use std::sync::Arc;
use std::time::{Duration, Instant};
use tokio::sync::RwLock;
use tracing::{debug, error, info, warn, Level};
use tracing_subscriber::{
    fmt::{format::FmtSpan, time::ChronoUtc},
    layer::SubscriberExt,
    util::SubscriberInitExt,
    EnvFilter, Layer,
};

/// Logging configuration
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoggingConfig {
    /// Log level (trace, debug, info, warn, error)
    pub level: String,
    /// Enable debug mode with verbose output
    pub debug_mode: bool,
    /// Log to file in addition to console
    pub log_to_file: bool,
    /// Log file path
    pub log_file: Option<PathBuf>,
    /// Enable performance monitoring
    pub enable_performance_monitoring: bool,
    /// Enable structured JSON logging
    pub json_format: bool,
    /// Maximum log file size in MB
    pub max_file_size_mb: u64,
    /// Number of log files to keep in rotation
    pub max_files: u32,
    /// Enable component-specific logging levels
    pub component_levels: HashMap<String, String>,
}

impl Default for LoggingConfig {
    fn default() -> Self {
        Self {
            level: "info".to_string(),
            debug_mode: false,
            log_to_file: false,
            log_file: None,
            enable_performance_monitoring: false,
            json_format: false,
            max_file_size_mb: 100,
            max_files: 5,
            component_levels: HashMap::new(),
        }
    }
}

/// Performance metrics for operations
#[derive(Debug, Clone)]
pub struct PerformanceMetrics {
    /// Operation name
    pub operation: String,
    /// Component that performed the operation
    pub component: String,
    /// Duration of the operation
    pub duration: Duration,
    /// Start time
    pub start_time: Instant,
    /// End time
    pub end_time: Instant,
    /// Additional metadata
    pub metadata: HashMap<String, String>,
    /// Success/failure status
    pub success: bool,
    /// Error message if failed
    pub error_message: Option<String>,
}

impl PerformanceMetrics {
    /// Create new performance metrics
    pub fn new(operation: String, component: String) -> Self {
        let now = Instant::now();
        Self {
            operation,
            component,
            duration: Duration::default(),
            start_time: now,
            end_time: now,
            metadata: HashMap::new(),
            success: true,
            error_message: None,
        }
    }

    /// Mark operation as completed successfully
    pub fn complete(mut self) -> Self {
        self.end_time = Instant::now();
        self.duration = self.end_time.duration_since(self.start_time);
        self.success = true;
        self
    }

    /// Mark operation as failed
    pub fn fail(mut self, error: &GitMailError) -> Self {
        self.end_time = Instant::now();
        self.duration = self.end_time.duration_since(self.start_time);
        self.success = false;
        self.error_message = Some(error.to_string());
        self
    }

    /// Add metadata to the metrics
    pub fn with_metadata(mut self, key: String, value: String) -> Self {
        self.metadata.insert(key, value);
        self
    }

    /// Get duration in milliseconds
    pub fn duration_ms(&self) -> u64 {
        self.duration.as_millis() as u64
    }

    /// Check if operation was slow (over threshold)
    pub fn is_slow(&self, threshold_ms: u64) -> bool {
        self.duration_ms() > threshold_ms
    }
}

impl fmt::Display for PerformanceMetrics {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(
            f,
            "{}.{} took {}ms ({})",
            self.component,
            self.operation,
            self.duration_ms(),
            if self.success { "success" } else { "failed" }
        )
    }
}

/// Performance monitor for tracking operation metrics
#[derive(Debug)]
pub struct PerformanceMonitor {
    /// Collected metrics
    metrics: Arc<RwLock<Vec<PerformanceMetrics>>>,
    /// Configuration
    config: LoggingConfig,
    /// Slow operation threshold in milliseconds
    slow_threshold_ms: u64,
}

impl PerformanceMonitor {
    /// Create a new performance monitor
    pub fn new(config: LoggingConfig) -> Self {
        Self {
            metrics: Arc::new(RwLock::new(Vec::new())),
            config,
            slow_threshold_ms: 1000, // 1 second default
        }
    }

    /// Start tracking an operation
    pub fn start_operation(&self, operation: String, component: String) -> OperationTracker {
        OperationTracker::new(
            operation,
            component,
            self.metrics.clone(),
            self.config.clone(),
        )
    }

    /// Record completed metrics
    pub async fn record_metrics(&self, metrics: PerformanceMetrics) {
        if !self.config.enable_performance_monitoring {
            return;
        }

        // Log slow operations
        if metrics.is_slow(self.slow_threshold_ms) {
            warn!(
                operation = %metrics.operation,
                component = %metrics.component,
                duration_ms = metrics.duration_ms(),
                "Slow operation detected"
            );
        }

        // Log failed operations
        if !metrics.success {
            error!(
                operation = %metrics.operation,
                component = %metrics.component,
                duration_ms = metrics.duration_ms(),
                error = %metrics.error_message.as_deref().unwrap_or("unknown"),
                "Operation failed"
            );
        } else {
            debug!(
                operation = %metrics.operation,
                component = %metrics.component,
                duration_ms = metrics.duration_ms(),
                "Operation completed"
            );
        }

        // Store metrics
        let mut metrics_guard = self.metrics.write().await;
        metrics_guard.push(metrics);

        // Keep only recent metrics to prevent memory growth
        const MAX_METRICS: usize = 1000;
        if metrics_guard.len() > MAX_METRICS {
            let excess = metrics_guard.len() - MAX_METRICS;
            metrics_guard.drain(0..excess);
        }
    }

    /// Get performance statistics
    pub async fn get_statistics(&self) -> PerformanceStatistics {
        let metrics_guard = self.metrics.read().await;
        PerformanceStatistics::from_metrics(&metrics_guard)
    }

    /// Clear collected metrics
    pub async fn clear_metrics(&self) {
        let mut metrics_guard = self.metrics.write().await;
        metrics_guard.clear();
    }

    /// Set slow operation threshold
    pub fn set_slow_threshold(&mut self, threshold_ms: u64) {
        self.slow_threshold_ms = threshold_ms;
    }
}

/// Operation tracker for measuring performance
pub struct OperationTracker {
    metrics: PerformanceMetrics,
    monitor: Arc<RwLock<Vec<PerformanceMetrics>>>,
    config: LoggingConfig,
}

impl OperationTracker {
    fn new(
        operation: String,
        component: String,
        monitor: Arc<RwLock<Vec<PerformanceMetrics>>>,
        config: LoggingConfig,
    ) -> Self {
        let metrics = PerformanceMetrics::new(operation, component);
        Self {
            metrics,
            monitor,
            config,
        }
    }

    /// Add metadata to the operation
    pub fn with_metadata(mut self, key: String, value: String) -> Self {
        self.metrics = self.metrics.with_metadata(key, value);
        self
    }

    /// Complete the operation successfully
    pub async fn complete(self) {
        if !self.config.enable_performance_monitoring {
            return;
        }

        let metrics = self.metrics.complete();
        let mut monitor_guard = self.monitor.write().await;
        monitor_guard.push(metrics);
    }

    /// Complete the operation with failure
    pub async fn fail(self, error: &GitMailError) {
        if !self.config.enable_performance_monitoring {
            return;
        }

        let metrics = self.metrics.fail(error);
        let mut monitor_guard = self.monitor.write().await;
        monitor_guard.push(metrics);
    }
}

/// Performance statistics summary
#[derive(Debug, Serialize)]
pub struct PerformanceStatistics {
    /// Total number of operations
    pub total_operations: usize,
    /// Number of successful operations
    pub successful_operations: usize,
    /// Number of failed operations
    pub failed_operations: usize,
    /// Average operation duration in milliseconds
    pub average_duration_ms: f64,
    /// Median operation duration in milliseconds
    pub median_duration_ms: u64,
    /// 95th percentile duration in milliseconds
    pub p95_duration_ms: u64,
    /// Slowest operation duration in milliseconds
    pub max_duration_ms: u64,
    /// Fastest operation duration in milliseconds
    pub min_duration_ms: u64,
    /// Operations by component
    pub operations_by_component: HashMap<String, usize>,
    /// Average duration by component
    pub avg_duration_by_component: HashMap<String, f64>,
    /// Slow operations (over 1 second)
    pub slow_operations: usize,
}

impl PerformanceStatistics {
    pub fn from_metrics(metrics: &[PerformanceMetrics]) -> Self {
        if metrics.is_empty() {
            return Self::default();
        }

        let total_operations = metrics.len();
        let successful_operations = metrics.iter().filter(|m| m.success).count();
        let failed_operations = total_operations - successful_operations;

        let durations: Vec<u64> = metrics.iter().map(|m| m.duration_ms()).collect();
        let total_duration: u64 = durations.iter().sum();
        let average_duration_ms = total_duration as f64 / total_operations as f64;

        let mut sorted_durations = durations.clone();
        sorted_durations.sort_unstable();

        let median_duration_ms = if sorted_durations.is_empty() {
            0
        } else {
            sorted_durations[sorted_durations.len() / 2]
        };

        let p95_index = (sorted_durations.len() as f64 * 0.95) as usize;
        let p95_duration_ms = sorted_durations.get(p95_index).copied().unwrap_or(0);

        let max_duration_ms = sorted_durations.last().copied().unwrap_or(0);
        let min_duration_ms = sorted_durations.first().copied().unwrap_or(0);

        let mut operations_by_component = HashMap::new();
        let mut duration_by_component: HashMap<String, Vec<u64>> = HashMap::new();

        for metric in metrics {
            *operations_by_component
                .entry(metric.component.clone())
                .or_insert(0) += 1;
            duration_by_component
                .entry(metric.component.clone())
                .or_insert_with(Vec::new)
                .push(metric.duration_ms());
        }

        let avg_duration_by_component = duration_by_component
            .into_iter()
            .map(|(component, durations)| {
                let avg = durations.iter().sum::<u64>() as f64 / durations.len() as f64;
                (component, avg)
            })
            .collect();

        let slow_operations = metrics.iter().filter(|m| m.is_slow(1000)).count();

        Self {
            total_operations,
            successful_operations,
            failed_operations,
            average_duration_ms,
            median_duration_ms,
            p95_duration_ms,
            max_duration_ms,
            min_duration_ms,
            operations_by_component,
            avg_duration_by_component,
            slow_operations,
        }
    }
}

impl Default for PerformanceStatistics {
    fn default() -> Self {
        Self {
            total_operations: 0,
            successful_operations: 0,
            failed_operations: 0,
            average_duration_ms: 0.0,
            median_duration_ms: 0,
            p95_duration_ms: 0,
            max_duration_ms: 0,
            min_duration_ms: 0,
            operations_by_component: HashMap::new(),
            avg_duration_by_component: HashMap::new(),
            slow_operations: 0,
        }
    }
}

/// Debug context for enhanced debugging
#[derive(Debug, Clone)]
pub struct DebugContext {
    /// Component name
    pub component: String,
    /// Operation being performed
    pub operation: String,
    /// Debug information
    pub debug_info: HashMap<String, String>,
    /// Trace ID for request correlation
    pub trace_id: String,
}

impl DebugContext {
    /// Create a new debug context
    pub fn new(component: String, operation: String) -> Self {
        Self {
            component,
            operation,
            debug_info: HashMap::new(),
            trace_id: uuid::Uuid::new_v4().to_string(),
        }
    }

    /// Add debug information
    pub fn with_info(mut self, key: String, value: String) -> Self {
        self.debug_info.insert(key, value);
        self
    }

    /// Log debug information
    pub fn debug(&self, message: &str) {
        debug!(
            component = %self.component,
            operation = %self.operation,
            trace_id = %self.trace_id,
            debug_info = ?self.debug_info,
            "{}",
            message
        );
    }

    /// Log info with context
    pub fn info(&self, message: &str) {
        info!(
            component = %self.component,
            operation = %self.operation,
            trace_id = %self.trace_id,
            "{}",
            message
        );
    }

    /// Log warning with context
    pub fn warn(&self, message: &str) {
        warn!(
            component = %self.component,
            operation = %self.operation,
            trace_id = %self.trace_id,
            "{}",
            message
        );
    }

    /// Log error with context
    pub fn error(&self, message: &str, error: Option<&GitMailError>) {
        if let Some(err) = error {
            error!(
                component = %self.component,
                operation = %self.operation,
                trace_id = %self.trace_id,
                error = %err,
                severity = %err.severity(),
                "{}",
                message
            );
        } else {
            error!(
                component = %self.component,
                operation = %self.operation,
                trace_id = %self.trace_id,
                "{}",
                message
            );
        }
    }
}

/// Logging system manager
pub struct LoggingSystem {
    config: LoggingConfig,
    performance_monitor: PerformanceMonitor,
}

impl LoggingSystem {
    /// Initialize the logging system
    pub fn init(config: LoggingConfig) -> Result<Self> {
        // Build the tracing subscriber
        let mut layers = Vec::new();

        // Console layer
        let console_layer = tracing_subscriber::fmt::layer()
            .with_timer(ChronoUtc::rfc_3339())
            .with_span_events(FmtSpan::CLOSE)
            .with_target(true)
            .with_thread_ids(config.debug_mode)
            .with_thread_names(config.debug_mode);

        if config.json_format {
            let json_layer = console_layer.json().boxed();
            layers.push(json_layer);
        } else {
            let pretty_layer = console_layer.pretty().boxed();
            layers.push(pretty_layer);
        }

        // File layer if enabled
        if config.log_to_file {
            if let Some(log_file) = &config.log_file {
                // Create log directory if it doesn't exist
                if let Some(parent) = log_file.parent() {
                    std::fs::create_dir_all(parent).map_err(|e| {
                        GitMailError::Io(e).with_context(
                            crate::error::ErrorContext::file_operation(
                                "create_log_directory",
                                &parent.to_string_lossy(),
                            ),
                        )
                    })?;
                }

                let file_appender = tracing_appender::rolling::daily(
                    log_file
                        .parent()
                        .unwrap_or_else(|| std::path::Path::new(".")),
                    log_file
                        .file_stem()
                        .and_then(|s| s.to_str())
                        .unwrap_or("git-mail"),
                );

                let file_layer = tracing_subscriber::fmt::layer()
                    .with_writer(file_appender)
                    .with_timer(ChronoUtc::rfc_3339())
                    .with_ansi(false);

                if config.json_format {
                    layers.push(file_layer.json().boxed());
                } else {
                    layers.push(file_layer.boxed());
                }
            }
        }

        // Build environment filter
        let mut env_filter = EnvFilter::new(&config.level);

        // Add component-specific levels
        for (component, level) in &config.component_levels {
            env_filter =
                env_filter.add_directive(format!("{}={}", component, level).parse().map_err(
                    |e| GitMailError::Config(format!("Invalid log level directive: {}", e)),
                )?);
        }

        // Initialize subscriber
        tracing_subscriber::registry()
            .with(env_filter)
            .with(layers)
            .init();

        let performance_monitor = PerformanceMonitor::new(config.clone());

        info!("Logging system initialized with level: {}", config.level);
        if config.debug_mode {
            debug!("Debug mode enabled");
        }
        if config.enable_performance_monitoring {
            info!("Performance monitoring enabled");
        }

        Ok(Self {
            config,
            performance_monitor,
        })
    }

    /// Get the performance monitor
    pub fn performance_monitor(&self) -> &PerformanceMonitor {
        &self.performance_monitor
    }

    /// Create a debug context
    pub fn debug_context(&self, component: String, operation: String) -> DebugContext {
        DebugContext::new(component, operation)
    }

    /// Check if debug mode is enabled
    pub fn is_debug_mode(&self) -> bool {
        self.config.debug_mode
    }

    /// Get current logging configuration
    pub fn config(&self) -> &LoggingConfig {
        &self.config
    }

    /// Update logging level at runtime
    pub fn set_level(&mut self, level: String) -> Result<()> {
        // Validate level
        let _: Level = level
            .parse()
            .map_err(|e| GitMailError::Config(format!("Invalid log level '{}': {}", level, e)))?;

        self.config.level = level;
        info!("Log level updated to: {}", self.config.level);
        Ok(())
    }

    /// Enable or disable debug mode
    pub fn set_debug_mode(&mut self, enabled: bool) {
        self.config.debug_mode = enabled;
        if enabled {
            info!("Debug mode enabled");
        } else {
            info!("Debug mode disabled");
        }
    }

    /// Log system information for debugging
    pub fn log_system_info(&self) {
        info!("=== Git-Mail System Information ===");
        info!("Version: {}", env!("CARGO_PKG_VERSION"));

        // Use runtime environment variables with fallbacks
        let build_date =
            std::env::var("VERGEN_BUILD_DATE").unwrap_or_else(|_| "unknown".to_string());
        let git_sha = std::env::var("VERGEN_GIT_SHA").unwrap_or_else(|_| "unknown".to_string());
        let rust_version =
            std::env::var("VERGEN_RUSTC_SEMVER").unwrap_or_else(|_| "unknown".to_string());

        info!("Build: {} {}", build_date, git_sha);
        info!("Rust version: {}", rust_version);
        info!("Debug mode: {}", self.config.debug_mode);
        info!(
            "Performance monitoring: {}",
            self.config.enable_performance_monitoring
        );
        info!("Log level: {}", self.config.level);

        if let Some(log_file) = &self.config.log_file {
            info!("Log file: {}", log_file.display());
        }

        info!("===================================");
    }
}

/// Macro for creating performance-tracked operations
#[macro_export]
macro_rules! track_performance {
    ($monitor:expr, $component:expr, $operation:expr, $code:block) => {{
        let tracker = $monitor.start_operation($operation.to_string(), $component.to_string());
        let result = $code;
        match &result {
            Ok(_) => tracker.complete().await,
            Err(e) => tracker.fail(e).await,
        }
        result
    }};
}

/// Macro for debug logging with context
#[macro_export]
macro_rules! debug_with_context {
    ($ctx:expr, $($arg:tt)*) => {
        $ctx.debug(&format!($($arg)*))
    };
}

/// Macro for info logging with context
#[macro_export]
macro_rules! info_with_context {
    ($ctx:expr, $($arg:tt)*) => {
        $ctx.info(&format!($($arg)*))
    };
}

/// Macro for warning logging with context
#[macro_export]
macro_rules! warn_with_context {
    ($ctx:expr, $($arg:tt)*) => {
        $ctx.warn(&format!($($arg)*))
    };
}

/// Macro for error logging with context
#[macro_export]
macro_rules! error_with_context {
    ($ctx:expr, $error:expr, $($arg:tt)*) => {
        $ctx.error(&format!($($arg)*), Some($error))
    };
    ($ctx:expr, $($arg:tt)*) => {
        $ctx.error(&format!($($arg)*), None)
    };
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;
    use tokio::time::sleep;

    #[test]
    fn test_logging_config_default() {
        let config = LoggingConfig::default();
        assert_eq!(config.level, "info");
        assert!(!config.debug_mode);
        assert!(!config.log_to_file);
        assert!(!config.enable_performance_monitoring);
    }

    #[test]
    fn test_performance_metrics() {
        let mut metrics = PerformanceMetrics::new("test_op".to_string(), "test_comp".to_string());

        // Simulate some work
        std::thread::sleep(Duration::from_millis(10));

        metrics = metrics.complete();
        assert!(metrics.success);
        assert!(metrics.duration_ms() >= 10);
        assert!(!metrics.is_slow(1000));
        assert!(metrics.is_slow(5));
    }

    #[test]
    fn test_performance_metrics_with_error() {
        let mut metrics = PerformanceMetrics::new("test_op".to_string(), "test_comp".to_string());
        let error = GitMailError::Config("Test error".to_string());

        metrics = metrics.fail(&error);
        assert!(!metrics.success);
        assert!(metrics.error_message.is_some());
        assert!(metrics
            .error_message
            .as_ref()
            .unwrap()
            .contains("Test error"));
    }

    #[test]
    fn test_performance_statistics_empty() {
        let stats = PerformanceStatistics::from_metrics(&[]);
        assert_eq!(stats.total_operations, 0);
        assert_eq!(stats.successful_operations, 0);
        assert_eq!(stats.failed_operations, 0);
    }

    #[test]
    fn test_performance_statistics() {
        let metrics = vec![
            PerformanceMetrics::new("op1".to_string(), "comp1".to_string()).complete(),
            PerformanceMetrics::new("op2".to_string(), "comp1".to_string()).complete(),
            PerformanceMetrics::new("op3".to_string(), "comp2".to_string())
                .fail(&GitMailError::Config("test".to_string())),
        ];

        let stats = PerformanceStatistics::from_metrics(&metrics);
        assert_eq!(stats.total_operations, 3);
        assert_eq!(stats.successful_operations, 2);
        assert_eq!(stats.failed_operations, 1);
        assert_eq!(stats.operations_by_component.get("comp1"), Some(&2));
        assert_eq!(stats.operations_by_component.get("comp2"), Some(&1));
    }

    #[test]
    fn test_debug_context() {
        let ctx = DebugContext::new("test_comp".to_string(), "test_op".to_string())
            .with_info("key1".to_string(), "value1".to_string())
            .with_info("key2".to_string(), "value2".to_string());

        assert_eq!(ctx.component, "test_comp");
        assert_eq!(ctx.operation, "test_op");
        assert_eq!(ctx.debug_info.get("key1"), Some(&"value1".to_string()));
        assert_eq!(ctx.debug_info.get("key2"), Some(&"value2".to_string()));
        assert!(!ctx.trace_id.is_empty());
    }

    #[tokio::test]
    async fn test_performance_monitor() {
        let config = LoggingConfig {
            enable_performance_monitoring: true,
            ..Default::default()
        };
        let monitor = PerformanceMonitor::new(config);

        let tracker = monitor.start_operation("test_op".to_string(), "test_comp".to_string());
        sleep(Duration::from_millis(10)).await;
        tracker.complete().await;

        let stats = monitor.get_statistics().await;
        assert_eq!(stats.total_operations, 1);
        assert_eq!(stats.successful_operations, 1);
        assert_eq!(stats.failed_operations, 0);
    }
}
