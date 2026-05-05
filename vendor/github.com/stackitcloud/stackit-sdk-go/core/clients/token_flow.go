package clients

import (
	"fmt"
	"net/http"
)

const (
	// Service Account Token Flow
	// Auth flow env variables
	ServiceAccountToken = "STACKIT_SERVICE_ACCOUNT_TOKEN"
)

// TokenFlow handles auth with SA static token
type TokenFlow struct {
	rt     http.RoundTripper
	config *TokenFlowConfig
}

// TokenFlowConfig is the flow config
type TokenFlowConfig struct {
	// Deprecated: ServiceAccountEmail is not required and will be removed after 12th June 2025.
	ServiceAccountEmail string
	ServiceAccountToken string
	// Deprecated: retry options were removed to reduce complexity of the client. If this functionality is needed, you can provide your own custom HTTP client.
	ClientRetry   *RetryConfig
	HTTPTransport http.RoundTripper
}

// GetConfig returns the flow configuration
func (c *TokenFlow) GetConfig() TokenFlowConfig {
	if c.config == nil {
		return TokenFlowConfig{}
	}
	return *c.config
}

func (c *TokenFlow) Init(cfg *TokenFlowConfig) error {
	c.config = cfg

	if c.rt = cfg.HTTPTransport; c.rt == nil {
		c.rt = http.DefaultTransport
	}

	return c.validate()
}

// validate the client is configured well
func (c *TokenFlow) validate() error {
	if c.config.ServiceAccountToken == "" {
		return fmt.Errorf("service account access token cannot be empty")
	}
	return nil
}

// RoundTrip performs the request
func (c *TokenFlow) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.rt == nil {
		return nil, fmt.Errorf("please run Init()")
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.ServiceAccountToken))
	return c.rt.RoundTrip(req)
}
