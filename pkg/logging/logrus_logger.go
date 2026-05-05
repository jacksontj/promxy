// Package logging provides logrus-backed adapters for the loggers
// prometheus libraries (and a few legacy shims) ask for.
package logging

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	gokitlog "github.com/go-kit/log"
	gokitlevel "github.com/go-kit/log/level"
	"github.com/sirupsen/logrus"
)

// NewLogger returns an *slog.Logger that delegates to a logrus.FieldLogger.
func NewLogger(logger logrus.FieldLogger) *slog.Logger {
	return slog.New(&logrusHandler{field: logger})
}

// NewGoKitLogger returns a go-kit log.Logger that delegates to a logrus.FieldLogger.
// This is needed for the legacy glog-gokit and klog-gokit shims that are
// installed via go.mod replace directives — those still want a go-kit
// log.Logger, not an slog.Logger.
func NewGoKitLogger(logger logrus.FieldLogger) gokitlog.Logger {
	return gokitLogger{field: logger}
}

type gokitLogger struct {
	field logrus.FieldLogger
}

var errMissingValue = errors.New("(MISSING)")

func (l gokitLogger) Log(keyvals ...interface{}) error {
	fields := logrus.Fields{}
	var lvl gokitlevel.Value
	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			if keyvals[i] == gokitlevel.Key() {
				if v, ok := keyvals[i+1].(gokitlevel.Value); ok {
					lvl = v
				}
			} else {
				fields[fmt.Sprint(keyvals[i])] = keyvals[i+1]
			}
		} else {
			fields[fmt.Sprint(keyvals[i])] = errMissingValue
		}
	}
	switch lvl {
	case gokitlevel.DebugValue():
		l.field.WithFields(fields).Debug()
	case gokitlevel.WarnValue():
		l.field.WithFields(fields).Warn()
	case gokitlevel.ErrorValue():
		l.field.WithFields(fields).Error()
	case gokitlevel.InfoValue():
		l.field.WithFields(fields).Info()
	default:
		l.field.WithFields(fields).Print()
	}
	return nil
}

type logrusHandler struct {
	field logrus.FieldLogger
	attrs []slog.Attr
	group string
}

func (h *logrusHandler) Enabled(_ context.Context, lvl slog.Level) bool {
	switch lvl {
	case slog.LevelDebug:
		return logrus.IsLevelEnabled(logrus.DebugLevel)
	case slog.LevelInfo:
		return logrus.IsLevelEnabled(logrus.InfoLevel)
	case slog.LevelWarn:
		return logrus.IsLevelEnabled(logrus.WarnLevel)
	case slog.LevelError:
		return logrus.IsLevelEnabled(logrus.ErrorLevel)
	}
	return true
}

func (h *logrusHandler) Handle(_ context.Context, r slog.Record) error {
	fields := logrus.Fields{}
	for _, a := range h.attrs {
		fields[h.qualify(a.Key)] = attrValue(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[h.qualify(a.Key)] = attrValue(a)
		return true
	})
	entry := h.field.WithFields(fields)
	switch r.Level {
	case slog.LevelDebug:
		entry.Debug(r.Message)
	case slog.LevelWarn:
		entry.Warn(r.Message)
	case slog.LevelError:
		entry.Error(r.Message)
	default:
		entry.Info(r.Message)
	}
	return nil
}

func (h *logrusHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := *h
	out.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &out
}

func (h *logrusHandler) WithGroup(name string) slog.Handler {
	out := *h
	if h.group == "" {
		out.group = name
	} else {
		out.group = h.group + "." + name
	}
	return &out
}

func (h *logrusHandler) qualify(key string) string {
	if h.group == "" {
		return key
	}
	return h.group + "." + key
}

func attrValue(a slog.Attr) interface{} {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return v.String()
	case slog.KindInt64:
		return v.Int64()
	case slog.KindUint64:
		return v.Uint64()
	case slog.KindFloat64:
		return v.Float64()
	case slog.KindBool:
		return v.Bool()
	case slog.KindDuration:
		return v.Duration()
	case slog.KindTime:
		return v.Time()
	case slog.KindAny:
		return v.Any()
	}
	return fmt.Sprintf("%v", v.Any())
}
