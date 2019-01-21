package promcache

import (
	"context"

	"github.com/jacksontj/promxy/promclient"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// StepNormalizingClient is client that simply normalizes the steps for QueryRange
// This implements the promclient.API interface, and as such can be used interchangeably
type StepNormalizingClient struct {
	promclient.API
}

func (c *StepNormalizingClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	r.Start = r.Start.Truncate(r.Step)
	r.End = r.End.Truncate(r.Step)

	return c.API.QueryRange(ctx, query, r)
}
