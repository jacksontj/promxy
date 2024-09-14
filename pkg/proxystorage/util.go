package proxystorage

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/prometheus/promql/parser"
)

// NewMultiVisitor takes a set of visitors and returns a MultiVisitor
func NewMultiVisitor(visitors []parser.Visitor) *MultiVisitor {
	return &MultiVisitor{
		visitors: visitors,
	}
}

// MultiVisitor runs a set of visitors on the same pass over the node tree
type MultiVisitor struct {
	l        sync.Mutex
	visitors []parser.Visitor
}

// Visit runs on each node in the tree
func (v *MultiVisitor) Visit(node parser.Node, path []parser.Node) (parser.Visitor, error) {
	var visitorErr error
	v.l.Lock()
	defer v.l.Unlock()
	for i, visitor := range v.visitors {
		if visitor == nil {
			continue
		}
		visitorRet, err := visitor.Visit(node, path)
		if err != nil {
			visitorErr = err
		}
		v.visitors[i] = visitorRet
	}

	return v, visitorErr

}

// OffsetFinder finds the offset (if any) within the tree
type OffsetFinder struct {
	l      sync.Mutex
	Found  bool
	Offset time.Duration
	Error  error
}

// Visit runs on each node in the tree
func (o *OffsetFinder) Visit(node parser.Node, path []parser.Node) (parser.Visitor, error) {
	o.l.Lock()
	defer o.l.Unlock()
	switch n := node.(type) {
	case *parser.SubqueryExpr:
		if !o.Found {
			o.Offset = n.OriginalOffset
			o.Found = true
			// If the top of the tree is a SubqueryExpr we can stop checking for offsets
			// as we'll "unwrap" that subquery Expr into its own query
			if len(path) == 0 {
				return nil, nil
			}
		} else {
			if n.OriginalOffset != o.Offset {
				o.Error = fmt.Errorf("mismatched offsets %v %v", n.OriginalOffset, o.Offset)
			}
		}
	case *parser.VectorSelector:
		if !o.Found {
			o.Offset = n.OriginalOffset
			o.Found = true
		} else {
			if n.OriginalOffset != o.Offset {
				o.Error = fmt.Errorf("mismatched offsets %v %v", n.OriginalOffset, o.Offset)
			}
		}
	}

	if o.Error == nil {
		return o, nil
	}
	return nil, nil
}

// OffsetRemover removes any offset found in the node tree
// This is required when we send the queries below as we want to actually *remove* the offset.
type OffsetRemover struct{}

// Visit runs on each node in the tree
func (o *OffsetRemover) Visit(node parser.Node, _ []parser.Node) (parser.Visitor, error) {
	switch n := node.(type) {
	case *parser.VectorSelector:
		n.OriginalOffset = 0
	}
	return o, nil
}

// BooleanFinder uses the given func to determine if something is in there or notret := &parser.VectorSelector{Offset: offset}
type BooleanFinder struct {
	Func  func(parser.Node) bool
	Found int
}

// Visit runs on each node in the tree
func (f *BooleanFinder) Visit(node parser.Node, _ []parser.Node) (parser.Visitor, error) {
	if f.Func(node) {
		f.Found++
		return f, nil
	}
	return f, nil
}

// CloneExp returns a cloned copy of `expr`
func CloneExpr(expr parser.Expr) (newExpr parser.Expr) {
	newExpr, _ = parser.ParseExpr(expr.String())
	return
}

// PreserveLabel wraps the input expression with a label replace in order to preserve the metadata through binary expressions
func PreserveLabel(expr parser.Expr, srcLabel string, dstLabel string) (relabelExpress parser.Expr) {
	relabelExpress, _ = parser.ParseExpr(fmt.Sprintf("label_replace(%s,`%s`,`$1`,`%s`,`(.*)`)", expr.String(), dstLabel, srcLabel))
	return relabelExpress
}

func UnwrapExpr(expr parser.Expr) parser.Expr {
	switch e := expr.(type) {
	case *parser.StepInvariantExpr:
		return e.Expr
	}
	return expr
}

func ExprIsLiteral(expr parser.Expr) bool {
	switch expr.(type) {
	case *parser.StringLiteral:
		return true
	case *parser.NumberLiteral:
		return true
	}
	return false
}
