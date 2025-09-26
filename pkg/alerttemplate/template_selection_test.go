package alerttemplate

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
)

func TestSelectTemplate(t *testing.T) {
	// Create a template manager with some named templates
	tm := NewTemplateManager()
	namedTemplates := map[string]string{
		"grafana":   "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
		"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
	}
	err := tm.LoadInlineTemplates(namedTemplates)
	if err != nil {
		t.Fatalf("Failed to load inline templates: %v", err)
	}

	tests := []struct {
		name            string
		rules           []TemplateRule
		defaultTemplate string
		alert           *rules.Alert
		expected        string
		description     string
	}{
		{
			name: "no rules, no default",
			rules: []TemplateRule{},
			defaultTemplate: "",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "TestAlert",
					"severity":  "warning",
				}),
			},
			expected:    "",
			description: "Should return empty string when no rules or default template",
		},
		{
			name: "no rules, with default named template",
			rules: []TemplateRule{},
			defaultTemplate: "grafana",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "TestAlert",
					"severity":  "warning",
				}),
			},
			expected:    "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			description: "Should resolve named default template",
		},
		{
			name: "single matching rule with inline template",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "https://critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "CriticalAlert",
					"severity":  "critical",
				}),
			},
			expected:    "https://critical.example.com/alert/{{.AlertName}}",
			description: "Should use matching rule template",
		},
		{
			name: "single matching rule with named template",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "pagerduty",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "CriticalAlert",
					"severity":  "critical",
					"incident_id": "INC-123",
				}),
			},
			expected:    "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
			description: "Should resolve named template from matching rule",
		},
		{
			name: "multiple rules, first match wins",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "https://critical.example.com/alert/{{.AlertName}}",
				},
				{
					MatchLabels: map[string]string{"alertname": "CriticalAlert"},
					Template:    "https://alertname.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "CriticalAlert",
					"severity":  "critical",
				}),
			},
			expected:    "https://critical.example.com/alert/{{.AlertName}}",
			description: "Should use first matching rule (top-to-bottom)",
		},
		{
			name: "no matching rules, use default",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "https://critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "WarningAlert",
					"severity":  "warning",
				}),
			},
			expected:    "https://default.example.com/alert/{{.AlertName}}",
			description: "Should fall back to default when no rules match",
		},
		{
			name: "multiple match labels, all must match",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{
						"severity": "critical",
						"team":     "frontend",
					},
					Template: "https://frontend-critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "FrontendAlert",
					"severity":  "critical",
					"team":      "frontend",
				}),
			},
			expected:    "https://frontend-critical.example.com/alert/{{.AlertName}}",
			description: "Should match when all match labels are satisfied",
		},
		{
			name: "multiple match labels, partial match fails",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{
						"severity": "critical",
						"team":     "frontend",
					},
					Template: "https://frontend-critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "BackendAlert",
					"severity":  "critical",
					"team":      "backend",
				}),
			},
			expected:    "https://default.example.com/alert/{{.AlertName}}",
			description: "Should not match when only some match labels are satisfied",
		},
		{
			name: "nil alert",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "https://critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "https://default.example.com/alert/{{.AlertName}}",
			alert:           nil,
			expected:        "https://default.example.com/alert/{{.AlertName}}",
			description:     "Should return default template for nil alert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectTemplate(tt.rules, tt.defaultTemplate, tm, tt.alert)
			if result != tt.expected {
				t.Errorf("SelectTemplate() = %q, expected %q\nDescription: %s", result, tt.expected, tt.description)
			}
		})
	}
}

func TestMatchesRule(t *testing.T) {
	tests := []struct {
		name        string
		rule        TemplateRule
		alertLabels map[string]string
		expected    bool
		description string
	}{
		{
			name: "empty match labels",
			rule: TemplateRule{
				MatchLabels: map[string]string{},
				Template:    "test",
			},
			alertLabels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "warning",
			},
			expected:    false,
			description: "Should not match when no match labels specified",
		},
		{
			name: "single label exact match",
			rule: TemplateRule{
				MatchLabels: map[string]string{"severity": "critical"},
				Template:    "test",
			},
			alertLabels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "critical",
			},
			expected:    true,
			description: "Should match when single label matches exactly",
		},
		{
			name: "single label no match",
			rule: TemplateRule{
				MatchLabels: map[string]string{"severity": "critical"},
				Template:    "test",
			},
			alertLabels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "warning",
			},
			expected:    false,
			description: "Should not match when single label value differs",
		},
		{
			name: "missing label",
			rule: TemplateRule{
				MatchLabels: map[string]string{"team": "frontend"},
				Template:    "test",
			},
			alertLabels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "warning",
			},
			expected:    false,
			description: "Should not match when required label is missing",
		},
		{
			name: "multiple labels all match",
			rule: TemplateRule{
				MatchLabels: map[string]string{
					"severity": "critical",
					"team":     "frontend",
					"env":      "production",
				},
				Template: "test",
			},
			alertLabels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "critical",
				"team":      "frontend",
				"env":       "production",
				"instance":  "server1",
			},
			expected:    true,
			description: "Should match when all multiple labels match (extra labels allowed)",
		},
		{
			name: "multiple labels partial match",
			rule: TemplateRule{
				MatchLabels: map[string]string{
					"severity": "critical",
					"team":     "frontend",
					"env":      "production",
				},
				Template: "test",
			},
			alertLabels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "critical",
				"team":      "backend", // Different value
				"env":       "production",
			},
			expected:    false,
			description: "Should not match when some labels don't match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesRule(tt.rule, tt.alertLabels)
			if result != tt.expected {
				t.Errorf("matchesRule() = %v, expected %v\nDescription: %s", result, tt.expected, tt.description)
			}
		})
	}
}
