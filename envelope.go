package nexus

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Envelope struct {
	ID               string   `json:"id"`
	RootID           string   `json:"root_id"`              // Root parent ID (partition key)
	RequestID        string   `json:"request_id,omitempty"` // For response correlation
	StartedAt        int64    `json:"started_at"`
	CreatedAt        int64    `json:"created_at"`
	SourceTopic      string   `json:"source_topic"`
	DestinationTopic string   `json:"destination_topic"`
	ReplyToTopics    []string `json:"reply_to_topics,omitempty"`
	SchemaVersion    string   `json:"schema_version"`
	Error            *string  `json:"error,omitempty"`
	Payload          any      `json:"payload"`
	hasResponse      bool
	required         bool
}

func NewEnvelope(sourceTopic, destinationTopic string, replyTopics []string, payload any) *Envelope {
	t := time.Now().UnixNano()
	id := fmt.Sprintf("%s:%s", sourceTopic, uuid.NewString())
	return &Envelope{
		ID:               id,
		RootID:           id, // Root message is its own root
		StartedAt:        t,
		CreatedAt:        t,
		SourceTopic:      sourceTopic,
		DestinationTopic: destinationTopic,
		Payload:          payload,
		ReplyToTopics:    replyTopics,
	}
}

func (e *Envelope) NewChildEnvelope(destinationTopic string, replyTopics []string, payload any) *Envelope {
	child := &Envelope{
		ID:               fmt.Sprintf("%s:%s", e.DestinationTopic, uuid.NewString()),
		RootID:           e.RootID, // Inherit root ID for Kafka routing
		StartedAt:        e.StartedAt,
		CreatedAt:        time.Now().UnixNano(),
		SourceTopic:      e.DestinationTopic,
		DestinationTopic: destinationTopic,
		Payload:          payload,
		ReplyToTopics:    replyTopics,
		SchemaVersion:    e.SchemaVersion,
	}

	// Mark that we expect a response, but don't set RequestID
	// RequestID is only for response envelopes
	if len(replyTopics) > 0 {
		child.hasResponse = true
	}

	return child
}

func (e *Envelope) MarkRequired() *Envelope {
	e.hasResponse = true
	e.required = true
	e.ReplyToTopics = append(e.ReplyToTopics, e.SourceTopic)
	return e
}

func (e *Envelope) ResponseExpected() bool {
	return e.hasResponse
}

func (e *Envelope) ResponseRequired() bool {
	return e.required
}

func (e *Envelope) SetExpectsResponse() *Envelope {
	e.hasResponse = true
	e.ReplyToTopics = append(e.ReplyToTopics, e.SourceTopic)
	return e
}

// NewResponseEnvelope creates a response envelope
func (e *Envelope) NewResponseEnvelope(payload any) *Envelope {
	if len(e.ReplyToTopics) == 0 {
		return nil
	}

	return &Envelope{
		ID:               uuid.NewString(),
		RootID:           e.RootID, // Same root for routing
		RequestID:        e.ID,     // Links to THIS request's ID
		StartedAt:        e.StartedAt,
		CreatedAt:        time.Now().UnixNano(),
		SourceTopic:      e.DestinationTopic,
		DestinationTopic: e.ReplyToTopics[0],
		Payload:          payload,
		SchemaVersion:    e.SchemaVersion,
	}
}
