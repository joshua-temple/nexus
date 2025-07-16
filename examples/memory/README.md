# Memory Broker Example

A lightweight, in-memory implementation of the Nexus broker pattern. Perfect for testing, development, and learning.

## Overview

The memory broker provides a fully functional Nexus implementation without external dependencies. Messages are passed through Go channels, making it ideal for:

- **Unit Testing**: Test your handlers without infrastructure
- **Local Development**: Rapid iteration without setup overhead  
- **Learning**: Understand Nexus concepts without complexity
- **Prototyping**: Quickly validate ideas

## Features

- ✅ Zero external dependencies
- ✅ Instant message delivery via channels
- ✅ Full support for request/response correlation
- ✅ Dynamic topic registration
- ✅ Thread-safe implementation
- ✅ Comprehensive test coverage

## Quick Start

### 1. Basic Usage

```go
package main

import (
    "context"
    "log"
    "github.com/joshua-temple/nexus"
    "github.com/joshua-temple/nexus/examples/memory"
)

func main() {
    // Create memory broker components
    pub := memory.NewPublisher()
    sub := memory.NewSubscriber(pub)
    
    // Create broker with 5 workers
    broker := nexus.New(5, pub, sub, nil)
    
    // Register your handler
    broker.Register(&MyHandler{})
    
    // Start processing
    ctx := context.Background()
    if err := broker.Start(ctx); err != nil {
        log.Fatal(err)
    }
    defer broker.Stop()
    
    // Your application logic here...
}
```

### 2. Running Tests

```bash
# Run all tests
go test -v

# Run specific test
go test -v -run TestMemoryBroker

# Run with race detection
go test -race -v

# Run benchmarks
go test -bench=.
```

### 3. Example Test

```go
func TestMyHandler(t *testing.T) {
    // Setup
    pub := memory.NewPublisher()
    sub := memory.NewSubscriber(pub)
    broker := nexus.New(1, pub, sub, nil)
    
    // Register handler
    broker.Register(&MyHandler{})
    broker.Start(context.Background())
    defer broker.Stop()
    
    // Create test envelope
    topic, _ := nexus.NewTopic("test", "command", 1)
    envelope := nexus.NewEnvelope(
        "test-client",
        topic.String(),
        []string{"reply-topic"},
        map[string]string{"key": "value"},
    )
    
    // Subscribe to replies
    replyChan := pub.Subscribe("reply-topic")
    
    // Send message
    err := pub.Publish(context.Background(), envelope)
    require.NoError(t, err)
    
    // Verify response
    select {
    case reply := <-replyChan:
        // Assert on reply
        assert.Equal(t, envelope.ID, reply.RequestID)
    case <-time.After(time.Second):
        t.Fatal("timeout waiting for reply")
    }
}
```

## Implementation Details

### Publisher

The `MemoryPublisher` manages topic subscriptions and message routing:

```go
type MemoryPublisher struct {
    subscribers map[string][]chan *nexus.Envelope
    mu          sync.RWMutex
}
```

Key methods:
- `Publish()`: Routes messages to all topic subscribers
- `Subscribe()`: Returns a channel for receiving topic messages
- `Unsubscribe()`: Removes a subscription

### Subscriber

The `MemorySubscriber` handles message consumption and handler registration:

```go
type MemorySubscriber struct {
    publisher    *MemoryPublisher
    handlers     map[string]nexus.Handler
    topics       []string
    messageChan  chan *nexus.Envelope
    stopChan     chan struct{}
}
```

Key methods:
- `RegisterTopics()`: Dynamically adds topic subscriptions
- `RegisterHandlers()`: Maps handlers to topics
- `Start()`: Begins message consumption
- `Stop()`: Graceful shutdown

## Advanced Usage

### Custom Message Router

```go
// Create a filtering publisher
type FilteringPublisher struct {
    *memory.MemoryPublisher
    filter func(*nexus.Envelope) bool
}

func (p *FilteringPublisher) Publish(ctx context.Context, env *nexus.Envelope) error {
    if p.filter(env) {
        return p.MemoryPublisher.Publish(ctx, env)
    }
    return nil
}
```

### Testing Timeout Scenarios

```go
func TestHandlerTimeout(t *testing.T) {
    // Create handler that delays response
    handler := &SlowHandler{delay: 2 * time.Second}
    
    // Dispatch with timeout
    response := nexus.DispatchTyped[Result](
        ctx, 
        topic,
        request,
        nexus.WithTimeout(1 * time.Second),
        nexus.WithRequired(),
    )
    
    // Should timeout
    _, err := response.Value()
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "timeout")
}
```

### Simulating Failures

```go
type FailingPublisher struct {
    *memory.MemoryPublisher
    failureRate float64
}

func (p *FailingPublisher) Publish(ctx context.Context, env *nexus.Envelope) error {
    if rand.Float64() < p.failureRate {
        return errors.New("simulated publish failure")
    }
    return p.MemoryPublisher.Publish(ctx, env)
}
```

## Performance Characteristics

- **Latency**: Sub-microsecond message delivery
- **Throughput**: 1M+ messages/second on modern hardware
- **Memory**: O(n) where n = number of active subscriptions
- **CPU**: Minimal overhead, bound by handler processing

## Limitations

The memory broker is not suitable for:
- Multi-process communication
- Persistent message storage
- Distributed systems
- Production deployments requiring durability

For these use cases, see the [Kafka example](../kafka/).

## Comparison with Other Implementations

| Feature | Memory | Kafka | RabbitMQ |
|---------|---------|--------|-----------|
| Setup Complexity | None | Medium | Low |
| Performance | Highest | High | Medium |
| Durability | No | Yes | Yes |
| Scalability | Single Process | Distributed | Distributed |
| Use Case | Testing/Dev | Production | Production |

## Common Issues

### Goroutine Leaks
Always stop the broker:
```go
broker.Start(ctx)
defer broker.Stop() // Important!
```

### Channel Deadlocks
Ensure adequate buffer size for high throughput:
```go
// Increase buffer for high-load scenarios
sub := memory.NewSubscriberWithBuffer(pub, 1000)
```

### Race Conditions
Use the race detector during development:
```bash
go test -race ./...
```

## Next Steps

1. Explore the [example handlers](../example_handler.go)
2. Try the [Kafka implementation](../kafka/) for production use
3. Read about [testing strategies](../../test/)
4. Check the [roadmap](../../ROADMAP.md) for upcoming features

## Contributing

We welcome improvements to the memory broker! Areas of interest:
- Performance optimizations
- Additional test scenarios
- Debugging utilities
- Metrics collection

Submit PRs with tests and benchmarks.
