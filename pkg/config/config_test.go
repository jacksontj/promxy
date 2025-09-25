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
		name           string
		configContent  string
		expectedTemplate string
		expectError    bool
	}{
		{
			name: "config with generator URL template",
			configContent: `
promxy:
  generator_url_template: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}"
  server_groups:
    - static_configs:
        - targets: ["localhost:9090"]
`,
			expectedTemplate: "https://grafana.example.com/alerting/groups?alertname={{.AlertName|urlquery}}",
			expectError:      false,
		},
		{
			name: "config without generator URL template",
			configContent: `
promxy:
  server_groups:
    - static_configs:
        - targets: ["localhost:9090"]
`,
			expectedTemplate: "",
			expectError:      false,
		},
		{
			name: "invalid YAML",
			configContent: `
promxy:
  generator_url_template: "test"
  server_groups:
    - static_configs
        - targets: ["localhost:9090"]
`,
			expectedTemplate: "",
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
			
			if cfg.PromxyConfig.GeneratorURLTemplate != tt.expectedTemplate {
				t.Errorf("expected template %q, got %q", tt.expectedTemplate, cfg.PromxyConfig.GeneratorURLTemplate)
			}
		})
	}
}

func TestConfigFromFile_NonExistentFile(t *testing.T) {
	_, err := ConfigFromFile("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}