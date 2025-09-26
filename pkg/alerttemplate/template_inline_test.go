package alerttemplate

import (
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
)

func TestTemplateManager_InlineTemplateValidation(t *testing.T) {
	tm := NewTemplateManager()

	tests := []struct {
		name      string
		templates map[string]string
		expectLog bool // Whether we expect error logs (invalid templates should be skipped)
	}{
		{
			name: "valid templates",
			templates: map[string]string{
				"valid1": "https://example.com/{{.AlertName}}",
				"valid2": "https://example.com/{{.Labels.severity}}",
			},
			expectLog: false,
		},
		{
			name: "invalid template name - empty",
			templates: map[string]string{
				"": "https://example.com/{{.AlertName}}",
			},
			expectLog: true,
		},
		{
			name: "invalid template name - path separator",
			templates: map[string]string{
				"path/name": "https://example.com/{{.AlertName}}",
			},
			expectLog: true,
		},
		{
			name: "invalid template name - starts with dot",
			templates: map[string]string{
				".hidden": "https://example.com/{{.AlertName}}",
			},
			expectLog: true,
		},
		{
			name: "invalid template content - empty",
			templates: map[string]string{
				"empty": "",
			},
			expectLog: true,
		},
		{
			name: "invalid template content - malformed",
			templates: map[string]string{
				"malformed": "https://example.com/{{.AlertName",
			},
			expectLog: true,
		},
		{
			name: "mixed valid and invalid",
			templates: map[string]string{
				"valid":     "https://example.com/{{.AlertName}}",
				"":          "https://example.com/empty-name",
				"malformed": "https://example.com/{{.AlertName",
			},
			expectLog: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tm.LoadInlineTemplates(tt.templates)
			if err != nil {
				t.Errorf("LoadInlineTemplates returned error: %v", err)
			}

			// Count valid templates that should have been loaded
			validCount := 0
			for name, content := range tt.templates {
				if tm.validateTemplateName(name) == nil && tm.validateInlineTemplateContent(name, content) == nil {
					validCount++
				}
			}

			// Check that only valid templates were loaded
			loadedTemplates := tm.GetInlineTemplates()
			if len(loadedTemplates) != validCount {
				t.Errorf("expected %d valid templates to be loaded, got %d", validCount, len(loadedTemplates))
			}
		})
	}
}

func TestTemplateManager_ExecuteInlineTemplate(t *testing.T) {
	tm := NewTemplateManager()

	// Load inline templates
	inlineTemplates := map[string]string{
		"grafana":   "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
		"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
		"complex":   "{{.ExternalURL}}/graph?g0.expr={{.Expr|urlquery}}&g0.tab=1&alertname={{.AlertName|urlquery}}&severity={{.Labels.severity}}",
	}

	err := tm.LoadInlineTemplates(inlineTemplates)
	if err != nil {
		t.Fatalf("failed to load inline templates: %v", err)
	}

	// Create test alert
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname":   "HighCPU",
			"severity":    "critical",
			"incident_id": "INC-12345",
		}),
		Annotations: labels.FromMap(map[string]string{
			"summary": "High CPU usage detected",
		}),
	}

	tests := []struct {
		templateName string
		expectedURL  string
	}{
		{
			templateName: "grafana",
			expectedURL:  "https://grafana.example.com/alerting/groups?alertname=HighCPU",
		},
		{
			templateName: "pagerduty",
			expectedURL:  "https://pagerduty.example.com/incidents/INC-12345",
		},
		{
			templateName: "complex",
			expectedURL:  "http://prometheus.example.com:9090/graph?g0.expr=up+%3D%3D+0&g0.tab=1&alertname=HighCPU&severity=critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.templateName, func(t *testing.T) {
			url, err := ExecuteTemplateByName(tm, tt.templateName, alert, "up == 0", "http://prometheus.example.com:9090")
			if err != nil {
				t.Errorf("failed to execute template %q: %v", tt.templateName, err)
				return
			}

			if url != tt.expectedURL {
				t.Errorf("template %q: expected URL %q, got %q", tt.templateName, tt.expectedURL, url)
			}
		})
	}

	// Test nonexistent template
	_, err = ExecuteTemplateByName(tm, "nonexistent", alert, "up == 0", "http://prometheus.example.com:9090")
	if err == nil {
		t.Error("expected error for nonexistent template")
	}
}
