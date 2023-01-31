package promclient

import (
	"github.com/prometheus/prometheus/promql/parser"
)

func NewLabelReplaceVisitor(lr map[string]string) *LabelReplaceVisitor {
	return &LabelReplaceVisitor{
		lr: lr,
	}
}

// LabelReplaceVisitor implements the parser.Visitor interface to replace the labels
type LabelReplaceVisitor struct {
	lr map[string]string
}

func (v *LabelReplaceVisitor) Visit(node parser.Node, path []parser.Node) (w parser.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *parser.VectorSelector:
		nodeTyped.LabelMatchers = RewriteMatchers(v.lr, nodeTyped.LabelMatchers)
	case *parser.MatrixSelector:
		nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers =
			RewriteMatchers(v.lr, nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers)
	case *parser.AggregateExpr:
		nodeTyped.Grouping = v.replaceLabels(nodeTyped.Grouping)
	// TODO: binary expression is split by promxy itself, so this case probably is never hit
	case *parser.BinaryExpr:
		nodeTyped.VectorMatching.MatchingLabels = v.replaceLabels(nodeTyped.VectorMatching.MatchingLabels)
	}

	return v, nil
}

func (v *LabelReplaceVisitor) replaceLabels(labels []string) []string {
	replacedLabels := make([]string, 0, len(labels))

	for _, label := range labels {
		if repl, ok := v.lr[label]; ok {
			replacedLabels = append(replacedLabels, repl)
		} else {
			replacedLabels = append(replacedLabels, label)
		}
	}

	return replacedLabels
}
