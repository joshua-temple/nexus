package nexus

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"
)

// Context provides handlers with dispatch and response handling capabilities
type Context struct {
	context.Context
	Envelope *Envelope
	broker   *Broker
	tracker  *responseTracker
}

// Dispatch sends a command with configurable options
func (c *Context) Dispatch(topic Topic, payload interface{}, opts ...DispatchOption) *Response {
	// Apply options
	config := defaultDispatchConfig()
	for _, opt := range opts {
		opt(config)
	}

	// Create child envelope
	var replyTopics []string
	if config.expectResponse {
		// Reply back to the current handler's topic, not the original source
		replyTopics = []string{c.Envelope.DestinationTopic}
	}

	// Ensure payload is JSON bytes
	var jsonPayload interface{}
	switch p := payload.(type) {
	case []byte:
		jsonPayload = p
	case string:
		jsonPayload = []byte(p)
	default:
		// Marshal Go structs to JSON
		data, err := json.Marshal(p)
		if err != nil {
			return &Response{
				err:   fmt.Errorf("failed to marshal payload: %w", err),
				ready: make(chan struct{}),
			}
		}
		jsonPayload = data
	}

	childEnv := c.Envelope.NewChildEnvelope(
		topic.String(),
		replyTopics,
		jsonPayload,
	)

	// If not expecting response, just publish and return nil
	if !config.expectResponse {
		go func() {
			if err := c.broker.Publish(c, childEnv); err != nil {
				c.broker.Errorf(c, "failed to publish command %s: %v", topic, err)
			} else {
				c.broker.Debugf(c, "successfully published command %s", topic)
			}
		}()
		return nil
	}

	// Create response tracker
	resp := &Response{
		topic:    topic,
		envelope: childEnv,
		ready:    make(chan struct{}),
		required: config.required,
		timeout:  config.timeout,
	}

	// Track response in both local tracker and broker inbox
	c.tracker.add(childEnv.ID, resp)
	// Store the response directly in inbox for routing
	c.broker.inbox.Store(childEnv.ID, resp)

	// Publish async
	go func() {
		if err := c.broker.Publish(c, childEnv); err != nil {
			resp.err = err
			close(resp.ready)
		}
	}()

	return resp
}

// DispatchTyped is a helper function that returns a typed response
func DispatchTyped[T any](ctx *Context, topic Topic, payload interface{}, opts ...DispatchOption) *TypedResponse[T] {
	resp := ctx.Dispatch(topic, payload, opts...)
	if resp == nil {
		return nil // Fire-and-forget, no response expected
	}
	return &TypedResponse[T]{
		Response:  resp,
		unmarshal: json.Unmarshal,
	}
}

// Wait waits for all required responses
func (c *Context) Wait() error {
	return c.tracker.waitForRequired(60 * time.Second)
}

// WaitWithTimeout waits for all required responses with custom timeout
func (c *Context) WaitWithTimeout(timeout time.Duration) error {
	return c.tracker.waitForRequired(timeout)
}

// WaitAll waits for all responses including optional ones
func (c *Context) WaitAll() error {
	return c.tracker.waitForAll(60 * time.Second)
}

// WaitAllWithTimeout waits for all responses with custom timeout
func (c *Context) WaitAllWithTimeout(timeout time.Duration) error {
	return c.tracker.waitForAll(timeout)
}

// Reply sends a response if anyone is waiting
func (c *Context) Reply(payload interface{}) error {
	if len(c.Envelope.ReplyToTopics) == 0 {
		return nil // No one waiting for response
	}

	replyEnv := c.Envelope.NewResponseEnvelope(payload)
	if replyEnv == nil {
		return nil
	}

	return c.broker.Publish(c, replyEnv)
}

// ReplyError sends an error response
func (c *Context) ReplyError(err error) error {
	errStr := err.Error()
	c.Envelope.Error = &errStr
	return c.Reply(c.Envelope)
}

// Unmarshal unmarshals the envelope payload into the target
func (c *Context) Unmarshal(target interface{}) error {
	switch payload := c.Envelope.Payload.(type) {
	case []byte:
		return json.Unmarshal(payload, target)
	case string:
		// Try to decode as base64 first (happens when []byte is JSON marshaled)
		if decoded, err := base64.StdEncoding.DecodeString(payload); err == nil {
			return json.Unmarshal(decoded, target)
		}
		// Otherwise treat as JSON string
		return json.Unmarshal([]byte(payload), target)
	case []interface{}:
		// Handle case where JSON unmarshaling turned []byte into []interface{} of float64s
		bytes := make([]byte, len(payload))
		for i, v := range payload {
			if f, ok := v.(float64); ok {
				bytes[i] = byte(f)
			} else {
				// Fall back to re-marshaling
				data, err := json.Marshal(c.Envelope.Payload)
				if err != nil {
					return fmt.Errorf("failed to re-marshal payload: %w", err)
				}
				return json.Unmarshal(data, target)
			}
		}
		return json.Unmarshal(bytes, target)
	default:
		// For other types (like map[string]interface{}), marshal back to JSON first
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to re-marshal payload: %w", err)
		}
		return json.Unmarshal(data, target)
	}
}
