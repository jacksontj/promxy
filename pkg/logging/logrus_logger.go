// A logrus adapter to the go-kit log.Logger interface.
package logging

import (
	"errors"
	"fmt"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/sirupsen/logrus"
)

type Logger struct {
	field logrus.FieldLogger
}

type Option func(*Logger)

var errMissingValue = errors.New("(MISSING)")

// NewLogger returns a Go kit log.Logger that sends log events to a logrus.Logger.
func NewLogger(logger logrus.FieldLogger, options ...Option) log.Logger {
	l := &Logger{
		field: logger,
	}

	for _, optFunc := range options {
		optFunc(l)
	}

	return l
}

func (l Logger) Log(keyvals ...interface{}) error {
	fields := logrus.Fields{}

	var lvl level.Value

	for i := 0; i < len(keyvals); i += 2 {
		if i+1 < len(keyvals) {
			if keyvals[i] == level.Key() {
				tmpLevel, ok := keyvals[i+1].(level.Value)
				if ok {
					lvl = tmpLevel
				}
			} else {
				fields[fmt.Sprint(keyvals[i])] = keyvals[i+1]
			}
		} else {
			fields[fmt.Sprint(keyvals[i])] = errMissingValue
		}
	}

	switch lvl {
	case level.InfoValue():
		l.field.WithFields(fields).Info()
	case level.ErrorValue():
		l.field.WithFields(fields).Error()
	case level.DebugValue():
		l.field.WithFields(fields).Debug()
	case level.WarnValue():
		l.field.WithFields(fields).Warn()
	default:
		l.field.WithFields(fields).Print()
	}

	return nil
}
