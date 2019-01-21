package promcache

import (
	"context"
	fmt "fmt"
	"log"
	"sync"

	"github.com/jacksontj/promxy/promclient"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	yaml "gopkg.in/yaml.v2"
)

type Cache interface {
	SetAPI(promclient.API)
	GetMatrix(context.Context, CacheKey, v1.Range) (model.Value, error)
}

type newCache func(options interface{}) (Cache, error)

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

func New(name string, opts map[string]interface{}) (Cache, error) {
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
	return cacheType.newCache(options)
}
