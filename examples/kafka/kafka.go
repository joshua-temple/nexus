package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/twmb/franz-go/pkg/kgo"

	broker "github.com/joshua-temple/nexus"
)

// Publisher implements broker.Publisher for RedPanda/Kafka
type Publisher struct {
	client *kgo.Client
}

// NewPublisher creates a new Kafka publisher 
func NewPublisher(brokers []string) (*Publisher, error) {
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ProducerBatchMaxBytes(1000000), // 1MB batches
		kgo.AllowAutoTopicCreation(),
		kgo.DefaultProduceTopic(""),                          // No default topic
		kgo.RecordPartitioner(kgo.StickyKeyPartitioner(nil)), // Sticky partitioning by key
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &Publisher{
		client: client,
	}, nil
}

// Publish sends an envelope to the appropriate topic
func (p *Publisher) Publish(ctx context.Context, envelope *broker.Envelope) error {
	// Serialize the envelope to JSON
	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("failed to marshal envelope: %w", err)
	}

	// Create record with RootID as the key for partitioning
	record := &kgo.Record{
		Topic: envelope.DestinationTopic,
		Key:   []byte(envelope.RootID),
		Value: data,
	}

	// Produce synchronously
	result := p.client.ProduceSync(ctx, record)
	if err := result.FirstErr(); err != nil {
		return fmt.Errorf("failed to publish message: %w", err)
	}

	return nil
}

// Close closes the publisher
func (p *Publisher) Close() error {
	if p.client != nil {
		p.client.Close()
	}
	return nil
}

// Subscriber implements broker.Subscriber for RedPanda/Kafka
type Subscriber struct {
	client        *kgo.Client
	topics        []string
	consumerGroup string
	stopChan      chan struct{}
	wg            sync.WaitGroup
	logger        broker.Logger
	mu            sync.RWMutex
}

// NewSubscriber creates a new Kafka subscriber
func NewSubscriber(brokers []string, topics []string, consumerGroup string, logger broker.Logger) (*Subscriber, error) {
	if logger == nil {
		logger = &broker.NoOpLogger{}
	}

	// Create client with consumer group configuration
	client, err := kgo.NewClient(
		kgo.SeedBrokers(brokers...),
		kgo.ConsumerGroup(consumerGroup),
		kgo.ConsumeTopics(topics...),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()), // Start from beginning for new consumer groups
		kgo.DisableAutoCommit(),                           // Manual commit for better control
		kgo.OnPartitionsRevoked(func(ctx context.Context, _ *kgo.Client, _ map[string][]int32) {
			logger.Infof(ctx, "Partitions revoked for consumer group %s", consumerGroup)
		}),
		kgo.OnPartitionsAssigned(func(ctx context.Context, _ *kgo.Client, assigned map[string][]int32) {
			logger.Infof(ctx, "Partitions assigned for consumer group %s: %v", consumerGroup, assigned)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create kafka client: %w", err)
	}

	return &Subscriber{
		client:        client,
		topics:        topics,
		consumerGroup: consumerGroup,
		logger:        logger,
	}, nil
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

	// Add new topics to our list
	for _, topic := range topics {
		found := false
		for _, existing := range s.topics {
			if existing == topic {
				found = true
				break
			}
		}
		if !found {
			s.topics = append(s.topics, topic)
		}
	}

	// Update franz-go client to consume new topics
	s.client.AddConsumeTopics(topics...)

	s.logger.Infof(ctx, "Added topics to subscription: %v", topics)
	return nil
}

// Start begins consuming messages and sends them to the returned channel
func (s *Subscriber) Start(ctx context.Context) (<-chan *broker.Envelope, error) {
	s.stopChan = make(chan struct{})
	out := make(chan *broker.Envelope, 100)

	// Start consumer goroutine
	s.wg.Add(1)
	go s.consume(ctx, out)

	return out, nil
}

// consume processes messages from Kafka
func (s *Subscriber) consume(ctx context.Context, out chan<- *broker.Envelope) {
	defer s.wg.Done()
	defer close(out)

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopChan:
			return
		default:
			// Fetch messages
			fetches := s.client.PollFetches(ctx)

			// Check for errors
			if errs := fetches.Errors(); len(errs) > 0 {
				for _, err := range errs {
					s.logger.Errorf(ctx, "Fetch error: topic=%s, partition=%d, error=%v",
						err.Topic, err.Partition, err.Err)
				}
			}

			// Process records
			fetches.EachPartition(func(p kgo.FetchTopicPartition) {
				for _, record := range p.Records {
					// Deserialize envelope
					var envelope broker.Envelope
					if err := json.Unmarshal(record.Value, &envelope); err != nil {
						s.logger.Errorf(ctx, "Error unmarshaling envelope: %v", err)
						// Continue processing other messages
						continue
					}

					// Send to output channel
					select {
					case out <- &envelope:
						// Successfully sent
					case <-ctx.Done():
						return
					case <-s.stopChan:
						return
					}
				}
			})

			// Commit offsets after processing
			if err := s.client.CommitUncommittedOffsets(ctx); err != nil {
				s.logger.Errorf(ctx, "Error committing offsets: %v", err)
			}
		}
	}
}

// Stop gracefully shuts down the subscriber
func (s *Subscriber) Stop() {
	if s.stopChan != nil {
		close(s.stopChan)
	}
	s.wg.Wait()
	if s.client != nil {
		s.client.Close()
	}
}

// NewPublisherCompat creates a Publisher that implements the broker.Publisher interface
// This is a compatibility wrapper for the existing interface
func NewPublisherCompat(brokers []string) *PublisherCompat {
	return &PublisherCompat{
		brokers: brokers,
	}
}

// PublisherCompat wraps the franz-go Publisher to match the existing interface exactly
type PublisherCompat struct {
	brokers []string
	pub     *Publisher
	once    sync.Once
}

// Publish implements broker.Publisher
func (p *PublisherCompat) Publish(ctx context.Context, envelope *broker.Envelope) error {
	// Lazy initialization on first use
	var initErr error
	p.once.Do(func() {
		p.pub, initErr = NewPublisher(p.brokers)
	})
	if initErr != nil {
		return initErr
	}
	return p.pub.Publish(ctx, envelope)
}

// Close closes the publisher
func (p *PublisherCompat) Close() error {
	if p.pub != nil {
		return p.pub.Close()
	}
	return nil
}

// NewSubscriberCompat creates a Subscriber that implements the broker.Subscriber interface
// This is a compatibility wrapper for the existing interface
func NewSubscriberCompat(brokers []string, topics []string, consumerGroup string, logger broker.Logger) *SubscriberCompat {
	return &SubscriberCompat{
		brokers:       brokers,
		topics:        topics,
		consumerGroup: consumerGroup,
		logger:        logger,
	}
}

// SubscriberCompat wraps the franz-go Subscriber to match the existing interface exactly
type SubscriberCompat struct {
	brokers       []string
	topics        []string
	consumerGroup string
	logger        broker.Logger
	sub           *Subscriber
	once          sync.Once
}

// RegisterHandlers implements broker.Subscriber
func (s *SubscriberCompat) RegisterHandlers(ctx context.Context, handlers ...broker.Handler) error {
	// Ensure subscriber is initialized
	var initErr error
	s.once.Do(func() {
		s.sub, initErr = NewSubscriber(s.brokers, s.topics, s.consumerGroup, s.logger)
	})
	if initErr != nil {
		return initErr
	}
	return s.sub.RegisterHandlers(ctx, handlers...)
}

// RegisterTopics implements broker.Subscriber
func (s *SubscriberCompat) RegisterTopics(ctx context.Context, topics ...string) error {
	// Ensure subscriber is initialized
	var initErr error
	s.once.Do(func() {
		s.sub, initErr = NewSubscriber(s.brokers, s.topics, s.consumerGroup, s.logger)
	})
	if initErr != nil {
		return initErr
	}
	return s.sub.RegisterTopics(ctx, topics...)
}

// Start implements broker.Subscriber
func (s *SubscriberCompat) Start(ctx context.Context) (<-chan *broker.Envelope, error) {
	// Lazy initialization on first use
	var initErr error
	s.once.Do(func() {
		s.sub, initErr = NewSubscriber(s.brokers, s.topics, s.consumerGroup, s.logger)
	})
	if initErr != nil {
		return nil, initErr
	}
	return s.sub.Start(ctx)
}

// Stop implements broker.Subscriber
func (s *SubscriberCompat) Stop() {
	if s.sub != nil {
		s.sub.Stop()
	}
}
