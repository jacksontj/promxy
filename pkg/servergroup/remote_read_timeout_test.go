package servergroup

import (
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

func TestRemoteReadTimeoutFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		timeout time.Duration
		want    time.Duration
	}{
		{
			name:    "unset falls back to the default",
			timeout: 0,
			want:    DefaultRemoteReadTimeout,
		},
		{
			name:    "a configured timeout is honored",
			timeout: 30 * time.Second,
			want:    30 * time.Second,
		},
		{
			name:    "a timeout longer than the default is honored too",
			timeout: 10 * time.Minute,
			want:    10 * time.Minute,
		},
		{
			name:    "a negative timeout falls back rather than disabling the bound",
			timeout: -1 * time.Second,
			want:    DefaultRemoteReadTimeout,
		},
	}

	u, err := url.Parse("http://localhost:9090/api/v1/read")
	if err != nil {
		t.Fatalf("parsing url: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{Timeout: tt.timeout}

			got := newRemoteReadConfig(cfg, u)

			if time.Duration(got.Timeout) != tt.want {
				t.Errorf("Timeout = %v, want %v", time.Duration(got.Timeout), tt.want)
			}
			if got.URL == nil || got.URL.URL != u {
				t.Errorf("URL = %v, want %v", got.URL, u)
			}
			if got.ChunkedReadLimit == 0 {
				t.Errorf("expected ChunkedReadLimit to be set")
			}
		})
	}
}

// TestRemoteReadTimeoutFromYAML checks the server group's timeout reaches the
// remote_read client config, rather than only the helper honoring it.
func TestRemoteReadTimeoutFromYAML(t *testing.T) {
	tests := []struct {
		name       string
		yamlConfig string
		want       time.Duration
	}{
		{
			name: "timeout applies to remote_read",
			yamlConfig: `
remote_read: true
remote_read_path: /api/v1/read
timeout: 15s
static_configs:
  - targets:
      - localhost:9090
`,
			want: 15 * time.Second,
		},
		{
			name: "no timeout keeps the default",
			yamlConfig: `
remote_read: true
remote_read_path: /api/v1/read
static_configs:
  - targets:
      - localhost:9090
`,
			want: DefaultRemoteReadTimeout,
		},
	}

	u, err := url.Parse("http://localhost:9090/api/v1/read")
	if err != nil {
		t.Fatalf("parsing url: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg Config
			if err := yaml.Unmarshal([]byte(tt.yamlConfig), &cfg); err != nil {
				t.Fatalf("unmarshaling config: %v", err)
			}
			if !cfg.RemoteRead {
				t.Fatalf("expected remote_read to be enabled")
			}

			got := newRemoteReadConfig(&cfg, u)

			if model.Duration(tt.want) != got.Timeout {
				t.Errorf("Timeout = %v, want %v", time.Duration(got.Timeout), tt.want)
			}
		})
	}
}
