package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/exporter-toolkit/web"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/logging"
)

func CreateAndStart(bindAddr string, logFormat string, webReadTimeout time.Duration, accessLogOut io.Writer, router http.Handler, webConfigFile string) (*http.Server, error) {
	handler := createHandler(accessLogOut, router, logFormat)

	if err := web.Validate(webConfigFile); err != nil {
		return nil, err
	}

	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Addr:        ln.Addr().String(),
		Handler:     handler,
		ReadTimeout: webReadTimeout,
	}

	flags := &web.FlagConfig{WebConfigFile: &webConfigFile}
	logger := slog.New(&logrusSlogHandler{})

	go func() {
		if webConfigFile == "" {
			logrus.Infof("promxy starting with HTTP...")
		} else {
			logrus.Infof("promxy starting with web config %s...", webConfigFile)
		}
		if err := web.Serve(ln, srv, flags, logger); err != nil && err != http.ErrServerClosed {
			logrus.Errorf("Error listening: %v", err)
		}
	}()
	return srv, nil
}

func createHandler(accessLogOut io.Writer, router http.Handler, logFormat string) http.Handler {
	var handler http.Handler
	if accessLogOut == nil {
		handler = router
	} else {
		switch logFormat {
		case "json":
			handler = logging.NewApacheLoggingHandler(router, logging.LogJsonToWriter(accessLogOut))
		default:
			handler = logging.NewApacheLoggingHandler(router, logging.LogToWriter(accessLogOut))
		}
	}

	return handler
}

// logrusSlogHandler forwards exporter-toolkit's slog output to logrus so promxy
// has a single log stream.
type logrusSlogHandler struct {
	attrs []slog.Attr
}

func (h *logrusSlogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *logrusSlogHandler) Handle(_ context.Context, r slog.Record) error {
	fields := logrus.Fields{}
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})
	entry := logrus.WithFields(fields)
	switch {
	case r.Level >= slog.LevelError:
		entry.Error(r.Message)
	case r.Level >= slog.LevelWarn:
		entry.Warn(r.Message)
	case r.Level >= slog.LevelInfo:
		entry.Info(r.Message)
	default:
		entry.Debug(r.Message)
	}
	return nil
}

func (h *logrusSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	merged := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	merged = append(merged, h.attrs...)
	merged = append(merged, attrs...)
	return &logrusSlogHandler{attrs: merged}
}

func (h *logrusSlogHandler) WithGroup(_ string) slog.Handler { return h }
