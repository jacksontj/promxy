package server

import (
	"crypto/tls"
	"io"
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

type WebConfigFile struct {
	TlsConfigFile string
	WebConfigFile string
}

func (f *WebConfigFile) isSet() bool {
	return f.TlsConfigFile != "" || f.WebConfigFile != ""
}

func (f *WebConfigFile) useWebConfig() bool {
	return f.WebConfigFile != ""
}

func hasTLSConfig(c *web.TLSStruct) bool {
	if c.TLSCertPath == "" && c.TLSKeyPath == "" && c.ClientAuth == "" && c.ClientCAs == "" {
		return false
	}
	return true
}

func CreateAndStart(bindAddr string, logFormat string, webReadTimeout time.Duration, accessLogOut io.Writer, router http.Handler, webConfigFile WebConfigFile) (*http.Server, error) {
	handler := createHandler(accessLogOut, router, logFormat)

	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, err
	}
	srv := &http.Server{
		Addr:        ln.Addr().String(),
		Handler:     handler,
		ReadTimeout: webReadTimeout,
	}

	var webConfig *web.Config
	if webConfigFile.useWebConfig() {
		webConfig, err = parseWebConfigFile(webConfigFile.WebConfigFile)
		if err != nil {
			return nil, err
		}
	}

	if !webConfigFile.isSet() || webConfigFile.useWebConfig() && !hasTLSConfig(&webConfig.TLSConfig) {
		return createAndStartHTTP(srv, ln, webConfigFile)
	}

	return createAndStartHTTPS(srv, ln, webConfigFile)
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

func createAndStartHTTP(srv *http.Server, ln net.Listener, webConfigFile WebConfigFile) (*http.Server, error) {
	srv.TLSConfig = nil

	go func() {
		logrus.Infof("promxy starting with HTTP...")
		var err error
		if webConfigFile.useWebConfig() {
			logger := logging.NewLogger(logrus.StandardLogger())
			err = web.Serve(ln, srv, webConfigFile.WebConfigFile, logger)
		} else {
			err = srv.Serve(ln)
		}
		if err != nil {
			if err == http.ErrServerClosed {
				return
			}
			logrus.Errorf("Error listening: %v", err)
		}
	}()
	return srv, nil
}

func createAndStartHTTPS(srv *http.Server, ln net.Listener, webConfigFile WebConfigFile) (*http.Server, error) {
	// Validate the config and get tlsConfig from it
	tlsConfig, err := parseConfigFile(webConfigFile)
	if err != nil {
		return nil, err
	}

	go func() {
		logrus.Infof("promxy starting with TLS...")
		var err error
		if webConfigFile.useWebConfig() {
			logger := logging.NewLogger(logrus.StandardLogger())
			err = web.Serve(ln, srv, webConfigFile.WebConfigFile, logger)
		} else {
			srv.TLSConfig = tlsConfig
			err = srv.ServeTLS(ln, "", "")
		}
		if err != nil {
			if err == http.ErrServerClosed {
				return
			}
			logrus.Errorf("Error listening: %v", err)
		}
	}()
	return srv, nil
}

func parseConfigFile(webConfigFile WebConfigFile) (*tls.Config, error) {
	if webConfigFile.useWebConfig() {
		webConfig, err := parseWebConfigFile(webConfigFile.WebConfigFile)
		if err != nil {
			return nil, err
		}
		tlsConfig, err := web.ConfigToTLSConfig(&webConfig.TLSConfig)
		if err != nil {
			return nil, err
		}

		return tlsConfig, err
	} else {
		return parseTlsConfigFile(webConfigFile.TlsConfigFile)
	}
}

func parseTlsConfigFile(tlsConfigFile string) (*tls.Config, error) {
	content, err := os.ReadFile(tlsConfigFile)
	if err != nil {
		return nil, err
	}
	tlsStruct := &web.TLSStruct{
		MinVersion:               tls.VersionTLS12,
		MaxVersion:               tls.VersionTLS13,
		PreferServerCipherSuites: true,
	}
	err = yaml.UnmarshalStrict(content, tlsStruct)
	if err != nil {
		return nil, err
	}

	tlsConfig, err := web.ConfigToTLSConfig(tlsStruct)
	if err != nil {
		return nil, err
	}

	return tlsConfig, err
}

func parseWebConfigFile(webConfigFile string) (*web.Config, error) {
	content, err := os.ReadFile(webConfigFile)
	if err != nil {
		return nil, err
	}
	webConfig := &web.Config{
		TLSConfig: web.TLSStruct{
			MinVersion:               tls.VersionTLS12,
			MaxVersion:               tls.VersionTLS13,
			PreferServerCipherSuites: true,
		},
	}
	err = yaml.UnmarshalStrict(content, webConfig)
	if err != nil {
		return nil, err
	}

	webConfig.TLSConfig.SetDirectory(filepath.Dir(webConfigFile))
	return webConfig, err
}
