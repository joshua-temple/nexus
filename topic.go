package nexus

import (
	"fmt"
	"strings"
)

type Topics []Topic

// Topic represents a topic in the broker system.
// standardizes the topic taxonomy across the system.
// <team>.<domain>.<purpose>.<type>.v<version>
// eg.
// delivery.handler.orderDelivered.cmd.v1
// delivery.broker.v1
// delivery.api.outbox.v1
// delivery.api.reply.v1
// delivery.consumer.driver.v1
// Team and Domain are required fields.
// purpose is optional, but if provided, should be a verb indicating the purpose taken.
// Type is implied if a topic has either a result or an event.
// - Type will inherently dictate adding a suffix before the version.
type Topic struct {
	// team responsible for the topic
	team string
	// domain of the topic
	domain string // Domain of the topic
	// purpose of the topic
	purpose string
	// type of the topic, e.g., command, event, result
	topicType TopicType
	// version of the topic, used for schema evolution
	version int
}

type TopicType int

const (
	TopicTypeUnspecified TopicType = iota
	TopicTypeCommand
	TopicTypeEvent
	TopicTypeResult
)

func (t TopicType) String() string {
	switch t {
	case TopicTypeCommand:
		return "cmd"
	case TopicTypeEvent:
		return "evt"
	case TopicTypeResult:
		return "res"
	default:
		return ""
	}
}

func NewTopic(team, domain string, version int) (*Topic, error) {
	if team == "" || domain == "" {
		return nil, fmt.Errorf("team and domain must be specified")
	}
	if version <= 0 {
		return nil, fmt.Errorf("version must be postivie and non-zero")
	}
	return &Topic{
		team:    team,
		domain:  domain,
		version: version,
	}, nil
}

func (t *Topic) WithPurpose(purpose string) *Topic {
	t.purpose = purpose
	return t
}

func (t *Topic) WithTopicType(topicType TopicType) *Topic {
	t.topicType = topicType
	return t
}

func (t *Topic) String() string {
	var b strings.Builder
	defer b.Reset()
	b.WriteString(t.team + "." + t.domain)
	if t.purpose != "" {
		b.WriteString("." + t.purpose)
	}
	if t.topicType != 0 {
		b.WriteString("." + t.topicType.String())
	}
	if t.version > 0 {
		b.WriteString(fmt.Sprintf(".v%d", t.version))
	}
	return b.String()
}

func (t *Topic) AsEvent() string {
	tt := &Topic{
		team:      t.team,
		domain:    t.domain,
		purpose:   t.purpose,
		topicType: TopicTypeEvent,
		version:   t.version,
	}
	return tt.String()
}

func (t *Topic) AsCommand() string {
	tt := &Topic{
		team:      t.team,
		domain:    t.domain,
		purpose:   t.purpose,
		topicType: TopicTypeCommand,
		version:   t.version,
	}
	return tt.String()
}

func (t *Topic) AsResult() string {
	tt := &Topic{
		team:      t.team,
		domain:    t.domain,
		purpose:   t.purpose,
		topicType: TopicTypeResult,
		version:   t.version,
	}
	return tt.String()
}

func (t *Topic) TopicType() TopicType {
	return t.topicType
}

func (t *Topic) Team() string {
	return t.team
}

func (t *Topic) Domain() string {
	return t.domain
}

func (t *Topic) Purpose() string {
	return t.purpose
}
