package temporallog

import (
	"context"
	"log/slog"

	sdklog "go.temporal.io/sdk/log"
)

var _ sdklog.Logger = (*Logger)(nil)
var _ sdklog.WithLogger = (*Logger)(nil)
var _ sdklog.WithSkipCallers = (*Logger)(nil)

// Logger adapts slog to Temporal's logger interface so SDK logs flow through
// the repo-wide OTel log pipeline instead of contaminating stdout/stderr.
type Logger struct {
	logger *slog.Logger
}

func New(logger *slog.Logger) *Logger {
	if logger == nil {
		logger = slog.Default()
	}
	return &Logger{logger: logger}
}

func (l *Logger) Debug(msg string, keyvals ...interface{}) {
	l.logger.DebugContext(context.Background(), msg, keyvals...)
}

func (l *Logger) Info(msg string, keyvals ...interface{}) {
	l.logger.InfoContext(context.Background(), msg, keyvals...)
}

func (l *Logger) Warn(msg string, keyvals ...interface{}) {
	l.logger.WarnContext(context.Background(), msg, keyvals...)
}

func (l *Logger) Error(msg string, keyvals ...interface{}) {
	l.logger.ErrorContext(context.Background(), msg, keyvals...)
}

func (l *Logger) With(keyvals ...interface{}) sdklog.Logger {
	return &Logger{logger: l.logger.With(keyvals...)}
}

func (l *Logger) WithCallerSkip(int) sdklog.Logger {
	return l
}
