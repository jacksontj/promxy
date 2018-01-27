package proxystorage

import (
	"fmt"
	"time"

	"github.com/prometheus/prometheus/promql"
)

// TODO: move?
type OffsetFinder struct {
	Found  bool
	Offset time.Duration
	Error  error
}

func (o *OffsetFinder) Visit(node promql.Node) (w promql.Visitor) {
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
		return o
	} else {
		return nil
	}
}

// Use given func to determine if something is in there or notret := &promql.VectorSelector{Offset: offset}
type BooleanFinder struct {
	Func  func(promql.Node) bool
	Found bool
}

func (f *BooleanFinder) Visit(node promql.Node) (w promql.Visitor) {
	if f.Func(node) {
		f.Found = true
		return nil
	}
	return f
}
