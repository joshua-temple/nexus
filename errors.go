package nexus

import "errors"

var (
	// ErrNoHandlersForTopic is returned when no handlers are registered for a topic
	ErrNoHandlersForTopic = errors.New("no handlers registered for topic")
)
