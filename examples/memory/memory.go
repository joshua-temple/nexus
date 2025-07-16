// Package memory provides an in-memory implementation of the broker interfaces.
// This is useful for testing and development without external dependencies.
package memory

import (
	"context"
	"fmt"
	"sync"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// Hub manages all in-memory pub/sub operations
type Hub struct {
	mu           sync.RWMutex
	topics       map[string][]chan *broker.Envelope
	subscribers  map[string][]*Subscriber
	messageStore map[string][]*broker.Envelope // Store messages per topic
	storeLimit   int                           // Max messages to store per topic
}

// NewHub creates a new in-memory message hub
func NewHub() *Hub {
	return &Hub{
		topics:       make(map[string][]chan *broker.Envelope),
		subscribers:  make(map[string][]*Subscriber),
		messageStore: make(map[string][]*broker.Envelope),
		storeLimit:   1000, // Default: store last 1000 messages per topic
	}
}

// SetStoreLimit sets the maximum number of messages to store per topic
func (h *Hub) SetStoreLimit(limit int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.storeLimit = limit
}

// publish sends a message to all subscribers of a topic
func (h *Hub) publish(envelope *broker.Envelope) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Store the message
	if h.storeLimit > 0 {
		messages := h.messageStore[envelope.DestinationTopic]
		messages = append(messages, envelope)

		// Trim to store limit
		if len(messages) > h.storeLimit {
			messages = messages[len(messages)-h.storeLimit:]
		}
		h.messageStore[envelope.DestinationTopic] = messages
	}

	// Send to all subscribers
	if channels, ok := h.topics[envelope.DestinationTopic]; ok {
		for _, ch := range channels {
			select {
			case ch <- envelope:
				// Sent successfully
			default:
				// Channel is full, skip this subscriber
				// In a real implementation, you might want to handle this differently
			}
		}
	}
}

// subscribe adds a channel to receive messages for a topic
func (h *Hub) subscribe(topic string, ch chan *broker.Envelope) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.topics[topic] = append(h.topics[topic], ch)
}

// unsubscribe removes a channel from a topic
func (h *Hub) unsubscribe(topic string, ch chan *broker.Envelope) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if channels, ok := h.topics[topic]; ok {
		for i, c := range channels {
			if c == ch {
				// Remove the channel
				h.topics[topic] = append(channels[:i], channels[i+1:]...)
				break
			}
		}

		// Clean up empty topic entries
		if len(h.topics[topic]) == 0 {
			delete(h.topics, topic)
		}
	}
}

// GetStoredMessages returns stored messages for a topic (useful for testing)
func (h *Hub) GetStoredMessages(topic string) []*broker.Envelope {
	h.mu.RLock()
	defer h.mu.RUnlock()

	messages := h.messageStore[topic]
	result := make([]*broker.Envelope, len(messages))
	copy(result, messages)
	return result
}

// Publisher implements broker.Publisher for in-memory messaging
type Publisher struct {
	hub *Hub
}

// NewPublisher creates a new in-memory publisher
func NewPublisher(hub *Hub) *Publisher {
	return &Publisher{hub: hub}
}

// Publish sends an envelope to the in-memory hub
func (p *Publisher) Publish(ctx context.Context, envelope *broker.Envelope) error {
	if envelope == nil {
		return fmt.Errorf("envelope cannot be nil")
	}
	if envelope.DestinationTopic == "" {
		return fmt.Errorf("destination topic cannot be empty")
	}

	// Create a copy to avoid data races
	envCopy := *envelope

	// Simulate async publish with context support
	done := make(chan struct{})
	go func() {
		p.hub.publish(&envCopy)
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Close is a no-op for in-memory publisher
func (p *Publisher) Close() error {
	return nil
}

// Subscriber implements broker.Subscriber for in-memory messaging
type Subscriber struct {
	hub           *Hub
	topics        []string
	consumerGroup string
	stopChan      chan struct{}
	outChan       chan *broker.Envelope
	topicChans    map[string]chan *broker.Envelope
	wg            sync.WaitGroup
	mu            sync.RWMutex
	logger        broker.Logger
	started       bool
}

// NewSubscriber creates a new in-memory subscriber
func NewSubscriber(hub *Hub, topics []string, consumerGroup string, logger broker.Logger) *Subscriber {
	if logger == nil {
		logger = &broker.NoOpLogger{}
	}

	return &Subscriber{
		hub:           hub,
		topics:        topics,
		consumerGroup: consumerGroup,
		topicChans:    make(map[string]chan *broker.Envelope),
		logger:        logger,
	}
}

// RegisterHandlers implements broker.Subscriber
func (s *Subscriber) RegisterHandlers(ctx context.Context, handlers ...broker.Handler) error {
	// No-op - handlers are managed by the broker, not the transport
	return nil
}

// RegisterTopics adds new topics to the subscription
func (s *Subscriber) RegisterTopics(ctx context.Context, topics ...string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, topic := range topics {
		// Check if already subscribed
		found := false
		for _, existing := range s.topics {
			if existing == topic {
				found = true
				break
			}
		}

		if !found {
			s.topics = append(s.topics, topic)

			// If already started, subscribe to the new topic
			if s.started && s.outChan != nil {
				ch := make(chan *broker.Envelope, 100)
				s.topicChans[topic] = ch
				s.hub.subscribe(topic, ch)

				// Start goroutine to forward messages
				s.wg.Add(1)
				go s.forwardMessages(topic, ch)
			}
		}
	}

	s.logger.Infof(ctx, "Added topics to subscription: %v", topics)
	return nil
}

// Start begins consuming messages
func (s *Subscriber) Start(ctx context.Context) (<-chan *broker.Envelope, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.started {
		return nil, fmt.Errorf("subscriber already started")
	}

	s.stopChan = make(chan struct{})
	s.outChan = make(chan *broker.Envelope, 100)
	s.started = true

	// Subscribe to all topics
	for _, topic := range s.topics {
		ch := make(chan *broker.Envelope, 100)
		s.topicChans[topic] = ch
		s.hub.subscribe(topic, ch)

		// Start goroutine to forward messages from topic channel to output channel
		s.wg.Add(1)
		go s.forwardMessages(topic, ch)
	}

	// If subscriber is configured to replay stored messages, do it here
	// This simulates Kafka's offset behavior
	if s.consumerGroup != "" && s.hub.storeLimit > 0 {
		go s.replayStoredMessages()
	}

	return s.outChan, nil
}

// forwardMessages forwards messages from a topic channel to the output channel
func (s *Subscriber) forwardMessages(topic string, ch chan *broker.Envelope) {
	defer s.wg.Done()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}

			select {
			case s.outChan <- msg:
				// Message forwarded
			case <-s.stopChan:
				return
			}

		case <-s.stopChan:
			return
		}
	}
}

// replayStoredMessages replays stored messages for all subscribed topics
func (s *Subscriber) replayStoredMessages() {
	// Small delay to ensure subscriber is fully set up
	time.Sleep(100 * time.Millisecond)

	s.mu.RLock()
	topics := make([]string, len(s.topics))
	copy(topics, s.topics)
	s.mu.RUnlock()

	for _, topic := range topics {
		messages := s.hub.GetStoredMessages(topic)
		for _, msg := range messages {
			select {
			case <-s.stopChan:
				return
			default:
				// Try to send, but don't block
				select {
				case s.outChan <- msg:
					// Message sent
				case <-s.stopChan:
					return
				default:
					// Channel full or closed, skip
				}
			}
		}
	}
}

// Stop gracefully shuts down the subscriber
func (s *Subscriber) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.started {
		return
	}

	// Signal stop
	close(s.stopChan)

	// Unsubscribe from all topics
	for topic, ch := range s.topicChans {
		s.hub.unsubscribe(topic, ch)
		close(ch)
	}

	// Wait for all goroutines to finish
	s.wg.Wait()

	// Close output channel
	if s.outChan != nil {
		close(s.outChan)
	}

	// Reset state
	s.topicChans = make(map[string]chan *broker.Envelope)
	s.started = false
}

// NewMemoryBroker creates a complete in-memory broker setup for testing
func NewMemoryBroker(topics []string, consumerGroup string, logger broker.Logger) (*broker.Broker, *Hub) {
	hub := NewHub()
	pub := NewPublisher(hub)
	sub := NewSubscriber(hub, topics, consumerGroup, logger)

	return broker.New(4, pub, sub, logger), hub
}
