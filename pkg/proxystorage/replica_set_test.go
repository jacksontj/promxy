package proxystorage

import (
	"testing"

	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// TestGroupReplicaSetsOrder checks that each set takes its first member's
// place: the top-level merge breaks sample and error ties by position, so
// naming a set must not reorder the other server groups.
func TestGroupReplicaSetsOrder(t *testing.T) {
	cfg := func(set string) *servergroup.Config {
		c := servergroup.DefaultConfig
		c.ReplicaSet = set
		return &c
	}
	apis := []promclient.API{&stubAPI{}, &stubAPI{}, &stubAPI{}, &stubAPI{}, &stubAPI{}}

	t.Run("no sets", func(t *testing.T) {
		cfgs := []*servergroup.Config{cfg(""), cfg(""), cfg("")}
		out, err := groupReplicaSets(cfgs, apis[:3])
		if err != nil {
			t.Fatal(err)
		}
		for i := range out {
			if out[i] != apis[i] {
				t.Fatalf("out[%d] is not apis[%d]", i, i)
			}
		}
	})

	t.Run("sets", func(t *testing.T) {
		cfgs := []*servergroup.Config{cfg(""), cfg("eu"), cfg(""), cfg("eu"), cfg("us")}
		out, err := groupReplicaSets(cfgs, apis)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != 4 {
			t.Fatalf("got %d apis, want 4", len(out))
		}
		if out[0] != apis[0] || out[2] != apis[2] {
			t.Errorf("server groups without a set moved: %v", out)
		}
		for _, i := range []int{1, 3} {
			if w, ok := out[i].(*promclient.ErrorWrap); !ok {
				t.Errorf("out[%d] is %T, want the set's *promclient.ErrorWrap", i, out[i])
			} else if _, ok := w.A.(*promclient.MultiAPI); !ok {
				t.Errorf("out[%d] wraps %T, want *promclient.MultiAPI", i, w.A)
			}
		}
	})
}
