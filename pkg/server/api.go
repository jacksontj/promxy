package server

import (
	"crypto/tls"
	"io"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/logging"
)

func CreateAndStart(bindAddr string, logFormat string, webReadTimeout time.Duration, accessLogOut io.Writer, router *httprouter.Router, tlsConfigFile string) (*http.Server, error) {
	handler := createHandler(accessLogOut, router, logFormat)

	srv := &http.Server{
		Addr:        bindAddr,
		Handler:     handler,
		ReadTimeout: webReadTimeout,
	}

	if tlsConfigFile == "" {
		return createAndStartHTTP(srv)
	}

	return createAndStartHTTPS(srv, tlsConfigFile)
}

func createHandler(accessLogOut io.Writer, router *httprouter.Router, logFormat string) http.Handler {
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

func createAndStartHTTP(srv *http.Server) (*http.Server, error) {
	srv.TLSConfig = nil

	go func() {
		logrus.Infof("promxy starting with HTTP...")
		if err := srv.ListenAndServe(); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			logrus.Errorf("Error listening: %v", err)
		}
	}()
	return srv, nil
}

func createAndStartHTTPS(srv *http.Server, tlsConfigFile string) (*http.Server, error) {
	tlsConfig, err := parseConfigFile(tlsConfigFile)
	if err != nil {
		return nil, err
	}

	srv.TLSConfig = tlsConfig

	go func() {
		logrus.Infof("promxy starting with TLS...")
		if err := srv.ListenAndServeTLS("", ""); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			logrus.Errorf("Error listening: %v", err)
		}
	}()
	return srv, nil
}

func parseConfigFile(tlsConfigFile string) (*tls.Config, error) {
	content, err := ioutil.ReadFile(tlsConfigFile)
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
