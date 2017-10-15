package servergroup

import "github.com/prometheus/prometheus/config"

type Config struct {
	Hosts config.ServiceDiscoveryConfig `yaml:",inline"`
}
