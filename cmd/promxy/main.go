package main

import (
	"net/http"
	"strings"

	"github.com/jacksontj/promxy/proxystorage"
	"github.com/jessevdk/go-flags"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"
)

//http://localhost:8080/api/v1/query?query=scrape_duration_seconds%5B1m%5D&time=1507256489.103&_=1507256486365

var opts struct {
	ConfigFile string `long:"config" description:"path to the config file" required:"true"`
}

func main() {
	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		logrus.Fatalf("Error parsing flags: %v", err)
	}

	cfg, err := proxystorage.ConfigFromFile(opts.ConfigFile)
	if err != nil {
		logrus.Fatalf("Error loading cfg: %v", err)
	}

	var storage local.Storage

	ps, err := proxystorage.NewProxyStorage(cfg)
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}
	storage = ps

	engine := promql.NewEngine(storage, nil)

	// TODO
	cfgFunc := func() config.Config { return config.DefaultConfig }
	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(engine, storage, nil, nil, cfgFunc, readyFunc)

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

	http.ListenAndServe(":8082", r)
}
