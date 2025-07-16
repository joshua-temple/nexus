# Kafka/RedPanda Integration Example

Production-ready Kafka implementation for Nexus, featuring high-throughput message processing with RedPanda.

## Overview

This example demonstrates how to use Nexus with Apache Kafka (via RedPanda) for building scalable, distributed systems. It includes:

- **Complete Kafka Publisher/Subscriber implementation**
- **Docker Compose setup** with RedPanda (Kafka-compatible)
- **Integration tests** showing real-world usage
- **Performance optimizations** for production use
- **Automatic partition key routing** using RootID

## Prerequisites

- Docker and Docker Compose
- Go 1.21+
- Basic understanding of Kafka concepts

## Quick Start

### 1. Start RedPanda

```bash
# Start RedPanda container
docker-compose up -d

# Verify it's running
docker-compose ps

# View logs
docker-compose logs -f redpanda
```

### 2. Run the Examples

```bash
# Run all tests
go test -v

# Run integration tests only
go test -v -tags=integration

# Run specific example
go test -v -run TestKafkaExample
```

### 3. Stop RedPanda

```bash
docker-compose down

# Remove volumes (clean slate)
docker-compose down -v
```

## Architecture

### Components

1. **KafkaPublisher**: Produces messages to Kafka topics
2. **KafkaSubscriber**: Consumes messages and routes to handlers
3. **RedPanda**: Kafka-compatible broker (faster, easier to run)
4. **Integration Tests**: Real-world usage examples

### Message Flow

```
Handler A                    Kafka                      Handler B
    |                         |                            |
    |-- Dispatch -------->    |                            |
    |                         |                            |
    |                         |---- Route by topic ----->  |
    |                         |                            |
    |                         |<--- Reply (RequestID) ---  |
    |                         |                            |
    |<-- Response --------    |                            |
```

## Implementation Details

### Publisher

```go
type KafkaPublisher struct {
    client *kgo.Client
}

func NewKafkaPublisher(brokers []string, opts ...PublisherOption) (*KafkaPublisher, error) {
    config := &PublisherConfig{
        ProducerConfig: kgo.ProducerBatchCompression(kgo.GzipCompression()),
    }
    
    // Apply options
    for _, opt := range opts {
        opt(config)
    }
    
    client, err := kgo.NewClient(
        kgo.SeedBrokers(brokers...),
        config.ProducerConfig,
    )
    
    return &KafkaPublisher{client: client}, err
}
```

Key features:
- Automatic compression
- Configurable batching
- Partition key routing via RootID
- Idempotent producing

### Subscriber

```go
type KafkaSubscriber struct {
    client      *kgo.Client
    handlers    map[string]nexus.Handler
    topics      []string
    consumerGroup string
}

func NewKafkaSubscriber(brokers []string, group string, opts ...SubscriberOption) (*KafkaSubscriber, error) {
    // Consumer configuration
    client, err := kgo.NewClient(
        kgo.SeedBrokers(brokers...),
        kgo.ConsumerGroup(group),
        kgo.ConsumeTopics(topics...),
        kgo.DisableAutoCommit(),
    )
    
    return &KafkaSubscriber{
        client: client,
        consumerGroup: group,
        handlers: make(map[string]nexus.Handler),
    }, err
}
```

Key features:
- Consumer group support
- Manual offset management
- At-least-once delivery
- Dynamic topic subscription

## Configuration Options

### Publisher Options

```go
// With custom producer config
pub, err := kafka.NewKafkaPublisher(
    []string{"localhost:9092"},
    kafka.WithProducerConfig(
        kgo.ProducerBatchMaxBytes(1048576), // 1MB batches
        kgo.ProducerLinger(10*time.Millisecond),
        kgo.RequiredAcks(kgo.AllISRAcks()),
    ),
)

// With metrics
pub, err := kafka.NewKafkaPublisher(
    brokers,
    kafka.WithMetrics(prometheusRegistry),
)
```

### Subscriber Options

```go
// With custom consumer config
sub, err := kafka.NewKafkaSubscriber(
    []string{"localhost:9092"},
    "my-service-group",
    kafka.WithConsumerConfig(
        kgo.FetchMaxBytes(5242880), // 5MB
        kgo.SessionTimeout(20*time.Second),
    ),
)

// With error handler
sub, err := kafka.NewKafkaSubscriber(
    brokers,
    group,
    kafka.WithErrorHandler(func(err error) {
        log.Printf("Kafka error: %v", err)
    }),
)
```

## Production Considerations

### 1. Partitioning Strategy

Nexus uses the `RootID` as the partition key, ensuring:
- All messages in a conversation go to the same partition
- Message ordering within conversations
- Even distribution across partitions

### 2. Consumer Groups

Use different consumer groups for:
- Multiple instances of the same service (load balancing)
- Different services consuming the same topics
- Blue-green deployments

### 3. Performance Tuning

```go
// High throughput configuration
kgo.ProducerBatchMaxBytes(1048576),     // 1MB batches
kgo.ProducerLinger(100*time.Millisecond), // Wait for batches
kgo.ProducerBatchCompression(kgo.LZ4Compression()), // Fast compression

// Low latency configuration  
kgo.ProducerBatchMaxBytes(16384),      // 16KB batches
kgo.ProducerLinger(0),                 // No waiting
kgo.RequiredAcks(kgo.LeaderAck()),     // Faster acks
```

### 4. Monitoring

Key metrics to monitor:
- Producer batch size
- Consumer lag
- Message processing time
- Error rates
- Partition distribution

### 5. Error Handling

```go
// Implement retry logic
type RetryHandler struct {
    nexus.Handler
    maxRetries int
}

func (h *RetryHandler) Handle(ctx *nexus.Context) error {
    for i := 0; i < h.maxRetries; i++ {
        err := h.Handler.Handle(ctx)
        if err == nil {
            return nil
        }
        
        // Exponential backoff
        time.Sleep(time.Duration(math.Pow(2, float64(i))) * time.Second)
    }
    
    // Send to DLQ after max retries
    return sendToDLQ(ctx)
}
```

## Testing

### Unit Tests

```go
func TestKafkaPublisher(t *testing.T) {
    // Use embedded Kafka for unit tests
    pub := kafka.NewMockPublisher()
    
    // Test publishing
    err := pub.Publish(ctx, envelope)
    assert.NoError(t, err)
    
    // Verify message was published
    assert.Equal(t, 1, pub.MessageCount())
}
```

### Integration Tests

```go
//go:build integration

func TestKafkaIntegration(t *testing.T) {
    // Requires running RedPanda
    if !isRedPandaRunning() {
        t.Skip("RedPanda not running")
    }
    
    // Full integration test
    // ...
}
```

## Troubleshooting

### Connection Issues

```bash
# Check RedPanda is running
docker-compose ps

# Test connectivity
docker exec -it kafka-redpanda-1 rpk cluster info

# Check topics
docker exec -it kafka-redpanda-1 rpk topic list
```

### Consumer Lag

```bash
# Check consumer group status
docker exec -it kafka-redpanda-1 rpk group describe your-group

# Reset consumer offset
docker exec -it kafka-redpanda-1 rpk group seek your-group --to start
```

### Performance Issues

1. **High Latency**: Reduce batch size, disable compression
2. **Low Throughput**: Increase batch size, add producers
3. **Memory Usage**: Tune fetch sizes, add backpressure
4. **CPU Usage**: Check compression settings

## Migration from Other Brokers

### From RabbitMQ
- Topics work similarly to exchanges
- Consumer groups replace competing consumers
- No built-in request/reply - handled by Nexus

### From SQS
- Topics replace queues for pub/sub
- Consumer groups provide similar semantics
- Much higher throughput potential

## Best Practices

1. **Topic Naming**: Use hierarchical names (e.g., `orders.created.v1`)
2. **Versioning**: Include version in topic names
3. **Idempotency**: Make handlers idempotent
4. **Monitoring**: Set up alerts for consumer lag
5. **Capacity Planning**: Plan for peak load + 50%

## Next Steps

1. Review the [integration tests](kafka_test.go) for more examples
2. Explore [producer options](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kgo)
3. Set up monitoring with Prometheus
4. Configure for your production environment

## Resources

- [RedPanda Documentation](https://docs.redpanda.com/)
- [Franz-go Library](https://github.com/twmb/franz-go)
- [Kafka Best Practices](https://kafka.apache.org/documentation/#bestpractices)
- [Nexus Main Documentation](../../README.md)
