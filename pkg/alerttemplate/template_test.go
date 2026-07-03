package alerttemplate

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
)

// newAlert builds a minimal *rules.Alert with the given labels for testing.
func newAlert(lbls map[string]string, annotations map[string]string) *rules.Alert {
	return &rules.Alert{
		Labels:      labels.FromMap(lbls),
		Annotations: labels.FromMap(annotations),
	}
}

func TestGeneratorURL_NoConfig(t *testing.T) {
	m := NewManager()
	alert := newAlert(map[string]string{model.AlertNameLabel: "HighLatency"}, nil)

	if url, ok := m.GeneratorURL(alert, "up == 0", "http://promxy"); ok {
		t.Fatalf("expected no template to apply, got %q", url)
	}
}

func TestApply_Default(t *testing.T) {
	m := NewManager()
	err := m.Apply(Config{
		Default: `http://grafana/alerting?alert={{.AlertName | urlquery}}&sev={{.Labels.severity}}`,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	alert := newAlert(map[string]string{
		model.AlertNameLabel: "High Latency",
		"severity":           "critical",
	}, nil)

	url, ok := m.GeneratorURL(alert, "up == 0", "http://promxy")
	if !ok {
		t.Fatal("expected default template to apply")
	}
	want := "http://grafana/alerting?alert=High+Latency&sev=critical"
	if url != want {
		t.Fatalf("got %q, want %q", url, want)
	}
}

func TestApply_RuleSelectionOrder(t *testing.T) {
	m := NewManager()
	err := m.Apply(Config{
		Default: "http://default",
		Named: map[string]string{
			"pd": "http://pagerduty/new?title={{.AlertName | urlquery}}",
		},
		Rules: []Rule{
			// First matching rule wins.
			{MatchLabels: map[string]string{"severity": "critical"}, Template: "pd"},
			{MatchLabels: map[string]string{"team": "frontend"}, Template: "http://grafana/{{.Labels.team}}"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	tests := []struct {
		name   string
		labels map[string]string
		want   string
	}{
		{
			name:   "named template via rule",
			labels: map[string]string{model.AlertNameLabel: "DBDown", "severity": "critical"},
			want:   "http://pagerduty/new?title=DBDown",
		},
		{
			name:   "inline template via rule",
			labels: map[string]string{"team": "frontend"},
			want:   "http://grafana/frontend",
		},
		{
			name:   "falls through to default",
			labels: map[string]string{"team": "backend"},
			want:   "http://default",
		},
		{
			name:   "first rule wins over later match",
			labels: map[string]string{"severity": "critical", "team": "frontend"},
			want:   "http://pagerduty/new?title=",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			url, ok := m.GeneratorURL(newAlert(tc.labels, nil), "up == 0", "http://promxy")
			if !ok {
				t.Fatal("expected a template to apply")
			}
			if url != tc.want {
				t.Fatalf("got %q, want %q", url, tc.want)
			}
		})
	}
}

func TestApply_MultiLabelRuleRequiresAll(t *testing.T) {
	m := NewManager()
	err := m.Apply(Config{
		Rules: []Rule{
			{MatchLabels: map[string]string{"severity": "critical", "team": "db"}, Template: "http://match"},
		},
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	// Only one of the two labels matches -> no rule, no default -> not ok.
	if _, ok := m.GeneratorURL(newAlert(map[string]string{"severity": "critical"}, nil), "e", "x"); ok {
		t.Fatal("expected partial label match to not match the rule")
	}
	// Both labels match.
	if _, ok := m.GeneratorURL(newAlert(map[string]string{"severity": "critical", "team": "db"}, nil), "e", "x"); !ok {
		t.Fatal("expected full label match to match the rule")
	}
}

func TestGeneratorURL_DataFields(t *testing.T) {
	m := NewManager()
	err := m.Apply(Config{
		Default: "{{.ExternalURL}}|{{.Expr}}|{{.AlertName}}|{{.Labels.instance}}|{{.Annotations.summary}}",
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	alert := newAlert(
		map[string]string{model.AlertNameLabel: "Foo", "instance": "host:9090"},
		map[string]string{"summary": "it broke"},
	)
	url, ok := m.GeneratorURL(alert, "up == 0", "http://promxy")
	if !ok {
		t.Fatal("expected template to apply")
	}
	want := "http://promxy|up == 0|Foo|host:9090|it broke"
	if url != want {
		t.Fatalf("got %q, want %q", url, want)
	}
}

func TestGeneratorURL_RenderErrorFallsBack(t *testing.T) {
	m := NewManager()
	// Valid syntax, but references a field that does not exist on the data ->
	// execution error at render time.
	if err := m.Apply(Config{Default: "{{.NoSuchField.Nested}}"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if _, ok := m.GeneratorURL(newAlert(map[string]string{"x": "y"}, nil), "e", "x"); ok {
		t.Fatal("expected render error to report not-ok so caller falls back")
	}
}

func TestApply_InvalidTemplateRejected(t *testing.T) {
	tests := []struct {
		name string
		cfg  Config
	}{
		{"bad default", Config{Default: "{{.Unclosed"}},
		{"bad named", Config{Named: map[string]string{"x": "{{.Unclosed"}}},
		{"bad inline rule", Config{Rules: []Rule{{MatchLabels: map[string]string{"a": "b"}, Template: "{{.Unclosed"}}}},
		{"rule without template", Config{Rules: []Rule{{MatchLabels: map[string]string{"a": "b"}}}}},
		{"rule without match_labels", Config{Rules: []Rule{{Template: "http://x"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := (NewManager()).Apply(tc.cfg); err == nil {
				t.Fatal("expected Apply to reject invalid config")
			}
		})
	}
}

func TestApply_BadReloadKeepsPreviousConfig(t *testing.T) {
	m := NewManager()
	if err := m.Apply(Config{Default: "http://good"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	// A subsequent bad reload must not clobber the working configuration.
	if err := m.Apply(Config{Default: "{{.Unclosed"}); err == nil {
		t.Fatal("expected bad config to error")
	}
	url, ok := m.GeneratorURL(newAlert(map[string]string{"a": "b"}, nil), "e", "x")
	if !ok || url != "http://good" {
		t.Fatalf("expected previous config retained, got ok=%v url=%q", ok, url)
	}
}
