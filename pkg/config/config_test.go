package proxyconfig

import (
	"os"
	"testing"
)

func TestConfigFromFile(t *testing.T) {
	file, err := os.CreateTemp(os.TempDir(), "")
	if err != nil {
		t.Errorf("Could not create temp file:")
	}

	fileContents := `
tls_server_config:
  cert_file: "server.crt"
  key_file : "server.key"
  client_auth_type : "VerifyClientCertIfGiven"
  client_ca_file : "tls-ca-chain.pem"
`
	file.Write([]byte(fileContents))
	configFilePath := file.Name()

	cfg, err := ConfigFromFile(configFilePath)
	if err != nil {
		t.Errorf("Error was not nil: %+v", err)
	}

	if cfg.WebConfig.TLSCertPath != "server.crt" {
		t.Errorf("Invalid TLSKeypath. Expected 'server.crt', Got '%s'", cfg.WebConfig.TLSCertPath)
	}
	if cfg.WebConfig.TLSKeyPath != "server.key" {
		t.Errorf("Invalid TLSCertPath. Expected 'server.key', Got '%s'", cfg.WebConfig.TLSKeyPath)
	}
	if cfg.WebConfig.ClientAuth != "VerifyClientCertIfGiven" {
		t.Errorf("Invalid ClientAuth. Expected 'VerifyClientCertIfGiven', Got '%s'", cfg.WebConfig.ClientAuth)
	}
	if cfg.WebConfig.ClientCAs != "tls-ca-chain.pem" {
		t.Errorf("Invalid ClientCAs. Expected 'tls-ca-chain.pem', Got '%s'", cfg.WebConfig.ClientCAs)
	}
}

// TestRemoteWriteMaxSamplesPerSendDefault is a regression test for
// https://github.com/jacksontj/promxy/issues/781. Upstream's default
// max_samples_per_send (2000) can produce remote_write requests that decompress
// past the 32 MiB snappy limit Prometheus 3.5.3+ enforces on the receiver, so
// promxy lowers the default to DefaultMaxSamplesPerSend when the user does not
// set it explicitly -- while still honoring an explicit value.
func TestRemoteWriteMaxSamplesPerSendDefault(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []int
	}{
		{
			name: "unset uses promxy default",
			raw: `
remote_write:
  - url: http://localhost:1/api/v1/write
`,
			want: []int{DefaultMaxSamplesPerSend},
		},
		{
			name: "explicit value honored",
			raw: `
remote_write:
  - url: http://localhost:1/api/v1/write
    queue_config:
      max_samples_per_send: 2000
`,
			want: []int{2000},
		},
		{
			name: "explicit value matching promxy default honored",
			raw: `
remote_write:
  - url: http://localhost:1/api/v1/write
    queue_config:
      max_samples_per_send: 100
`,
			want: []int{100},
		},
		{
			name: "per-entry: only unset entries get the default",
			raw: `
remote_write:
  - url: http://localhost:1/api/v1/write
  - url: http://localhost:2/api/v1/write
    queue_config:
      max_samples_per_send: 1500
`,
			want: []int{DefaultMaxSamplesPerSend, 1500},
		},
		{
			name: "queue_config set without max_samples_per_send still gets default",
			raw: `
remote_write:
  - url: http://localhost:1/api/v1/write
    queue_config:
      max_shards: 10
`,
			want: []int{DefaultMaxSamplesPerSend},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ConfigFromBytes([]byte(tc.raw))
			if err != nil {
				t.Fatalf("ConfigFromBytes: %v", err)
			}
			if got := len(cfg.PromConfig.RemoteWriteConfigs); got != len(tc.want) {
				t.Fatalf("got %d remote_write configs, want %d", got, len(tc.want))
			}
			for i, want := range tc.want {
				if got := cfg.PromConfig.RemoteWriteConfigs[i].QueueConfig.MaxSamplesPerSend; got != want {
					t.Errorf("remote_write[%d] MaxSamplesPerSend = %d, want %d", i, got, want)
				}
			}
		})
	}
}
