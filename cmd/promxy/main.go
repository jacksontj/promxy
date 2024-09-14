package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	_ "net/http/pprof"

	kitlog "github.com/go-kit/log"
	"github.com/golang/glog"
	"github.com/grafana/regexp"
	"github.com/jessevdk/go-flags"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/version"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	_ "github.com/prometheus/prometheus/discovery/install" // Register service discovery implementations.
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	promlogging "github.com/prometheus/prometheus/util/logging"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/prometheus/prometheus/web"
	"github.com/sirupsen/logrus"
	"go.uber.org/atomic"
	"k8s.io/klog"

	"github.com/jacksontj/promxy/pkg/alertbackfill"
	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/logging"
	"github.com/jacksontj/promxy/pkg/middleware"
	"github.com/jacksontj/promxy/pkg/proxystorage"
	"github.com/jacksontj/promxy/pkg/server"
)

var (
	configSuccess = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "prometheus_config_last_reload_successful",
		Help: "Whether the last configuration reload attempt was successful.",
	})
	configSuccessTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "prometheus_config_last_reload_success_timestamp_seconds",
		Help: "Timestamp of the last successful configuration reload.",
	})

	reloadTime = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "process_reload_time_seconds",
		Help: "Last reload (SIGHUP) time of the process since unix epoch in seconds.",
	})
)

func init() {
	prometheus.MustRegister(version.NewCollector("promxy"))
}

type cliOpts struct {
	Version     bool `long:"version" short:"v" description:"print out version and exit"`
	CheckConfig bool `long:"check-config" description:"check config files and exit"`

	BindAddr         string `long:"bind-addr" description:"address for promxy to listen on" default:":8082"`
	ConfigFile       string `long:"config" description:"path to the config file" default:"config.yaml"`
	LogLevel         string `long:"log-level" description:"Log level" default:"info"`
	LogFormat        string `long:"log-format" description:"Log format(text|json)" default:"text"`
	LogMaxFormPrefix int    `long:"log-max-form-prefix" description:"Max prefix for form values in log entries" default:"256"`

	WebConfigFile      string        `long:"web.config.file" description:"[EXPERIMENTAL] Path to configuration file that can enable TLS or authentication."`
	WebCORSOriginRegex string        `long:"web.cors.origin" description:"Regex for CORS origin. It is fully anchored." default:".*"`
	WebReadTimeout     time.Duration `long:"web.read-timeout" description:"Maximum duration before timing out read of the request, and closing idle connections." default:"5m"`

	MetricsPath  string   `long:"metrics-path" description:"URL path for the prometheus metrics endpoint." default:"/metrics"`
	ProxyHeaders []string `long:"proxy-headers" env:"PROXY_HEADERS" description:"a list of headers to proxy to downstream servergroups."`

	ExternalURL     string `long:"web.external-url" description:"The URL under which Prometheus is externally reachable (for example, if Prometheus is served via a reverse proxy). Used for generating relative and absolute links back to Prometheus itself. If the URL has a path portion, it will be used to prefix all HTTP endpoints served by Prometheus. If omitted, relevant URL components will be derived automatically."`
	RoutePrefix     string `long:"web.route-prefix" description:"Prefix for the internal routes of web endpoints. Defaults to path of --web.external-url."`
	EnableLifecycle bool   `long:"web.enable-lifecycle" description:"Enable shutdown and reload via HTTP request."`

	QueryTimeout        time.Duration `long:"query.timeout" description:"Maximum time a query may take before being aborted." default:"2m"`
	QueryMaxSamples     int           `long:"query.max-samples" description:"Maximum number of samples a single query can load into memory. Note that queries will fail if they would load more samples than this into memory, so this also limits the number of samples a query can return." default:"50000000"`
	QueryLookbackDelta  time.Duration `long:"query.lookback-delta" description:"The maximum lookback duration for retrieving metrics during expression evaluations." default:"5m"`
	QueryMaxConcurrency int           `long:"query.max-concurrency" default:"-1" description:"Maximum number of queries executed concurrently."`
	LocalStoragePath    string        `long:"storage.tsdb.path" description:"Base path for metrics storage."`

	RemoteReadMaxConcurrency int `long:"remote-read.max-concurrency" description:"Maximum number of concurrent remote read calls." default:"10"`

	NotificationQueueCapacity int           `long:"alertmanager.notification-queue-capacity" description:"The capacity of the queue for pending alert manager notifications." default:"10000"`
	AccessLogDestination      string        `long:"access-log-destination" description:"where to log access logs, options (none, stderr, stdout)" default:"stdout"`
	ForOutageTolerance        time.Duration `long:"rules.alert.for-outage-tolerance" description:"Max time to tolerate prometheus outage for restoring for state of alert." default:"1h"`
	ForGracePeriod            time.Duration `long:"rules.alert.for-grace-period" description:"Minimum duration between alert and restored for state. This is maintained only for alerts with configured for time greater than grace period." default:"10m"`
	ResendDelay               time.Duration `long:"rules.alert.resend-delay" description:"Minimum amount of time to wait before resending an alert to Alertmanager." default:"1m"`
	AlertBackfill             bool          `long:"rules.alertbackfill" description:"Enable promxy to recalculate alert state on startup when the downstream datastore doesn't have an ALERTS_FOR_STATE"`

	ShutdownDelay   time.Duration `long:"http.shutdown-delay" description:"time to wait before shutting down the http server, this allows for a grace period for upstreams (e.g. LoadBalancers) to discover the new stopping status through healthchecks" default:"10s"`
	ShutdownTimeout time.Duration `long:"http.shutdown-timeout" description:"max time to wait for a graceful shutdown of the HTTP server" default:"60s"`
}

func (c *cliOpts) ToFlags() map[string]string {
	tmp := make(map[string]string)
	// TODO: better
	b, _ := json.Marshal(c)
	json.Unmarshal(b, &tmp)
	return tmp
}

var opts cliOpts

func reloadConfig(noStepSuqueryInterval *safePromQLNoStepSubqueryInterval, rls ...proxyconfig.Reloadable) (err error) {
	defer func() {
		if err == nil {
			configSuccess.Set(1)
			configSuccessTime.SetToCurrentTime()
		} else {
			configSuccess.Set(0)
		}
	}()

	cfg, err := proxyconfig.ConfigFromFile(opts.ConfigFile)
	if err != nil {
		return fmt.Errorf("error loading cfg: %v", err)
	}

	failed := false
	for _, rl := range rls {
		if err := rl.ApplyConfig(cfg); err != nil {
			logrus.Errorf("failed to apply configuration: %v", err)
			failed = true
		}
	}

	if failed {
		return fmt.Errorf("one or more errors occurred while applying new configuration")
	}
	noStepSuqueryInterval.Set(cfg.PromConfig.GlobalConfig.EvaluationInterval)
	reloadTime.Set(float64(time.Now().Unix()))
	return nil
}

func main() {
	// Wait for reload or termination signals. Start the handler for SIGHUP as
	// early as possible, but ignore it until we are ready to handle reloading
	// our config.
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)

	reloadables := make([]proxyconfig.Reloadable, 0)

	parser := flags.NewParser(&opts, flags.Default)
	if _, err := parser.Parse(); err != nil {
		// If the error was from the parser, then we can simply return
		// as Parse() prints the error already
		if _, ok := err.(*flags.Error); ok {
			os.Exit(1)
		}
		logrus.Fatalf("error parsing flags: %v", err)
	}

	if opts.Version {
		fmt.Println(version.Print("promxy"))
		os.Exit(0)
	}

	// CheckConfig simply will load the config, check for errors, and exit
	if opts.CheckConfig {
		if _, err := proxyconfig.ConfigFromFile(opts.ConfigFile); err != nil {
			logrus.Fatalf("Error loading cfg: %v", err)
		}
		fmt.Printf("%s if valid promxy config file syntax\n", opts.ConfigFile)
		os.Exit(0)
	}

	logging.SetMaxFormPrefix(opts.LogMaxFormPrefix)

	// Use log level
	level, err := logrus.ParseLevel(opts.LogLevel)
	if err != nil {
		logrus.Fatalf("Unknown log level %s: %v", opts.LogLevel, err)
	}
	logrus.SetLevel(level)

	var formatter logrus.Formatter
	switch opts.LogFormat {
	case "json":
		formatter = &logrus.JSONFormatter{}
	default:
		// Set the log format to have a reasonable timestamp
		formatter = &logrus.TextFormatter{
			FullTimestamp: true,
		}
	}

	logrus.SetFormatter(formatter)

	// Above level 6, the k8s client would log bearer tokens in clear-text.
	glog.ClampLevel(6)
	glog.SetLogger(logging.NewLogger(logrus.WithField("component", "k8s_client_runtime").Logger))

	// Above level 6, the k8s client would log bearer tokens in clear-text.
	klog.ClampLevel(6)
	klog.SetLogger(logging.NewLogger(logrus.WithField("component", "k8s_client_runtime").Logger))

	// Create base context for this daemon
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	noStepSubqueryInterval := &safePromQLNoStepSubqueryInterval{}
	noStepSubqueryInterval.Set(config.DefaultGlobalConfig.EvaluationInterval)

	// Reload ready -- channel to close once we are ready to start reloaders
	reloadReady := make(chan struct{})

	// Create the proxy storage
	var proxyStorage storage.Storage

	ps, err := proxystorage.NewProxyStorage(noStepSubqueryInterval.Get)
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}
	reloadables = append(reloadables, ps)
	proxyStorage = ps

	logCfg := &promlog.Config{
		Level:  &promlog.AllowedLevel{},
		Format: &promlog.AllowedFormat{},
	}
	if err := logCfg.Level.Set("info"); err != nil {
		logrus.Fatalf("Unable to set log level: %v", err)
	}

	logger := promlog.New(logCfg)

	engineOpts := promql.EngineOpts{
		Reg:                      prometheus.DefaultRegisterer,
		Timeout:                  opts.QueryTimeout,
		MaxSamples:               opts.QueryMaxSamples,
		NoStepSubqueryIntervalFn: noStepSubqueryInterval.Get,
		LookbackDelta:            opts.QueryLookbackDelta,

		// EnableAtModifier and EnableNegativeOffset have to be
		// always on for regular PromQL as of Prometheus v2.33.
		EnableAtModifier:     true,
		EnableNegativeOffset: true,
	}

	if opts.QueryMaxConcurrency != -1 {
		if opts.LocalStoragePath == "" {
			logrus.Fatalf("local storage path must be defined if you wish to enable max query concurrency limits")
		}
		engineOpts.ActiveQueryTracker = promql.NewActiveQueryTracker(opts.LocalStoragePath, opts.QueryMaxConcurrency, kitlog.With(logger, "component", "activeQueryTracker"))
	}

	engine := promql.NewEngine(engineOpts)
	engine.NodeReplacer = ps.NodeReplacer

	externalUrl, err := computeExternalURL(opts.ExternalURL, opts.BindAddr)
	if err != nil {
		logrus.Fatalf("Unable to parse external URL %s", "tmp")
	}

	// Alert notifier
	notifierManager := notifier.NewManager(
		&notifier.Options{
			Registerer:    prometheus.DefaultRegisterer,
			QueueCapacity: opts.NotificationQueueCapacity,
		},
		kitlog.With(logger, "component", "notifier"),
	)
	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(notifierManager))

	discoveryManagerNotify := discovery.NewManager(ctx, kitlog.With(logger, "component", "discovery manager notify"))

	reloadables = append(reloadables,
		proxyconfig.WrapPromReloadable(&proxyconfig.ApplyConfigFunc{func(cfg *config.Config) error {
			c := make(map[string]discovery.Configs)
			for k, v := range cfg.AlertingConfig.AlertmanagerConfigs.ToMap() {
				c[k] = v.ServiceDiscoveryConfigs
			}
			return discoveryManagerNotify.ApplyConfig(c)
		}}),
	)

	go func() {
		if err := discoveryManagerNotify.Run(); err != nil {
			logrus.Errorf("Error running Notify discovery manager: %v", err)
		} else {
			logrus.Infof("Notify discovery manager stopped")
		}
	}()
	go func() {
		<-reloadReady
		notifierManager.Run(discoveryManagerNotify.SyncCh())
		logrus.Infof("Notifier manager stopped")
	}()

	var ruleQueryable storage.Queryable
	// If alertbackfill is enabled; wire it up!
	if opts.AlertBackfill {
		ruleQueryable = alertbackfill.NewAlertBackfillQueryable(engine, proxyStorage)
	} else {
		ruleQueryable = proxyStorage
	}
	ruleManager := rules.NewManager(&rules.ManagerOptions{
		Context:         ctx,         // base context for all background tasks
		ExternalURL:     externalUrl, // URL listed as URL for "who fired this alert"
		QueryFunc:       rules.EngineQueryFunc(engine, proxyStorage),
		NotifyFunc:      sendAlerts(notifierManager, externalUrl.String()),
		Appendable:      proxyStorage,
		Queryable:       ruleQueryable,
		Logger:          logger,
		Registerer:      prometheus.DefaultRegisterer,
		OutageTolerance: opts.ForOutageTolerance,
		ForGracePeriod:  opts.ForGracePeriod,
		ResendDelay:     opts.ResendDelay,
	})

	if q, ok := ruleQueryable.(*alertbackfill.AlertBackfillQueryable); ok {
		q.SetRuleGroupFetcher(ruleManager.RuleGroups)
	}

	go ruleManager.Run()

	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(&proxyconfig.ApplyConfigFunc{func(cfg *config.Config) error {
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
		if err := ruleManager.Update(time.Duration(cfg.GlobalConfig.EvaluationInterval), files, cfg.GlobalConfig.ExternalLabels, externalUrl.String(), nil); err != nil {
			return err
		}

		if cfg.RemoteWriteConfigs == nil {
			ruleList := ruleManager.Rules()
			// check for any recording rules, if we find any lets log a fatal and stop
			for _, rule := range ruleList {
				if _, ok := rule.(*rules.RecordingRule); ok {
					return fmt.Errorf("promxy doesn't support recording rules: %s", rule)
				}
			}

			if len(ruleList) > 0 {
				logrus.Warning("Alerting rules are configured but no remote_write endpoint is configured.")
			}
		}

		return nil
	}}))

	// PromQL query engine reloadable
	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(&proxyconfig.ApplyConfigFunc{func(cfg *config.Config) error {
		if cfg.GlobalConfig.QueryLogFile == "" {
			engine.SetQueryLogger(nil)
			return nil
		}

		l, err := promlogging.NewJSONFileLogger(cfg.GlobalConfig.QueryLogFile)
		if err != nil {
			return err
		}
		engine.SetQueryLogger(l)

		return nil
	}}))

	// We need an empty scrape manager, simply to make the API not panic and error out
	scrapeManager := scrape.NewManager(nil, kitlog.With(logger, "component", "scrape manager"), nil)

	webOptions := &web.Options{
		Registerer:      prometheus.DefaultRegisterer,
		Gatherer:        prometheus.DefaultGatherer,
		Context:         ctx,
		Storage:         proxyStorage,
		LocalStorage:    ps,
		ExemplarStorage: ps,
		QueryEngine:     engine,
		ScrapeManager:   scrapeManager,
		RuleManager:     ruleManager,
		Notifier:        notifierManager,
		LookbackDelta:   opts.QueryLookbackDelta,

		RemoteReadConcurrencyLimit: opts.RemoteReadMaxConcurrency,

		EnableLifecycle: opts.EnableLifecycle,

		Flags:       opts.ToFlags(),
		RoutePrefix: opts.RoutePrefix,
		ExternalURL: externalUrl,
		Version: &web.PrometheusVersion{
			Version:   version.Version,
			Revision:  version.Revision,
			Branch:    version.Branch,
			BuildUser: version.BuildUser,
			BuildDate: version.BuildDate,
			GoVersion: version.GoVersion,
		},
	}

	webOptions.CORSOrigin, err = compileCORSRegexString(opts.WebCORSOriginRegex)
	if err != nil {
		logrus.Fatalf("Error parsing CORS regex: %v", err)
	}

	// Default -web.route-prefix to path of -web.external-url.
	if webOptions.RoutePrefix == "" {
		webOptions.RoutePrefix = externalUrl.Path
	}
	// RoutePrefix must always be at least '/'.
	webOptions.RoutePrefix = "/" + strings.Trim(webOptions.RoutePrefix, "/")

	webHandler := web.New(logger, webOptions)
	reloadables = append(reloadables, proxyconfig.WrapPromReloadable(webHandler))
	webHandler.SetReady(true)

	apiPrefix := path.Join(webOptions.RoutePrefix, "/api/v1")
	// Register API endpoint with correct route prefix
	webHandler.Getv1API().Register(webHandler.GetRouter().WithPrefix(apiPrefix))

	// Create our router
	r := httprouter.New()

	r.HandlerFunc("GET", opts.MetricsPath, promhttp.Handler().ServeHTTP)

	stopping := false
	r.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Have our fallback rules
		if strings.HasPrefix(r.URL.Path, path.Join(webOptions.RoutePrefix, "/debug")) {
			http.StripPrefix(strings.Trim(webOptions.RoutePrefix, "/"), http.DefaultServeMux).ServeHTTP(w, r)
		} else if r.URL.Path == path.Join(webOptions.RoutePrefix, "/-/ready") {
			if stopping {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprintf(w, "Promxy is Stopping.\n")
			} else {
				webHandler.GetRouter().ServeHTTP(w, r)
			}
		} else if r.URL.Path == path.Join(webOptions.RoutePrefix, "/api/v1/status/config") {
			ps.ConfigHandler(w, r)
		} else if r.URL.Path == path.Join(webOptions.RoutePrefix, "/api/v1/metadata") {
			ps.MetadataHandler(w, r)
		} else {
			// all else we send direct to the local prometheus UI
			webHandler.GetRouter().ServeHTTP(w, r)
		}
	})

	if err := reloadConfig(noStepSubqueryInterval, reloadables...); err != nil {
		logrus.Fatalf("Error loading config: %s", err)
	}

	configSuccess.Set(1)
	configSuccessTime.SetToCurrentTime()

	close(reloadReady)

	// Set up access logger
	var accessLogOut io.Writer
	switch strings.ToLower(opts.AccessLogDestination) {
	case "stderr":
		accessLogOut = os.Stderr
	case "stdout":
		accessLogOut = os.Stdout
	case "none":
	default:
		logrus.Fatalf("Invalid AccessLogDestination: %s", opts.AccessLogDestination)
	}

	srv, err := server.CreateAndStart(opts.BindAddr, opts.LogFormat, opts.WebReadTimeout, accessLogOut, middleware.NewProxyHeaders(r, opts.ProxyHeaders), opts.WebConfigFile)
	if err != nil {
		logrus.Fatalf("Error creating server: %v", err)
	}

	// wait for signals etc.
	for {
		select {
		case rc := <-webHandler.Reload():
			logrus.Infof("Reloading config")
			if err := reloadConfig(noStepSubqueryInterval, reloadables...); err != nil {
				logrus.Errorf("Error reloading config: %s", err)
				rc <- err
			} else {
				rc <- nil
			}
		case sig := <-sigs:
			switch sig {
			case syscall.SIGHUP:
				logrus.Infof("Reloading config")
				if err := reloadConfig(noStepSubqueryInterval, reloadables...); err != nil {
					logrus.Errorf("Error reloading config: %s", err)
				}
			case syscall.SIGTERM, syscall.SIGINT:
				logrus.Info("promxy received exit signal, starting graceful shutdown")

				// Stop all services we are running
				stopping = true        // start failing healthchecks
				notifierManager.Stop() // stop alert notifier
				ruleManager.Stop()     // Stop rule manager

				if opts.ShutdownDelay > 0 {
					logrus.Infof("promxy delaying shutdown by %v", opts.ShutdownDelay)
					time.Sleep(opts.ShutdownDelay)
				}
				logrus.Infof("promxy exiting with timeout: %v", opts.ShutdownTimeout)
				defer cancel()
				if opts.ShutdownTimeout > 0 {
					ctx, cancel = context.WithTimeout(ctx, opts.ShutdownTimeout)
					defer cancel()
				}
				srv.Shutdown(ctx)
				return
			default:
				logrus.Errorf("Uncaught signal: %v", sig)
			}

		}
	}
}

// sendAlerts implements the rules.NotifyFunc for a Notifier.
// It filters any non-firing alerts from the input.
func sendAlerts(n *notifier.Manager, externalURL string) rules.NotifyFunc {
	return func(ctx context.Context, expr string, alerts ...*rules.Alert) {
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
	}
}

func startsOrEndsWithQuote(s string) bool {
	return strings.HasPrefix(s, "\"") || strings.HasPrefix(s, "'") ||
		strings.HasSuffix(s, "\"") || strings.HasSuffix(s, "'")
}

// computeExternalURL computes a sanitized external URL from a raw input. It infers unset
// URL parts from the OS and the given listen address.
func computeExternalURL(u, listenAddr string) (*url.URL, error) {
	if u == "" {
		hostname, err := os.Hostname()
		if err != nil {
			return nil, err
		}
		_, port, err := net.SplitHostPort(listenAddr)
		if err != nil {
			return nil, err
		}
		u = fmt.Sprintf("http://%s:%s/", hostname, port)
	}

	if startsOrEndsWithQuote(u) {
		return nil, fmt.Errorf("URL must not begin or end with quotes")
	}

	eu, err := url.Parse(u)
	if err != nil {
		return nil, err
	}

	ppref := strings.TrimRight(eu.Path, "/")
	if ppref != "" && !strings.HasPrefix(ppref, "/") {
		ppref = "/" + ppref
	}
	eu.Path = ppref

	return eu, nil
}

// compileCORSRegexString compiles given string and adds anchors
func compileCORSRegexString(s string) (*regexp.Regexp, error) {
	r, err := relabel.NewRegexp(s)
	if err != nil {
		return nil, err
	}
	return r.Regexp, nil
}

type safePromQLNoStepSubqueryInterval struct {
	value atomic.Int64
}

func durationToInt64Millis(d time.Duration) int64 {
	return int64(d / time.Millisecond)
}
func (i *safePromQLNoStepSubqueryInterval) Set(ev model.Duration) {
	i.value.Store(durationToInt64Millis(time.Duration(ev)))
}

func (i *safePromQLNoStepSubqueryInterval) Get(int64) int64 {
	return i.value.Load()
}
