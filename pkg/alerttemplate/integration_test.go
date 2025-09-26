package alerttemplate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/notifier"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/util/strutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationScenarios tests real-world template scenarios
func TestIntegrationScenarios(t *testing.T) {
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "HighMemoryUsage",
			"instance":  "web-server-01:9100",
			"job":       "node-exporter",
		}),
	}

	scenarios := []struct {
		name     string
		template string
		expected string
	}{
		{
			name:     "Grafana dashboard link",
			template: "https://grafana.example.com/d/dashboard?var-instance={{.Labels.instance}}",
			expected: "https://grafana.example.com/d/dashboard?var-instance=web-server-01:9100",
		},
		{
			name:     "Prometheus query link",
			template: "{{.ExternalURL}}/graph?g0.expr={{.Expr|urlquery}}",
			expected: "http://prometheus.example.com:9090/graph?g0.expr=up%7Binstance%3D%22web-server-01%22%7D",
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(
				scenario.template,
				alert,
				"up{instance=\"web-server-01\"}",
				"http://prometheus.example.com:9090",
			)
			
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if result != scenario.expected {
				t.Errorf("expected %q, got %q", scenario.expected, result)
			}
		})
	}
}

// TestEndToEndAlertProcessing tests the complete alert processing flow
func TestEndToEndAlertProcessing(t *testing.T) {
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "TestAlert",
		}),
	}

	tests := []struct {
		name        string
		template    string
		expectedURL string
		expectError bool
	}{
		{
			name:        "successful template execution",
			template:    "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			expectedURL: "https://grafana.example.com/alerting/groups?alertname=TestAlert",
			expectError: false,
		},
		{
			name:        "template execution failure",
			template:    "{{.NonExistentField}}",
			expectedURL: "",
			expectError: true,
		},
		{
			name:        "empty template",
			template:    "",
			expectedURL: "",
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecuteGeneratorURLTemplate(tt.template, alert, "up", "http://prometheus.example.com:9090")
			
			if tt.expectError {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			
			if result != tt.expectedURL {
				t.Errorf("expected %q, got %q", tt.expectedURL, result)
			}
		})
	}
}

// mockAlertConfig simulates the alertConfig struct from main.go for testing
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

// mockNotifierManager simulates the notifier.Manager for testing
type mockNotifierManager struct {
	sentAlerts []*notifier.Alert
}

func (m *mockNotifierManager) Send(alerts ...*notifier.Alert) {
	m.sentAlerts = append(m.sentAlerts, alerts...)
}

// createSendAlertsFunc creates a sendAlerts function similar to the one in main.go
func createSendAlertsFunc(nm *mockNotifierManager, externalURL string, alertCfg *mockAlertConfig) rules.NotifyFunc {
	return func(ctx context.Context, expr string, alerts ...*rules.Alert) {
		var res []*notifier.Alert

		for _, alert := range alerts {
			// Only send actually firing alerts.
			if alert.State == rules.StatePending {
				continue
			}
			
			// Generate the URL using template if configured, otherwise use default
			var generatorURL string
			if alertCfg.generatorURLTemplate != "" {
				templateURL, err := ExecuteGeneratorURLTemplate(alertCfg.generatorURLTemplate, alert, expr, externalURL)
				if err != nil {
					// In real implementation, this would log the error
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

// TestCompleteAlertProcessingFlow tests the complete alert processing flow with custom templates
func TestCompleteAlertProcessingFlow(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "up{job=\"test\"} < 1"
	
	tests := []struct {
		name             string
		template         string
		cliTemplate      string
		alertState       rules.AlertState
		expectedURL      string
		expectFallback   bool
		shouldSendAlert  bool
	}{
		{
			name:            "firing alert with custom template",
			template:        "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			alertState:      rules.StateFiring,
			expectedURL:     "https://grafana.example.com/alerting/groups?alertname=TestAlert",
			expectFallback:  false,
			shouldSendAlert: true,
		},
		{
			name:            "firing alert with CLI template override",
			template:        "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			cliTemplate:     "https://custom.example.com/alert/{{.AlertName}}",
			alertState:      rules.StateFiring,
			expectedURL:     "https://custom.example.com/alert/TestAlert",
			expectFallback:  false,
			shouldSendAlert: true,
		},
		{
			name:            "firing alert without template (default behavior)",
			template:        "",
			alertState:      rules.StateFiring,
			expectedURL:     externalURL + strutil.TableLinkForExpression(expr),
			expectFallback:  false,
			shouldSendAlert: true,
		},
		{
			name:            "pending alert should not be sent",
			template:        "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			alertState:      rules.StatePending,
			expectedURL:     "",
			expectFallback:  false,
			shouldSendAlert: false,
		},
		{
			name:            "firing alert with malformed template falls back",
			template:        "{{.NonExistentField}}",
			alertState:      rules.StateFiring,
			expectedURL:     externalURL + strutil.TableLinkForExpression(expr),
			expectFallback:  true,
			shouldSendAlert: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock components
			mockNotifier := &mockNotifierManager{}
			alertCfg := &mockAlertConfig{}
			alertCfg.setGeneratorURLTemplate(tt.template, tt.cliTemplate)
			
			// Create the sendAlerts function
			sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
			
			// Create test alert
			alert := &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "TestAlert",
					"severity":  "critical",
				}),
				Annotations: labels.FromMap(map[string]string{
					"summary": "Test alert summary",
				}),
				State:    tt.alertState,
				FiredAt:  time.Now(),
			}
			
			// Execute the alert processing
			ctx := context.Background()
			sendAlertsFunc(ctx, expr, alert)
			
			// Verify results
			if tt.shouldSendAlert {
				if len(mockNotifier.sentAlerts) != 1 {
					t.Errorf("expected 1 alert to be sent, got %d", len(mockNotifier.sentAlerts))
					return
				}
				
				sentAlert := mockNotifier.sentAlerts[0]
				if sentAlert.GeneratorURL != tt.expectedURL {
					t.Errorf("expected GeneratorURL %q, got %q", tt.expectedURL, sentAlert.GeneratorURL)
				}
				
				// Verify other alert properties are preserved
				if sentAlert.Labels.Get("alertname") != "TestAlert" {
					t.Errorf("expected alertname to be preserved")
				}
				if sentAlert.Annotations.Get("summary") != "Test alert summary" {
					t.Errorf("expected annotations to be preserved")
				}
			} else {
				if len(mockNotifier.sentAlerts) != 0 {
					t.Errorf("expected no alerts to be sent, got %d", len(mockNotifier.sentAlerts))
				}
			}
		})
	}
}

// TestFallbackBehaviorOnTemplateFailure tests that the system falls back to default URL generation when templates fail
func TestFallbackBehaviorOnTemplateFailure(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "up{job=\"test\"} < 1"
	
	tests := []struct {
		name            string
		template        string
		expectedFallback bool
	}{
		{
			name:            "malformed template syntax",
			template:        "{{.InvalidSyntax",
			expectedFallback: true,
		},
		{
			name:            "template execution error",
			template:        "{{.NonExistentField}}",
			expectedFallback: true,
		},
		{
			name:            "template with invalid function",
			template:        "{{.AlertName|nonexistentfunc}}",
			expectedFallback: true,
		},
		{
			name:            "valid template",
			template:        "https://grafana.example.com/alert/{{.AlertName}}",
			expectedFallback: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockNotifier := &mockNotifierManager{}
			alertCfg := &mockAlertConfig{}
			alertCfg.setGeneratorURLTemplate(tt.template, "")
			
			sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
			
			alert := &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "TestAlert",
				}),
				State:   rules.StateFiring,
				FiredAt: time.Now(),
			}
			
			ctx := context.Background()
			sendAlertsFunc(ctx, expr, alert)
			
			if len(mockNotifier.sentAlerts) != 1 {
				t.Errorf("expected 1 alert to be sent, got %d", len(mockNotifier.sentAlerts))
				return
			}
			
			sentAlert := mockNotifier.sentAlerts[0]
			expectedFallbackURL := externalURL + strutil.TableLinkForExpression(expr)
			
			if tt.expectedFallback {
				if sentAlert.GeneratorURL != expectedFallbackURL {
					t.Errorf("expected fallback URL %q, got %q", expectedFallbackURL, sentAlert.GeneratorURL)
				}
			} else {
				expectedURL := "https://grafana.example.com/alert/TestAlert"
				if sentAlert.GeneratorURL != expectedURL {
					t.Errorf("expected template URL %q, got %q", expectedURL, sentAlert.GeneratorURL)
				}
			}
		})
	}
}

// TestConfigurationReloading tests that template changes are applied correctly during configuration reloads
func TestConfigurationReloading(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "up{job=\"test\"} < 1"
	
	// Simulate configuration reloading
	alertCfg := &mockAlertConfig{}
	
	// Initial configuration
	alertCfg.setGeneratorURLTemplate("https://initial.example.com/alert/{{.AlertName}}", "")
	
	mockNotifier := &mockNotifierManager{}
	sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
	
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "TestAlert",
		}),
		State:   rules.StateFiring,
		FiredAt: time.Now(),
	}
	
	// Test initial configuration
	ctx := context.Background()
	sendAlertsFunc(ctx, expr, alert)
	
	if len(mockNotifier.sentAlerts) != 1 {
		t.Errorf("expected 1 alert to be sent, got %d", len(mockNotifier.sentAlerts))
		return
	}
	
	expectedInitialURL := "https://initial.example.com/alert/TestAlert"
	if mockNotifier.sentAlerts[0].GeneratorURL != expectedInitialURL {
		t.Errorf("expected initial URL %q, got %q", expectedInitialURL, mockNotifier.sentAlerts[0].GeneratorURL)
	}
	
	// Simulate configuration reload with new template
	mockNotifier.sentAlerts = nil // Reset sent alerts
	alertCfg.setGeneratorURLTemplate("https://reloaded.example.com/alert/{{.AlertName}}", "")
	
	// Test reloaded configuration
	sendAlertsFunc(ctx, expr, alert)
	
	if len(mockNotifier.sentAlerts) != 1 {
		t.Errorf("expected 1 alert to be sent after reload, got %d", len(mockNotifier.sentAlerts))
		return
	}
	
	expectedReloadedURL := "https://reloaded.example.com/alert/TestAlert"
	if mockNotifier.sentAlerts[0].GeneratorURL != expectedReloadedURL {
		t.Errorf("expected reloaded URL %q, got %q", expectedReloadedURL, mockNotifier.sentAlerts[0].GeneratorURL)
	}
	
	// Test CLI override takes precedence
	mockNotifier.sentAlerts = nil // Reset sent alerts
	alertCfg.setGeneratorURLTemplate("https://config.example.com/alert/{{.AlertName}}", "https://cli.example.com/alert/{{.AlertName}}")
	
	sendAlertsFunc(ctx, expr, alert)
	
	if len(mockNotifier.sentAlerts) != 1 {
		t.Errorf("expected 1 alert to be sent with CLI override, got %d", len(mockNotifier.sentAlerts))
		return
	}
	
	expectedCLIURL := "https://cli.example.com/alert/TestAlert"
	if mockNotifier.sentAlerts[0].GeneratorURL != expectedCLIURL {
		t.Errorf("expected CLI override URL %q, got %q", expectedCLIURL, mockNotifier.sentAlerts[0].GeneratorURL)
	}
}

// TestBackwardCompatibility tests that existing alert processing continues to work unchanged when no templates are configured
func TestBackwardCompatibility(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "up{job=\"test\"} < 1"
	
	// Test with no template configured (backward compatibility)
	mockNotifier := &mockNotifierManager{}
	alertCfg := &mockAlertConfig{}
	// Don't set any template - should use default behavior
	
	sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
	
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "TestAlert",
			"severity":  "critical",
		}),
		Annotations: labels.FromMap(map[string]string{
			"summary": "Test alert summary",
		}),
		State:   rules.StateFiring,
		FiredAt: time.Now(),
	}
	
	ctx := context.Background()
	sendAlertsFunc(ctx, expr, alert)
	
	if len(mockNotifier.sentAlerts) != 1 {
		t.Errorf("expected 1 alert to be sent, got %d", len(mockNotifier.sentAlerts))
		return
	}
	
	sentAlert := mockNotifier.sentAlerts[0]
	expectedDefaultURL := externalURL + strutil.TableLinkForExpression(expr)
	
	if sentAlert.GeneratorURL != expectedDefaultURL {
		t.Errorf("expected default URL %q, got %q", expectedDefaultURL, sentAlert.GeneratorURL)
	}
	
	// Verify all other alert properties are preserved (backward compatibility)
	if sentAlert.Labels.Get("alertname") != "TestAlert" {
		t.Errorf("expected alertname to be preserved")
	}
	if sentAlert.Labels.Get("severity") != "critical" {
		t.Errorf("expected severity label to be preserved")
	}
	if sentAlert.Annotations.Get("summary") != "Test alert summary" {
		t.Errorf("expected summary annotation to be preserved")
	}
	if sentAlert.StartsAt.IsZero() {
		t.Errorf("expected StartsAt to be set")
	}
}

// TestTemplateDirectoryIntegration tests directory-based template loading
func TestTemplateDirectoryIntegration(t *testing.T) {
	// Create temporary directory structure
	tempDir, err := os.MkdirTemp("", "template_integration_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create template files with nested structure
	templates := map[string]string{
		"grafana.tmpl": `https://grafana.example.com/d/alerts?var-alertname={{.AlertName|urlquery}}`,
		"alerts/critical.tmpl": `https://oncall.example.com/critical/{{.AlertName}}?instance={{.Labels.instance}}`,
	}

	// Create template files
	for filePath, content := range templates {
		fullPath := filepath.Join(tempDir, filePath)
		
		// Create subdirectories
		dir := filepath.Dir(fullPath)
		err := os.MkdirAll(dir, 0755)
		require.NoError(t, err)
		
		err = os.WriteFile(fullPath, []byte(content), 0644)
		require.NoError(t, err)
	}

	// Initialize template manager and load templates
	tm := NewTemplateManager()
	err = tm.LoadFromDirectory(tempDir)
	require.NoError(t, err)

	// Verify templates were loaded with correct names
	expectedTemplates := []string{"grafana", "alerts.critical"}
	loadedTemplates := tm.ListTemplates()
	assert.Len(t, loadedTemplates, len(expectedTemplates))

	// Test template execution
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "HighCPU",
			"instance":  "server1:9100",
		}),
	}

	result, err := ExecuteTemplateByName(tm, "grafana", alert, "up", "http://prometheus.example.com:9090")
	assert.NoError(t, err)
	assert.Equal(t, "https://grafana.example.com/d/alerts?var-alertname=HighCPU", result)

	result, err = ExecuteTemplateByName(tm, "alerts.critical", alert, "up", "http://prometheus.example.com:9090")
	assert.NoError(t, err)
	assert.Equal(t, "https://oncall.example.com/critical/HighCPU?instance=server1:9100", result)
}

// TestTemplateDirectoryErrorHandling tests error handling for directory templates
func TestTemplateDirectoryErrorHandling(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "template_error_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create mix of valid and invalid templates
	templates := map[string]string{
		"valid.tmpl":   "https://grafana.example.com/alert/{{.AlertName}}",
		"invalid.tmpl": "https://example.com/{{.InvalidSyntax",
		"readme.txt":   "This is not a template file",
	}

	for filename, content := range templates {
		err = os.WriteFile(filepath.Join(tempDir, filename), []byte(content), 0644)
		require.NoError(t, err)
	}

	// Load templates - should not fail even with invalid templates
	tm := NewTemplateManager()
	err = tm.LoadFromDirectory(tempDir)
	require.NoError(t, err)

	// Should only load valid templates
	loadedTemplates := tm.ListTemplates()
	assert.Len(t, loadedTemplates, 1) // only valid.tmpl

	// Verify valid template works
	content, exists := tm.GetTemplate("valid")
	assert.True(t, exists)
	assert.Equal(t, "https://grafana.example.com/alert/{{.AlertName}}", content)

	// Verify invalid templates don't exist
	_, exists = tm.GetTemplate("invalid")
	assert.False(t, exists)
	_, exists = tm.GetTemplate("readme")
	assert.False(t, exists)
}

// TestComplexTemplateScenarios tests complex real-world template scenarios
func TestComplexTemplateScenarios(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "rate(http_requests_total[5m]) > 0.1"
	
	tests := []struct {
		name        string
		template    string
		alert       *rules.Alert
		expectedURL string
	}{
		{
			name:     "Grafana dashboard with multiple variables",
			template: "https://grafana.example.com/d/dashboard?var-instance={{.Labels.instance|urlquery}}&var-job={{.Labels.job|urlquery}}&from={{.Labels.start_time|urlquery}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname":  "HighRequestRate",
					"instance":   "web-01:8080",
					"job":        "web-server",
					"start_time": "2023-01-01T00:00:00Z",
				}),
				State:   rules.StateFiring,
				FiredAt: time.Now(),
			},
			expectedURL: "https://grafana.example.com/d/dashboard?var-instance=web-01%3A8080&var-job=web-server&from=2023-01-01T00%3A00%3A00Z",
		},
		{
			name:     "PagerDuty integration with alert context",
			template: "https://pagerduty.example.com/incidents/new?title={{.AlertName|urlquery}}&description={{.Annotations.summary|urlquery}}&severity={{.Labels.severity|urlquery}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "DatabaseDown",
					"severity":  "critical",
				}),
				Annotations: labels.FromMap(map[string]string{
					"summary": "Database connection failed",
				}),
				State:   rules.StateFiring,
				FiredAt: time.Now(),
			},
			expectedURL: "https://pagerduty.example.com/incidents/new?title=DatabaseDown&description=Database+connection+failed&severity=critical",
		},
		{
			name:     "Custom dashboard with expression and external URL",
			template: "{{.ExternalURL}}/custom/dashboard?query={{.Expr|urlquery}}&alert={{.AlertName|urlquery}}",
			alert: &rules.Alert{
				Labels: labels.FromMap(map[string]string{
					"alertname": "CustomAlert",
				}),
				State:   rules.StateFiring,
				FiredAt: time.Now(),
			},
			expectedURL: "http://prometheus.example.com:9090/custom/dashboard?query=rate%28http_requests_total%5B5m%5D%29+%3E+0.1&alert=CustomAlert",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockNotifier := &mockNotifierManager{}
			alertCfg := &mockAlertConfig{}
			alertCfg.setGeneratorURLTemplate(tt.template, "")
			
			sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
			
			ctx := context.Background()
			sendAlertsFunc(ctx, expr, tt.alert)
			
			if len(mockNotifier.sentAlerts) != 1 {
				t.Errorf("expected 1 alert to be sent, got %d", len(mockNotifier.sentAlerts))
				return
			}
			
			sentAlert := mockNotifier.sentAlerts[0]
			if sentAlert.GeneratorURL != tt.expectedURL {
				t.Errorf("expected URL %q, got %q", tt.expectedURL, sentAlert.GeneratorURL)
			}
		})
	}
}



