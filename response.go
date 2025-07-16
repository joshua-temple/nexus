package nexus

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Response represents a future response from a dispatched command
type Response struct {
	topic    Topic
	envelope *Envelope
	result   *Envelope
	required bool
	ready    chan struct{}
	err      error
	timeout  time.Duration
	mu       sync.RWMutex
	onResult func(*Envelope) error
}

// TypedResponse provides type-safe access to response data
type TypedResponse[T any] struct {
	*Response
	value     T
	unmarshal func([]byte, interface{}) error
}

// OnResult sets a callback to process the response
func (r *TypedResponse[T]) OnResult(fn func(T) error) *TypedResponse[T] {
	r.Response.onResult = func(env *Envelope) error {
		// Unmarshal based on payload type
		switch payload := env.Payload.(type) {
		case []byte:
			if err := r.unmarshal(payload, &r.value); err != nil {
				return fmt.Errorf("unmarshal failed: %w", err)
			}
		case string:
			if err := r.unmarshal([]byte(payload), &r.value); err != nil {
				return fmt.Errorf("unmarshal failed: %w", err)
			}
		default:
			// Try direct assignment for simple types
			if v, ok := payload.(T); ok {
				r.value = v
			} else {
				// For complex types like map[string]interface{}, re-marshal to JSON
				data, err := json.Marshal(payload)
				if err != nil {
					return fmt.Errorf("failed to re-marshal payload: %w", err)
				}
				if err := r.unmarshal(data, &r.value); err != nil {
					return fmt.Errorf("unmarshal failed: %w", err)
				}
			}
		}
		return fn(r.value)
	}
	return r
}

// Required marks this response as required (deprecated: use WithRequired option instead)
func (r *TypedResponse[T]) Required() *TypedResponse[T] {
	r.Response.required = true
	return r
}

// Value waits for and returns the typed response value
func (r *TypedResponse[T]) Value() (T, error) {
	timeout := r.timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if err := r.wait(timeout); err != nil {
		return r.value, err
	}

	// Unmarshal if not already done by OnResult
	if r.result != nil && r.onResult == nil {
		switch payload := r.result.Payload.(type) {
		case []byte:
			if err := r.unmarshal(payload, &r.value); err != nil {
				r.err = fmt.Errorf("unmarshal failed: %w", err)
			}
		case string:
			if err := r.unmarshal([]byte(payload), &r.value); err != nil {
				r.err = fmt.Errorf("unmarshal failed: %w", err)
			}
		default:
			if v, ok := payload.(T); ok {
				r.value = v
			} else {
				// For complex types like map[string]interface{}, re-marshal to JSON
				data, err := json.Marshal(payload)
				if err != nil {
					r.err = fmt.Errorf("failed to re-marshal payload: %w", err)
				} else if err := r.unmarshal(data, &r.value); err != nil {
					r.err = fmt.Errorf("unmarshal failed: %w", err)
				}
			}
		}
	}

	return r.value, r.err
}

// wait blocks until response is ready or timeout
func (r *Response) wait(timeout time.Duration) error {
	select {
	case <-r.ready:
		return r.err
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for response from %s", r.topic.String())
	}
}

// responseTracker manages multiple responses for a handler
type responseTracker struct {
	responses map[string]*Response // envelope ID -> Response
	mu        sync.RWMutex
}

func newResponseTracker() *responseTracker {
	return &responseTracker{
		responses: make(map[string]*Response),
	}
}

// add registers a response for tracking
func (rt *responseTracker) add(id string, resp *Response) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.responses[id] = resp
}

// waitForRequired waits for all required responses
func (rt *responseTracker) waitForRequired(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	// Check all required responses
	var errors []error
	for id, resp := range rt.responses {
		if resp.required {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return fmt.Errorf("timeout waiting for required responses")
			}

			if err := resp.wait(remaining); err != nil {
				errors = append(errors, fmt.Errorf("%s: %w", id, err))
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("response errors: %v", errors)
	}

	return nil
}

// waitForAll waits for all responses (including optional)
func (rt *responseTracker) waitForAll(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	rt.mu.RLock()
	defer rt.mu.RUnlock()

	var errors []error
	for id, resp := range rt.responses {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("timeout waiting for all responses")
		}

		if err := resp.wait(remaining); err != nil {
			errors = append(errors, fmt.Errorf("%s: %w", id, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("response errors: %v", errors)
	}

	return nil
}
