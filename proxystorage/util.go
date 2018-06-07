package proxystorage

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql"
)

type OffsetFinder struct {
	Found  bool
	Offset time.Duration
	Error  error
}

func (o *OffsetFinder) Visit(node promql.Node, _ []promql.Node) (promql.Visitor, error) {
	switch n := node.(type) {
	case *promql.VectorSelector:
		if !o.Found {
			o.Offset = n.Offset
			o.Found = true
		} else {
			if n.Offset != o.Offset {
				o.Error = fmt.Errorf("Mismatched offsets %v %v", n.Offset, o.Offset)
			}
		}

	case *promql.MatrixSelector:
		if !o.Found {
			o.Offset = n.Offset
			o.Found = true
		} else {
			if n.Offset != o.Offset {
				o.Error = fmt.Errorf("Mismatched offsets %v %v", n.Offset, o.Offset)
			}
		}
	}
	if o.Error == nil {
		return o, nil
	} else {
		return nil, o.Error
	}
}

// When we send the queries below, we want to actually *remove* the offset.
type OffsetRemover struct{}

func (o *OffsetRemover) Visit(node promql.Node, _ []promql.Node) (promql.Visitor, error) {
	switch n := node.(type) {
	case *promql.VectorSelector:
		n.Offset = 0

	case *promql.MatrixSelector:
		n.Offset = 0
	}
	return o, nil
}

// Use given func to determine if something is in there or notret := &promql.VectorSelector{Offset: offset}
type BooleanFinder struct {
	Func  func(promql.Node) bool
	Found int
}

func (f *BooleanFinder) Visit(node promql.Node, _ []promql.Node) (promql.Visitor, error) {
	if f.Func(node) {
		f.Found++
		return f, nil
	}
	return f, nil
}
