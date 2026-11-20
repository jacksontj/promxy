package proxyconfig

import (
	"os"
	"strings"
	"testing"

	"github.com/jacksontj/promxy/pkg/alerttemplate"
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

// TestAlertTemplatesConfig verifies the promxy.alert_templates block unmarshals
// into the alerttemplate.Config wired onto PromxyConfig.
func TestAlertTemplatesConfig(t *testing.T) {
	raw := `
promxy:
  server_groups: []
  alert_templates:
    default: 'http://default'
    named:
      grafana: 'http://grafana/{{.AlertName}}'
    rules:
      - match_labels:
          severity: critical
        template: grafana
`
	cfg, err := ConfigFromBytes([]byte(raw))
	if err != nil {
		t.Fatalf("ConfigFromBytes: %v", err)
	}

	at := cfg.PromxyConfig.AlertTemplates
	if at.Default != "http://default" {
		t.Errorf("default = %q, want %q", at.Default, "http://default")
	}
	if at.Named["grafana"] != "http://grafana/{{.AlertName}}" {
		t.Errorf("named[grafana] = %q", at.Named["grafana"])
	}
	if len(at.Rules) != 1 || at.Rules[0].MatchLabels["severity"] != "critical" || at.Rules[0].Template != "grafana" {
		t.Errorf("unexpected rules: %+v", at.Rules)
	}

	// The parsed config must be accepted by the manager.
	if err := alerttemplate.NewManager().Apply(at); err != nil {
		t.Fatalf("manager rejected parsed config: %v", err)
	}
}

// TestReplicaSetValidation checks the rules that keep a replica_set's members
// mergeable as copies of the same data (#842).
func TestReplicaSetValidation(t *testing.T) {
	member := func(extra string) string {
		return "    - static_configs: [{targets: [localhost:9090]}]\n      replica_set: eu\n" + extra
	}
	tests := []struct {
		name    string
		groups  []string
		wantErr string
	}{
		{
			name:   "identical members",
			groups: []string{member("      labels: {region: eu}\n"), member("      labels: {region: eu}\n")},
		},
		{
			name: "unrelated groups may differ",
			groups: []string{
				member("      labels: {region: eu}\n"),
				"    - static_configs: [{targets: [localhost:9090]}]\n      labels: {region: us}\n      ignore_error: true\n",
				"    - static_configs: [{targets: [localhost:9090]}]\n      replica_set: us\n      labels: {region: us}\n",
			},
		},
		{
			name:    "differing labels",
			groups:  []string{member("      labels: {region: eu}\n"), member("      labels: {region: eu, tenant: b}\n")},
			wantErr: "identical labels",
		},
		{
			name:    "differing anti_affinity",
			groups:  []string{member(""), member("      anti_affinity: 30s\n")},
			wantErr: "identical anti_affinity",
		},
		{
			name:   "anti_affinity equal once truncated to seconds",
			groups: []string{member("      anti_affinity: 10s\n"), member("      anti_affinity: 10500ms\n")},
		},
		{
			name:    "differing relabel_configs",
			groups:  []string{member(""), member("      relabel_configs: [{target_label: tenant, replacement: b}]\n")},
			wantErr: "identical relabel_configs",
		},
		{
			name:    "differing metrics_relabel_configs",
			groups:  []string{member(""), member("      metrics_relabel_configs: [{action: labeldrop, source_label: tenant}]\n")},
			wantErr: "identical metrics_relabel_configs",
		},
		{
			name:    "differing relative_time_range",
			groups:  []string{member("      relative_time_range: {start: -72h}\n"), member("")},
			wantErr: "identical relative_time_range",
		},
		{
			name:    "differing absolute_time_range",
			groups:  []string{member(""), member("      absolute_time_range: {start: '2026-01-01T00:00:00Z'}\n")},
			wantErr: "identical absolute_time_range",
		},
		{
			name:    "differing label_filter",
			groups:  []string{member(""), member("      label_filter: {static_labels_include: {job: [node]}}\n")},
			wantErr: "identical label_filter",
		},
		{
			name:    "differing inject_matchers",
			groups:  []string{member("      inject_matchers: ['env=\"prod\"']\n"), member("")},
			wantErr: "identical inject_matchers",
		},
		{
			name:   "identical time range and relabel",
			groups: []string{member("      relative_time_range: {start: -72h}\n      relabel_configs: [{target_label: x, replacement: y}]\n"), member("      relative_time_range: {start: -72h}\n      relabel_configs: [{target_label: x, replacement: y}]\n")},
		},
		{
			name:    "null server group",
			groups:  []string{"    -\n", member("")},
			wantErr: "server_groups[0]: empty server group",
		},
		{
			name:    "ignore_error member",
			groups:  []string{member(""), member("      ignore_error: true\n")},
			wantErr: "ignore_error",
		},
		{
			name:    "downgrade_error member",
			groups:  []string{member("      downgrade_error: true\n"), member("")},
			wantErr: "downgrade_error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := "promxy:\n  server_groups:\n" + strings.Join(tc.groups, "")
			_, err := ConfigFromBytes([]byte(raw))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got error %v, want one mentioning %q", err, tc.wantErr)
			}
		})
	}
}
