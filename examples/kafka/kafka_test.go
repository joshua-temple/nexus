package kafka

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	broker "github.com/joshua-temple/nexus"
)

func TestNewPublisher(t *testing.T) {
	brokers := []string{"localhost:9092"}
	pub, err := NewPublisher(brokers)

	require.NoError(t, err)
	assert.NotNil(t, pub)
	assert.NotNil(t, pub.client)

	// Close should not panic
	assert.NoError(t, pub.Close())
}

func TestNewSubscriber(t *testing.T) {
	brokers := []string{"localhost:9092"}
	topics := []string{"test.topic"}
	consumerGroup := "test-group"

	sub, err := NewSubscriber(brokers, topics, consumerGroup, nil)

	require.NoError(t, err)
	assert.NotNil(t, sub)
	assert.NotNil(t, sub.client)
	assert.Equal(t, topics, sub.topics)
	assert.Equal(t, consumerGroup, sub.consumerGroup)
	assert.NotNil(t, sub.logger) // Should have default logger
}

func TestNewSubscriberWithLogger(t *testing.T) {
	brokers := []string{"localhost:9092"}
	topics := []string{"test.topic"}
	consumerGroup := "test-group"
	logger := &mockTestLogger{}

	sub, err := NewSubscriber(brokers, topics, consumerGroup, logger)

	require.NoError(t, err)
	assert.NotNil(t, sub)
	assert.Equal(t, logger, sub.logger)
}

func TestSubscriberStop(t *testing.T) {
	sub := &Subscriber{
		stopChan: make(chan struct{}),
	}

	// Stop should not panic even when not started
	sub.Stop()

	// Verify channel is closed
	select {
	case <-sub.stopChan:
		// Good, channel is closed
	default:
		t.Error("stopChan should be closed")
	}
}

func TestEnvelopeJSONSerialization(t *testing.T) {
	// This test verifies that our envelope serialization works correctly
	envelope := &broker.Envelope{
		ID:               "test-1",
		RootID:           "root-1",
		SourceTopic:      "source",
		DestinationTopic: "dest",
		Payload: map[string]interface{}{
			"key": "value",
			"num": 42,
		},
		CreatedAt: time.Now().UnixNano(),
	}

	// Simulate what Publisher.Publish does
	data, err := json.Marshal(envelope)
	require.NoError(t, err)

	// Simulate what Subscriber does
	var decoded broker.Envelope
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, envelope.ID, decoded.ID)
	assert.Equal(t, envelope.RootID, decoded.RootID)
	assert.Equal(t, envelope.SourceTopic, decoded.SourceTopic)
	assert.Equal(t, envelope.DestinationTopic, decoded.DestinationTopic)
}

func TestPublisherCompatWrapper(t *testing.T) {
	brokers := []string{"localhost:9092"}
	pub := NewPublisherCompat(brokers)

	assert.NotNil(t, pub)
	assert.Equal(t, brokers, pub.brokers)

	// Multiple closes should not panic
	assert.NoError(t, pub.Close())
	assert.NoError(t, pub.Close())
}

func TestSubscriberCompatWrapper(t *testing.T) {
	brokers := []string{"localhost:9092"}
	topics := []string{"test.topic"}
	consumerGroup := "test-group"
	logger := &mockTestLogger{}

	sub := NewSubscriberCompat(brokers, topics, consumerGroup, logger)

	assert.NotNil(t, sub)
	assert.Equal(t, brokers, sub.brokers)
	assert.Equal(t, topics, sub.topics)
	assert.Equal(t, consumerGroup, sub.consumerGroup)
	assert.Equal(t, logger, sub.logger)
}

func TestSubscriberStartContext(t *testing.T) {
	brokers := []string{"localhost:9092"}
	topics := []string{"test.topic"}
	sub, err := NewSubscriber(brokers, topics, "test-group", nil)
	require.NoError(t, err)

	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Start should handle canceled context gracefully
	msgChan, err := sub.Start(ctx)
	assert.NoError(t, err)
	assert.NotNil(t, msgChan)

	// Stop should work
	sub.Stop()
}

// Mock logger for testing
type mockTestLogger struct {
	debugCount int
	infoCount  int
	errorCount int
	warnCount  int
	fatalCount int
}

func (l *mockTestLogger) Debugf(_ context.Context, format string, args ...any) { l.debugCount++ }
func (l *mockTestLogger) Infof(_ context.Context, format string, args ...any)  { l.infoCount++ }
func (l *mockTestLogger) Errorf(_ context.Context, format string, args ...any) { l.errorCount++ }
func (l *mockTestLogger) Warnf(_ context.Context, format string, args ...any)  { l.warnCount++ }
func (l *mockTestLogger) Fatalf(_ context.Context, format string, args ...any) { l.fatalCount++ }
