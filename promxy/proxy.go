package promxy

import (
	"context"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/jacksontj/promxy/proxyquerier"
	"github.com/julienschmidt/httprouter"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/storage/metric"
)

func NewProxy(c *Config) (*Proxy, error) {
	// TODO: validate config
	p := &Proxy{
		ServerGroups: c.ServerGroups,
	}
	p.e = promql.NewEngine(p, nil)
	return p, nil
}

// TODO: rename?
// TODO: move to its own package?
type Proxy struct {
	// Groups of servers to connect to
	ServerGroups [][]string
	// query engine
	e *promql.Engine
}

func (p *Proxy) Querier() (local.Querier, error) {
	return &proxyquerier.ProxyQuerier{p.ServerGroups}, nil
}

func (p *Proxy) ListenAndServe() error {
	// TODO: instrument these routes
	// TODO: check that all of these implement all the same params (maybe use the same tests if the have them?)
	router := httprouter.New()

	router.GET("/api/v1/query", CORSWrap(p.queryHandler))
	router.GET("/api/v1/query_range", CORSWrap(p.queryRangeHandler))

	router.GET("/api/v1/series", CORSWrap(p.seriesHandler))

	router.GET("/api/v1/label/:name/values", CORSWrap(p.labelValuesHandler))
	/*



	   r.Get("/series", instr("series", api.series))
	   r.Del("/series", instr("drop_series", api.dropSeries))

	   r.Get("/targets", instr("targets", api.targets))
	   r.Get("/alertmanagers", instr("alertmanagers", api.alertmanagers))

	   r.Get("/status/config", instr("config", api.serveConfig))
	   r.Post("/read", prometheus.InstrumentHandler("read", http.HandlerFunc(api.remoteRead)))


	*/

	router.NotFound = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Have our fallback rules
		if strings.HasPrefix(r.URL.Path, "/api") {
			http.NotFound(w, r)
		} else {
			// For all remainingunknown paths we'll simply proxy them to *a* prometheus host
			p.proxyHandler(w, r)
		}

	})
	return http.ListenAndServe(":8082", router)
}

// Handler to proxy requests to *a* server in serverGroups
func (p *Proxy) proxyHandler(w http.ResponseWriter, r *http.Request) {

	serverGroup := p.ServerGroups[rand.Int()%len(p.ServerGroups)]
	server := serverGroup[rand.Int()%len(serverGroup)]
	// TODO: failover
	parsedUrl, _ := url.Parse(server)

	proxy := httputil.NewSingleHostReverseProxy(parsedUrl)
	proxy.ServeHTTP(w, r)
}

// Handler for /query
func (p *Proxy) queryHandler(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	ts, err := promhttputil.ParseTime(r.URL.Query().Get("time"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 	query?_=1507411944.663&query=scrape_duration_seconds&time=1507412244.663
	q, err := p.e.NewInstantQuery(r.URL.Query().Get("query"), ts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := q.Exec(r.Context())

	if result.Err != nil {
		// TODO: type switch on all of them
		http.Error(w, result.Err.Error(), http.StatusInternalServerError)
		return
	}

	data := &promhttputil.QueryData{
		ResultType: result.Value.Type(),
		Result:     result.Value,
	}
	promhttputil.Respond(w, data)
}

// Handler for /query_range
func (p *Proxy) queryRangeHandler(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	ctx := r.Context()

	start, err := promhttputil.ParseTime(r.URL.Query().Get("start"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	end, err := promhttputil.ParseTime(r.URL.Query().Get("end"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	interval, err := promhttputil.ParseDuration(r.URL.Query().Get("step"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// TODO: better, context values should be a specific type
	ctx = context.WithValue(ctx, "step", interval)

	q, err := p.e.NewRangeQuery(r.URL.Query().Get("query"), start, end, interval)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	result := q.Exec(ctx)

	if result.Err != nil {
		// TODO: type switch on all of them
		http.Error(w, result.Err.Error(), http.StatusInternalServerError)
		return
	}

	data := &promhttputil.QueryData{
		ResultType: result.Value.Type(),
		Result:     result.Value,
	}
	promhttputil.Respond(w, data)
}

func (p *Proxy) seriesHandler(w http.ResponseWriter, r *http.Request, _ httprouter.Params) {
	r.ParseForm()
	if len(r.Form["match[]"]) == 0 {
		http.Error(w, "no match[] parameter provided", http.StatusInternalServerError)
		return
	}

	var start model.Time
	if t := r.FormValue("start"); t != "" {
		var err error
		start, err = promhttputil.ParseTime(t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		start = model.Earliest
	}

	var end model.Time
	if t := r.FormValue("end"); t != "" {
		var err error
		end, err = promhttputil.ParseTime(t)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		end = model.Latest
	}

	var matcherSets []metric.LabelMatchers
	for _, s := range r.Form["match[]"] {
		matchers, err := promql.ParseMetricSelector(s)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		matcherSets = append(matcherSets, matchers)
	}

	q, err := p.Querier()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer q.Close()

	res, err := q.MetricsForLabelMatchers(r.Context(), start, end, matcherSets...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	metrics := make([]model.Metric, 0, len(res))
	for _, met := range res {
		metrics = append(metrics, met.Metric)
	}

	promhttputil.Respond(w, metrics)
}

func (p *Proxy) labelValuesHandler(w http.ResponseWriter, r *http.Request, ps httprouter.Params) {
	name := ps.ByName("name")

	if !model.LabelNameRE.MatchString(name) {
		http.Error(w, "name doesn't match label RE", http.StatusInternalServerError)
		return
	}
	q, err := p.Querier()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer q.Close()

	vals, err := q.LabelValuesForLabelName(r.Context(), model.LabelName(name))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sort.Sort(vals)

	promhttputil.Respond(w, vals)
}
