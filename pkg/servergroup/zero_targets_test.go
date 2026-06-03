package servergroup

import (
	"strconv"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

func TestGroupIdentifier(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "no config",
			cfg:  nil,
			want: "unknown",
		},
		{
			// The ordinal is always included since it is guaranteed unique.
			name: "ordinal only",
			cfg:  &Config{Ordinal: 2},
			want: "ord=2",
		},
		{
			// The optional name is appended; the ordinal is still present.
			name: "ordinal and name",
			cfg:  &Config{Ordinal: 2, Name: "primary-cluster"},
			want: "ord=2 name=primary-cluster",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg := &ServerGroup{Cfg: tt.cfg}
			if got := sg.groupIdentifier(); got != tt.want {
				t.Fatalf("groupIdentifier() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestServerGroupTargetsMetric verifies that loadTargetGroupMap publishes the
// discovered target count to the server_group_targets gauge, so a zero-target
// group (issue #742) can be alerted on.
func TestServerGroupTargetsMetric(t *testing.T) {
	tests := []struct {
		name        string
		ordinal     int
		groupName   string
		targetGroup map[string][]*targetgroup.Group
		want        float64
	}{
		{
			name:        "zero targets",
			ordinal:     91,
			groupName:   "empty-group",
			targetGroup: map[string][]*targetgroup.Group{},
			want:        0,
		},
		{
			name:      "one target",
			ordinal:   92,
			groupName: "one-target-group",
			targetGroup: map[string][]*targetgroup.Group{
				"x": {{Targets: []model.LabelSet{{model.AddressLabel: "localhost:9090"}}}},
			},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sg, err := NewServerGroup()
			if err != nil {
				t.Fatalf("NewServerGroup: %v", err)
			}
			defer sg.Cancel()
			sg.Cfg = &Config{Ordinal: tt.ordinal, Name: tt.groupName, Scheme: "http"}

			if err := sg.loadTargetGroupMap(tt.targetGroup); err != nil {
				t.Fatalf("loadTargetGroupMap: %v", err)
			}

			gauge := serverGroupTargets.WithLabelValues(strconv.Itoa(tt.ordinal), tt.groupName)
			if got := testutil.ToFloat64(gauge); got != tt.want {
				t.Fatalf("server_group_targets{ordinal=%d,name=%q} = %v, want %v", tt.ordinal, tt.groupName, got, tt.want)
			}
		})
	}
}
