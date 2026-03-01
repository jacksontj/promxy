package clients

import (
	"time"
)

const (
	DefaultClientTimeout = time.Minute
)

// Deprecated: retry options were removed to reduce complexity of the client. If this functionality is needed, you can provide your own custom HTTP client.
type RetryConfig struct {
	MaxRetries       int           // Max retries
	WaitBetweenCalls time.Duration // Time to wait between requests
	RetryTimeout     time.Duration // Max time to re-try
	ClientTimeout    time.Duration // HTTP Client timeout
}

// Deprecated: retry options were removed to reduce complexity of the client. If this functionality is needed, you can provide your own custom HTTP client.
func NewRetryConfig() *RetryConfig {
	return &RetryConfig{}
}
