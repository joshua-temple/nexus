package nexus

import (
	"context"
	"fmt"
	"sync"
)

type Publisher interface {
	Publish(ctx context.Context, envelope *Envelope) error
}

type Subscriber interface {

	// RegisterHandlers registers handlers for specific topics
	RegisterHandlers(ctx context.Context, handlers ...Handler) error
	// RegisterTopics adds new topics to the subscription
	RegisterTopics(ctx context.Context, topics ...string) error
	// Start begins consuming messages and sends them to the returned channel
	Start(ctx context.Context) (<-chan *Envelope, error)
	// Stop gracefully shuts down the subscriber
	Stop()
}

type Broker struct {
	Publisher
	Subscriber
	Logger
	handlers *HandlerMap
	inbox    *sync.Map // RequestID -> *Response
	workers  int
	running  bool
	stopChan chan struct{}
	wg       sync.WaitGroup
}

func New(workers int, pub Publisher, sub Subscriber, logger Logger) *Broker {
	if logger == nil {
		logger = &NoOpLogger{}
	}
	return &Broker{
		Publisher:  pub,
		Subscriber: sub,
		Logger:     logger,
		handlers:   newHandlerMap(),
		inbox:      &sync.Map{},
		workers:    workers,
		stopChan:   make(chan struct{}),
	}
}

// Register registers handlers with the broker
// It appends the handlers to the internal handler map and registers them with the subscriber if already running.
func (b *Broker) Register(handlers ...Handler) error {
	b.handlers.Append(handlers...)

	// Extract all topics from the handlers
	var topics []string
	for _, handler := range handlers {
		for _, topic := range handler.Topics() {
			topics = append(topics, topic.String())
		}
	}

	// Register topics if we have any and broker is running
	if len(topics) > 0 && b.running {
		ctx := context.Background()

		// Register the topics with the subscriber
		if err := b.RegisterTopics(ctx, topics...); err != nil {
			return fmt.Errorf("failed to register topics: %w", err)
		}

		// Also notify subscriber about the handlers
		if err := b.RegisterHandlers(ctx, handlers...); err != nil {
			return fmt.Errorf("failed to register handlers: %w", err)
		}
	}

	return nil
}

// Start starts the broker and begins processing messages
func (b *Broker) Start(ctx context.Context) error {
	msgChan, err := b.Subscriber.Start(ctx)
	if err != nil {
		return err
	}

	// Mark broker as running
	b.running = true

	// Start workers
	for i := 0; i < b.workers; i++ {
		b.wg.Add(1)
		go b.worker(ctx, msgChan)
	}

	return nil
}

// Stop gracefully shuts down the broker
func (b *Broker) Stop() {
	close(b.stopChan)
	b.Subscriber.Stop()
	b.wg.Wait()
}

// worker processes messages from the subscriber
func (b *Broker) worker(ctx context.Context, msgChan <-chan *Envelope) {
	defer b.wg.Done()

	for {
		select {
		case <-b.stopChan:
			return
		case env, ok := <-msgChan:
			if !ok {
				return
			}
			b.processEnvelope(ctx, env)
		}
	}
}

// processEnvelope handles a single envelope
func (b *Broker) processEnvelope(ctx context.Context, env *Envelope) {
	// Route responses based on RequestID
	if env.RequestID != "" {
		b.Debugf(ctx, "processing response with RequestID: %s", env.RequestID)
		if respObj, ok := b.inbox.Load(env.RequestID); ok {
			b.Debugf(ctx, "found response object for RequestID: %s", env.RequestID)
			if resp, ok := respObj.(*Response); ok {
				// Deliver response directly
				resp.mu.Lock()
				resp.result = env
				resp.mu.Unlock()

				// Execute callback if present
				if resp.onResult != nil {
					resp.err = resp.onResult(env)
				}

				// Signal completion
				select {
				case <-resp.ready:
					// Already closed
					b.Warnf(ctx, "response already delivered for RequestID: %s", env.RequestID)
				default:
					close(resp.ready)
					b.Debugf(ctx, "successfully delivered response for RequestID: %s", env.RequestID)
				}

				// Response was delivered, don't process as a new message
				return
			} else {
				b.Warnf(ctx, "object is not a *Response for RequestID: %s", env.RequestID)
			}
		} else {
			b.Debugf(ctx, "no response object found for RequestID: %s", env.RequestID)
		}
	}

	// Create handler context
	handlerCtx := &Context{
		Context:  ctx,
		Envelope: env,
		broker:   b,
		tracker:  newResponseTracker(),
	}

	// Execute handlers
	if err := b.handlers.work(handlerCtx, env); err != nil {
		b.Errorf(ctx, "handler error for topic %s: %v", env.DestinationTopic, err)
	}
}
