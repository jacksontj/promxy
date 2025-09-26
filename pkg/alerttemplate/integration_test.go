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

// TestIntegrationScenarios tests the complete alerttemplate package functionality
func TestIntegrationScenarios(t *testing.T) {
	externalURL := "http://prometheus.example.com:9090"
	expr := "up{instance=\"web-server-01\"}"
	
	alert := &rules.Alert{
		Labels: labels.FromMap(map[string]string{
			"alertname": "HighMemoryUsage",
			"instance":  "web-server-01:9100",
			"job":       "node-exporter",
			"severity":  "critical",
		}),
		Annotations: labels.FromMap(map[string]string{
			"summary": "High memory usage detected",
		}),
		State:   rules.StateFiring,
		FiredAt: time.Now(),
	}

	// Test basic template execution
	result, err := ExecuteGeneratorURLTemplate(
		"https://grafana.example.com/d/dashboard?var-instance={{.Labels.instance}}",
		alert, expr, externalURL,
	)
	assert.NoError(t, err)
	assert.Equal(t, "https://grafana.example.com/d/dashboard?var-instance=web-server-01:9100", result)

	// Test template with external URL and expression
	result, err = ExecuteGeneratorURLTemplate(
		"{{.ExternalURL}}/graph?g0.expr={{.Expr|urlquery}}",
		alert, expr, externalURL,
	)
	assert.NoError(t, err)
	assert.Equal(t, "http://prometheus.example.com:9090/graph?g0.expr=up%7Binstance%3D%22web-server-01%22%7D", result)

	// Test error handling with malformed template
	_, err = ExecuteGeneratorURLTemplate("{{.NonExistentField}}", alert, expr, externalURL)
	assert.Error(t, err)

	// Test directory-based template loading
	tempDir, err := os.MkdirTemp("", "template_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	err = os.WriteFile(filepath.Join(tempDir, "grafana.tmpl"), 
		[]byte("https://grafana.example.com/alert/{{.AlertName}}"), 0644)
	require.NoError(t, err)

	tm := NewTemplateManager()
	err = tm.LoadFromDirectory(tempDir)
	assert.NoError(t, err)
	assert.Len(t, tm.ListTemplates(), 1)

	// Test rule-based template selection
	templateRules := []TemplateRule{
		{
			MatchLabels: map[string]string{"severity": "critical"},
			Template:    "grafana",
		},
	}
	
	namedTemplates := map[string]string{
		"grafana": "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
	}
	err = tm.LoadInlineTemplates(namedTemplates)
	require.NoError(t, err)

	selectedTemplate := SelectTemplate(templateRules, "default", tm, alert)
	assert.Equal(t, "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}", selectedTemplate)

	// Test end-to-end alert processing with mock notifier
	mockNotifier := &mockNotifierManager{}
	alertCfg := &mockAlertConfig{}
	alertCfg.setGeneratorURLTemplate("https://grafana.example.com/alert/{{.AlertName}}", "")
	
	sendAlertsFunc := createSendAlertsFunc(mockNotifier, externalURL, alertCfg)
	ctx := context.Background()
	sendAlertsFunc(ctx, expr, alert)
	
	require.Len(t, mockNotifier.sentAlerts, 1)
	assert.Equal(t, "https://grafana.example.com/alert/HighMemoryUsage", mockNotifier.sentAlerts[0].GeneratorURL)
	assert.Equal(t, "HighMemoryUsage", mockNotifier.sentAlerts[0].Labels.Get("alertname"))

	// Test fallback behavior on template failure
	mockNotifier.sentAlerts = nil
	alertCfg.setGeneratorURLTemplate("{{.InvalidField}}", "")
	sendAlertsFunc(ctx, expr, alert)
	
	require.Len(t, mockNotifier.sentAlerts, 1)
	expectedFallbackURL := externalURL + strutil.TableLinkForExpression(expr)
	assert.Equal(t, expectedFallbackURL, mockNotifier.sentAlerts[0].GeneratorURL)

	// Test backward compatibility (no template configured)
	mockNotifier.sentAlerts = nil
	alertCfg.setGeneratorURLTemplate("", "")
	sendAlertsFunc(ctx, expr, alert)
	
	require.Len(t, mockNotifier.sentAlerts, 1)
	assert.Equal(t, expectedFallbackURL, mockNotifier.sentAlerts[0].GeneratorURL)
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





