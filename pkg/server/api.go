package server

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/exporter-toolkit/web"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/logging"
)

func CreateAndStart(bindAddr string, logFormat string, webReadTimeout time.Duration, accessLogOut io.Writer, router http.Handler, webConfigFile string) (*http.Server, error) {
	handler := createHandler(accessLogOut, router, logFormat)

	// Prior to delegating the web server to exporter-toolkit, promxy parsed
	// --web.config.file directly into web.TLSConfig, so TLS keys (cert_file,
	// key_file, ...) lived at the top level of the file. exporter-toolkit
	// instead expects them nested under tls_server_config. To avoid breaking
	// existing deployments we transparently keep serving the legacy flat schema.
	legacyTLS, err := legacyTLSConfig(webConfigFile)
	if err != nil {
		return nil, err
	}

	if legacyTLS == nil {
		if err := web.Validate(webConfigFile); err != nil {
			return nil, err
		}
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

	if legacyTLS != nil {
		srv.TLSConfig = legacyTLS
		go func() {
			logrus.Warnf("--web.config.file %q uses the deprecated flat TLS schema; nest these keys under 'tls_server_config:'. Support for the flat schema will be removed in a future release.", webConfigFile)
			logrus.Infof("promxy starting with TLS (legacy web config %s)...", webConfigFile)
			if err := srv.ServeTLS(ln, "", ""); err != nil && err != http.ErrServerClosed {
				logrus.Errorf("Error listening: %v", err)
			}
		}()
		return srv, nil
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

// legacyTLSConfig detects the pre-exporter-toolkit web config schema, in which
// TLS settings lived at the top level of the file instead of nested under
// tls_server_config. It returns a ready-to-use *tls.Config when the file uses
// that legacy schema, or nil when the file is empty, uses the current
// exporter-toolkit schema, or otherwise should be handled by exporter-toolkit.
func legacyTLSConfig(webConfigFile string) (*tls.Config, error) {
	if webConfigFile == "" {
		return nil, nil
	}

	content, err := os.ReadFile(webConfigFile)
	if err != nil {
		return nil, err
	}

	// A top-level map with any exporter-toolkit section is, by definition, the
	// current schema; anything else with content is treated as the legacy flat
	// schema. Empty files fall through to exporter-toolkit, which serves plain
	// HTTP, matching the original behavior.
	var top map[string]interface{}
	if err := yaml.Unmarshal(content, &top); err != nil {
		// Let exporter-toolkit surface the parse error in its own terms.
		return nil, nil
	}
	if len(top) == 0 {
		return nil, nil
	}
	for _, section := range []string{"tls_server_config", "http_server_config", "basic_auth_users"} {
		if _, ok := top[section]; ok {
			return nil, nil
		}
	}

	tlsStruct := &web.TLSConfig{
		MinVersion:               tls.VersionTLS12,
		MaxVersion:               tls.VersionTLS13,
		PreferServerCipherSuites: true,
	}
	if err := yaml.UnmarshalStrict(content, tlsStruct); err != nil {
		return nil, err
	}
	// Resolve relative cert/key paths against the config file's directory, the
	// same as exporter-toolkit does for the current schema.
	tlsStruct.SetDirectory(filepath.Dir(webConfigFile))

	return web.ConfigToTLSConfig(tlsStruct)
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
