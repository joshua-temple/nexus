package test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// Test infrastructure helpers

// In-memory test publisher
type testPublisher struct {
	mu        sync.RWMutex
	messages  []*broker.Envelope
	listeners map[string][]chan *broker.Envelope
}

func newTestPublisher() *testPublisher {
	return &testPublisher{
		messages:  make([]*broker.Envelope, 0),
		listeners: make(map[string][]chan *broker.Envelope),
	}
}

func (p *testPublisher) Publish(ctx context.Context, envelope *broker.Envelope) error {
	p.mu.Lock()
	p.messages = append(p.messages, envelope)

	// Get listeners for the destination topic
	listeners := make([]chan *broker.Envelope, 0)
	if ls, ok := p.listeners[envelope.DestinationTopic]; ok {
		listeners = append(listeners, ls...)
	}
	p.mu.Unlock()

	// Notify listeners
	for _, ch := range listeners {
		select {
		case ch <- envelope:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (p *testPublisher) Subscribe(topic string) <-chan *broker.Envelope {
	p.mu.Lock()
	defer p.mu.Unlock()

	ch := make(chan *broker.Envelope, 100)
	p.listeners[topic] = append(p.listeners[topic], ch)
	return ch
}

// In-memory test subscriber
type testSubscriber struct {
	publisher *testPublisher
	messages  chan *broker.Envelope
	stopCh    chan struct{}
	processed map[string]bool
	mu        sync.RWMutex
}

func newTestSubscriber(publisher *testPublisher) *testSubscriber {
	return &testSubscriber{
		publisher: publisher,
		messages:  make(chan *broker.Envelope, 100),
		stopCh:    make(chan struct{}),
		processed: make(map[string]bool),
	}
}

func (s *testSubscriber) Start(ctx context.Context) (<-chan *broker.Envelope, error) {
	// Route published messages to the broker
	go func() {
		// Subscribe to all topics for routing
		allMessages := make(chan *broker.Envelope, 100)

		// Monitor publisher for new messages
		go func() {
			lastIndex := 0
			for {
				select {
				case <-ctx.Done():
					return
				case <-s.stopCh:
					return
				default:
					s.publisher.mu.RLock()
					currentLen := len(s.publisher.messages)
					s.publisher.mu.RUnlock()

					if currentLen > lastIndex {
						s.publisher.mu.RLock()
						// Get new messages
						for i := lastIndex; i < currentLen; i++ {
							msg := s.publisher.messages[i]

							// Check if already processed
							s.mu.RLock()
							processed := s.processed[msg.ID]
							s.mu.RUnlock()

							if !processed {
								select {
								case allMessages <- msg:
									s.mu.Lock()
									s.processed[msg.ID] = true
									s.mu.Unlock()
								case <-ctx.Done():
									s.publisher.mu.RUnlock()
									return
								}
							}
						}
						lastIndex = currentLen
						s.publisher.mu.RUnlock()
					}

					time.Sleep(5 * time.Millisecond)
				}
			}
		}()

		// Route messages to broker
		for {
			select {
			case msg := <-allMessages:
				select {
				case s.messages <- msg:
				case <-ctx.Done():
					return
				case <-s.stopCh:
					return
				}
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			}
		}
	}()

	return s.messages, nil
}

func (s *testSubscriber) Stop() {
	select {
	case <-s.stopCh:
		// Already closed
	default:
		close(s.stopCh)
	}
}

func (s *testSubscriber) RegisterHandlers(ctx context.Context, handlers ...broker.Handler) error {
	// No-op for test subscriber
	return nil
}

func (s *testSubscriber) RegisterTopics(ctx context.Context, topics ...string) error {
	// No-op for test subscriber
	return nil
}

// Test logger
type testLogger struct {
	t    *testing.T
	mu   sync.Mutex
	logs []string
}

func newTestLogger(t *testing.T) *testLogger {
	return &testLogger{t: t, logs: make([]string, 0)}
}

func (l *testLogger) Debugf(_ context.Context, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, "DEBUG: "+format)
	l.t.Logf("DEBUG: "+format, args...)
}

func (l *testLogger) Infof(_ context.Context, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, "INFO: "+format)
	l.t.Logf("INFO: "+format, args...)
}

func (l *testLogger) Warnf(_ context.Context, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, "WARN: "+format)
	l.t.Logf("WARN: "+format, args...)
}

func (l *testLogger) Errorf(_ context.Context, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, "ERROR: "+format)
	l.t.Logf("ERROR: "+format, args...)
}

func (l *testLogger) Fatalf(_ context.Context, format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, "FATAL: "+format)
	l.t.Fatalf("FATAL: "+format, args...)
}

// Helper to create test broker
func createTestBroker(t *testing.T, numWorkers int) (*broker.Broker, *testPublisher, func()) {
	pub := newTestPublisher()
	sub := newTestSubscriber(pub)
	logger := newTestLogger(t)

	b := broker.New(numWorkers, pub, sub, logger)

	cleanup := func() {
		b.Stop()
		sub.Stop()
	}

	return b, pub, cleanup
}

// Helper to send test message and wait for response
func sendAndWaitForResponse(t *testing.T, pub *testPublisher, request *broker.Envelope, replyTopic string, timeout time.Duration) (interface{}, error) {
	// Subscribe to reply topic
	replyCh := pub.Subscribe(replyTopic)

	// Send request
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if err := pub.Publish(ctx, request); err != nil {
		return nil, err
	}

	// Wait for response
	select {
	case reply := <-replyCh:
		return reply.Payload, nil
	case <-ctx.Done():
		t.Logf("Timeout waiting for response on topic %s", replyTopic)
		return nil, ctx.Err()
	}
}

// Helper to marshal payload
func marshalPayload(v interface{}) []byte {
	data, _ := json.Marshal(v)
	return data
}

// Helper to unmarshal response
func unmarshalResponse(t *testing.T, payload interface{}, target interface{}) {
	var data []byte
	switch p := payload.(type) {
	case []byte:
		data = p
	case string:
		data = []byte(p)
	default:
		var err error
		data, err = json.Marshal(p)
		if err != nil {
			t.Fatalf("Failed to marshal payload: %v", err)
		}
	}

	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}
}

// Helper to create topics
func mustTopic(team, domain string, version int) *broker.Topic {
	t, err := broker.NewTopic(team, domain, version)
	if err != nil {
		panic(err)
	}
	return t
}
