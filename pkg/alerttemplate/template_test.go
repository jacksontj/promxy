package alerttemplate

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

// TestTemplateManager tests template loading and management
func TestTemplateManager(t *testing.T) {
	t.Run("directory loading", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "template_test_*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create test files including nested directories and non-template files
		testFiles := map[string]string{
			"grafana.tmpl":         "https://grafana.example.com/alert/{{.AlertName}}",
			"subdir/custom.tmpl":   "https://custom.example.com/{{.Labels.severity}}",
			"invalid.tmpl":         "https://example.com/{{.InvalidSyntax", // Invalid template
			"readme.txt":           "This is not a template",               // Non-template file
		}

		for filePath, content := range testFiles {
			fullPath := filepath.Join(tempDir, filePath)
			dir := filepath.Dir(fullPath)
			if dir != tempDir {
				err := os.MkdirAll(dir, 0755)
				require.NoError(t, err)
			}
			err := os.WriteFile(fullPath, []byte(content), 0644)
			require.NoError(t, err)
		}

		tm := NewTemplateManager()
		err = tm.LoadFromDirectory(tempDir)
		require.NoError(t, err)

		// Should load 2 valid templates (grafana and subdir.custom)
		names := tm.ListTemplates()
		assert.Len(t, names, 2)
		assert.Contains(t, names, "grafana")
		assert.Contains(t, names, "subdir.custom")

		// Verify template content
		content, exists := tm.GetTemplate("grafana")
		assert.True(t, exists)
		assert.Equal(t, "https://grafana.example.com/alert/{{.AlertName}}", content)
	})

	t.Run("inline template validation", func(t *testing.T) {
		tm := NewTemplateManager()

		// Test valid templates
		validTemplates := map[string]string{
			"valid1": "https://example.com/{{.AlertName}}",
			"valid2": "https://example.com/{{.Labels.severity}}",
		}
		err := tm.LoadInlineTemplates(validTemplates)
		assert.NoError(t, err)
		assert.Len(t, tm.GetInlineTemplates(), 2)

		// Test invalid templates (should be skipped)
		invalidTemplates := map[string]string{
			"":          "https://example.com/empty-name",     // Empty name
			"path/name": "https://example.com/{{.AlertName}}", // Path separator
			".hidden":   "https://example.com/{{.AlertName}}", // Starts with dot
			"empty":     "",                                   // Empty content
			"malformed": "https://example.com/{{.AlertName",   // Malformed template
		}
		err = tm.LoadInlineTemplates(invalidTemplates)
		assert.NoError(t, err)
		assert.Len(t, tm.GetInlineTemplates(), 0) // All should be rejected
	})

	t.Run("directory override behavior", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "template_override_test_*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create directory template
		err = os.WriteFile(filepath.Join(tempDir, "shared.tmpl"), 
			[]byte("https://directory.example.com/{{.AlertName}}"), 0644)
		require.NoError(t, err)

		tm := NewTemplateManager()
		
		// Load inline template first
		inlineTemplates := map[string]string{
			"shared":      "https://inline.example.com/{{.AlertName}}",
			"inline_only": "https://inline-only.example.com/{{.AlertName}}",
		}
		err = tm.LoadInlineTemplates(inlineTemplates)
		require.NoError(t, err)

		// Load directory templates (should override inline)
		err = tm.LoadFromDirectory(tempDir)
		require.NoError(t, err)

		// Verify directory template overrides inline template
		content, exists := tm.GetTemplate("shared")
		assert.True(t, exists)
		assert.Equal(t, "https://directory.example.com/{{.AlertName}}", content)

		// Verify inline-only template still exists
		content, exists = tm.GetTemplate("inline_only")
		assert.True(t, exists)
		assert.Equal(t, "https://inline-only.example.com/{{.AlertName}}", content)
	})

	t.Run("edge cases", func(t *testing.T) {
		tm := NewTemplateManager()
		
		// Empty path should not error
		err := tm.LoadFromDirectory("")
		assert.NoError(t, err)
		assert.Len(t, tm.ListTemplates(), 0)
		
		// Non-existent directory should not error
		err = tm.LoadFromDirectory("/non/existent/directory")
		assert.NoError(t, err)
		assert.Len(t, tm.ListTemplates(), 0)
	})
}

// TestTemplateSelection tests rule-based template selection
func TestTemplateSelection(t *testing.T) {
	// Create a template manager with named templates
	tm := NewTemplateManager()
	namedTemplates := map[string]string{
		"grafana":   "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
		"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
	}
	err := tm.LoadInlineTemplates(namedTemplates)
	require.NoError(t, err)

	tests := []struct {
		name            string
		rules           []TemplateRule
		defaultTemplate string
		alertLabels     map[string]string
		expected        string
	}{
		{
			name:            "no rules, use default named template",
			rules:           []TemplateRule{},
			defaultTemplate: "grafana",
			alertLabels:     map[string]string{"alertname": "TestAlert"},
			expected:        "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
		},
		{
			name: "single matching rule with inline template",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "https://critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "grafana",
			alertLabels:     map[string]string{"alertname": "CriticalAlert", "severity": "critical"},
			expected:        "https://critical.example.com/alert/{{.AlertName}}",
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
			defaultTemplate: "grafana",
			alertLabels:     map[string]string{"alertname": "CriticalAlert", "severity": "critical"},
			expected:        "https://critical.example.com/alert/{{.AlertName}}",
		},
		{
			name: "multiple match labels, all must match",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical", "team": "frontend"},
					Template:    "https://frontend-critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "grafana",
			alertLabels:     map[string]string{"alertname": "Alert", "severity": "critical", "team": "frontend"},
			expected:        "https://frontend-critical.example.com/alert/{{.AlertName}}",
		},
		{
			name: "partial match fails, use default",
			rules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical", "team": "frontend"},
					Template:    "https://frontend-critical.example.com/alert/{{.AlertName}}",
				},
			},
			defaultTemplate: "grafana",
			alertLabels:     map[string]string{"alertname": "Alert", "severity": "critical", "team": "backend"},
			expected:        "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := &rules.Alert{
				Labels: labels.FromMap(tt.alertLabels),
			}
			result := SelectTemplate(tt.rules, tt.defaultTemplate, tm, alert)
			assert.Equal(t, tt.expected, result)
		})
	}

	// Test nil alert
	result := SelectTemplate([]TemplateRule{}, "grafana", tm, nil)
	assert.Equal(t, "grafana", result)
}

// TestEndToEndIntegration tests complete workflow from template selection to alert processing
func TestEndToEndIntegration(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "up{instance=\"web-server-01\"}"
	
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "HighMemoryUsage",
			"instance":  "web-server-01:9100",
			"severity":  "critical",
		}),
		State:   rules.StateFiring,
		FiredAt: time.Now(),
	}

	t.Run("template manager with rule selection", func(t *testing.T) {
		// Create template manager with inline and directory templates
		tm := NewTemplateManager()
		
		// Add inline templates first
		namedTemplates := map[string]string{
			"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
		}
		err := tm.LoadInlineTemplates(namedTemplates)
		require.NoError(t, err)

		// Create directory template (will override inline if same name)
		tempDir, err := os.MkdirTemp("", "integration_test_*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		err = os.WriteFile(filepath.Join(tempDir, "grafana.tmpl"), 
			[]byte("https://grafana.example.com/alert/{{.AlertName}}"), 0644)
		require.NoError(t, err)

		err = tm.LoadFromDirectory(tempDir)
		require.NoError(t, err)

		// Test rule-based selection
		rules := []TemplateRule{
			{
				MatchLabels: map[string]string{"severity": "critical"},
				Template:    "grafana", // References directory template
			},
		}

		selectedTemplate := SelectTemplate(rules, "pagerduty", tm, alert)
		assert.Equal(t, "https://grafana.example.com/alert/{{.AlertName}}", selectedTemplate)

		// Execute the selected template
		result, err := ExecuteGeneratorURLTemplate(selectedTemplate, alert, expr, externalURL)
		assert.NoError(t, err)
		assert.Equal(t, "https://grafana.example.com/alert/HighMemoryUsage", result)
	})

	t.Run("alert processing with fallback", func(t *testing.T) {
		mockNotifier := &mockNotifierManager{}
		alertCfg := &mockAlertConfig{}
		
		// Test successful template execution
		alertCfg.setGeneratorURLTemplate("https://grafana.example.com/alert/{{.AlertName}}", "")
		sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
		
		sendAlertsFunc(context.Background(), expr, alert)
		
		require.Len(t, mockNotifier.sentAlerts, 1)
		assert.Equal(t, "https://grafana.example.com/alert/HighMemoryUsage", mockNotifier.sentAlerts[0].GeneratorURL)

		// Test fallback on template error
		mockNotifier.sentAlerts = nil
		alertCfg.setGeneratorURLTemplate("{{.InvalidField}}", "")
		sendAlertsFunc(context.Background(), expr, alert)
		
		require.Len(t, mockNotifier.sentAlerts, 1)
		expectedFallbackURL := externalURL + strutil.TableLinkForExpression(expr)
		assert.Equal(t, expectedFallbackURL, mockNotifier.sentAlerts[0].GeneratorURL)
	})
}

// Mock types for testing alert processing workflow
type mockAlertConfig struct {
	generatorURLTemplate string
}

func (ac *mockAlertConfig) setGeneratorURLTemplate(configTemplate, cliTemplate string) {
	if cliTemplate != "" {
		ac.generatorURLTemplate = cliTemplate
	} else {
		ac.generatorURLTemplate = configTemplate
	}
}

type mockNotifierManager struct {
	sentAlerts []*notifier.Alert
}

func (m *mockNotifierManager) Send(alerts ...*notifier.Alert) {
	m.sentAlerts = append(m.sentAlerts, alerts...)
}

func createSendAlertsFunc(nm *mockNotifierManager, externalURL string, alertCfg *mockAlertConfig) rules.NotifyFunc {
	return func(ctx context.Context, expr string, alerts ...*rules.Alert) {
		var res []*notifier.Alert

		for _, alert := range alerts {
			if alert.State == rules.StatePending {
				continue
			}
			
			var generatorURL string
			if alertCfg.generatorURLTemplate != "" {
				templateURL, err := ExecuteGeneratorURLTemplate(alertCfg.generatorURLTemplate, alert, expr, externalURL)
				if err != nil {
					generatorURL = externalURL + strutil.TableLinkForExpression(expr)
				} else {
					generatorURL = templateURL
				}
			} else {
				generatorURL = externalURL + strutil.TableLinkForExpression(expr)
			}
			
			a := &notifier.Alert{
				StartsAt:     alert.FiredAt,
				Labels:       alert.Labels,
				Annotations:  alert.Annotations,
				GeneratorURL: generatorURL,
			}
			if !alert.ResolvedAt.IsZero() {
				a.EndsAt = alert.ResolvedAt
			}
			res = append(res, a)
		}

		if len(res) > 0 {
			nm.Send(res...)
		}
	}
}

