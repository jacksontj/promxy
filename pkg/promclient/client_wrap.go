package promclient

import (
	"net/url"

	"github.com/prometheus/client_golang/api"
)

// NewClientArgsWrap returns a client that will add the given args
func NewClientArgsWrap(api api.Client, args map[string]string) *ClientArgsWrap {
	return &ClientArgsWrap{api, args}
}

// ClientArgsWrap wraps the prom API client to add query params to any given urls
type ClientArgsWrap struct {
	api.Client

	args map[string]string
}

// URL returns a URL for the given endpoint + args
func (c *ClientArgsWrap) URL(ep string, args map[string]string) *url.URL {
	u := c.Client.URL(ep, args)

	q := u.Query()
	for k, v := range c.args {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	return u
}
