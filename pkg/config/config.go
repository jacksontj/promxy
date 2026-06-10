package proxyconfig

import (
	"fmt"
	"os"

	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/prometheus/config"
	yaml "gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/servergroup"
)

// DefaultPromxyConfig is the default promxy config that the config file
// is loaded into
var DefaultPromxyConfig = PromxyConfig{}

// DefaultMaxSamplesPerSend is promxy's default remote_write batch size when the
// user does not set queue_config.max_samples_per_send explicitly.
//
// Upstream Prometheus defaults this to 2000. promxy historically (in its old
// vendored remote_write fork) used 100, and shipping recording-rule output with
// large/high-cardinality series in 2000-sample batches can decompress to more
// than the 32 MiB snappy limit that Prometheus 3.5.3+ enforces on the
// remote-write receiver (GHSA-8rm2-7qqf-34qm). When that happens the receiver
// rejects the request with "snappy: decoded length N exceeds limit 33554432"
// and the queue manager drops the batch. Restoring the historical default keeps
// promxy's requests comfortably under that limit. See issue #781.
const DefaultMaxSamplesPerSend = 100

// ConfigFromFile loads a config file at path
func ConfigFromFile(path string) (*Config, error) {
	configBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error loading config: %v", err)
	}
	return ConfigFromBytes(configBytes)
}

// ConfigFromBytes loads a config from raw YAML bytes.
func ConfigFromBytes(configBytes []byte) (*Config, error) {
	cfg := &Config{
		PromConfig:   config.DefaultConfig,
		PromxyConfig: DefaultPromxyConfig,
	}
	if err := yaml.Unmarshal(configBytes, cfg); err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %v", err)
	}

	if err := applyRemoteWriteDefaults(cfg, configBytes); err != nil {
		return nil, fmt.Errorf("error applying remote_write defaults: %v", err)
	}

	return cfg, nil
}

// applyRemoteWriteDefaults lowers the remote_write max_samples_per_send default
// from upstream's 2000 to promxy's DefaultMaxSamplesPerSend, but only for
// remote_write entries where the user did not set it explicitly. The base
// unmarshal always populates QueueConfig from Prometheus' DefaultQueueConfig, so
// an explicit value is indistinguishable from the upstream default after the
// fact; we re-parse the raw YAML to detect which entries set it.
func applyRemoteWriteDefaults(cfg *Config, configBytes []byte) error {
	if len(cfg.PromConfig.RemoteWriteConfigs) == 0 {
		return nil
	}

	var probe struct {
		RemoteWrite []struct {
			QueueConfig *struct {
				MaxSamplesPerSend *int `yaml:"max_samples_per_send"`
			} `yaml:"queue_config"`
		} `yaml:"remote_write"`
	}
	if err := yaml.Unmarshal(configBytes, &probe); err != nil {
		return err
	}

	for i, rwcfg := range cfg.PromConfig.RemoteWriteConfigs {
		explicit := i < len(probe.RemoteWrite) &&
			probe.RemoteWrite[i].QueueConfig != nil &&
			probe.RemoteWrite[i].QueueConfig.MaxSamplesPerSend != nil
		if !explicit {
			rwcfg.QueueConfig.MaxSamplesPerSend = DefaultMaxSamplesPerSend
		}
	}

	return nil
}

// Config is the entire config file. This includes both the Prometheus Config
// as well as the Promxy config. This is done by "inline-ing" the promxy
// config into the prometheus config under the "promxy" key
type Config struct {
	// Prometheus configs -- this includes configurations for
	// recording rules, alerting rules, etc.
	PromConfig config.Config `yaml:",inline"`

	// Promxy specific configuration -- under its own namespace
	PromxyConfig `yaml:"promxy"`

	WebConfig web.TLSConfig `yaml:"tls_server_config"`
}

func (c *Config) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("<error creating config string: %s>", err)
	}
	return string(b)
}

// PromxyConfig is the configuration for Promxy itself
type PromxyConfig struct {
	// Config for each of the server groups promxy is configured to aggregate
	ServerGroups []*servergroup.Config `yaml:"server_groups"`
}
