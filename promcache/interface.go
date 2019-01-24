package promcache

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/prometheus/common/model"
	yaml "gopkg.in/yaml.v2"
)

// Interface which actually fetches data from the real source
type Getter interface {
	// Get will retrive the model.Value for a given CacheKey
	Get(ctx context.Context, key CacheKey) (model.Value, error)
}

// Cache is the interface for cache implementations to implement
type Cache interface {
	Get(context.Context, CacheKey) (model.Value, error)
}

type newCache func(options interface{}, getter Getter) (Cache, error)

type cacheType struct {
	newOptions func() interface{}
	newCache
}

var cacheMap = map[string]*cacheType{}
var cacheMapMutex sync.Mutex

func Register(name string, newOptions func() interface{}, n newCache) {
	cacheMapMutex.Lock()
	defer cacheMapMutex.Unlock()

	_, ok := cacheMap[name]
	if ok {
		log.Fatalf("Cache %s already registered", name)
	}

	cacheMap[name] = &cacheType{newOptions, n}
}

func New(name string, opts map[string]interface{}, g Getter) (Cache, error) {
	cacheMapMutex.Lock()
	cacheType, ok := cacheMap[name]
	cacheMapMutex.Unlock()
	if !ok {
		return nil, fmt.Errorf("Unknown cache type %s", name)
	}

	options := cacheType.newOptions()
	b, _ := yaml.Marshal(opts)
	if err := yaml.Unmarshal(b, options); err != nil {
		return nil, err
	}
	return cacheType.newCache(options, g)
}
