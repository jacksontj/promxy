package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/proxystorage"
	"github.com/jessevdk/go-flags"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"
)

//http://localhost:8080/api/v1/query?query=scrape_duration_seconds%5B1m%5D&time=1507256489.103&_=1507256486365

var opts struct {
	ConfigFile string `long:"config" description:"path to the config file" required:"true"`
}

func reloadConfig(rls ...proxyconfig.Reloadable) error {
	cfg, err := proxyconfig.ConfigFromFile(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("Error loading cfg: %v", err)
	}

	failed := false
	for _, rl := range rls {
		if err := rl.ApplyConfig(cfg); err != nil {
			logrus.Errorf("Failed to apply configuration: %v", err)
			failed = true
		}
	}

	if failed {
		return fmt.Errorf("One or more errors occured while applying new configuration")
	}
	return nil
}

func main() {
	reloadables := make([]proxyconfig.Reloadable, 0)

	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		logrus.Fatalf("Error parsing flags: %v", err)
	}

	var proxyStorage local.Storage

	ps, err := proxystorage.NewProxyStorage()
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}
	reloadables = append(reloadables, ps)
	proxyStorage = ps

	engine := promql.NewEngine(proxyStorage, nil)

	// Register alertmanager stuff
	var (
		// TODO: config option
		Notifier = notifier.New(&notifier.Options{QueueCapacity: 10000}, log.Base())
	)

	// TODO: config option
	u, err := url.Parse("http://localhost:8082")
	if err != nil {
		logrus.Fatalf("Err: %v", err)
	}

	ruleManager := rules.NewManager(&rules.ManagerOptions{
		Notifier:       Notifier,             // Client to send alerts to alertmanager
		SampleAppender: proxyStorage,         // appender for recording rules
		QueryEngine:    engine,               // Engine for querying
		Context:        context.Background(), // base context for all background tasks
		ExternalURL:    u,                    // URL listed as URL for "who fired this alert"
	})

	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(Notifier))
	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(ruleManager))
	go ruleManager.Run()
	go Notifier.Run()

	// TODO:
	cfgFunc := func() config.Config { return config.DefaultConfig }
	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(engine, proxyStorage, nil, nil, cfgFunc, readyFunc)

	apiRouter := route.New()
	api.Register(apiRouter.WithPrefix("/api/v1"))

	// API go to their router
	// Some stuff go to me
	// rest proxy

	// Create our router
	r := httprouter.New()

	// TODO: configurable path
	r.HandlerFunc("GET", "/metrics", prometheus.Handler().ServeHTTP)
	// TODO: additional endpoints?

	r.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Have our fallback rules
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiRouter.ServeHTTP(w, r)
		} else {
			// For all remainingunknown paths we'll simply proxy them to *a* prometheus host
			prometheus.InstrumentHandlerFunc("proxy", ps.ProxyHandler)(w, r)
		}
	})

	if err := reloadConfig(reloadables...); err != nil {
		logrus.Fatalf("Error loading config: %s", err)
	}

	// Wait for reload or termination signals. Start the handler for SIGHUP as
	// early as possible, but ignore it until we are ready to handle reloading
	// our config.
	hup := make(chan os.Signal)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for {
			select {
			case <-hup:
				if err := reloadConfig(reloadables...); err != nil {
					log.Errorf("Error reloading config: %s", err)
				}
			}
		}
	}()

	// TODO: listen address/port option
	http.ListenAndServe(":8082", r)
}
