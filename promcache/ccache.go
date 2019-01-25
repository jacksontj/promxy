package promcache

import (
	"context"
	"fmt"
	"time"

	"github.com/karlseguin/ccache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

var (
	ccacheHitRate = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ccache_requests_total",
		Help: "total number of requests",
	}, []string{"status"})
)

func init() {
	prometheus.MustRegister(ccacheHitRate)
}

var DefaultCCacheOptions = CCacheOptions{
	MaxSize:      10000,
	ItemsToPrune: 100,
	TTL:          time.Minute * 10,
}

func init() {
	Register(
		"ccache",
		func() interface{} {
			c := DefaultCCacheOptions
			return &c
		},
		func(options interface{}, getter Getter) (Cache, error) {
			o, ok := options.(*CCacheOptions)
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
			return &CCache{Cache: ccache.New(config), opts: o, g: getter}, nil
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
	opts *CCacheOptions
	g    Getter
}

func (c *CCache) Get(ctx context.Context, key CacheKey) (model.Value, error) {
	b, _ := key.Marshal()
	k := string(b)

	item := c.Cache.Get(k)
	if item != nil && !item.Expired() {
		ccacheHitRate.WithLabelValues("hit").Inc()
		return item.Value().(model.Value), nil
	}
	ccacheHitRate.WithLabelValues("miss").Inc()
	value, err := c.g.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	// miss
	c.Set(k, value, c.opts.TTL)
	return value, nil
}
