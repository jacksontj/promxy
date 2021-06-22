package server

import (
	"github.com/jacksontj/promxy/pkg/logging"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/common/log"
	"github.com/sirupsen/logrus"
	"io"
	"net/http"
	"time"
)

func Placeholder(bindAddr string, logFormat string, webReadTimeout time.Duration, accessLogOut io.Writer, router *httprouter.Router) *http.Server{
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

	//if opts.WebConfigFile == "" {
	srv := &http.Server{
		Addr:        bindAddr,
		Handler:     handler,
		TLSConfig:   nil,
		ReadTimeout: webReadTimeout,
	}

	go func() {
		logrus.Infof("promxy starting")
		if err := srv.ListenAndServe(); err != nil {
			if err == http.ErrServerClosed {
				return
			}
			log.Errorf("Error listening: %v", err)
		}
	}()
	//}
	//TODO return err
	return srv
}