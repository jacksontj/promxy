package alerttemplate

import (
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
)

func TestExecuteGeneratorURLTemplate(t *testing.T) {
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "HighCPU",
			"severity":  "critical",
			"instance":  "server1.example.com:9100",
			"job":       "node-exporter",
		}),
		Annotations: labels.FromMap(map[string]string{
			"summary": "High CPU usage detected",
		}),
	}

	tests := []struct {
		name         string
		template     string
		expr         string
		externalURL  string
		expected     string
		expectError  bool
	}{
		{
			name:        "empty template returns empty string",
			template:    "",
			expr:        "up",
			externalURL: "http://localhost:9090",
			expected:    "",
			expectError: false,
		},
		{
			name:        "simple template with alert name",
			template:    "https://grafana.example.com/alerting/groups?alertname={{.AlertName}}",
			expr:        "up",
			externalURL: "http://localhost:9090",
			expected:    "https://grafana.example.com/alerting/groups?alertname=HighCPU",
			expectError: false,
		},
		{
			name:        "template with URL encoding",
			template:    "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			expr:        "up",
			externalURL: "http://localhost:9090",
			expected:    "https://grafana.example.com/alerting/groups?alertname=HighCPU",
			expectError: false,
		},
		{
			name:        "template with labels",
			template:    "https://grafana.example.com/alerting/groups?severity={{.Labels.severity}}",
			expr:        "up",
			externalURL: "http://localhost:9090",
			expected:    "https://grafana.example.com/alerting/groups?severity=critical",
			expectError: false,
		},
		{
			name:        "template with annotations",
			template:    "https://grafana.example.com/alerting/groups?summary={{.Annotations.summary|urlquery}}",
			expr:        "up",
			externalURL: "http://localhost:9090",
			expected:    "https://grafana.example.com/alerting/groups?summary=High+CPU+usage+detected",
			expectError: false,
		},
		{
			name:        "link back to Prometheus graph",
			template:    "{{.ExternalURL}}/graph?g0.expr={{.Expr|urlquery}}&g0.tab=1",
			expr:        "up{job=\"node-exporter\"}",
			externalURL: "http://prometheus.example.com:9090",
			expected:    "http://prometheus.example.com:9090/graph?g0.expr=up%7Bjob%3D%22node-exporter%22%7D&g0.tab=1",
			expectError: false,
		},
		{
			name:        "link to Prometheus alerts page",
			template:    "{{.ExternalURL}}/alerts?search={{.AlertName|urlquery}}",
			expr:        "up",
			externalURL: "http://prometheus.example.com:9090",
			expected:    "http://prometheus.example.com:9090/alerts?search=HighCPU",
			expectError: false,
		},
		{
			name:        "link to external Grafana with Prometheus datasource",
			template:    "https://grafana.example.com/explore?left=%7B%22datasource%22:%22{{.ExternalURL|urlquery}}%22,%22queries%22:%5B%7B%22expr%22:%22{{.Expr|urlquery}}%22%7D%5D%7D",
			expr:        "up{job=\"prometheus\"}",
			externalURL: "http://prometheus.example.com:9090",
			expected:    "https://grafana.example.com/explore?left=%7B%22datasource%22:%22http%3A%2F%2Fprometheus.example.com%3A9090%22,%22queries%22:%5B%7B%22expr%22:%22up%7Bjob%3D%22prometheus%22%7D%22%7D%5D%7D",
			expectError: false,
		},

		{
			name:        "Grafana dashboard with variables",
			template:    "https://grafana.example.com/d/node-exporter/node-exporter?var-instance={{.Labels.instance}}&var-job={{.Labels.job}}",
			expr:        "up",
			externalURL: "http://prometheus.example.com:9090",
			expected:    "https://grafana.example.com/d/node-exporter/node-exporter?var-instance=server1.example.com:9100&var-job=node-exporter",
			expectError: false,
		},
		{
			name:        "Alertmanager silence with URL encoding",
			template:    "https://alertmanager.example.com/#/silences/new?filter=%7Balertname%3D%22{{.AlertName}}%22%2Cinstance%3D%22{{.Labels.instance|urlquery}}%22%7D",
			expr:        "up",
			externalURL: "http://prometheus.example.com:9090",
			expected:    "https://alertmanager.example.com/#/silences/new?filter=%7Balertname%3D%22HighCPU%22%2Cinstance%3D%22server1.example.com%3A9100%22%7D",
			expectError: false,
		},
		{
			name:        "malformed template should error",
			template:    "{{.InvalidField",
			expr:        "up",
			externalURL: "http://localhost:9090",
			expected:    "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(tt.template, alert, tt.expr, tt.externalURL)
			
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTemplateFunctions(t *testing.T) {
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "High CPU Usage",
			"instance":  "server1.example.com:9100",
			"service":   "web/api",
		}),
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "urlquery function",
			template: "{{.AlertName|urlquery}}",
			expected: "High+CPU+Usage",
		},
		{
			name:     "urlpath function",
			template: "{{.Labels.instance|urlpath}}",
			expected: "server1.example.com:9100",
		},
		{
			name:     "urlquery with special characters",
			template: "{{.Labels.service|urlquery}}",
			expected: "web%2Fapi",
		},
		{
			name:     "urlpath with special characters",
			template: "{{.Labels.service|urlpath}}",
			expected: "web%2Fapi",
		},
		{
			name:     "multiple template functions",
			template: "https://example.com/{{.AlertName|urlpath}}?instance={{.Labels.instance|urlquery}}",
			expected: "https://example.com/High%20CPU%20Usage?instance=server1.example.com%3A9100",
		},
		{
			name:     "nested template functions",
			template: "{{.Labels.service|urlquery|urlpath}}",
			expected: "web%252Fapi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(tt.template, alert, "up", "http://localhost:9090")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTemplateErrorHandling(t *testing.T) {
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "TestAlert",
		}),
	}

	tests := []struct {
		name        string
		template    string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "malformed template - unclosed action",
			template:    "{{.AlertName",
			expectError: true,
			errorMsg:    "failed to parse template",
		},
		{
			name:        "malformed template - invalid syntax",
			template:    "{{.AlertName}}{{",
			expectError: true,
			errorMsg:    "failed to parse template",
		},
		{
			name:        "template with invalid field access",
			template:    "{{.NonExistentField}}",
			expectError: true,
			errorMsg:    "failed to execute template",
		},
		{
			name:        "template with invalid function",
			template:    "{{.AlertName|nonexistentfunc}}",
			expectError: true,
			errorMsg:    "failed to parse template",
		},
		{
			name:        "template with nil map access",
			template:    "{{.Labels.nonexistent}}",
			expectError: false, // This should not error, just return <no value>
		},
		{
			name:        "complex template with partial errors",
			template:    "{{.AlertName}}-{{.Labels.nonexistent}}-{{.Labels.alertname}}",
			expectError: false,
			errorMsg:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(tt.template, alert, "up", "http://localhost:9090")
			
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if !strings.Contains(err.Error(), tt.errorMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errorMsg, err.Error())
				}
				return
			}
			
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			// For non-error cases, just verify we got some result
			if tt.template == "{{.Labels.nonexistent}}" && result != "<no value>" {
				t.Errorf("expected '<no value>' for nonexistent label, got %q", result)
			}
		})
	}
}

func TestTemplateDataStructure(t *testing.T) {
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "TestAlert",
			"severity":  "critical",
			"instance":  "server1:9100",
		}),
		Annotations: labels.FromMap(map[string]string{
			"summary":     "Test summary",
			"description": "Test description",
		}),
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "access ExternalURL",
			template: "{{.ExternalURL}}",
			expected: "http://prometheus.example.com:9090",
		},
		{
			name:     "access Expr",
			template: "{{.Expr}}",
			expected: "up{job=\"test\"}",
		},
		{
			name:     "access AlertName",
			template: "{{.AlertName}}",
			expected: "TestAlert",
		},
		{
			name:     "access Labels map",
			template: "{{.Labels.severity}}",
			expected: "critical",
		},
		{
			name:     "access Annotations map",
			template: "{{.Annotations.summary}}",
			expected: "Test summary",
		},
		{
			name:     "access all fields",
			template: "{{.ExternalURL}}/graph?g0.expr={{.Expr|urlquery}}&alertname={{.AlertName}}&severity={{.Labels.severity}}",
			expected: "http://prometheus.example.com:9090/graph?g0.expr=up%7Bjob%3D%22test%22%7D&alertname=TestAlert&severity=critical",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(tt.template, alert, "up{job=\"test\"}", "http://prometheus.example.com:9090")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTemplateWithEmptyData(t *testing.T) {
	// Test with alert that has minimal data
	alert := &rules.Alert{
		Labels:      labels.FromMap(map[string]string{}),
		Annotations: labels.FromMap(map[string]string{}),
	}

	tests := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "empty AlertName",
			template: "{{.AlertName}}",
			expected: "",
		},
		{
			name:     "empty Labels access",
			template: "{{.Labels.severity}}",
			expected: "<no value>",
		},
		{
			name:     "empty Annotations access",
			template: "{{.Annotations.summary}}",
			expected: "<no value>",
		},
		{
			name:     "template with default values",
			template: "{{if .AlertName}}{{.AlertName}}{{else}}unknown{{end}}",
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(tt.template, alert, "up", "http://localhost:9090")
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestTemplateWithNilAlert(t *testing.T) {
	_, err := ExecuteGeneratorURLTemplate("{{.AlertName}}", nil, "up", "http://localhost:9090")
	if err == nil {
		t.Error("expected error when passing nil alert")
	}
	
	expectedMsg := "alert cannot be nil"
	if !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("expected error to contain %q, got %q", expectedMsg, err.Error())
	}
}

