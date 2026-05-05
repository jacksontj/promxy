package clients

import (
	"fmt"
	"net/http"
)

type NoAuthFlow struct {
	rt     http.RoundTripper
	config *NoAuthFlowConfig
}

// NoAuthFlowConfig holds the configuration for the unauthenticated flow
type NoAuthFlowConfig struct {
	// Deprecated: retry options were removed to reduce complexity of the client. If this functionality is needed, you can provide your own custom HTTP client.
	ClientRetry   *RetryConfig
	HTTPTransport http.RoundTripper
}

// GetConfig returns the flow configuration
func (c *NoAuthFlow) GetConfig() NoAuthFlowConfig {
	if c.config == nil {
		return NoAuthFlowConfig{}
	}
	return *c.config
}

func (c *NoAuthFlow) Init(cfg NoAuthFlowConfig) error {
	c.config = &NoAuthFlowConfig{}

	if c.rt = cfg.HTTPTransport; c.rt == nil {
		c.rt = http.DefaultTransport
	}

	return nil
}

// RoundTrip performs the request
func (c *NoAuthFlow) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.rt == nil {
		return nil, fmt.Errorf("please run Init()")
	}

	return c.rt.RoundTrip(req)
}
