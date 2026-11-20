package servergroup

import (
	"bytes"
	"context"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	yaml "gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// ValidateReplicaSets checks that the server groups sharing a replica_set can
// stand in for one another. A set answers as soon as any one member answers,
// so every member must return the same series for a query:
//
//   - The merge deduplicates by series labels, so anything that shapes those
//     labels (labels, relabel_configs, metrics_relabel_configs) must match, or
//     the copies are double counted.
//   - A member that skips a query without asking its backend (time ranges,
//     label_filter, inject_matchers) answers with an empty success. If the
//     members differed there, that empty answer would hide the failure of the
//     member that does hold the data.
//   - ignore_error or downgrade_error would turn a fully-down set into an empty
//     success.
//   - They share one merge, so they must agree on how it dedupes.
func ValidateReplicaSets(cfgs []*Config) error {
	first := make(map[string]int)
	for i, c := range cfgs {
		if c == nil {
			return fmt.Errorf("server_groups[%d]: empty server group", i)
		}
		if c.ReplicaSet == "" {
			continue
		}
		if c.IgnoreError {
			return fmt.Errorf("server_groups[%d]: replica_set %q members may not set ignore_error", i, c.ReplicaSet)
		}
		if c.DowngradeError {
			return fmt.Errorf("server_groups[%d]: replica_set %q members may not set downgrade_error", i, c.ReplicaSet)
		}

		j, ok := first[c.ReplicaSet]
		if !ok {
			first[c.ReplicaSet] = i
			continue
		}
		ref := cfgs[j]
		if !c.Labels.Equal(ref.Labels) {
			return fmt.Errorf("server_groups[%d]: replica_set %q members must declare identical labels: %v differs from server_groups[%d]'s %v", i, c.ReplicaSet, c.Labels, j, ref.Labels)
		}

		same := []struct {
			field string
			a, b  interface{}
		}{
			{"anti_affinity", c.GetAntiAffinity(), ref.GetAntiAffinity()},
			{"anti_affinity_dynamic", c.AntiAffinityDynamic, ref.AntiAffinityDynamic},
			{"prefer_max", c.PreferMax, ref.PreferMax},
			{"relabel_configs", c.RelabelConfigs, ref.RelabelConfigs},
			{"metrics_relabel_configs", c.MetricsRelabelConfigs, ref.MetricsRelabelConfigs},
			{"relative_time_range", c.RelativeTimeRangeConfig, ref.RelativeTimeRangeConfig},
			{"absolute_time_range", c.AbsoluteTimeRangeConfig, ref.AbsoluteTimeRangeConfig},
			{"label_filter", c.LabelFilterConfig, ref.LabelFilterConfig},
			{"inject_matchers", c.InjectMatchers, ref.InjectMatchers},
		}
		for _, s := range same {
			eq, err := sameYAML(s.a, s.b)
			if err != nil {
				return fmt.Errorf("server_groups[%d]: comparing %s: %w", i, s.field, err)
			}
			if !eq {
				return fmt.Errorf("server_groups[%d]: replica_set %q members must declare identical %s (differs from server_groups[%d])", i, c.ReplicaSet, s.field, j)
			}
		}
	}
	return nil
}

// sameYAML compares two config values by their YAML form, which is how they
// were written and sidesteps comparing compiled regexps.
func sameYAML(a, b interface{}) (bool, error) {
	ya, err := yaml.Marshal(a)
	if err != nil {
		return false, err
	}
	yb, err := yaml.Marshal(b)
	if err != nil {
		return false, err
	}
	return bytes.Equal(ya, yb), nil
}

// noTargetsAPI answers every request with an error. A replica_set member with
// no targets uses it: a server group with no targets otherwise answers with an
// empty success, which the set would accept in place of a member that holds
// the data.
type noTargetsAPI struct{ err error }

func (n noTargetsAPI) LabelNames(context.Context, []string, time.Time, time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, n.err
}

func (n noTargetsAPI) LabelValues(context.Context, string, []string, time.Time, time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, n.err
}

func (n noTargetsAPI) Query(context.Context, string, time.Time) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, n.err)
}

func (n noTargetsAPI) QueryRange(context.Context, string, v1.Range) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, n.err)
}

func (n noTargetsAPI) Series(context.Context, []string, time.Time, time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, n.err
}

func (n noTargetsAPI) GetValue(context.Context, time.Time, time.Time, []*labels.Matcher) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, n.err)
}

func (n noTargetsAPI) Metadata(context.Context, string, string) (map[string][]v1.Metadata, error) {
	return nil, n.err
}

func (n noTargetsAPI) QueryExemplars(context.Context, string, time.Time, time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, n.err
}
