package servergroup

import (
	"net"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var (
	serverGroupLastResolve = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "server_group_last_resolve_time",
		Help: "Time of the last resolve operation",
	}, []string{"host", "status"})
)

func init() {
	prometheus.MustRegister(serverGroupLastResolve)
}

type ServerGroup struct {
	OriginalURLs []string
	urls         atomic.Value
}

func (s *ServerGroup) UnmarshalYAML(unmarshal func(interface{}) error) error {

	if err := unmarshal(&s.OriginalURLs); err != nil {
		return err
	}

	return s.Resolve()
}

func (s *ServerGroup) URLs() []string {
	return s.urls.Load().([]string)
}

func (s *ServerGroup) Resolve() error {
	resolvedUrls := make([]string, 0, len(s.OriginalURLs))
	for _, server := range s.OriginalURLs {
		serverParsed, err := url.Parse(server)
		if err != nil {
			return err
		}

		hostname := serverParsed.Hostname()
		ips, err := net.LookupIP(hostname)
		if err != nil {
			serverGroupLastResolve.WithLabelValues(hostname, "error").Set(float64(time.Now().Unix()))
			return err
		} else {
			serverGroupLastResolve.WithLabelValues(hostname, "success").Set(float64(time.Now().Unix()))
		}

		for _, ip := range ips {
			// TODO: support ipv6, for now we just skip
			if ip.To4() == nil {
				continue
			}
			newUrl := *serverParsed
			newUrl.Host = strings.Replace(serverParsed.Host, hostname, ip.String(), 1)
			resolvedUrls = append(resolvedUrls, newUrl.String())
		}
	}

	s.urls.Store(resolvedUrls)

	return nil
}

func (s ServerGroup) AutoRefresh(interval time.Duration) {
	go func() {
		t := time.NewTicker(interval)
		for range t.C {
			s.Resolve()
		}
	}()
}
