package servergroup

import (
	"net"
	"net/url"
	"strings"
	"sync/atomic"
)

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
			return err
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
