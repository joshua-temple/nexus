package test

import (
	"context"
	"testing"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// TestWorkingExample demonstrates a complete working example of the broker
func TestWorkingExample(t *testing.T) {
	// This test shows:
	// 1. Basic message handling works
	// 2. Handlers can dispatch to other handlers
	// 3. Responses are automatically correlated
	// 4. The broker manages the entire flow

	t.Log("=== dsub/broker Working Example ===")
	t.Log("This demonstrates automatic cyclical response handling")

	// Create broker infrastructure
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)
	b := broker.New(3, pub, sub, logger)

	// Create a simple service that processes orders
	orderService := &OrderService{t: t}
	inventoryService := &InventoryService{t: t}

	// Register services
	b.Register(orderService)     // nolint: errcheck
	b.Register(inventoryService) // nolint: errcheck

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create order processing request
	orderTopic, _ := broker.NewTopic("shop", "order", 1)
	orderTopic = orderTopic.WithPurpose("process").WithTopicType(broker.TopicTypeCommand)

	// Send order
	orderRequest := map[string]interface{}{
		"order_id": "ORDER-123",
		"item_id":  "WIDGET-42",
		"quantity": 5,
	}

	envelope := &broker.Envelope{
		ID:               "order-req-1",
		RootID:           "root-order-1",
		RequestID:        "order-req-1", // Set RequestID for response correlation
		SourceTopic:      "test-client",
		DestinationTopic: orderTopic.String(),
		ReplyToTopics:    []string{"order-reply"},
		Payload:          marshalPayload(orderRequest),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Subscribe to reply
	replyCh := pub.Subscribe("order-reply")

	// Send order
	if err := pub.Publish(ctx, envelope); err != nil {
		t.Fatalf("Failed to publish order: %v", err)
	}

	// Wait for order processing result
	select {
	case reply := <-replyCh:
		t.Log("=== Order Processing Complete ===")

		var result map[string]interface{}
		unmarshalResponse(t, reply.Payload, &result)

		// Verify order was processed
		if result["status"] != "processed" {
			t.Errorf("Expected status 'processed', got %v", result["status"])
		}

		if result["order_id"] != "ORDER-123" {
			t.Errorf("Expected order_id ORDER-123, got %v", result["order_id"])
		}

		// Verify inventory was checked (child handler was called)
		if result["inventory_checked"] != true {
			t.Errorf("Expected inventory to be checked")
		}

		if result["quantity_available"] != float64(5) {
			t.Errorf("Expected quantity_available 5, got %v", result["quantity_available"])
		}

		t.Logf("✓ Order processed successfully: %+v", result)
		t.Log("✓ Automatic response correlation worked")
		t.Log("✓ Child handler (inventory) was called and response was received")

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for order reply")
	}
}

// OrderService handles order processing
type OrderService struct {
	t *testing.T
}

func (s *OrderService) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("shop", "order", 1)
	topic = topic.WithPurpose("process").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*topic}
}

func (s *OrderService) Handle(ctx *broker.Context) error {
	s.t.Logf("OrderService: Received order request (ID: %s, RequestID: %s)", ctx.Envelope.ID, ctx.Envelope.RequestID)

	// Unmarshal order
	var order map[string]interface{}
	if err := ctx.Unmarshal(&order); err != nil {
		return err
	}

	s.t.Logf("OrderService: Processing order %s for item %s", order["order_id"], order["item_id"])

	// Check inventory (dispatch to child service)
	inventoryTopic, _ := broker.NewTopic("shop", "inventory", 1)
	inventoryTopic = inventoryTopic.WithPurpose("check").WithTopicType(broker.TopicTypeCommand)

	inventoryRequest := map[string]interface{}{
		"item_id":  order["item_id"],
		"quantity": order["quantity"],
	}

	s.t.Log("OrderService: Dispatching to inventory service")

	// Here's the key: we dispatch and mark it as required
	// Make sure to send JSON bytes, not native Go types
	inventoryResp := broker.DispatchTyped[map[string]interface{}](
		ctx,
		*inventoryTopic,
		marshalPayload(inventoryRequest), // Convert to JSON bytes
		broker.WithRequired(),
		broker.WithTimeout(3*time.Second), // Increase timeout
	)

	// Wait for required responses (inventory check)
	s.t.Log("OrderService: Waiting for inventory response...")
	if err := ctx.Wait(); err != nil {
		s.t.Logf("OrderService: Failed to get inventory response: %v", err)
		return err
	}

	// Get inventory result
	inventory, err := inventoryResp.Value()
	if err != nil {
		s.t.Logf("OrderService: Error getting inventory value: %v", err)
		return err
	}

	s.t.Logf("OrderService: Got inventory response: %+v", inventory)

	// Process order based on inventory
	result := map[string]interface{}{
		"order_id":           order["order_id"],
		"status":             "processed",
		"inventory_checked":  true,
		"quantity_available": inventory["available"],
		"can_fulfill":        inventory["available"].(float64) >= order["quantity"].(float64),
	}

	s.t.Log("OrderService: Replying with processed order")
	return ctx.Reply(result)
}

// InventoryService checks inventory
type InventoryService struct {
	t *testing.T
}

func (s *InventoryService) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("shop", "inventory", 1)
	topic = topic.WithPurpose("check").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*topic}
}

func (s *InventoryService) Handle(ctx *broker.Context) error {
	s.t.Logf("InventoryService: Received inventory check request (ID: %s, RequestID: %s, ReplyTo: %v)",
		ctx.Envelope.ID, ctx.Envelope.RequestID, ctx.Envelope.ReplyToTopics)

	var request map[string]interface{}
	if err := ctx.Unmarshal(&request); err != nil {
		return err
	}

	s.t.Logf("InventoryService: Checking inventory for item %s", request["item_id"])

	// Simulate inventory check - we have the requested quantity
	result := map[string]interface{}{
		"item_id":   request["item_id"],
		"available": request["quantity"], // We have exactly what's needed
		"in_stock":  true,
	}

	s.t.Logf("InventoryService: Replying with inventory status to %v", ctx.Envelope.ReplyToTopics)
	return ctx.Reply(result)
}

// TestTypedResponseExample shows the typed response functionality working
func TestTypedResponseExample(t *testing.T) {
	t.Log("=== Typed Response Example ===")

	// Create broker
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)
	b := broker.New(2, pub, sub, logger)

	// Create calculation service
	calcService := &CalculationService{t: t}
	b.Register(calcService) // nolint: errcheck

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	time.Sleep(100 * time.Millisecond)

	// Send calculation request
	calcTopic, _ := broker.NewTopic("math", "calc", 1)
	calcTopic = calcTopic.WithPurpose("multiply").WithTopicType(broker.TopicTypeCommand)

	calcRequest := CalculationRequest{
		A: 7,
		B: 6,
	}

	envelope := &broker.Envelope{
		ID:               "calc-1",
		RootID:           "root-calc-1",
		RequestID:        "calc-1", // Set RequestID for response correlation
		SourceTopic:      "test-client",
		DestinationTopic: calcTopic.String(),
		ReplyToTopics:    []string{"calc-reply"},
		Payload:          marshalPayload(calcRequest),
		CreatedAt:        time.Now().UnixNano(),
	}

	replyCh := pub.Subscribe("calc-reply")

	if err := pub.Publish(ctx, envelope); err != nil {
		t.Fatalf("Failed to publish calc request: %v", err)
	}

	select {
	case reply := <-replyCh:
		var result CalculationResult
		unmarshalResponse(t, reply.Payload, &result)

		if result.Result != 42 {
			t.Errorf("Expected result 42, got %d", result.Result)
		}

		t.Logf("✓ Typed response worked: %d * %d = %d",
			result.A, result.B, result.Result)

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for calc reply")
	}
}

// Typed request/response for calculation
type CalculationRequest struct {
	A int `json:"a"`
	B int `json:"b"`
}

type CalculationResult struct {
	A      int `json:"a"`
	B      int `json:"b"`
	Result int `json:"result"`
}

type CalculationService struct {
	t *testing.T
}

func (s *CalculationService) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("math", "calc", 1)
	topic = topic.WithPurpose("multiply").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*topic}
}

func (s *CalculationService) Handle(ctx *broker.Context) error {
	var req CalculationRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	result := CalculationResult{
		A:      req.A,
		B:      req.B,
		Result: req.A * req.B,
	}

	s.t.Logf("CalculationService: %d * %d = %d", req.A, req.B, result.Result)

	return ctx.Reply(result)
}
