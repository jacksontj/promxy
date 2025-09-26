package proxyconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigFromFile(t *testing.T) {
	file, err := os.CreateTemp(os.TempDir(), "")
	if err != nil {
		t.Errorf("Could not create temp file: %v", err)
	}
	defer os.Remove(file.Name())

	fileContents := `tls_server_config:
  cert_file: "server.crt"
  key_file: "server.key"
  client_auth_type: "VerifyClientCertIfGiven"
  client_ca_file: "tls-ca-chain.pem"`

	file.Write([]byte(fileContents))
	configFilePath := file.Name()

	cfg, err := ConfigFromFile(configFilePath)
	if err != nil {
		t.Errorf("Error was not nil: %+v", err)
	}

	if cfg.WebConfig.TLSCertPath != "server.crt" {
		t.Errorf("Invalid TLSCertPath. Expected 'server.crt', Got '%s'", cfg.WebConfig.TLSCertPath)
	}

	if cfg.WebConfig.TLSKeyPath != "server.key" {
		t.Errorf("Invalid TLSKeyPath. Expected 'server.key', Got '%s'", cfg.WebConfig.TLSKeyPath)
	}

	if cfg.WebConfig.ClientAuth != "VerifyClientCertIfGiven" {
		t.Errorf("Invalid ClientAuth. Expected 'VerifyClientCertIfGiven', Got '%s'", cfg.WebConfig.ClientAuth)
	}

	if cfg.WebConfig.ClientCAs != "tls-ca-chain.pem" {
		t.Errorf("Invalid ClientCAs. Expected 'tls-ca-chain.pem', Got '%s'", cfg.WebConfig.ClientCAs)
	}
}

func TestConfigFromFile_AlertTemplates(t *testing.T) {
	tests := []struct {
		name                 string
		configContent        string
		expectedDefault      string
		expectedDirectory    string
		expectedNamed        map[string]string
		expectedRules        []TemplateRule
		expectError          bool
	}{
		{
			name: "config with default template",
			configContent: `
promxy:
  alert_templates:
    default: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
`,
			expectedDefault:   "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			expectedDirectory: "",
			expectedNamed:     nil,
			expectedRules:     nil,
			expectError:       false,
		},
		{
			name: "config with template directory",
			configContent: `
promxy:
  alert_templates:
    directory: "/etc/promxy/templates"
`,
			expectedDefault:   "",
			expectedDirectory: "/etc/promxy/templates",
			expectedNamed:     nil,
			expectedRules:     nil,
			expectError:       false,
		},
		{
			name: "config with named templates",
			configContent: `
promxy:
  alert_templates:
    named:
      grafana: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
      pagerduty: "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}"
`,
			expectedDefault:   "",
			expectedDirectory: "",
			expectedNamed: map[string]string{
				"grafana":   "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
				"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
			},
			expectedRules: nil,
			expectError:   false,
		},
		{
			name: "config with template rules",
			configContent: `
promxy:
  alert_templates:
    default: "https://prometheus.example.com/graph?g0.expr={{.Expr|urlquery}}"
    rules:
      - match_labels:
          severity: "critical"
        template: "https://pagerduty.example.com/incidents/{{.AlertName}}"
      - match_labels:
          team: "frontend"
        template: "https://grafana.example.com/d/frontend?alertname={{.AlertName|urlquery}}"
`,
			expectedDefault:   "https://prometheus.example.com/graph?g0.expr={{.Expr|urlquery}}",
			expectedDirectory: "",
			expectedNamed:     nil,
			expectedRules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "https://pagerduty.example.com/incidents/{{.AlertName}}",
				},
				{
					MatchLabels: map[string]string{"team": "frontend"},
					Template:    "https://grafana.example.com/d/frontend?alertname={{.AlertName|urlquery}}",
				},
			},
			expectError: false,
		},
		{
			name: "config with all template features",
			configContent: `
promxy:
  alert_templates:
    default: "https://prometheus.example.com/graph?g0.expr={{.Expr|urlquery}}"
    directory: "/etc/promxy/templates"
    named:
      grafana: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
      pagerduty: "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}"
    rules:
      - match_labels:
          severity: "critical"
        template: "pagerduty"
`,
			expectedDefault:   "https://prometheus.example.com/graph?g0.expr={{.Expr|urlquery}}",
			expectedDirectory: "/etc/promxy/templates",
			expectedNamed: map[string]string{
				"grafana":   "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
				"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
			},
			expectedRules: []TemplateRule{
				{
					MatchLabels: map[string]string{"severity": "critical"},
					Template:    "pagerduty",
				},
			},
			expectError: false,
		},
		{
			name: "empty alert templates config",
			configContent: `
promxy:
  alert_templates: {}
`,
			expectedDefault:   "",
			expectedDirectory: "",
			expectedNamed:     nil,
			expectedRules:     nil,
			expectError:       false,
		},
		{
			name: "invalid YAML",
			configContent: `
promxy:
  alert_templates:
    default: "test"
    invalid_yaml_syntax
`,
			expectedDefault:   "",
			expectedDirectory: "",
			expectedNamed:     nil,
			expectedRules:     nil,
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temporary config file
			tmpDir := t.TempDir()
			configFile := filepath.Join(tmpDir, "config.yaml")
			
			err := os.WriteFile(configFile, []byte(tt.configContent), 0644)
			if err != nil {
				t.Fatalf("failed to write config file: %v", err)
			}

			// Load config
			cfg, err := ConfigFromFile(configFile)
			
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
			
			alertTemplates := cfg.PromxyConfig.AlertTemplates
			
			if alertTemplates.Default != tt.expectedDefault {
				t.Errorf("expected default template %q, got %q", tt.expectedDefault, alertTemplates.Default)
			}
			
			if alertTemplates.Directory != tt.expectedDirectory {
				t.Errorf("expected template directory %q, got %q", tt.expectedDirectory, alertTemplates.Directory)
			}
			
			// Check named templates
			if tt.expectedNamed == nil {
				if alertTemplates.Named != nil && len(alertTemplates.Named) > 0 {
					t.Errorf("expected no named templates, got %v", alertTemplates.Named)
				}
			} else {
				if alertTemplates.Named == nil {
					t.Error("expected named templates but got nil")
					return
				}
				
				if len(alertTemplates.Named) != len(tt.expectedNamed) {
					t.Errorf("expected %d named templates, got %d", len(tt.expectedNamed), len(alertTemplates.Named))
				}
				
				for name, expectedContent := range tt.expectedNamed {
					if actualContent, exists := alertTemplates.Named[name]; !exists {
						t.Errorf("expected named template %q not found", name)
					} else if actualContent != expectedContent {
						t.Errorf("named template %q: expected %q, got %q", name, expectedContent, actualContent)
					}
				}
			}
			
			// Check template rules
			if tt.expectedRules == nil {
				if alertTemplates.Rules != nil && len(alertTemplates.Rules) > 0 {
					t.Errorf("expected no template rules, got %v", alertTemplates.Rules)
				}
			} else {
				if alertTemplates.Rules == nil {
					t.Error("expected template rules but got nil")
					return
				}
				
				if len(alertTemplates.Rules) != len(tt.expectedRules) {
					t.Errorf("expected %d template rules, got %d", len(tt.expectedRules), len(alertTemplates.Rules))
				}
				
				for i, expectedRule := range tt.expectedRules {
					if i >= len(alertTemplates.Rules) {
						t.Errorf("expected rule %d not found", i)
						continue
					}
					
					actualRule := alertTemplates.Rules[i]
					
					if actualRule.Template != expectedRule.Template {
						t.Errorf("rule %d template: expected %q, got %q", i, expectedRule.Template, actualRule.Template)
					}
					
					if len(actualRule.MatchLabels) != len(expectedRule.MatchLabels) {
						t.Errorf("rule %d match labels count: expected %d, got %d", i, len(expectedRule.MatchLabels), len(actualRule.MatchLabels))
					}
					
					for key, expectedValue := range expectedRule.MatchLabels {
						if actualValue, exists := actualRule.MatchLabels[key]; !exists {
							t.Errorf("rule %d match label %q not found", i, key)
						} else if actualValue != expectedValue {
							t.Errorf("rule %d match label %q: expected %q, got %q", i, key, expectedValue, actualValue)
						}
					}
				}
			}
		})
	}
}
