package alertbackfill

import (
	"fmt"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
)

func FindGroupAndAlert(groups []*rules.Group, matchers []*labels.Matcher) (string, *rules.AlertingRule, time.Duration) {
	// right now upstream alert state restore *just* uses the name of the alert and labels
	// this causes issues if there are any additional alerts (as only one will be queried
	// and it's state will be used for all alerts with the same name + labels).
	// TODO: Once upstream fixes this (https://github.com/prometheus/prometheus/issues/12714)
	// we'll want to adjust this logic. For now we're effectively mirroring upstream logic
	// Now we need to "backfill" the data (regenerate the series that it would have queried)
	var (
		matchingGroupIdx int
		matchingRuleIdx  int
		matchingGroup    *rules.Group
		matchingRule     *rules.AlertingRule
		found            bool
	)
	var alertname string
	for _, matcher := range matchers {
		if matcher.Name == model.AlertNameLabel {
			alertname = matcher.Value
			break
		}
	}
	if alertname == "" {
		panic("what")
	}

FIND_RULE:
	for i, group := range groups {
	RULE_LOOP:
		for ii, rule := range group.Rules() {
			alertingRule, ok := rule.(*rules.AlertingRule)
			if !ok {
				continue
			}
			// For now we check if the name and all given labels match
			// which is both the best we can do, and equivalent to
			// direct prometheus behavior
			if alertingRule.Name() == alertname {
				// Check the rule labels fit the matcher set
				for _, lbl := range alertingRule.Labels() {
				MATCHERLOOP:
					for _, m := range matchers {
						// skip matchers that we know we will overwrite
						switch m.Name {
						case model.MetricNameLabel, model.AlertNameLabel:
							continue MATCHERLOOP
						}
						if lbl.Name == m.Name {
							if !m.Matches(lbl.Value) {
								continue RULE_LOOP
							}
							break
						}
					}
				}

				matchingGroupIdx = i
				matchingRuleIdx = ii
				matchingGroup = group
				matchingRule = alertingRule
				found = true
				break FIND_RULE
			}
		}
	}

	if !found {
		return "", nil, time.Duration(0)
	}

	key := fmt.Sprintf("%d.%d", matchingGroupIdx, matchingRuleIdx)
	return key, matchingRule, matchingGroup.Interval()
}
