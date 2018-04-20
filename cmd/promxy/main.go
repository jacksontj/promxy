package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "net/http/pprof"

	kitlog "github.com/go-kit/kit/log"
	"github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/logging"
	"github.com/jacksontj/promxy/proxystorage"
	"github.com/jessevdk/go-flags"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"
)

//http://localhost:8080/api/v1/query?query=scrape_duration_seconds%5B1m%5D&time=1507256489.103&_=1507256486365

var opts struct {
	BindAddr   string `long:"bind-addr" description:"address for promxy to listen on" default:":8082"`
	ConfigFile string `long:"config" description:"path to the config file" required:"true"`
	LogLevel   string `long:"log-level" description:"Log level" default:"info"`
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
		return fmt.Errorf("One or more errors occurred while applying new configuration")
	}
	return nil
}

func main() {
	reloadables := make([]proxyconfig.Reloadable, 0)

	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		logrus.Fatalf("Error parsing flags: %v", err)
	}

	// Use log level
	level := logrus.InfoLevel
	switch strings.ToLower(opts.LogLevel) {
	case "panic":
		level = logrus.PanicLevel
	case "fatal":
		level = logrus.FatalLevel
	case "error":
		level = logrus.ErrorLevel
	case "warn":
		level = logrus.WarnLevel
	case "info":
		level = logrus.InfoLevel
	case "debug":
		level = logrus.DebugLevel
	default:
		logrus.Fatalf("Unknown log level: %s", opts.LogLevel)
	}
	logrus.SetLevel(level)

	// Set the log format to have a reasonable timestamp
	formatter := &logrus.TextFormatter{
		FullTimestamp: true,
	}
	logrus.SetFormatter(formatter)

	var proxyStorage storage.Storage

	ps, err := proxystorage.NewProxyStorage()
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}
	reloadables = append(reloadables, ps)
	proxyStorage = ps

	// TODO: config for the timeout
	engine := promql.NewEngine(nil, nil, 20, 120*time.Second)

	engine.NodeReplacer = ps.NodeReplacer

	// TODO: config option
	u, err := url.Parse("http://localhost:8082")
	if err != nil {
		logrus.Fatalf("Err: %v", err)
	}

	// Alert notifier
	lvl := promlog.AllowedLevel{}
	if err := lvl.Set("info"); err != nil {
		panic(err)
	}
	logger := promlog.New(lvl)
	notifierManager := notifier.NewManager(&notifier.Options{Registerer: prometheus.DefaultRegisterer}, kitlog.With(logger, "component", "notifier"))
	ruleManager := rules.NewManager(&rules.ManagerOptions{
		Context:     context.Background(), // base context for all background tasks
		ExternalURL: u,                    // URL listed as URL for "who fired this alert"
		NotifyFunc:  sendAlerts(notifierManager, u.String()),
	})
	go ruleManager.Run()

	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(&ApplyConfigFunc{func(cfg *config.Config) error {
		// Get all rule files matching the configuration oaths.
		var files []string
		for _, pat := range cfg.RuleFiles {
			fs, err := filepath.Glob(pat)
			if err != nil {
				// The only error can be a bad pattern.
				return fmt.Errorf("error retrieving rule files for %s: %s", pat, err)
			}
			files = append(files, fs...)
		}
		return ruleManager.Update(time.Duration(cfg.GlobalConfig.EvaluationInterval), files)
	}}))

	// TODO:
	cfgFunc := func() config.Config { return config.DefaultConfig }
	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(
		engine,
		proxyStorage,
		nil,
		nil,
		cfgFunc,
		nil,
		readyFunc,
		nil,
		true,
	)

	apiRouter := route.New()
	api.Register(apiRouter.WithPrefix("/api/v1"))

	// API go to their router
	// Some stuff go to me
	// rest proxy

	// Create our router
	r := httprouter.New()

	// TODO: configurable path
	r.HandlerFunc("GET", "/metrics", prometheus.Handler().ServeHTTP)

	r.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Have our fallback rules
		if strings.HasPrefix(r.URL.Path, "/api/") {
			apiRouter.ServeHTTP(w, r)
		} else if strings.HasPrefix(r.URL.Path, "/debug") {
			http.DefaultServeMux.ServeHTTP(w, r)
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

	// Set up access logger
	loggedRouter := logging.NewApacheLoggingHandler(r, os.Stdout)

	logrus.Infof("promxy starting")
	if err := http.ListenAndServe(opts.BindAddr, loggedRouter); err != nil {
		log.Fatalf("Error listening: %v", err)
	}
}

// sendAlerts implements the rules.NotifyFunc for a Notifier.
// It filters any non-firing alerts from the input.
func sendAlerts(n *notifier.Manager, externalURL string) rules.NotifyFunc {
	return func(ctx context.Context, expr string, alerts ...*rules.Alert) error {
		var res []*notifier.Alert

		for _, alert := range alerts {
			// Only send actually firing alerts.
			if alert.State == rules.StatePending {
				continue
			}
			a := &notifier.Alert{
				StartsAt:     alert.FiredAt,
				Labels:       alert.Labels,
				Annotations:  alert.Annotations,
				GeneratorURL: externalURL + strutil.TableLinkForExpression(expr),
			}
			if !alert.ResolvedAt.IsZero() {
				a.EndsAt = alert.ResolvedAt
			}
			res = append(res, a)
		}

		if len(alerts) > 0 {
			n.Send(res...)
		}
		return nil
	}
}

type ApplyConfigFunc struct {
	f func(*config.Config) error
}

func (a *ApplyConfigFunc) ApplyConfig(cfg *config.Config) error {
	return a.f(cfg)
}
