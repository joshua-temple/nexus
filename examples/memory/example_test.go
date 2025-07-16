package memory_test

import (
	"context"
	"fmt"
	"log"
	"time"

	broker "github.com/joshua-temple/nexus"
	"github.com/joshua-temple/nexus/examples/memory"
)

// ExampleNewMemoryBroker demonstrates creating and using an in-memory broker
func ExampleNewMemoryBroker() {
	ctx := context.Background()

	// Create topics
	orderTopic, _ := broker.NewTopic("shop", "order", 1)
	paymentTopic, _ := broker.NewTopic("shop", "payment", 1)

	// Create in-memory broker
	b, hub := memory.NewMemoryBroker(
		[]string{orderTopic.String(), paymentTopic.String()},
		"example-group",
		nil,
	)

	// Register handlers
	_ = b.Register(&OrderHandler{paymentTopic: *paymentTopic})
	_ = b.Register(&PaymentHandler{})

	// Start broker
	if err := b.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer b.Stop()

	// Create a publisher
	pub := memory.NewPublisher(hub)

	// Create a subscriber for results
	resultSub := memory.NewSubscriber(hub, []string{"shop.result"}, "client", nil)
	results, _ := resultSub.Start(ctx)
	defer resultSub.Stop()

	// Give everything time to start
	time.Sleep(100 * time.Millisecond)

	// Send an order
	order := map[string]interface{}{
		"id":     "order-123",
		"amount": 99.99,
		"items":  []string{"widget", "gadget"},
	}

	envelope := &broker.Envelope{
		ID:               "req-1",
		RootID:           "root-1",
		SourceTopic:      "client",
		DestinationTopic: orderTopic.String(),
		ReplyToTopics:    []string{"shop.result"},
		Payload:          order,
		CreatedAt:        time.Now().UnixNano(),
	}

	if err := pub.Publish(ctx, envelope); err != nil {
		log.Fatal(err)
	}

	// Wait for result
	select {
	case result := <-results:
		if data, ok := result.Payload.(map[string]interface{}); ok {
			fmt.Printf("Order %s processed: %s\n", data["orderId"], data["status"])
		}
	case <-time.After(2 * time.Second):
		fmt.Println("Timeout waiting for result")
	}

	// Output: Order order-123 processed: payment confirmed
}

// ExampleHub_SetStoreLimit demonstrates message storage and replay
func ExampleHub_SetStoreLimit() {
	ctx := context.Background()

	// Create hub with limited storage
	hub := memory.NewHub()
	hub.SetStoreLimit(3) // Only store last 3 messages per topic

	// Publish 5 messages
	pub := memory.NewPublisher(hub)
	for i := 1; i <= 5; i++ {
		envelope := &broker.Envelope{
			ID:               fmt.Sprintf("msg-%d", i),
			DestinationTopic: "events",
			Payload:          fmt.Sprintf("Event %d", i),
		}
		_ = pub.Publish(ctx, envelope)
	}

	// New subscriber joins
	sub := memory.NewSubscriber(hub, []string{"events"}, "late-joiner", nil)
	msgChan, _ := sub.Start(ctx)
	defer sub.Stop()

	// Collect replayed messages
	replayedCount := 0
	timeout := time.After(200 * time.Millisecond)

	for {
		select {
		case msg := <-msgChan:
			fmt.Printf("Replayed: %s\n", msg.ID)
			replayedCount++
		case <-timeout:
			fmt.Printf("Total replayed: %d messages\n", replayedCount)
			return
		}
	}

	// Output:
	// Replayed: msg-3
	// Replayed: msg-4
	// Replayed: msg-5
	// Total replayed: 3 messages
}

// Example handlers
type OrderHandler struct {
	paymentTopic broker.Topic
}

func (h *OrderHandler) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("shop", "order", 1)
	return []broker.Topic{*topic}
}

func (h *OrderHandler) Handle(ctx *broker.Context) error {
	var order map[string]interface{}
	if err := ctx.Unmarshal(&order); err != nil {
		return err
	}

	// Process order and request payment
	paymentRequest := map[string]interface{}{
		"orderId": order["id"],
		"amount":  order["amount"],
	}

	// Dispatch to payment service
	paymentResp := broker.DispatchTyped[map[string]interface{}](
		ctx,
		h.paymentTopic,
		paymentRequest,
		broker.WithRequired(),
	)

	// Wait for payment confirmation
	if err := ctx.Wait(); err != nil {
		return err
	}

	payment, err := paymentResp.Value()
	if err != nil {
		return err
	}

	// Reply with result
	return ctx.Reply(map[string]interface{}{
		"orderId": order["id"],
		"status":  payment["status"],
	})
}

type PaymentHandler struct{}

func (h *PaymentHandler) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("shop", "payment", 1)
	return []broker.Topic{*topic}
}

func (h *PaymentHandler) Handle(ctx *broker.Context) error {
	var payment map[string]interface{}
	if err := ctx.Unmarshal(&payment); err != nil {
		return err
	}

	// Simulate payment processing
	return ctx.Reply(map[string]interface{}{
		"orderId": payment["orderId"],
		"status":  "payment confirmed",
	})
}
