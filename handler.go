package nexus

import (
	"sync"

	"github.com/pkg/errors"
)

type Handlers []Handler

type Handler interface {
	Topics() []Topic
	Handle(ctx *Context) error
}

type HandlerMap struct {
	m map[string]Handlers
	*sync.RWMutex
}

func (h *HandlerMap) Append(handlers ...Handler) {
	h.Lock()
	defer h.Unlock()
	for _, handler := range handlers {
		for _, topic := range handler.Topics() {
			h.m[topic.String()] = append(h.m[topic.String()], handler)
		}
	}
}

func (h *HandlerMap) work(ctx *Context, e *Envelope) error {
	h.RLock()
	defer h.RUnlock()
	handlers, ok := h.m[e.DestinationTopic]
	if !ok {
		return errors.Wrap(ErrNoHandlersForTopic, e.DestinationTopic)
	}

	for _, handler := range handlers {
		if err := handler.Handle(ctx); err != nil {
			return err
		}
	}

	return nil
}

func newHandlerMap() *HandlerMap {
	return &HandlerMap{
		m:       make(map[string]Handlers),
		RWMutex: &sync.RWMutex{},
	}
}
