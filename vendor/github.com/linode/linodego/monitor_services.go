package linodego

import (
	"context"
)

// MonitorService represents a MonitorService object
type MonitorService struct {
	Label       string `json:"label"`
	ServiceType string `json:"service_type"`
}

// ListMonitorServices lists all the registered ACLP MonitorServices
func (c *Client) ListMonitorServices(ctx context.Context, opts *ListOptions) ([]MonitorService, error) {
	return getPaginatedResults[MonitorService](ctx, c, "monitor/services", opts)
}

// ListMonitorServiceByType lists monitor services by a given service_type
func (c *Client) ListMonitorServiceByType(ctx context.Context, serviceType string, opts *ListOptions) ([]MonitorService, error) {
	e := formatAPIPath("monitor/services/%s", serviceType)
	return getPaginatedResults[MonitorService](ctx, c, e, opts)
}
