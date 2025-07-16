//go:build integration
// +build integration

package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	broker "github.com/joshua-temple/nexus"
)

var (
	redpandaBrokers []string
	pool            *dockertest.Pool
	resource        *dockertest.Resource
)

func TestMain(m *testing.M) {
	// Check if we should skip integration tests
	if os.Getenv("SKIP_INTEGRATION") == "true" {
		fmt.Println("Skipping integration tests (SKIP_INTEGRATION=true)")
		os.Exit(0)
	}

	// Setup RedPanda using dockertest
	var err error
	pool, err = dockertest.NewPool("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Could not connect to Docker: %s\n", err)
		fmt.Fprintf(os.Stderr, "\nIntegration tests require Docker to be installed and running.\n")
		fmt.Fprintf(os.Stderr, "You can skip integration tests by setting SKIP_INTEGRATION=true\n")
		os.Exit(1)
	}

	// Uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Docker is not running: %s\n", err)
		fmt.Fprintf(os.Stderr, "\nPlease start Docker to run integration tests.\n")
		fmt.Fprintf(os.Stderr, "You can skip integration tests by setting SKIP_INTEGRATION=true\n")
		os.Exit(1)
	}

	// Start RedPanda container
	resource, err = pool.RunWithOptions(&dockertest.RunOptions{
		Repository: "redpandadata/redpanda",
		Tag:        "latest",
		Cmd: []string{
			"redpanda", "start",
			"--smp", "1",
			"--overprovisioned",
			"--kafka-addr", "internal://0.0.0.0:9092,external://0.0.0.0:19092",
			"--advertise-kafka-addr", "internal://redpanda:9092,external://localhost:19092",
		},
		ExposedPorts: []string{"19092/tcp"},
		PortBindings: map[docker.Port][]docker.PortBinding{
			"19092/tcp": {{HostIP: "localhost", HostPort: "19092"}},
		},
	}, func(config *docker.HostConfig) {
		config.AutoRemove = true
		config.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})

	if err != nil {
		panic(fmt.Sprintf("Could not start resource: %s", err))
	}

	// Get the port
	port := resource.GetPort("19092/tcp")
	redpandaBrokers = []string{fmt.Sprintf("localhost:%s", port)}

	// Exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		conn, err := kafka.Dial("tcp", redpandaBrokers[0])
		if err != nil {
			return err
		}
		defer conn.Close()

		// Test if we can get the controller
		_, err = conn.Controller()
		return err
	}); err != nil {
		panic(fmt.Sprintf("Could not connect to RedPanda: %s", err))
	}

	// Run tests
	m.Run()

	// Clean up
	if err := pool.Purge(resource); err != nil {
		panic(fmt.Sprintf("Could not purge resource: %s", err))
	}

	// Exit with test code
	pool.RemoveContainerByName("redpanda-broker")
	pool.RemoveContainerByName("redpanda-console")
}

func TestRedPandaBasicPubSub(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create unique topic for this test to avoid conflicts
	testTopic := fmt.Sprintf("test.basic.topic.%d", time.Now().UnixNano())

	// Create publisher and subscriber
	pub, err := NewPublisher(redpandaBrokers)
	require.NoError(t, err)
	defer pub.Close()

	sub, err := NewSubscriber(redpandaBrokers, []string{testTopic}, "test-group", nil)
	require.NoError(t, err)

	// Start subscriber
	msgChan, err := sub.Start(ctx)
	require.NoError(t, err)
	defer sub.Stop()

	// Give subscriber time to connect and start consuming
	time.Sleep(3 * time.Second)

	// Publish a test message
	testEnvelope := &broker.Envelope{
		ID:               "test-1",
		RootID:           "root-1",
		SourceTopic:      "test-source",
		DestinationTopic: testTopic,
		Payload:          map[string]string{"message": "hello redpanda"},
		CreatedAt:        time.Now().UnixNano(),
	}

	err = pub.Publish(ctx, testEnvelope)
	require.NoError(t, err)

	// Wait for message
	select {
	case received := <-msgChan:
		assert.Equal(t, testEnvelope.ID, received.ID)
		assert.Equal(t, testEnvelope.RootID, received.RootID)

		// Check payload
		payload, ok := received.Payload.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "hello redpanda", payload["message"])
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for message")
	}
}

func TestRedPandaMultipleTopics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create publisher
	pub, err := NewPublisher(redpandaBrokers)
	require.NoError(t, err)
	defer pub.Close()

	// Subscribe to multiple topics
	topics := []string{"test.topic1", "test.topic2", "test.topic3"}
	sub, err := NewSubscriber(redpandaBrokers, topics, "test-multi-group", nil)
	require.NoError(t, err)

	msgChan, err := sub.Start(ctx)
	require.NoError(t, err)
	defer sub.Stop()

	// Give subscriber time to connect
	time.Sleep(2 * time.Second)

	// Publish to each topic
	receivedCount := 0
	expectedMessages := make(map[string]bool)

	for i, topic := range topics {
		envelope := &broker.Envelope{
			ID:               fmt.Sprintf("msg-%d", i),
			RootID:           "root-multi",
			SourceTopic:      "test-source",
			DestinationTopic: topic,
			Payload:          map[string]string{"topic": topic},
			CreatedAt:        time.Now().UnixNano(),
		}
		expectedMessages[envelope.ID] = false

		err = pub.Publish(ctx, envelope)
		require.NoError(t, err)
	}

	// Collect messages
	timeout := time.After(10 * time.Second)
	for receivedCount < len(topics) {
		select {
		case msg := <-msgChan:
			if _, exists := expectedMessages[msg.ID]; exists {
				expectedMessages[msg.ID] = true
				receivedCount++
			}
		case <-timeout:
			t.Fatalf("Timeout: received %d/%d messages", receivedCount, len(topics))
		}
	}

	// Verify all messages received
	for id, received := range expectedMessages {
		assert.True(t, received, "Message %s not received", id)
	}
}

func TestRedPandaHighVolume(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Create publisher and subscriber
	pub, err := NewPublisher(redpandaBrokers)
	require.NoError(t, err)
	defer pub.Close()

	sub, err := NewSubscriber(redpandaBrokers, []string{"test.highvolume"}, "test-hv-group", nil)
	require.NoError(t, err)

	msgChan, err := sub.Start(ctx)
	require.NoError(t, err)
	defer sub.Stop()

	// Give subscriber time to connect
	time.Sleep(2 * time.Second)

	// Configuration for high volume test
	const (
		numProducers        = 10
		messagesPerProducer = 100
		totalMessages       = numProducers * messagesPerProducer
	)

	// Track sent and received messages
	var sentCount atomic.Int64
	var receivedCount atomic.Int64
	receivedMessages := &sync.Map{}

	// Start consumer goroutine
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case msg := <-msgChan:
				receivedMessages.Store(msg.ID, true)
				count := receivedCount.Add(1)
				if count >= totalMessages {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Start producer goroutines
	var wg sync.WaitGroup
	start := time.Now()

	for p := 0; p < numProducers; p++ {
		wg.Add(1)
		go func(producerID int) {
			defer wg.Done()

			for i := 0; i < messagesPerProducer; i++ {
				envelope := &broker.Envelope{
					ID:               fmt.Sprintf("hv-%d-%d", producerID, i),
					RootID:           fmt.Sprintf("root-hv-%d", producerID),
					SourceTopic:      fmt.Sprintf("producer-%d", producerID),
					DestinationTopic: "test.highvolume",
					Payload: map[string]interface{}{
						"producer":  producerID,
						"sequence":  i,
						"timestamp": time.Now().UnixNano(),
					},
					CreatedAt: time.Now().UnixNano(),
				}

				if err := pub.Publish(ctx, envelope); err != nil {
					t.Errorf("Failed to publish message: %v", err)
					return
				}
				sentCount.Add(1)
			}
		}(p)
	}

	// Wait for all producers to finish
	wg.Wait()
	publishDuration := time.Since(start)

	// Wait for all messages to be consumed or timeout
	select {
	case <-done:
		consumeDuration := time.Since(start)
		t.Logf("High volume test completed:")
		t.Logf("  - Published %d messages in %v (%.2f msgs/sec)",
			sentCount.Load(), publishDuration, float64(totalMessages)/publishDuration.Seconds())
		t.Logf("  - Consumed %d messages in %v (%.2f msgs/sec)",
			receivedCount.Load(), consumeDuration, float64(totalMessages)/consumeDuration.Seconds())
	case <-ctx.Done():
		t.Fatalf("Timeout: sent %d, received %d out of %d messages",
			sentCount.Load(), receivedCount.Load(), totalMessages)
	}

	// Verify all messages were received
	assert.Equal(t, int64(totalMessages), receivedCount.Load())
}

func TestRedPandaConsumerGroups(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create publisher
	pub, err := NewPublisher(redpandaBrokers)
	require.NoError(t, err)
	defer pub.Close()

	// Create two subscribers in the same consumer group
	sub1, err := NewSubscriber(redpandaBrokers, []string{"test.consumergroup"}, "test-cg", nil)
	require.NoError(t, err)
	sub2, err := NewSubscriber(redpandaBrokers, []string{"test.consumergroup"}, "test-cg", nil)
	require.NoError(t, err)

	msgChan1, err := sub1.Start(ctx)
	require.NoError(t, err)
	defer sub1.Stop()

	msgChan2, err := sub2.Start(ctx)
	require.NoError(t, err)
	defer sub2.Stop()

	// Give subscribers time to connect and balance
	time.Sleep(3 * time.Second)

	// Send messages
	const numMessages = 20
	messagesReceived := make(map[string]int)
	var mu sync.Mutex

	// Start consumers
	var wg sync.WaitGroup
	wg.Add(2)

	consumeMessages := func(id int, msgChan <-chan *broker.Envelope) {
		defer wg.Done()
		for {
			select {
			case msg := <-msgChan:
				mu.Lock()
				messagesReceived[msg.ID]++
				mu.Unlock()

				if len(messagesReceived) >= numMessages {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}

	go consumeMessages(1, msgChan1)
	go consumeMessages(2, msgChan2)

	// Publish messages
	for i := 0; i < numMessages; i++ {
		envelope := &broker.Envelope{
			ID:               fmt.Sprintf("cg-msg-%d", i),
			RootID:           "root-cg",
			SourceTopic:      "test-source",
			DestinationTopic: "test.consumergroup",
			Payload:          map[string]int{"sequence": i},
			CreatedAt:        time.Now().UnixNano(),
		}

		err = pub.Publish(ctx, envelope)
		require.NoError(t, err)
	}

	// Wait for consumers to finish
	wg.Wait()

	// Verify each message was received exactly once
	mu.Lock()
	defer mu.Unlock()

	assert.Len(t, messagesReceived, numMessages)
	for msgID, count := range messagesReceived {
		assert.Equal(t, 1, count, "Message %s received %d times", msgID, count)
	}
}

func TestRedPandaBrokerIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create RedPanda publisher and subscriber
	pub, err := NewPublisher(redpandaBrokers)
	require.NoError(t, err)
	defer pub.Close()

	topics := []string{
		"test.ping.request.cmd.v1",
		"test.pong.response.cmd.v1",
	}

	sub, err := NewSubscriber(redpandaBrokers, topics, "test-broker-group", nil)
	require.NoError(t, err)

	// Create broker
	logger := &testLogger{t: t}
	b := broker.New(2, pub, sub, logger)

	// Create test topics
	pingTopic, _ := broker.NewTopic("test", "ping", 1)
	pingTopic = pingTopic.WithPurpose("request").WithTopicType(broker.TopicTypeCommand)

	pongTopic, _ := broker.NewTopic("test", "pong", 1)
	pongTopic = pongTopic.WithPurpose("response").WithTopicType(broker.TopicTypeCommand)

	// Register handlers
	b.Register(&pingHandler{topic: *pingTopic, pongTopic: *pongTopic})
	b.Register(&pongHandler{topic: *pongTopic})

	// Start broker
	err = b.Start(ctx)
	require.NoError(t, err)
	defer b.Stop()

	// Give broker time to start
	time.Sleep(2 * time.Second)

	// Send ping request
	request := map[string]string{"message": "hello"}
	requestData, _ := json.Marshal(request)

	envelope := &broker.Envelope{
		ID:               "test-broker-1",
		RootID:           "root-broker-1",
		SourceTopic:      "client",
		DestinationTopic: pingTopic.String(),
		ReplyToTopics:    []string{"client-reply"},
		Payload:          requestData,
		CreatedAt:        time.Now().UnixNano(),
	}

	// Track response
	responseChan := make(chan *broker.Envelope, 1)

	// Subscribe to client-reply topic
	clientSub, err := NewSubscriber(redpandaBrokers, []string{"client-reply"}, "client-group", nil)
	require.NoError(t, err)
	clientMsgChan, err := clientSub.Start(ctx)
	require.NoError(t, err)
	defer clientSub.Stop()

	// Collect response
	go func() {
		select {
		case msg := <-clientMsgChan:
			responseChan <- msg
		case <-ctx.Done():
		}
	}()

	// Give client subscriber time to connect
	time.Sleep(2 * time.Second)

	// Send the request
	err = pub.Publish(ctx, envelope)
	require.NoError(t, err)

	// Wait for response
	select {
	case response := <-responseChan:
		// Verify response
		var result map[string]string
		if data, ok := response.Payload.([]byte); ok {
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)
		} else if data, ok := response.Payload.(map[string]interface{}); ok {
			// Convert map[string]interface{} to map[string]string
			result = make(map[string]string)
			for k, v := range data {
				result[k] = fmt.Sprintf("%v", v)
			}
		}

		assert.Equal(t, "hello", result["original"])
		assert.Equal(t, "pong", result["reply"])
	case <-ctx.Done():
		t.Fatal("Timeout waiting for response")
	}
}

// Test helpers from the broker integration test
type pingHandler struct {
	topic     broker.Topic
	pongTopic broker.Topic
}

func (h *pingHandler) Topics() []broker.Topic {
	return []broker.Topic{h.topic}
}

func (h *pingHandler) Handle(ctx *broker.Context) error {
	// Unmarshal request
	var request map[string]string
	if err := ctx.Unmarshal(&request); err != nil {
		return err
	}

	// Dispatch to pong service
	pongRequest := map[string]string{"ping": request["message"]}
	pongResp := broker.DispatchTyped[map[string]string](
		ctx,
		h.pongTopic,
		pongRequest,
		broker.WithRequired(),
	)

	// Wait for response
	if err := ctx.Wait(); err != nil {
		return err
	}

	// Get pong response
	pong, err := pongResp.Value()
	if err != nil {
		return err
	}

	// Reply with combined result
	return ctx.Reply(map[string]string{
		"original": request["message"],
		"reply":    pong["pong"],
	})
}

type pongHandler struct {
	topic broker.Topic
}

func (h *pongHandler) Topics() []broker.Topic {
	return []broker.Topic{h.topic}
}

func (h *pongHandler) Handle(ctx *broker.Context) error {
	// Simply reply with pong
	return ctx.Reply(map[string]string{
		"pong": "pong",
	})
}

type testLogger struct {
	t *testing.T
}

func (l *testLogger) Debugf(_ context.Context, format string, args ...any) {
	l.t.Logf("[DEBUG] "+format, args...)
}

func (l *testLogger) Infof(_ context.Context, format string, args ...any) {
	l.t.Logf("[INFO] "+format, args...)
}

func (l *testLogger) Errorf(_ context.Context, format string, args ...any) {
	l.t.Logf("[ERROR] "+format, args...)
}

func (l *testLogger) Warnf(_ context.Context, format string, args ...any) {
	l.t.Logf("[WARN] "+format, args...)
}

func (l *testLogger) Fatalf(_ context.Context, format string, args ...any) {
	l.t.Fatalf("[FATAL] "+format, args...)
}
