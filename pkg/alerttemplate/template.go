// Package alerttemplate renders configurable GeneratorURL values for promxy's
// alerts. By default promxy emits the same Prometheus-style GeneratorURL as
// upstream (a link to promxy's own graph page for the alert expression). That
// is not useful when promxy is the central evaluation point for many backends
// and operators triage alerts elsewhere (Grafana alerting, an incident manager,
// a per-tenant dashboard, ...). This package lets an operator configure Go
// templates, selected per-alert by label, that produce the GeneratorURL
// instead. It is entirely opt-in: with no configuration the caller falls back
// to the built-in URL.
package alerttemplate

import (
	"bytes"
	"fmt"
	"net/url"
	"sync"
	"text/template"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/rules"
	"github.com/sirupsen/logrus"
)

// Config is the promxy alert-template configuration, nested under
// `promxy.alert_templates` in the config file.
type Config struct {
	// Default is used when no rule matches. It is either an inline template
	// body or the name of one of the Named templates. When empty, promxy uses
	// its built-in Prometheus-style GeneratorURL.
	Default string `yaml:"default,omitempty"`

	// Named holds reusable inline templates, addressable by name from Default
	// and from a rule's Template.
	Named map[string]string `yaml:"named,omitempty"`

	// Rules select a template based on alert labels. They are evaluated
	// top-to-bottom and the first match wins.
	Rules []Rule `yaml:"rules,omitempty"`
}

// Rule selects a template when every entry in MatchLabels equals the
// corresponding alert label.
type Rule struct {
	// MatchLabels must all be present on the alert with the given value for the
	// rule to match. An empty MatchLabels never matches (use Default for a
	// catch-all).
	MatchLabels map[string]string `yaml:"match_labels"`

	// Template is an inline template body or the name of a Named template.
	Template string `yaml:"template"`
}

// Data is the value passed to a GeneratorURL template during execution.
type Data struct {
	// ExternalURL is promxy's configured external URL.
	ExternalURL string
	// Expr is the PromQL expression that triggered the alert.
	Expr string
	// Labels are the alert's labels.
	Labels map[string]string
	// Annotations are the alert's annotations.
	Annotations map[string]string
	// AlertName is a convenience for the value of the "alertname" label.
	AlertName string
}

// templateFuncs are the helpers available to every template.
var templateFuncs = template.FuncMap{
	"urlquery": url.QueryEscape,
	"urlpath":  url.PathEscape,
}

// compiledRule is a Rule with its template pre-parsed.
type compiledRule struct {
	match map[string]string
	tmpl  *template.Template
}

// matches reports whether every label in the rule is present with the expected
// value. An empty match set never matches.
func (r compiledRule) matches(labels map[string]string) bool {
	if len(r.match) == 0 {
		return false
	}
	for k, v := range r.match {
		if labels[k] != v {
			return false
		}
	}
	return true
}

// Manager holds the compiled templates and renders GeneratorURLs. Its zero
// value is not usable; construct one with NewManager. It is safe for concurrent
// use: Apply swaps the compiled template set under a write lock while
// GeneratorURL reads it under a read lock.
type Manager struct {
	mu    sync.RWMutex
	def   *template.Template // nil when no default configured
	rules []compiledRule
}

// NewManager returns an empty Manager. Until Apply installs a configuration,
// GeneratorURL always reports that no template applies.
func NewManager() *Manager {
	return &Manager{}
}

// Apply compiles cfg and atomically installs it. It returns an error (leaving
// the previous configuration in place) if any template fails to parse, so a bad
// template fails the config reload loudly rather than silently disabling alert
// URLs.
func (m *Manager) Apply(cfg Config) error {
	// Pre-compile all named templates so typos are caught even when a name is
	// only referenced indirectly.
	named := make(map[string]*template.Template, len(cfg.Named))
	for name, body := range cfg.Named {
		t, err := template.New(name).Funcs(templateFuncs).Parse(body)
		if err != nil {
			return fmt.Errorf("alert_templates.named[%q]: %w", name, err)
		}
		named[name] = t
	}

	// resolve turns a template reference (a Named name or an inline body) into a
	// compiled template. An empty reference yields a nil template.
	resolve := func(ref string) (*template.Template, error) {
		if ref == "" {
			return nil, nil
		}
		if t, ok := named[ref]; ok {
			return t, nil
		}
		return template.New("generatorURL").Funcs(templateFuncs).Parse(ref)
	}

	def, err := resolve(cfg.Default)
	if err != nil {
		return fmt.Errorf("alert_templates.default: %w", err)
	}

	compiled := make([]compiledRule, 0, len(cfg.Rules))
	for i, r := range cfg.Rules {
		if r.Template == "" {
			return fmt.Errorf("alert_templates.rules[%d]: template is required", i)
		}
		if len(r.MatchLabels) == 0 {
			return fmt.Errorf("alert_templates.rules[%d]: match_labels is required", i)
		}
		t, err := resolve(r.Template)
		if err != nil {
			return fmt.Errorf("alert_templates.rules[%d]: %w", i, err)
		}
		compiled = append(compiled, compiledRule{match: r.MatchLabels, tmpl: t})
	}

	m.mu.Lock()
	m.def = def
	m.rules = compiled
	m.mu.Unlock()
	return nil
}

// GeneratorURL renders the GeneratorURL for alert. ok is false when no template
// is configured for the alert or when rendering fails (the failure is logged),
// in which case the caller should fall back to the default GeneratorURL.
func (m *Manager) GeneratorURL(alert *rules.Alert, expr, externalURL string) (url string, ok bool) {
	m.mu.RLock()
	tmpl := m.def
	rules := m.rules
	m.mu.RUnlock()

	labels := alert.Labels.Map()
	for _, r := range rules {
		if r.matches(labels) {
			tmpl = r.tmpl
			break
		}
	}
	if tmpl == nil {
		return "", false
	}

	data := Data{
		ExternalURL: externalURL,
		Expr:        expr,
		Labels:      labels,
		Annotations: alert.Annotations.Map(),
		AlertName:   alert.Labels.Get(model.AlertNameLabel),
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		logrus.Warnf("alert %q: rendering GeneratorURL template failed, falling back to default: %v", data.AlertName, err)
		return "", false
	}
	return buf.String(), true
}
