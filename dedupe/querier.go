package dedupe

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

func NewQuerier(ctx context.Context, mint, maxt int64, q storage.Querier, c *DedupeController) *Querier {
	return &Querier{
		ctx:  ctx,
		mint: mint,
		maxt: maxt,
		q:    q,
		c:    c,
	}
}

type Querier struct {
	q    storage.Querier
	ctx  context.Context
	mint int64
	maxt int64

	c *DedupeController
}

// Select returns a set of series that matches the given label matchers.
func (q *Querier) Select(params *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, error) {
	// generate key
	b, err := json.Marshal([]interface{}{"select", q.mint, q.maxt, params, matchers})
	if err != nil {
		return q.q.Select(params, matchers...)
	}

	key := string(b)

	result := q.c.JobForKey(key, "select", func() interface{} {
		set, err := q.q.Select(params, matchers...)

		if err == nil {
			return NewConcurrentSeriesSet(set)
		} else {
			return err
		}
	})

	switch resultTyped := result.(type) {
	// TODO: make a copy (AFAIK there are no guarantees about concurrent access on this interface)
	case *ConcurrentSeriesSet:
		return resultTyped.Clone(), nil
	case error:
		return nil, resultTyped
	default:
		return nil, fmt.Errorf("Unknown return type")
	}
}

// LabelValues returns all potential values for a label name.
func (q *Querier) LabelValues(name string) ([]string, error) {
	// generate key
	b, err := json.Marshal([]interface{}{"labelValues", name})
	if err != nil {
		return q.q.LabelValues(name)
	}

	key := string(b)
	result := q.c.JobForKey(key, "label_values", func() interface{} {
		names, err := q.q.LabelValues(name)

		if err == nil {
			return names
		} else {
			return err
		}
	})

	switch resultTyped := result.(type) {
	case []string:
		return resultTyped, nil
	case error:
		return nil, resultTyped
	default:
		return nil, fmt.Errorf("Unknown return type")
	}
}

// Close releases the resources of the Querier.
func (q *Querier) Close() error {
	return q.q.Close()
}
