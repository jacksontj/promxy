package dedupe

import (
	"fmt"
	"hash/crc32"
	"io"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	jump "github.com/renstrom/go-jump-consistent-hash"
)

var (
	dedupes = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "promxy_dedupe_count",
			Help: "deduplicated reqeusts",
		},
		[]string{"type"},
	)
)

func init() {
	prometheus.MustRegister(dedupes)
}

func NewDedupeController(n int) *DedupeController {
	// create locks
	locks := make([]*sync.Mutex, n)
	for i := 0; i < n; i++ {
		locks[i] = &sync.Mutex{}
	}

	// create maps
	maps := make([]map[string][]chan interface{}, n)
	for i := 0; i < n; i++ {
		maps[i] = make(map[string][]chan interface{})
	}

	return &DedupeController{
		locks: locks,
		maps:  maps,
		n:     int32(n),
	}
}

// DedupeController encapsulates the logic to have separate maps sharded by the access key
type DedupeController struct {
	n     int32
	locks []*sync.Mutex
	maps  []map[string][]chan interface{}
}

func (i *DedupeController) LockForString(key string) (*sync.Mutex, map[string][]chan interface{}) {
	// Hash key to a number
	h := crc32.NewIEEE()
	_, err := io.WriteString(h, key)
	if err != nil {
		panic(err)
	}

	// find bucket
	bucket := jump.Hash(uint64(h.Sum32()), i.n)

	// Return the associated items
	return i.locks[bucket], i.maps[bucket]
}

func (i *DedupeController) JobForKey(key string, jobType string, f func() interface{}) interface{} {
	l, inflightMap := i.LockForString(key)

	l.Lock()
	if responseChan, ok := inflightMap[key]; ok {
		dedupes.WithLabelValues(jobType).Add(1)
		resultChan := make(chan interface{}, 1)
		inflightMap[key] = append(responseChan, resultChan)
		l.Unlock()

		result, ok := <-resultChan
		if !ok {
			return fmt.Errorf("broken result?")
		}
		return result
	} else {
		inflightMap[key] = make([]chan interface{}, 0)
		l.Unlock()
		ret := f()

		l.Lock()
		resultWaiters, ok := inflightMap[key]
		if ok {
			delete(inflightMap, key)
		}
		l.Unlock()

		for _, resultChan := range resultWaiters {
			select {
			case resultChan <- ret:
			default:
				close(resultChan)
			}
		}
		return ret
	}
}
