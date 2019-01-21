package promcache

import (
	"context"

	"github.com/jacksontj/promxy/promclient"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type CacheClient struct {
	promclient.API
}

func (c *CacheClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	return c.API.QueryRange(ctx, query, r)
}
