package proxystorage

import (
	"context"
	"math/rand"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/jacksontj/promxy/proxyquerier"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/storage/metric"
)

func NewProxyStorage(c *Config) (*ProxyStorage, error) {
	// TODO: validate config
	return &ProxyStorage{
		ServerGroups: c.ServerGroups,
	}, nil
}

// TODO: rename?
// TODO: move to its own package?
type ProxyStorage struct {
	// Groups of servers to connect to
	ServerGroups [][]string
}

// Handler to proxy requests to *a* server in serverGroups
func (p *ProxyStorage) ProxyHandler(w http.ResponseWriter, r *http.Request) {

	serverGroup := p.ServerGroups[rand.Int()%len(p.ServerGroups)]
	server := serverGroup[rand.Int()%len(serverGroup)]
	// TODO: failover
	parsedUrl, _ := url.Parse(server)

	proxy := httputil.NewSingleHostReverseProxy(parsedUrl)
	proxy.ServeHTTP(w, r)
}

func (p *ProxyStorage) Querier() (local.Querier, error) {
	return &proxyquerier.ProxyQuerier{p.ServerGroups}, nil
}

// TODO: IMPLEMENT

// Append appends a sample to the underlying storage. Depending on the
// storage implementation, there are different guarantees for the fate
// of the sample after Append has returned. Remote storage
// implementation will simply drop samples if they cannot keep up with
// sending samples. Local storage implementations will only drop metrics
// upon unrecoverable errors.
func (p *ProxyStorage) Append(*model.Sample) error { return nil }

// NeedsThrottling returns true if the underlying storage wishes to not
// receive any more samples. Append will still work but might lead to
// undue resource usage. It is recommended to call NeedsThrottling once
// before an upcoming batch of Append calls (e.g. a full scrape of a
// target or the evaluation of a rule group) and only proceed with the
// batch if NeedsThrottling returns false. In that way, the result of a
// scrape or of an evaluation of a rule group will always be appended
// completely or not at all, and the work of scraping or evaluation will
// not be performed in vain. Also, a call of NeedsThrottling is
// potentially expensive, so limiting the number of calls is reasonable.
//
// Only SampleAppenders for which it is considered critical to receive
// each and every sample should ever return true. SampleAppenders that
// tolerate not receiving all samples should always return false and
// instead drop samples as they see fit to avoid overload.
func (p *ProxyStorage) NeedsThrottling() bool { return false }

// Drop all time series associated with the given label matchers. Returns
// the number series that were dropped.
func (p *ProxyStorage) DropMetricsForLabelMatchers(context.Context, ...*metric.LabelMatcher) (int, error) {
	// TODO: implement
	return 0, nil
}

// Run the various maintenance loops in goroutines. Returns when the
// storage is ready to use. Keeps everything running in the background
// until Stop is called.
func (p *ProxyStorage) Start() error {
	return nil
}

// Stop shuts down the Storage gracefully, flushes all pending
// operations, stops all maintenance loops,and frees all resources.
func (p *ProxyStorage) Stop() error {
	return nil
}

// WaitForIndexing returns once all samples in the storage are
// indexed. Indexing is needed for FingerprintsForLabelMatchers and
// LabelValuesForLabelName and may lag behind.
func (p *ProxyStorage) WaitForIndexing() {}
