package promql

import "github.com/prometheus/prometheus/storage"

func NewRawMatrixFromVector(ms *VectorSelector) *RawMatrix {
	return &RawMatrix{
		series: ms.series,
	}
}

func NewRawMatrixFromMatrix(ms *MatrixSelector) *RawMatrix {
	return &RawMatrix{
		series: ms.series,
	}
}

// RawMatrix is a Value that the promql engine evaluates as a matrix regardless of context
// This is used to replace SubqueryExpr as they return a Matrix of data that isn't a MatrixSelector
type RawMatrix struct {
	series []storage.Series
}

func (*RawMatrix) expr()             {}
func (e *RawMatrix) Type() ValueType { return ValueTypeMatrix }
func (e *RawMatrix) Value() (Value, error) {
	m := make(Matrix, len(e.series))
	for i, s := range e.series {
		m[i] = Series{
			Metric: s.Labels(),
			Points: make([]Point, 0, 10),
		}
		it := s.Iterator()
		for it.Next() {
			t, v := it.At()
			m[i].Points = append(m[i].Points, Point{T: t, V: v})
		}
		if err := it.Err(); err != nil {
			return nil, err
		}
	}
	return m, nil
}
func (e *RawMatrix) String() string {
	r, _ := e.Value()
	return r.String()
}
