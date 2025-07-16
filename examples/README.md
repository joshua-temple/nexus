# Nexus Examples

This directory contains practical examples demonstrating how to use Nexus in different scenarios.

## Available Examples

### 1. [Memory Broker](memory/)
A simple in-memory implementation perfect for:
- Unit testing
- Local development
- Learning Nexus concepts
- Prototyping

**Key Features:**
- No external dependencies
- Instant message delivery
- Perfect for testing handler logic

### 2. [Kafka/RedPanda Integration](kafka/)
Production-ready Kafka implementation featuring:
- Docker Compose setup with RedPanda
- High-throughput message processing
- Partition-based message ordering
- Integration tests

**Use Cases:**
- Production deployments
- Event streaming
- High-scale systems
- Microservices communication

### 3. [Example Handlers](example_handler.go)
Common handler patterns including:
- Simple request/response
- Parent-child orchestration
- Parallel dispatching
- Error handling
- Fire-and-forget events

## Quick Start

### Memory Example
```bash
cd memory
go test -v
```

### Kafka Example
```bash
cd kafka
docker-compose up -d
go test -v
```

## Creating Your Own Implementation

To create a custom broker implementation (e.g., for RabbitMQ, SQS, Redis):

1. **Implement the Publisher interface:**
```go
type Publisher interface {
    Publish(ctx context.Context, envelope *Envelope) error
}
```

2. **Implement the Subscriber interface:**
```go
type Subscriber interface {
    RegisterHandlers(ctx context.Context, handlers ...Handler) error
    RegisterTopics(ctx context.Context, topics ...string) error
    Start(ctx context.Context) (<-chan *Envelope, error)
    Stop()
}
```

3. **Create your broker:**
```go
broker := nexus.New(
    10,              // worker count
    myPublisher,     // your publisher
    mySubscriber,    // your subscriber
    logger,          // optional logger
)
```

## Best Practices

### 1. Handler Design
- Keep handlers focused on a single responsibility
- Use child dispatches for complex workflows
- Always validate input data
- Return meaningful errors

### 2. Error Handling
```go
// Use OnResult for validation
response := nexus.DispatchTyped[Result](ctx, topic, data).
    OnResult(func(r Result) error {
        if r.Status != "success" {
            return fmt.Errorf("operation failed: %s", r.Error)
        }
        return nil
    })
```

### 3. Testing
- Use the memory broker for unit tests
- Mock specific handlers for integration tests
- Test timeout scenarios
- Verify error propagation

### 4. Performance
- Set appropriate worker counts based on load
- Use fire-and-forget for notifications
- Consider batching for high-throughput scenarios
- Monitor memory usage with large payloads

## Common Patterns

### Saga Pattern
```go
func (h *SagaHandler) Handle(ctx *nexus.Context) error {
    // Step 1
    step1 := nexus.DispatchTyped[Step1Result](...)
    
    // Step 2 (depends on step 1)
    ctx.Wait()
    if step1Result, _ := step1.Value(); step1Result.Success {
        step2 := nexus.DispatchTyped[Step2Result](...)
        ctx.Wait()
    }
    
    // Compensate if needed
    if err := ctx.Wait(); err != nil {
        // Rollback logic
    }
}
```

### Scatter-Gather
```go
func (h *AggregatorHandler) Handle(ctx *nexus.Context) error {
    // Scatter requests
    results := []Response{
        ctx.Dispatch(ServiceA, data),
        ctx.Dispatch(ServiceB, data),
        ctx.Dispatch(ServiceC, data),
    }
    
    // Gather responses
    ctx.WaitAll() // Wait for all, including optional
    
    // Aggregate results
    // ...
}
```

## Troubleshooting

### Messages Not Being Processed
- Verify handlers are registered for the topic
- Check broker is started
- Ensure subscriber is receiving messages
- Look for errors in logs

### Response Timeouts
- Increase timeout with `WithTimeout(duration)`
- Check if child service is responding
- Verify network connectivity
- Monitor for processing bottlenecks

### Memory Issues
- Implement inbox cleanup (see roadmap)
- Limit payload sizes
- Use streaming for large data
- Monitor goroutine counts

## Contributing Examples

We welcome new examples! To contribute:

1. Create a new directory for your example
2. Include a comprehensive README
3. Add unit tests
4. Document any external dependencies
5. Submit a PR with a clear description

## Questions?

- Check the [main documentation](../)
- Review the [roadmap](../ROADMAP.md) for upcoming features
- Open an issue for bugs or feature requests
- Join our community discussions
