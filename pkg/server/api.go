package server

import (
	"crypto/tls"
	"github.com/jacksontj/promxy/pkg/logging"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/common/log"
	"github.com/prometheus/exporter-toolkit/web"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"net/http"
	"time"
)

func Placeholder(bindAddr string, logFormat string, webReadTimeout time.Duration, accessLogOut io.Writer, router *httprouter.Router, tlsConfigFile string) (*http.Server, error) {
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

	if tlsConfigFile == "" {
		srv := &http.Server{
			Addr:        bindAddr,
			Handler:     handler,
			TLSConfig:   nil,
			ReadTimeout: webReadTimeout,
		}

		go func() {
			logrus.Infof("promxy starting with HTTP...")
			if err := srv.ListenAndServe(); err != nil {
				if err == http.ErrServerClosed {
					return
				}
				log.Errorf("Error listening: %v", err)
			}
		}()
		return srv, nil
	}

	tlsConfig, err := parseConfigFile(tlsConfigFile)
	if err != nil {
		return nil, err
	}

	srv := &http.Server{
		Addr:        bindAddr,
		Handler:     handler,
		TLSConfig:   tlsConfig,
		ReadTimeout: webReadTimeout,
	}

	go func() {
		logrus.Infof("promxy starting with TLS...")
		if err := srv.ListenAndServeTLS("", ""); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			log.Errorf("Error listening: %v", err)
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
