package nexus

import (
	"context"
)

type Logger interface {
	Debugf(context.Context, string, ...any)
	Infof(context.Context, string, ...any)
	Errorf(context.Context, string, ...any)
	Warnf(context.Context, string, ...any)
	Fatalf(context.Context, string, ...any)
}

// NoOpLogger is a logger that does nothing
type NoOpLogger struct{}

func (n NoOpLogger) Debugf(_ context.Context, _ string, _ ...any) {}
func (n NoOpLogger) Infof(_ context.Context, _ string, _ ...any)  {}
func (n NoOpLogger) Errorf(_ context.Context, _ string, _ ...any) {}
func (n NoOpLogger) Warnf(_ context.Context, _ string, _ ...any)  {}
func (n NoOpLogger) Fatalf(_ context.Context, _ string, _ ...any) {}
