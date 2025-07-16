# Nexus Roadmap

This document outlines the planned improvements and features for the Nexus message broker package.

## Critical Fixes (P0) 🚨

These issues need immediate attention as they affect core functionality:

### 1. Race Condition in Response Delivery
- **Issue**: Potential panic when multiple goroutines try to close the `ready` channel simultaneously
- **Location**: `broker.go` - `processEnvelope` method
- **Solution**: Use `sync.Once` or a mutex to ensure channel is closed only once

### 2. Memory Leak in Inbox Map
- **Issue**: The `broker.inbox` sync.Map stores responses indefinitely, leading to unbounded memory growth
- **Solution**: Implement TTL-based cleanup or cleanup after response delivery

### 3. Missing go.mod File ✅
- **Status**: COMPLETED
- **Added**: Proper Go module configuration with all dependencies

## High Priority (P1) 🔥

Important improvements for production readiness:

### 1. Graceful Shutdown
- **Issue**: The broker's `Stop()` method doesn't wait for in-flight messages to complete
- **Solution**: 
  - Add a grace period for workers to finish processing
  - Track active message processing
  - Implement context cancellation properly

### 2. Input Validation
- **Missing**: 
  - Nil checks for handlers, publishers, subscribers
  - Empty string validation for topics
  - Payload size limits
- **Solution**: Add comprehensive validation with descriptive error messages

### 3. Context Cancellation Handling
- **Issue**: Worker goroutines don't check context cancellation
- **Risk**: Potential goroutine leaks
- **Solution**: Properly handle context cancellation in all goroutines

### 4. Enhanced Error Types
- **Current**: Only `ErrNoHandlersForTopic` is defined
- **Needed**:
  - `ErrTimeout` - for response timeouts
  - `ErrInvalidPayload` - for marshaling/unmarshaling errors
  - `ErrPublishFailed` - for publish failures
  - `ErrHandlerPanic` - for handler panic recovery

### 5. CI/CD & Quality Automation
- **Makefile Improvements**:
  - Integrate golangci-lint for comprehensive linting
  - Add targets for test coverage reports
  - Include benchmark targets
  - Add release automation
- **GitHub Actions**:
  - Automated testing on push/PR
  - Linting with golangci-lint
  - Code coverage reporting
  - Security scanning with gosec
  - Dependency vulnerability checks
  - Release automation with goreleaser
- **Quality Badges**:
  - Build status
  - Test coverage percentage
  - Go Report Card grade
  - Latest release version

## Medium Priority (P2) 🎯

Features that enhance functionality and developer experience:

### 1. Observability & Metrics
- **Metrics Collection**:
  - Message count by topic
  - Processing latency histograms
  - Error rates by handler
  - Queue depth monitoring
- **Integration**: Hooks for Prometheus, OpenTelemetry
- **Structured Logging**: Add correlation IDs, trace IDs to all log entries

### 2. Performance Optimizations
- **Connection Pooling**: For publisher implementations
- **Batch Publishing**: Support for high-throughput scenarios
- **Object Pooling**: Use `sync.Pool` for Envelope objects
- **Concurrent Handler Execution**: Option to process messages concurrently per handler

### 3. Testing Improvements
- **Benchmarks**: Add performance benchmarks for:
  - Message throughput
  - Response correlation overhead
  - Memory usage patterns
- **Fuzz Testing**: For unmarshaling logic
- **Integration Tests**: With real brokers (Kafka, RabbitMQ)
- **Test Coverage**: Increase to >90%

### 4. Documentation Enhancements
- **Godoc Comments**: Add comprehensive documentation for all exported types
- **Architecture Diagrams**: Visual representation of message flow
- **Best Practices Guide**: 
  - Error handling patterns
  - Testing strategies
  - Performance tuning
- **Migration Guide**: For users upgrading from other brokers

## Future Enhancements (P3) 🚀

Advanced features for complex use cases:

### 1. Circuit Breaker Pattern
- **Purpose**: Prevent cascading failures
- **Features**:
  - Automatic handler circuit breaking
  - Configurable failure thresholds
  - Half-open state testing
  - Metrics integration

### 2. Message Deduplication
- **Purpose**: Prevent duplicate processing
- **Implementation**:
  - Configurable deduplication window
  - Pluggable storage backends
  - Automatic cleanup

### 3. Dead Letter Queue (DLQ) Handling
- **Features**:
  - Automatic failed message routing
  - Retry policies
  - DLQ inspection tools
  - Reprocessing capabilities

### 4. Backpressure Handling
- **Purpose**: Prevent system overload
- **Implementation**:
  - Consumer rate limiting
  - Dynamic worker scaling
  - Queue depth monitoring
  - Automatic throttling

### 5. Message Versioning & Schema Evolution
- **Features**:
  - Schema registry integration
  - Backward/forward compatibility
  - Automatic version negotiation
  - Migration tooling

### 6. Advanced Routing
- **Content-Based Routing**: Route messages based on payload content
- **Topic Wildcards**: Support for pattern-based subscriptions
- **Message Filtering**: Client-side filtering before processing

### 7. Distributed Tracing
- **OpenTelemetry Integration**: Full distributed tracing support
- **Trace Propagation**: Through message headers
- **Visualization**: Integration with Jaeger, Zipkin

### 8. Security Features
- **Message Encryption**: End-to-end encryption support
- **Authentication**: Handler-level authentication
- **Authorization**: Topic-based access control
- **Audit Logging**: Complete audit trail

## Contributing

We welcome contributions! If you're interested in working on any of these items:

1. Check if someone is already working on it (see Issues)
2. Comment on the issue or create one if it doesn't exist
3. Submit a PR with tests and documentation
4. Tag it with the appropriate priority label

## Implementation Priority

1. **Q1 2025**: Complete all P0 critical fixes
2. **Q2 2025**: Implement P1 high priority items (including CI/CD)
3. **Q3 2025**: Begin P2 medium priority features
4. **Q4 2025**: P2 completion and P3 planning
5. **2026**: P3 feature implementation based on community feedback

## Feedback

This roadmap is not set in stone. We value community input:

- Vote on issues you'd like to see prioritized
- Suggest new features via GitHub Issues
- Join our discussions on implementation approaches

Last updated: January 2025
