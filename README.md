# Nexus

[![Build Status](https://github.com/joshua-temple/nexus/workflows/CI/badge.svg)](https://github.com/joshua-temple/nexus/actions)
[![Coverage Status](https://coveralls.io/repos/github/joshua-temple/nexus/badge.svg?branch=main)](https://coveralls.io/github/joshua-temple/nexus?branch=main)
[![Go Report Card](https://goreportcard.com/badge/github.com/joshua-temple/nexus)](https://goreportcard.com/report/github.com/joshua-temple/nexus)
[![GoDoc](https://pkg.go.dev/badge/github.com/joshua-temple/nexus)](https://pkg.go.dev/github.com/joshua-temple/nexus)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go Version](https://img.shields.io/github/go-mod/go-version/joshua-temple/nexus)](https://github.com/joshua-temple/nexus/blob/main/go.mod)

A powerful, minimal-boilerplate message broker for Go with automatic response correlation and hierarchical command orchestration.

## Quick Start

Get up and running in 2 minutes:

```bash
go get github.com/joshua-temple/nexus
```

```go
package main

import (
    "context"
    "fmt"
    "github.com/joshua-temple/nexus"
)

// Define your handler
type GreetingHandler struct{}

func (h *GreetingHandler) Topics() []nexus.Topic {
    topic, _ := nexus.NewTopic("demo", "greeting", 1)
    return []nexus.Topic{*topic}
}

func (h *GreetingHandler) Handle(ctx *nexus.Context) error {
    var request map[string]string
    ctx.Unmarshal(&request)
    
    return ctx.Reply(map[string]string{
        "message": fmt.Sprintf("Hello, %s!", request["name"]),
    })
}

func main() {
    // Create a simple in-memory broker (see examples for Kafka, RabbitMQ, etc.)
    broker := createMemoryBroker()
    
    // Register your handler
    broker.Register(&GreetingHandler{})
    
    // Start processing messages
    broker.Start(context.Background())
}
```

## What is Nexus?

Nexus is a Go package that simplifies building distributed systems with message-driven architectures. It provides:

- **🚀 Minimal Boilerplate** - Just implement a simple interface and Nexus handles the rest
- **🔄 Automatic Response Correlation** - Parent commands automatically receive child responses
- **🏗️ Hierarchical Command Support** - Build complex workflows with nested command orchestration
- **🎯 Type-Safe Responses** - Use Go generics for compile-time type safety
- **🔌 Implementation Agnostic** - Works with any message broker (Kafka, RabbitMQ, SQS, etc.)
- **⚡ Dynamic Topic Registration** - Add new capabilities at runtime without restarts

## Installation

```bash
go get github.com/joshua-temple/nexus
```

## Key Concepts

### 1. **Envelope**
Every message is wrapped in an envelope that handles routing and correlation:
```go
type Envelope struct {
    ID               string   // Unique message identifier
    RootID           string   // Root parent ID (used for partitioning)
    RequestID        string   // Links request/response pairs
    DestinationTopic string   // Where this message is going
    Payload          any      // Your actual message data
}
```

### 2. **Handler**
Your business logic implements this simple interface:
```go
type Handler interface {
    Topics() []Topic              // Topics this handler listens to
    Handle(ctx *Context) error    // Process the message
}
```

### 3. **Context**
Provides everything you need to process messages and dispatch commands:
```go
// Dispatch child commands
response := nexus.DispatchTyped[PaymentResult](
    ctx, 
    TopicProcessPayment, 
    paymentRequest,
    nexus.WithRequired(),  // This response is required
)

// Wait for responses
ctx.Wait()

// Get typed result
result, _ := response.Value()
```

## Basic Example

Here's a complete example showing parent-child command orchestration:

```go
// Parent handler that orchestrates child services
type OrderHandler struct{}

func (h *OrderHandler) Topics() []nexus.Topic {
    topic, _ := nexus.NewTopic("orders", "create", 1)
    return []nexus.Topic{*topic}
}

func (h *OrderHandler) Handle(ctx *nexus.Context) error {
    var order Order
    ctx.Unmarshal(&order)
    
    // Validate inventory (required)
    inventory := nexus.DispatchTyped[InventoryResult](
        ctx, TopicCheckInventory, order.Items,
        nexus.WithRequired(),
    )
    
    // Process payment (required)
    payment := nexus.DispatchTyped[PaymentResult](
        ctx, TopicChargeCard, order.Payment,
        nexus.WithRequired(),
        nexus.WithTimeout(30*time.Second),
    )
    
    // Send notification (fire-and-forget)
    ctx.Dispatch(TopicNotifyCustomer, notification, nexus.AsEvent())
    
    // Wait for required responses
    if err := ctx.Wait(); err != nil {
        return err
    }
    
    // Return order confirmation
    return ctx.Reply(OrderConfirmation{
        OrderID:     order.ID,
        PaymentID:   payment.Value().TransactionID,
        InventoryID: inventory.Value().ReservationID,
    })
}
```

## Examples Directory

Explore complete working examples:

- **[Memory Broker](examples/memory/)** - In-memory implementation for testing and development
- **[Kafka/RedPanda](examples/kafka/)** - Production-ready Kafka integration with Docker Compose
- **[Example Handlers](examples/example_handler.go)** - Common patterns and best practices

## Advanced Features

### Dynamic Topic Registration
Add new handlers at runtime without restarting:
```go
// Start with initial handlers
broker.Start(ctx)

// Later, add new capabilities dynamically
broker.Register(&NewFeatureHandler{})  // Automatically subscribes to new topics
```

### Type-Safe Responses
Use generics for compile-time type safety:
```go
result := nexus.DispatchTyped[CustomerData](ctx, TopicGetCustomer, id)
customer, err := result.Value()  // Returns CustomerData, not interface{}
```

### Flexible Response Handling
Configure how each command behaves:
```go
// Required response with timeout
nexus.WithRequired()
nexus.WithTimeout(45*time.Second)

// Optional response
// (no options needed)

// Fire-and-forget event
nexus.AsEvent()
```

## Roadmap

See [ROADMAP.md](ROADMAP.md) for planned features and improvements.

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

## License

MIT License - see [LICENSE](LICENSE) for details.
