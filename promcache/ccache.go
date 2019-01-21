package promcache

import (
	"context"
	fmt "fmt"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/karlseguin/ccache"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var DefaultCCacheOptions = CCacheOptions{
	MaxSize:      10000,
	ItemsToPrune: 100,
	TTL:          time.Minute * 10,
}

func init() {
	Register(
		"ccache",
		func() interface{} { return DefaultCCacheOptions },
		func(options interface{}) (Cache, error) {
			o, ok := options.(CCacheOptions)
			if !ok {
				return nil, fmt.Errorf("Invalid options")
			}
			config := ccache.Configure()
			if o.MaxSize > 0 {
				config.MaxSize(o.MaxSize)
			}
			if o.ItemsToPrune > 0 {
				config.ItemsToPrune(o.ItemsToPrune)
			}
			return &CCache{Cache: ccache.New(config), opts: o}, nil
		},
	)
}

type CCacheOptions struct {
	// Cache options
	MaxSize      int64  `yaml:"max_size"`
	ItemsToPrune uint32 `yaml:"items_to_prune"`

	// Object options
	TTL time.Duration `yaml:"ttl"`
}

type CCache struct {
	*ccache.Cache
	opts CCacheOptions
	api  promclient.API
}

func (c *CCache) SetAPI(api promclient.API) {
	c.api = api
}

func (c *CCache) GetMatrix(ctx context.Context, key CacheKey, r v1.Range) (model.Value, error) {
	b, _ := key.Marshal()

	// TODO: configurable TTL
	item, err := c.Cache.Fetch(string(b), c.opts.TTL, func() (interface{}, error) {
		return c.api.QueryRange(ctx, key.Query, r)
	})

	if err != nil {
		return nil, err
	}

	return item.Value().(model.Value), nil
}
