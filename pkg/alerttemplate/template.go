package alerttemplate

import (
	"bytes"
	"fmt"
	"net/url"
	"text/template"

	"github.com/prometheus/prometheus/rules"
)

// TemplateData represents the data available to GeneratorURL templates
type TemplateData struct {
	ExternalURL  string            `json:"external_url"`  // Base URL of the Prometheus instance (e.g., "http://prometheus.example.com:9090")
	Expr         string            `json:"expr"`          // PromQL expression that triggered the alert
	Labels       map[string]string `json:"labels"`        // All alert labels
	Annotations  map[string]string `json:"annotations"`   // All alert annotations
	AlertName    string            `json:"alert_name"`    // Value of the "alertname" label
}

// templateFuncs provides template functions for URL encoding
var templateFuncs = template.FuncMap{
	"urlquery": url.QueryEscape,
	"urlpath":  url.PathEscape,
}

// ExecuteGeneratorURLTemplate executes a Go template for generating alert URLs
func ExecuteGeneratorURLTemplate(templateStr string, alert *rules.Alert, expr, externalURL string) (string, error) {
	if templateStr == "" {
		// Return empty string to indicate no template was provided
		// Caller should handle fallback logic
		return "", nil
	}
	
	if alert == nil {
		return "", fmt.Errorf("alert cannot be nil")
	}
	
	tmpl, err := template.New("generator").Funcs(templateFuncs).Parse(templateStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}
	
	data := TemplateData{
		ExternalURL: externalURL,
		Expr:        expr,
		Labels:      alert.Labels.Map(),
		Annotations: alert.Annotations.Map(),
		AlertName:   alert.Labels.Get("alertname"),
	}
	
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}
	
	return buf.String(), nil
}

