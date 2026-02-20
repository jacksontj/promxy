package servergroup

import (
	"testing"

	"gopkg.in/yaml.v2"
)

func TestSigV4ConfigParsing(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid sigv4 with all fields",
			config: `
http_client:
  sigv4:
    region: us-west-2
    access_key: AKIAIOSFODNN7EXAMPLE
    secret_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
    profile: my-profile
    role_arn: arn:aws:iam::123456789012:role/PrometheusQueryRole
`,
			wantErr: false,
		},
		{
			name: "valid sigv4 with only region",
			config: `
http_client:
  sigv4:
    region: us-west-2
`,
			wantErr: false,
		},
		{
			name: "configuration without sigv4 section (backward compatibility)",
			config: `
http_client:
  dial_timeout: 5s
`,
			wantErr: false,
		},
		{
			name: "partial credentials - access_key without secret_key",
			config: `
http_client:
  sigv4:
    region: us-west-2
    access_key: AKIAIOSFODNN7EXAMPLE
`,
			wantErr: true,
			errMsg:  "must provide a AWS SigV4 Access key and Secret Key",
		},
		{
			name: "partial credentials - secret_key without access_key",
			config: `
http_client:
  sigv4:
    region: us-west-2
    secret_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
`,
			wantErr: true,
			errMsg:  "must provide a AWS SigV4 Access key and Secret Key",
		},
		{
			name: "empty sigv4 configuration block",
			config: `
http_client:
  sigv4: {}
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Verify backward compatibility - config without sigv4 should have nil SigV4Config
			if tt.name == "configuration without sigv4 section (backward compatibility)" {
				if cfg.HTTPConfig.SigV4Config != nil {
					t.Errorf("expected SigV4Config to be nil for config without sigv4 section")
				}
			}

			// Verify sigv4 with only region is parsed correctly
			if tt.name == "valid sigv4 with only region" {
				if cfg.HTTPConfig.SigV4Config == nil {
					t.Errorf("expected SigV4Config to be non-nil")
					return
				}
				if cfg.HTTPConfig.SigV4Config.Region != "us-west-2" {
					t.Errorf("expected region to be us-west-2, got %s", cfg.HTTPConfig.SigV4Config.Region)
				}
			}

			// Verify all fields are parsed correctly
			if tt.name == "valid sigv4 with all fields" {
				if cfg.HTTPConfig.SigV4Config == nil {
					t.Errorf("expected SigV4Config to be non-nil")
					return
				}
				if cfg.HTTPConfig.SigV4Config.Region != "us-west-2" {
					t.Errorf("expected region to be us-west-2, got %s", cfg.HTTPConfig.SigV4Config.Region)
				}
				if cfg.HTTPConfig.SigV4Config.AccessKey != "AKIAIOSFODNN7EXAMPLE" {
					t.Errorf("expected access_key to be AKIAIOSFODNN7EXAMPLE, got %s", cfg.HTTPConfig.SigV4Config.AccessKey)
				}
				if string(cfg.HTTPConfig.SigV4Config.SecretKey) != "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" {
					t.Errorf("expected secret_key to match")
				}
				if cfg.HTTPConfig.SigV4Config.Profile != "my-profile" {
					t.Errorf("expected profile to be my-profile, got %s", cfg.HTTPConfig.SigV4Config.Profile)
				}
				if cfg.HTTPConfig.SigV4Config.RoleARN != "arn:aws:iam::123456789012:role/PrometheusQueryRole" {
					t.Errorf("expected role_arn to match, got %s", cfg.HTTPConfig.SigV4Config.RoleARN)
				}
			}
		})
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestAuthenticationValidation(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "sigv4 and basic_auth (should fail)",
			config: `
http_client:
  sigv4:
    region: us-west-2
  basic_auth:
    username: user
    password: pass
`,
			wantErr: true,
			errMsg:  "at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured",
		},
		{
			name: "sigv4 and bearer_token (should fail)",
			config: `
http_client:
  sigv4:
    region: us-west-2
  bearer_token: mytoken
`,
			wantErr: true,
			errMsg:  "at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured",
		},
		{
			name: "sigv4 and bearer_token_file (should fail)",
			config: `
http_client:
  sigv4:
    region: us-west-2
  bearer_token_file: /path/to/token
`,
			wantErr: true,
			errMsg:  "at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured",
		},
		{
			name: "sigv4 and authorization (should fail)",
			config: `
http_client:
  sigv4:
    region: us-west-2
  authorization:
    type: Bearer
    credentials: mytoken
`,
			wantErr: true,
			errMsg:  "at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured",
		},
		{
			name: "sigv4 only (should succeed)",
			config: `
http_client:
  sigv4:
    region: us-west-2
`,
			wantErr: false,
		},
		{
			name: "basic_auth only (should succeed)",
			config: `
http_client:
  basic_auth:
    username: user
    password: pass
`,
			wantErr: false,
		},
		{
			name: "no auth methods (should succeed)",
			config: `
http_client:
  dial_timeout: 5s
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
		})
	}
}

func TestSigV4ErrorScenarios(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr bool
		errMsg  string
	}{
		{
			name: "missing region error scenario",
			config: `
http_client:
  sigv4:
    access_key: AKIAIOSFODNN7EXAMPLE
    secret_key: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY
`,
			wantErr: false, // Region validation happens at runtime in sigv4 package, not during config parsing
		},
		{
			name: "empty sigv4 config allows credential chain resolution",
			config: `
http_client:
  sigv4: {}
`,
			wantErr: false, // Empty config is valid, will use credential chain at runtime
		},
		{
			name: "sigv4 with profile only",
			config: `
http_client:
  sigv4:
    region: us-west-2
    profile: my-profile
`,
			wantErr: false,
		},
		{
			name: "sigv4 with role_arn only",
			config: `
http_client:
  sigv4:
    region: us-west-2
    role_arn: arn:aws:iam::123456789012:role/PrometheusQueryRole
`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
					return
				}
				if tt.errMsg != "" && !contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error containing %q, got %q", tt.errMsg, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Verify the config was parsed correctly
			if cfg.HTTPConfig.SigV4Config != nil {
				t.Logf("SigV4Config parsed successfully for test: %s", tt.name)
			}
		})
	}
}

func TestBackwardCompatibility(t *testing.T) {
	tests := []struct {
		name           string
		config         string
		wantErr        bool
		checkSigV4Nil  bool
		checkOtherAuth bool
	}{
		{
			name: "parsing configuration without sigv4 section",
			config: `
static_configs:
  - targets:
      - localhost:9090
http_client:
  dial_timeout: 5s
  tls_config:
    insecure_skip_verify: true
`,
			wantErr:       false,
			checkSigV4Nil: true,
		},
		{
			name: "existing basic_auth configuration continues to work",
			config: `
static_configs:
  - targets:
      - localhost:9090
http_client:
  basic_auth:
    username: admin
    password: secret
`,
			wantErr:        false,
			checkSigV4Nil:  true,
			checkOtherAuth: true,
		},
		{
			name: "existing bearer_token configuration continues to work",
			config: `
static_configs:
  - targets:
      - localhost:9090
http_client:
  bearer_token: mytoken123
`,
			wantErr:        false,
			checkSigV4Nil:  true,
			checkOtherAuth: true,
		},
		{
			name: "minimal configuration without any auth",
			config: `
static_configs:
  - targets:
      - localhost:9090
`,
			wantErr:       false,
			checkSigV4Nil: true,
		},
		{
			name: "configuration with only scheme and targets",
			config: `
scheme: https
static_configs:
  - targets:
      - prometheus.example.com:9090
`,
			wantErr:       false,
			checkSigV4Nil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			err := yaml.Unmarshal([]byte(tt.config), &cfg)

			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Verify backward compatibility - SigV4Config should be nil
			if tt.checkSigV4Nil {
				if cfg.HTTPConfig.SigV4Config != nil {
					t.Errorf("expected SigV4Config to be nil for backward compatibility, got %+v", cfg.HTTPConfig.SigV4Config)
				}
			}

			// Verify other auth methods still work
			if tt.checkOtherAuth {
				if tt.name == "existing basic_auth configuration continues to work" {
					if cfg.HTTPConfig.HTTPConfig.BasicAuth == nil {
						t.Errorf("expected BasicAuth to be configured")
					} else if cfg.HTTPConfig.HTTPConfig.BasicAuth.Username != "admin" {
						t.Errorf("expected username to be 'admin', got %s", cfg.HTTPConfig.HTTPConfig.BasicAuth.Username)
					}
				}
				if tt.name == "existing bearer_token configuration continues to work" {
					if len(cfg.HTTPConfig.HTTPConfig.BearerToken) == 0 {
						t.Errorf("expected BearerToken to be configured")
					}
				}
			}

			t.Logf("Backward compatibility verified for test: %s", tt.name)
		})
	}
}
