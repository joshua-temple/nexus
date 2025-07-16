package memory

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	broker "github.com/joshua-temple/nexus"
)

func TestMemoryPubSub(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := NewHub()
	pub := NewPublisher(hub)
	sub := NewSubscriber(hub, []string{"test.topic"}, "test-group", nil)

	// Start subscriber
	msgChan, err := sub.Start(ctx)
	require.NoError(t, err)
	defer sub.Stop()

	// Publish a message
	envelope := &broker.Envelope{
		ID:               "test-1",
		RootID:           "root-1",
		DestinationTopic: "test.topic",
		Payload:          map[string]string{"message": "hello"},
		CreatedAt:        time.Now().UnixNano(),
	}

	err = pub.Publish(ctx, envelope)
	require.NoError(t, err)

	// Receive message
	select {
	case received := <-msgChan:
		assert.Equal(t, envelope.ID, received.ID)
		assert.Equal(t, envelope.RootID, received.RootID)
		assert.Equal(t, envelope.Payload, received.Payload)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for message")
	}
}

func TestMemoryMultipleSubscribers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := NewHub()
	pub := NewPublisher(hub)

	// Create multiple subscribers to the same topic
	sub1 := NewSubscriber(hub, []string{"test.topic"}, "group1", nil)
	sub2 := NewSubscriber(hub, []string{"test.topic"}, "group2", nil)

	msgChan1, err := sub1.Start(ctx)
	require.NoError(t, err)
	defer sub1.Stop()

	msgChan2, err := sub2.Start(ctx)
	require.NoError(t, err)
	defer sub2.Stop()

	// Publish a message
	envelope := &broker.Envelope{
		ID:               "test-1",
		DestinationTopic: "test.topic",
		Payload:          "broadcast message",
	}

	err = pub.Publish(ctx, envelope)
	require.NoError(t, err)

	// Both subscribers should receive the message
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		select {
		case msg := <-msgChan1:
			assert.Equal(t, envelope.ID, msg.ID)
		case <-ctx.Done():
			t.Error("Subscriber 1 timeout")
		}
	}()

	go func() {
		defer wg.Done()
		select {
		case msg := <-msgChan2:
			assert.Equal(t, envelope.ID, msg.ID)
		case <-ctx.Done():
			t.Error("Subscriber 2 timeout")
		}
	}()

	wg.Wait()
}

func TestMemoryDynamicTopicRegistration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := NewHub()
	hub.SetStoreLimit(0) // Disable message replay for this test
	pub := NewPublisher(hub)
	sub := NewSubscriber(hub, []string{"topic1"}, "test-group", nil)

	// Start subscriber
	msgChan, err := sub.Start(ctx)
	require.NoError(t, err)
	defer sub.Stop()

	// Publish to topic1
	envelope1 := &broker.Envelope{
		ID:               "msg-1",
		DestinationTopic: "topic1",
		Payload:          "message 1",
	}
	err = pub.Publish(ctx, envelope1)
	require.NoError(t, err)

	// Receive first message
	select {
	case msg := <-msgChan:
		assert.Equal(t, "msg-1", msg.ID)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for message 1")
	}

	// Register new topic dynamically
	err = sub.RegisterTopics(ctx, "topic2")
	require.NoError(t, err)

	// Small delay to ensure registration is complete
	time.Sleep(100 * time.Millisecond)

	// Publish to new topic
	envelope2 := &broker.Envelope{
		ID:               "msg-2",
		DestinationTopic: "topic2",
		Payload:          "message 2",
	}
	err = pub.Publish(ctx, envelope2)
	require.NoError(t, err)

	// Should receive message from new topic
	select {
	case msg := <-msgChan:
		assert.Equal(t, "msg-2", msg.ID)
		assert.Equal(t, "topic2", msg.DestinationTopic)
	case <-ctx.Done():
		t.Fatal("Timeout waiting for message 2")
	}
}

func TestMemoryMessageStore(t *testing.T) {
	hub := NewHub()
	hub.SetStoreLimit(5) // Store only last 5 messages

	pub := NewPublisher(hub)
	ctx := context.Background()

	// Publish 10 messages
	for i := 0; i < 10; i++ {
		envelope := &broker.Envelope{
			ID:               fmt.Sprintf("msg-%d", i),
			DestinationTopic: "test.store",
			Payload:          i,
		}
		err := pub.Publish(ctx, envelope)
		require.NoError(t, err)
	}

	// Check stored messages
	stored := hub.GetStoredMessages("test.store")
	assert.Len(t, stored, 5) // Should only have last 5

	// Verify they are the last 5 messages
	for i := 0; i < 5; i++ {
		expectedID := fmt.Sprintf("msg-%d", i+5)
		assert.Equal(t, expectedID, stored[i].ID)
	}
}

func TestMemoryMessageReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hub := NewHub()
	pub := NewPublisher(hub)

	// Publish messages before subscriber starts
	for i := 0; i < 3; i++ {
		envelope := &broker.Envelope{
			ID:               fmt.Sprintf("pre-msg-%d", i),
			DestinationTopic: "test.replay",
			Payload:          i,
		}
		err := pub.Publish(ctx, envelope)
		require.NoError(t, err)
	}

	// Now start subscriber
	sub := NewSubscriber(hub, []string{"test.replay"}, "replay-group", nil)
	msgChan, err := sub.Start(ctx)
	require.NoError(t, err)
	defer sub.Stop()

	// Should receive replayed messages
	receivedCount := 0
	timeout := time.After(2 * time.Second)

	for receivedCount < 3 {
		select {
		case msg := <-msgChan:
			expectedID := fmt.Sprintf("pre-msg-%d", receivedCount)
			assert.Equal(t, expectedID, msg.ID)
			receivedCount++
		case <-timeout:
			t.Fatalf("Timeout: received only %d/3 messages", receivedCount)
		}
	}
}

func TestMemoryBrokerIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Create test topics
	pingTopic, _ := broker.NewTopic("test", "ping", 1)
	pongTopic, _ := broker.NewTopic("test", "pong", 1)

	// Create broker with memory transport - use the actual topic strings
	b, hub := NewMemoryBroker([]string{pingTopic.String(), pongTopic.String()}, "test-group", nil)

	// Create test handlers
	pingHandler := &testPingHandler{
		pongTopic: *pongTopic,
	}
	pongHandler := &testPongHandler{}

	// Register handlers
	b.Register(pingHandler)
	b.Register(pongHandler)

	// Start broker
	err := b.Start(ctx)
	require.NoError(t, err)
	defer b.Stop()

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create a separate publisher to send request
	pub := NewPublisher(hub)

	// Create response subscriber
	responseSub := NewSubscriber(hub, []string{"client.response"}, "client-group", nil)
	respChan, err := responseSub.Start(ctx)
	require.NoError(t, err)
	defer responseSub.Stop()

	// Give subscriber time to start
	time.Sleep(100 * time.Millisecond)

	// Send ping request
	request := map[string]string{"message": "hello"}
	envelope := &broker.Envelope{
		ID:               "req-1",
		RootID:           "root-1",
		SourceTopic:      "client",
		DestinationTopic: pingTopic.String(),
		ReplyToTopics:    []string{"client.response"},
		Payload:          request,
		CreatedAt:        time.Now().UnixNano(),
	}

	err = pub.Publish(ctx, envelope)
	require.NoError(t, err)

	// Wait for response
	select {
	case resp := <-respChan:
		result, ok := resp.Payload.(map[string]string)
		require.True(t, ok)
		assert.Equal(t, "hello", result["original"])
		assert.Equal(t, "pong", result["reply"])
	case <-ctx.Done():
		t.Fatal("Timeout waiting for response")
	}
}

func TestMemoryValidation(t *testing.T) {
	ctx := context.Background()
	hub := NewHub()
	pub := NewPublisher(hub)

	// Test nil envelope
	err := pub.Publish(ctx, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "envelope cannot be nil")

	// Test empty destination topic
	err = pub.Publish(ctx, &broker.Envelope{
		ID: "test",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "destination topic cannot be empty")
}

func TestMemoryContextCancellation(t *testing.T) {
	hub := NewHub()
	pub := NewPublisher(hub)

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Publishing should fail with context error
	err := pub.Publish(ctx, &broker.Envelope{
		ID:               "test",
		DestinationTopic: "test.topic",
	})
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// Test handlers for broker integration test
type testPingHandler struct {
	pongTopic broker.Topic
}

func (h *testPingHandler) Topics() []broker.Topic {
	pingTopic, _ := broker.NewTopic("test", "ping", 1)
	return []broker.Topic{*pingTopic}
}

func (h *testPingHandler) Handle(ctx *broker.Context) error {
	var request map[string]string
	if err := ctx.Unmarshal(&request); err != nil {
		return err
	}

	// Dispatch to pong
	pongResp := broker.DispatchTyped[map[string]string](
		ctx,
		h.pongTopic,
		map[string]string{"ping": request["message"]},
		broker.WithRequired(),
	)

	if err := ctx.Wait(); err != nil {
		return err
	}

	pong, err := pongResp.Value()
	if err != nil {
		return err
	}

	return ctx.Reply(map[string]string{
		"original": request["message"],
		"reply":    pong["pong"],
	})
}

type testPongHandler struct{}

func (h *testPongHandler) Topics() []broker.Topic {
	pongTopic, _ := broker.NewTopic("test", "pong", 1)
	return []broker.Topic{*pongTopic}
}

func (h *testPongHandler) Handle(ctx *broker.Context) error {
	return ctx.Reply(map[string]string{
		"pong": "pong",
	})
}
