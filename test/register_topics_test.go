package test

import (
	"context"
	"testing"

	"github.com/joshua-temple/nexus"
)

// Mock types for testing topic registration

type mockTopicPublisher struct {
	published []*nexus.Envelope
}

func (m *mockTopicPublisher) Publish(ctx context.Context, envelope *nexus.Envelope) error {
	m.published = append(m.published, envelope)
	return nil
}

type topicTestHandler struct {
	topics  []nexus.Topic
	handled []*nexus.Envelope
}

func (h *topicTestHandler) Topics() []nexus.Topic {
	return h.topics
}

func (h *topicTestHandler) Handle(ctx *nexus.Context) error {
	h.handled = append(h.handled, ctx.Envelope)
	return nil
}

// TestRegisterTopics tests dynamic topic registration
func TestRegisterTopics(t *testing.T) {
	pub := &mockTopicPublisher{}
	sub := &mockSubscriberWithTopics{
		messages: make(chan *nexus.Envelope),
		topics:   make(map[string]bool),
	}
	logger := &nexus.NoOpLogger{}

	b := nexus.New(2, pub, sub, logger)

	// Start broker
	ctx := context.Background()
	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}
	defer b.Stop()

	// Create initial topic and handler
	topic1, _ := nexus.NewTopic("test", "topic1", 1)
	topic1 = topic1.WithPurpose("test").WithTopicType(nexus.TopicTypeCommand)

	handler1 := &topicTestHandler{
		topics: []nexus.Topic{*topic1},
	}

	// Register first handler - should register topic1
	if err := b.Register(handler1); err != nil {
		t.Fatalf("Failed to register handler1: %v", err)
	}

	// Verify topic1 was registered
	if !sub.hasTopics(topic1.String()) {
		t.Errorf("Expected topic1 to be registered")
	}

	// Create second topic and handler
	topic2, _ := nexus.NewTopic("test", "topic2", 1)
	topic2 = topic2.WithPurpose("test").WithTopicType(nexus.TopicTypeCommand)

	handler2 := &topicTestHandler{
		topics: []nexus.Topic{*topic2},
	}

	// Register second handler - should register topic2
	if err := b.Register(handler2); err != nil {
		t.Fatalf("Failed to register handler2: %v", err)
	}

	// Verify topic2 was registered
	if !sub.hasTopics(topic2.String()) {
		t.Errorf("Expected topic2 to be registered")
	}

	// Verify both topics are registered
	if !sub.hasTopics(topic1.String(), topic2.String()) {
		t.Errorf("Expected both topics to be registered")
	}
}

// TestRegisterTopicsBeforeStart tests that topics are not registered before broker starts
func TestRegisterTopicsBeforeStart(t *testing.T) {
	pub := &mockTopicPublisher{}
	sub := &mockSubscriberWithTopics{
		messages: make(chan *nexus.Envelope),
		topics:   make(map[string]bool),
	}
	logger := &nexus.NoOpLogger{}

	b := nexus.New(2, pub, sub, logger)

	// Create topic and handler
	topic, _ := nexus.NewTopic("test", "topic", 1)
	topic = topic.WithPurpose("test").WithTopicType(nexus.TopicTypeCommand)

	handler := &topicTestHandler{
		topics: []nexus.Topic{*topic},
	}

	// Register handler before starting broker
	if err := b.Register(handler); err != nil {
		t.Fatalf("Failed to register handler: %v", err)
	}

	// Verify no topics were registered (broker not running)
	if len(sub.topics) > 0 {
		t.Errorf("Expected no topics to be registered before broker starts")
	}
}

// mockSubscriberWithTopics tracks registered topics
type mockSubscriberWithTopics struct {
	messages chan *nexus.Envelope
	topics   map[string]bool
}

func (m *mockSubscriberWithTopics) RegisterHandlers(ctx context.Context, handlers ...nexus.Handler) error {
	return nil
}

func (m *mockSubscriberWithTopics) RegisterTopics(ctx context.Context, topics ...string) error {
	for _, topic := range topics {
		m.topics[topic] = true
	}
	return nil
}

func (m *mockSubscriberWithTopics) Start(ctx context.Context) (<-chan *nexus.Envelope, error) {
	return m.messages, nil
}

func (m *mockSubscriberWithTopics) Stop() {
	close(m.messages)
}

func (m *mockSubscriberWithTopics) hasTopics(topics ...string) bool {
	for _, topic := range topics {
		if !m.topics[topic] {
			return false
		}
	}
	return true
}
