package nexus

import "time"

// DispatchOption configures how a message is dispatched
type DispatchOption func(*dispatchConfig)

// dispatchConfig holds dispatch configuration
type dispatchConfig struct {
	expectResponse bool
	required       bool
	timeout        time.Duration
}

// defaultDispatchConfig returns default configuration
func defaultDispatchConfig() *dispatchConfig {
	return &dispatchConfig{
		expectResponse: true, // Default to expecting response for backwards compatibility
		required:       false,
		timeout:        30 * time.Second,
	}
}

// WithResponse indicates that a response is expected (this is the default)
func WithResponse() DispatchOption {
	return func(c *dispatchConfig) {
		c.expectResponse = true
	}
}

// WithNoResponse indicates no response is expected (fire-and-forget)
func WithNoResponse() DispatchOption {
	return func(c *dispatchConfig) {
		c.expectResponse = false
	}
}

// WithRequired marks the response as required (only applies if expecting response)
func WithRequired() DispatchOption {
	return func(c *dispatchConfig) {
		c.required = true
	}
}

// WithTimeout sets a custom timeout for waiting for responses
func WithTimeout(timeout time.Duration) DispatchOption {
	return func(c *dispatchConfig) {
		c.timeout = timeout
	}
}

// AsEvent is a convenience option that combines WithNoResponse for event semantics
func AsEvent() DispatchOption {
	return WithNoResponse()
}
