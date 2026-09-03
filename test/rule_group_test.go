package test

import (
	"context"
	"fmt"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/rules"
)

// TestRuleGroupDependencyMap covers the crash from
// https://github.com/jacksontj/promxy/issues/809: every config reload has the
// rule manager analyse rule dependencies, which walks each rule's AST with
// rules.buildDependencyMap -- a visitor that writes to a plain map. Our
// prometheus fork walks the AST in parallel when a NodeReplacer is installed,
// which must NOT extend to read-only walks like this one, or the reload dies
// with "fatal error: concurrent map writes".
//
// The crash is a race, so this is a repeated smoke test rather than a
// deterministic assertion; the deterministic half lives in the fork
// (promql/parser: TestWalkVisitsSequentially).
func TestRuleGroupDependencyMap(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e", "f"}

	var rs []rules.Rule
	for i, name := range names {
		// Each rule depends on every other one, and has a wide enough AST
		// that a parallel walk would fan out.
		expr := ""
		for j, dep := range names {
			if j > 0 {
				expr += " + "
			}
			expr += fmt.Sprintf("sum(rate(%s[5m]))", dep)
		}
		parsed, err := parser.ParseExpr(expr)
		if err != nil {
			t.Fatal(err)
		}
		rs = append(rs, rules.NewRecordingRule(name, parsed, labels.FromStrings("i", fmt.Sprint(i))))
	}

	// NewManager installs the default RuleDependencyController; the manager
	// runs it over every group on each config reload (rules.Manager.Update).
	opts := &rules.ManagerOptions{Context: context.Background()}
	rules.NewManager(opts)

	for i := 0; i < 500; i++ {
		for _, r := range rs {
			r.SetDependentRules(nil)
			r.SetDependencyRules(nil)
		}
		opts.RuleDependencyController.AnalyseRules(rs)
	}

	// Every rule references every other, so none of them is independent.
	for _, r := range rs {
		if r.NoDependentRules() {
			t.Fatalf("rule %s: expected dependents", r.Name())
		}
		if r.NoDependencyRules() {
			t.Fatalf("rule %s: expected dependencies", r.Name())
		}
	}
}
