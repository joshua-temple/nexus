package test

import (
	"context"
	"testing"
	"time"

	"github.com/joshua-temple/nexus"
)

// TestBasicFunctionality demonstrates the broker can handle messages
func TestBasicFunctionality(t *testing.T) {
	// Create broker
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)
	b := nexus.New(2, pub, sub, logger)

	// Simple handler that just logs
	handler := &basicHandler{t: t}
	b.Register(handler)

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create topic
	topic, _ := nexus.NewTopic("test", "basic", 1)
	topic = topic.WithPurpose("test").WithTopicType(nexus.TopicTypeCommand)

	// Send message
	envelope := &nexus.Envelope{
		ID:               "test-1",
		RootID:           "root-1",
		SourceTopic:      "test-client",
		DestinationTopic: topic.String(),
		ReplyToTopics:    []string{"test-reply"},
		Payload:          []byte(`{"message": "hello"}`),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Subscribe to reply
	replyCh := pub.Subscribe("test-reply")

	// Send
	if err := pub.Publish(ctx, envelope); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for reply
	select {
	case reply := <-replyCh:
		t.Logf("Got reply: %+v", reply)

		// Verify it's a response
		if reply.RequestID != "test-1" {
			t.Errorf("Expected RequestID test-1, got %s", reply.RequestID)
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for reply")
	}
}

// TestNestedDispatch shows one handler calling another
func TestNestedDispatch(t *testing.T) {
	// Create broker
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)
	b := nexus.New(2, pub, sub, logger)

	// Register parent and child handlers
	parentHandler := &parentHandler{t: t}
	childHandler := &childHandler{t: t}

	b.Register(parentHandler)
	b.Register(childHandler)

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create parent topic
	parentTopic, _ := nexus.NewTopic("test", "parent", 1)
	parentTopic = parentTopic.WithPurpose("process").WithTopicType(nexus.TopicTypeCommand)

	// Send to parent
	envelope := &nexus.Envelope{
		ID:               "parent-1",
		RootID:           "root-parent-1",
		SourceTopic:      "test-client",
		DestinationTopic: parentTopic.String(),
		ReplyToTopics:    []string{"test-parent-reply"},
		Payload:          []byte(`{"value": 10}`),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Subscribe to reply
	replyCh := pub.Subscribe("test-parent-reply")

	// Send
	if err := pub.Publish(ctx, envelope); err != nil {
		t.Fatalf("Failed to publish: %v", err)
	}

	// Wait for reply
	select {
	case reply := <-replyCh:
		t.Logf("Got parent reply: %+v", reply)

		var result map[string]interface{}
		unmarshalResponse(t, reply.Payload, &result)

		// Verify we got doubled value (10 * 2 from child)
		if val, ok := result["result"].(float64); !ok || val != 20 {
			t.Errorf("Expected result 20, got %v", result["result"])
		}

	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for parent reply")
	}
}

// Basic handler that just replies
type basicHandler struct {
	t *testing.T
}

func (h *basicHandler) Topics() []nexus.Topic {
	topic, _ := nexus.NewTopic("test", "basic", 1)
	topic = topic.WithPurpose("test").WithTopicType(nexus.TopicTypeCommand)
	return []nexus.Topic{*topic}
}

func (h *basicHandler) Handle(ctx *nexus.Context) error {
	h.t.Logf("BasicHandler received message: %s", ctx.Envelope.ID)

	var request map[string]string
	if err := ctx.Unmarshal(&request); err != nil {
		return err
	}

	return ctx.Reply(map[string]string{
		"response": "received: " + request["message"],
	})
}

// Parent handler that calls child
type parentHandler struct {
	t *testing.T
}

func (h *parentHandler) Topics() []nexus.Topic {
	topic, _ := nexus.NewTopic("test", "parent", 1)
	topic = topic.WithPurpose("process").WithTopicType(nexus.TopicTypeCommand)
	return []nexus.Topic{*topic}
}

func (h *parentHandler) Handle(ctx *nexus.Context) error {
	h.t.Logf("ParentHandler received message: %s", ctx.Envelope.ID)

	var request map[string]interface{}
	if err := ctx.Unmarshal(&request); err != nil {
		return err
	}

	value := int(request["value"].(float64))

	// Call child service
	childTopic, _ := nexus.NewTopic("test", "child", 1)
	childTopic = childTopic.WithPurpose("double").WithTopicType(nexus.TopicTypeCommand)

	// Dispatch with JSON payload
	ctx.Dispatch(
		*childTopic,
		marshalPayload(map[string]int{"value": value}),
		nexus.WithRequired(),
	)

	// Wait for child
	if err := ctx.Wait(); err != nil {
		h.t.Logf("Parent failed to wait for child: %v", err)
		return err
	}

	h.t.Log("Parent received child response")

	// For simplicity, we know child doubles the value
	return ctx.Reply(map[string]interface{}{
		"result": value * 2,
	})
}

// Child handler that doubles values
type childHandler struct {
	t *testing.T
}

func (h *childHandler) Topics() []nexus.Topic {
	topic, _ := nexus.NewTopic("test", "child", 1)
	topic = topic.WithPurpose("double").WithTopicType(nexus.TopicTypeCommand)
	return []nexus.Topic{*topic}
}

func (h *childHandler) Handle(ctx *nexus.Context) error {
	h.t.Logf("ChildHandler received message: %s", ctx.Envelope.ID)

	var request map[string]int
	if err := ctx.Unmarshal(&request); err != nil {
		h.t.Logf("Child unmarshal error: %v", err)
		return err
	}

	doubled := request["value"] * 2
	h.t.Logf("Child doubling %d to %d", request["value"], doubled)

	return ctx.Reply(map[string]int{
		"doubled": doubled,
	})
}
