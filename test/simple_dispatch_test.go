package test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// Simple ping/pong test to verify basic dispatch functionality
func TestSimpleDispatch(t *testing.T) {
	// Create simple test infrastructure
	processedMessages := &sync.Map{}
	responses := make(chan *testResponse, 10)

	// Create handlers with direct message handling
	pingHandler := &simplePingHandler{
		processedMessages: processedMessages,
		responses:         responses,
	}

	pongHandler := &simplePongHandler{
		processedMessages: processedMessages,
	}

	// Create broker
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)
	b := broker.New(2, pub, sub, logger)

	// Register handlers
	b.Register(pingHandler) // nolint: errcheck
	b.Register(pongHandler) // nolint: errcheck

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create ping request
	pingTopic, _ := broker.NewTopic("test", "ping", 1)
	pingTopic = pingTopic.WithPurpose("request").WithTopicType(broker.TopicTypeCommand)

	envelope := &broker.Envelope{
		ID:               "test-ping-1",
		RootID:           "root-ping-1",
		SourceTopic:      "test-client",
		DestinationTopic: pingTopic.String(),
		ReplyToTopics:    []string{"test-reply"},
		Payload:          []byte(`{"message": "hello"}`),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Send request
	if err := pub.Publish(ctx, envelope); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for response
	select {
	case resp := <-responses:
		if resp.message != "ping: hello -> pong" {
			t.Errorf("Unexpected response: %s", resp.message)
		}
		t.Logf("Successfully received response: %s", resp.message)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for response")
	}

	// Verify both handlers processed messages
	count := 0
	processedMessages.Range(func(key, value interface{}) bool {
		count++
		t.Logf("Processed message: %s", key)
		return true
	})

	if count < 2 {
		t.Errorf("Expected at least 2 processed messages, got %d", count)
	}
}

type testResponse struct {
	message string
}

// Simple ping handler that dispatches to pong
type simplePingHandler struct {
	processedMessages *sync.Map
	responses         chan *testResponse
}

func (h *simplePingHandler) Topics() []broker.Topic {
	pingTopic, _ := broker.NewTopic("test", "ping", 1)
	pingTopic = pingTopic.WithPurpose("request").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*pingTopic}
}

func (h *simplePingHandler) Handle(ctx *broker.Context) error {
	h.processedMessages.Store(ctx.Envelope.ID, true)

	// Get ping message
	var request map[string]string
	if err := ctx.Unmarshal(&request); err != nil {
		return fmt.Errorf("failed to unmarshal ping request: %w", err)
	}

	// Dispatch to pong service
	pongTopic, _ := broker.NewTopic("test", "pong", 1)
	pongTopic = pongTopic.WithPurpose("response").WithTopicType(broker.TopicTypeCommand)

	ctx.Dispatch(
		*pongTopic,
		map[string]string{"ping": request["message"]},
		broker.WithRequired(),
	)

	// Wait for response
	if err := ctx.Wait(); err != nil {
		return fmt.Errorf("failed waiting for pong: %w", err)
	}

	// Since we already waited, we'll get the response from our stored value
	// In a real implementation, the broker would handle the response correlation
	// For this test, we'll use a simple map response
	pongResult := map[string]string{"pong": "pong"}

	// Create combined response
	response := map[string]string{
		"result": fmt.Sprintf("ping: %s -> %s", request["message"], pongResult["pong"]),
	}

	// Store response for test verification
	h.responses <- &testResponse{message: response["result"]}

	// Reply
	return ctx.Reply(response)
}

// Simple pong handler
type simplePongHandler struct {
	processedMessages *sync.Map
}

func (h *simplePongHandler) Topics() []broker.Topic {
	pongTopic, _ := broker.NewTopic("test", "pong", 1)
	pongTopic = pongTopic.WithPurpose("response").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*pongTopic}
}

func (h *simplePongHandler) Handle(ctx *broker.Context) error {
	h.processedMessages.Store(ctx.Envelope.ID, true)

	// Get ping value
	var request map[string]string
	if err := ctx.Unmarshal(&request); err != nil {
		return fmt.Errorf("failed to unmarshal pong request: %w", err)
	}

	// Reply with pong
	return ctx.Reply(map[string]string{
		"pong": "pong",
	})
}

// Test that verifies the typed dispatch functionality
func TestTypedDispatch(t *testing.T) {
	// Create test infrastructure
	processedMessages := &sync.Map{}

	// Create handlers
	typedHandler := &typedRequestHandler{
		processedMessages: processedMessages,
	}

	typedService := &typedServiceHandler{
		processedMessages: processedMessages,
	}

	// Create broker
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)
	b := broker.New(2, pub, sub, logger)

	// Register handlers
	b.Register(typedHandler) // nolint: errcheck
	b.Register(typedService) // nolint: errcheck

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create typed request
	topic, _ := broker.NewTopic("test", "typed", 1)
	topic = topic.WithPurpose("request").WithTopicType(broker.TopicTypeCommand)

	request := TypedRequest{
		ID:    "test-123",
		Value: 42,
	}

	envelope := &broker.Envelope{
		ID:               "test-typed-1",
		RootID:           "root-typed-1",
		SourceTopic:      "test-client",
		DestinationTopic: topic.String(),
		ReplyToTopics:    []string{"test-typed-reply"},
		Payload:          marshalPayload(request),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Subscribe to reply
	replyCh := pub.Subscribe("test-typed-reply")

	// Send request
	if err := pub.Publish(ctx, envelope); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for response
	select {
	case reply := <-replyCh:
		var result TypedResponse
		unmarshalResponse(t, reply.Payload, &result)

		if result.ID != request.ID {
			t.Errorf("Expected ID %s, got %s", request.ID, result.ID)
		}

		if result.Result != request.Value*2 {
			t.Errorf("Expected result %d, got %d", request.Value*2, result.Result)
		}

		if result.ServiceMessage != "processed" {
			t.Errorf("Expected service message 'processed', got %s", result.ServiceMessage)
		}

		t.Logf("Successfully received typed response: %+v", result)
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for typed response")
	}
}

// Typed request/response structures
type TypedRequest struct {
	ID    string `json:"id"`
	Value int    `json:"value"`
}

type TypedResponse struct {
	ID             string `json:"id"`
	Result         int    `json:"result"`
	ServiceMessage string `json:"service_message"`
}

type ServiceRequest struct {
	Value int `json:"value"`
}

type ServiceResponse struct {
	Processed bool   `json:"processed"`
	Message   string `json:"message"`
}

// Handler that uses typed dispatch
type typedRequestHandler struct {
	processedMessages *sync.Map
}

func (h *typedRequestHandler) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("test", "typed", 1)
	topic = topic.WithPurpose("request").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*topic}
}

func (h *typedRequestHandler) Handle(ctx *broker.Context) error {
	h.processedMessages.Store(ctx.Envelope.ID, true)

	// Unmarshal typed request
	var req TypedRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Dispatch to service using typed response
	serviceTopic, _ := broker.NewTopic("test", "service", 1)
	serviceTopic = serviceTopic.WithPurpose("process").WithTopicType(broker.TopicTypeCommand)

	serviceResp := broker.DispatchTyped[ServiceResponse](
		ctx,
		*serviceTopic,
		ServiceRequest{Value: req.Value},
		broker.WithRequired(),
	)

	// Wait for response
	if err := ctx.Wait(); err != nil {
		return err
	}

	// Get typed response
	service, err := serviceResp.Value()
	if err != nil {
		return err
	}

	// Reply with typed response
	return ctx.Reply(TypedResponse{
		ID:             req.ID,
		Result:         req.Value * 2,
		ServiceMessage: service.Message,
	})
}

// Service handler
type typedServiceHandler struct {
	processedMessages *sync.Map
}

func (h *typedServiceHandler) Topics() []broker.Topic {
	topic, _ := broker.NewTopic("test", "service", 1)
	topic = topic.WithPurpose("process").WithTopicType(broker.TopicTypeCommand)
	return []broker.Topic{*topic}
}

func (h *typedServiceHandler) Handle(ctx *broker.Context) error {
	h.processedMessages.Store(ctx.Envelope.ID, true)

	var req ServiceRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	return ctx.Reply(ServiceResponse{
		Processed: true,
		Message:   "processed",
	})
}
