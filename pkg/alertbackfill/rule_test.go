package alertbackfill

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/rules"
)

func TestFindGroupAndAlert(t *testing.T) {
	groups := []*rules.Group{
		rules.NewGroup(rules.GroupOptions{
			Name:     "a",
			Interval: time.Minute,
			Rules: []rules.Rule{
				rules.NewAlertingRule(
					"testalert", // name
					nil,         // expression
					time.Hour,   // hold
					labels.Labels{
						labels.Label{"labelkey", "labelvalue"},
					}, // labels
					nil,   // annotations
					nil,   // externalLabels
					"",    // externalURL
					false, // restored
					nil,   // logger
				),
				rules.NewAlertingRule(
					"alertWithLabels", // name
					nil,               // expression
					time.Hour,         // hold
					labels.Labels{
						labels.Label{"labelkey", "labelvalue"},
					}, // labels
					nil,   // annotations
					nil,   // externalLabels
					"",    // externalURL
					false, // restored
					nil,   // logger
				),
				rules.NewAlertingRule(
					"alertWithLabels", // name
					nil,               // expression
					time.Hour,         // hold
					labels.Labels{
						labels.Label{"labelkey", "labelvalue2"},
					}, // labels
					nil,   // annotations
					nil,   // externalLabels
					"",    // externalURL
					false, // restored
					nil,   // logger
				),
			},
			Opts: &rules.ManagerOptions{},
		}),
	}

	tests := []struct {
		matchers []*labels.Matcher
		key      string
	}{
		// basic finding of a single
		{
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "ALERTS_FOR_STATE"),
				labels.MustNewMatcher(labels.MatchEqual, model.AlertNameLabel, "testalert"),
			},
			key: "0.0",
		},
		// ask for an alert that doesn't exist
		{
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "ALERTS_FOR_STATE"),
				labels.MustNewMatcher(labels.MatchEqual, model.AlertNameLabel, "notanalert"),
			},
			key: "",
		},
		// More explicit matchers
		{
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "ALERTS_FOR_STATE"),
				labels.MustNewMatcher(labels.MatchEqual, model.AlertNameLabel, "alertWithLabels"),
				labels.MustNewMatcher(labels.MatchEqual, "labelkey", "labelvalue"),
			},
			key: "0.1",
		},
		{
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "ALERTS_FOR_STATE"),
				labels.MustNewMatcher(labels.MatchEqual, model.AlertNameLabel, "alertWithLabels"),
				labels.MustNewMatcher(labels.MatchEqual, "labelkey", "labelvalue2"),
			},
			key: "0.2",
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			k, alertingRule, interval := FindGroupAndAlert(groups, test.matchers)

			if k != test.key {
				t.Fatalf("Mismatch in key expected=%v actual=%v", test.key, k)
			}

			if k == "" {
				return
			}

			groupIdx, _ := strconv.Atoi(strings.Split(k, ".")[0])
			if groups[groupIdx].Interval() != interval {
				t.Fatalf("mismatch in interval expected=%v actual=%v", groups[groupIdx].Interval(), interval)
			}

			alertIdx, _ := strconv.Atoi(strings.Split(k, ".")[1])
			if groups[groupIdx].Rules()[alertIdx] != alertingRule {
				t.Fatalf("mismatch in alertingRule expected=%v actual=%v", groups[groupIdx].Rules()[alertIdx], alertingRule)
			}
		})
	}
}
