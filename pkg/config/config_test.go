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

func TestConfigFromFile_GeneratorURLTemplate(t *testing.T) {
	tests := []struct {
		name                        string
		configContent               string
		expectedGeneratorURLTemplate string
		expectedTemplateDirectory   string
		expectedInlineTemplates     map[string]string
		expectError                 bool
	}{
		{
			name: "config with generator URL template",
			configContent: `
promxy:
  generator_url_template: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
`,
			expectedGeneratorURLTemplate: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			expectedTemplateDirectory: "",
			expectedInlineTemplates: nil,
			expectError:      false,
		},
		{
			name: "config with template directory",
			configContent: `
promxy:
  template_directory: "/etc/promxy/templates"
`,
			expectedGeneratorURLTemplate: "",
			expectedTemplateDirectory: "/etc/promxy/templates",
			expectedInlineTemplates: nil,
			expectError:      false,
		},
		{
			name: "config with both template and directory",
			configContent: `
promxy:
  generator_url_template: "https://grafana.example.com/alert/{{.AlertName}}"
  template_directory: "/etc/promxy/templates"
`,
			expectedGeneratorURLTemplate: "https://grafana.example.com/alert/{{.AlertName}}",
			expectedTemplateDirectory: "/etc/promxy/templates",
			expectedInlineTemplates: nil,
			expectError:      false,
		},
		{
			name: "config with inline templates",
			configContent: `
promxy:
  templates:
    grafana: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
    pagerduty: "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}"
`,
			expectedGeneratorURLTemplate: "",
			expectedTemplateDirectory: "",
			expectedInlineTemplates: map[string]string{
				"grafana": "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
				"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
			},
			expectError:      false,
		},
		{
			name: "config with all template types",
			configContent: `
promxy:
  generator_url_template: "https://prometheus.example.com/graph?g0.expr={{.Expr|urlquery}}"
  template_directory: "/etc/promxy/templates"
  templates:
    grafana: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
    pagerduty: "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}"
`,
			expectedGeneratorURLTemplate: "https://prometheus.example.com/graph?g0.expr={{.Expr|urlquery}}",
			expectedTemplateDirectory: "/etc/promxy/templates",
			expectedInlineTemplates: map[string]string{
				"grafana": "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
				"pagerduty": "https://pagerduty.example.com/incidents/{{.Labels.incident_id}}",
			},
			expectError:      false,
		},
		{
			name: "empty promxy config",
			configContent: `
promxy: {}
`,
			expectedGeneratorURLTemplate: "",
			expectedTemplateDirectory: "",
			expectedInlineTemplates: nil,
			expectError:      false,
		},
		{
			name: "invalid YAML",
			configContent: `
promxy:
  generator_url_template: "test"
  invalid_yaml_syntax
`,
			expectedGeneratorURLTemplate: "",
			expectedTemplateDirectory: "",
			expectedInlineTemplates: nil,
			expectError:      true,
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
			
			if cfg.PromxyConfig.GeneratorURLTemplate != tt.expectedGeneratorURLTemplate {
				t.Errorf("expected generator URL template %q, got %q", tt.expectedGeneratorURLTemplate, cfg.PromxyConfig.GeneratorURLTemplate)
			}
			
			if cfg.PromxyConfig.TemplateDirectory != tt.expectedTemplateDirectory {
				t.Errorf("expected template directory %q, got %q", tt.expectedTemplateDirectory, cfg.PromxyConfig.TemplateDirectory)
			}
			
			// Check inline templates
			if tt.expectedInlineTemplates == nil {
				if cfg.PromxyConfig.Templates != nil && len(cfg.PromxyConfig.Templates) > 0 {
					t.Errorf("expected no inline templates, got %v", cfg.PromxyConfig.Templates)
				}
			} else {
				if cfg.PromxyConfig.Templates == nil {
					t.Error("expected inline templates but got nil")
					return
				}
				
				if len(cfg.PromxyConfig.Templates) != len(tt.expectedInlineTemplates) {
					t.Errorf("expected %d inline templates, got %d", len(tt.expectedInlineTemplates), len(cfg.PromxyConfig.Templates))
				}
				
				for name, expectedContent := range tt.expectedInlineTemplates {
					if actualContent, exists := cfg.PromxyConfig.Templates[name]; !exists {
						t.Errorf("expected inline template %q not found", name)
					} else if actualContent != expectedContent {
						t.Errorf("inline template %q: expected %q, got %q", name, expectedContent, actualContent)
					}
				}
			}
		})
	}
}
