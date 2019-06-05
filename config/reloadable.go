package proxyconfig

import "github.com/prometheus/prometheus/config"

// PromReloadable can apply a prometheus config
type PromReloadable interface {
	ApplyConfig(*config.Config) error
}

// Reloadable can apply a promxy config
type Reloadable interface {
	ApplyConfig(*Config) error
}

// PromReloadableWrap wraps a PromReloadable into a Reloadable
type PromReloadableWrap struct {
	R PromReloadable
}

// ApplyConfig applies new configuration
func (p *PromReloadableWrap) ApplyConfig(c *Config) error {
	return p.R.ApplyConfig(&c.PromConfig)
}

// WrapPromReloadable wraps a PromReloadable into a Reloadable
func WrapPromReloadable(p PromReloadable) Reloadable {
	return &PromReloadableWrap{p}
}

// ApplyConfigFunc is a struct that wraps a single function that Applys config
// into something that implements the `PromReloadable` interface
type ApplyConfigFunc struct {
	F func(*config.Config) error
}

// ApplyConfig applies new configuration
func (a *ApplyConfigFunc) ApplyConfig(cfg *config.Config) error {
	return a.F(cfg)
}
