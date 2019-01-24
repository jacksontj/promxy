package promcache

import (
	"context"
	"fmt"
	"time"

	"github.com/karlseguin/ccache"
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

	item, err := c.Cache.Fetch(string(b), c.opts.TTL, func() (interface{}, error) {
		fmt.Println("miss")
		return c.g.Get(ctx, key)
	})

	if err != nil {
		return nil, err
	}

	return item.Value().(model.Value), nil
}
