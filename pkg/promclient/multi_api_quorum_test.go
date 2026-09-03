package promclient

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var errReplicaDown = errors.New("replica down")

// quorumAPI is a target that either answers or fails. keyed controls whether it
// exposes a Key(), which is what decides the fingerprint bucket MultiAPI puts
// it in.
type quorumAPI struct {
	API

	key  model.LabelSet
	down bool
}

func (q *quorumAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	if q.down {
		return nil, nil, errReplicaDown
	}
	return model.LabelValues{"v"}, nil, nil
}

// keyedAPI exposes a Key(); quorumAPI on its own does not, which is how a
// target reaches NewMultiAPI in practice (ErrorWrap does not forward Key()).
type keyedAPI struct{ *quorumAPI }

func (k *keyedAPI) Key() model.LabelSet { return k.key }

// TestMultiAPIToleratesDownReplica pins the HA property a server group exists
// to provide: with requiredCount=1, one replica answering is enough.
//
// The mechanism is easy to break by accident. MultiAPI enforces requiredCount
// per fingerprint bucket, and a target's bucket comes from Key(). Targets reach
// NewMultiAPI wrapped in ErrorWrap, which does not implement APILabels, so they
// all share the zero fingerprint and land in one bucket. Give ErrorWrap a Key()
// that forwards to the wrapped client and targets with differing discovered
// labels split into a bucket each -- at which point requiredCount=1 per bucket
// means every replica must answer.
func TestMultiAPIToleratesDownReplica(t *testing.T) {
	newAPIs := func(keyed bool, keys ...model.LabelSet) []API {
		apis := make([]API, 0, len(keys))
		for i, k := range keys {
			q := &quorumAPI{key: k, down: i == 0}
			if keyed {
				apis = append(apis, &keyedAPI{q})
			} else {
				apis = append(apis, q)
			}
		}
		return apis
	}

	call := func(t *testing.T, apis []API) error {
		t.Helper()
		m, err := NewMultiAPI(apis, model.TimeFromUnix(0), false, nil, 1, false)
		if err != nil {
			t.Fatalf("NewMultiAPI: %v", err)
		}
		_, _, err = m.LabelValues(context.Background(), "l", nil, time.Now(), time.Now())
		return err
	}

	// How targets actually reach MultiAPI today: no Key(), so one bucket.
	t.Run("no Key on the targets", func(t *testing.T) {
		err := call(t, newAPIs(false,
			model.LabelSet{"instance": "a"},
			model.LabelSet{"instance": "b"},
		))
		if err != nil {
			t.Fatalf("a down replica failed the query: %v", err)
		}
	})

	// The arrangement a server group actually builds: each target carries its
	// own discovered labels and is wrapped in ErrorWrap before reaching
	// NewMultiAPI. This is the subtest that fails if ErrorWrap ever forwards
	// Key().
	t.Run("targets behind ErrorWrap, as a server group builds them", func(t *testing.T) {
		var apis []API
		for i, k := range []model.LabelSet{{"instance": "a"}, {"instance": "b"}} {
			target := &keyedAPI{&quorumAPI{key: k, down: i == 0}}
			apis = append(apis, &ErrorWrap{A: target, Msg: "error in target"})
		}
		if err := call(t, apis); err != nil {
			t.Fatalf("a down replica failed the query: %v", err)
		}
	})

	// Same, when the targets do share a key.
	t.Run("identical keys", func(t *testing.T) {
		err := call(t, newAPIs(true,
			model.LabelSet{"job": "sg"},
			model.LabelSet{"job": "sg"},
		))
		if err != nil {
			t.Fatalf("a down replica failed the query: %v", err)
		}
	})

	// The failure mode the ErrorWrap comment warns about: distinct keys put
	// each replica in its own bucket, and requiredCount is applied per bucket.
	// This is asserted so that anyone who "fixes" ErrorWrap to forward Key()
	// sees the consequence spelled out rather than a mysterious HA regression.
	t.Run("distinct keys remove all fault tolerance", func(t *testing.T) {
		err := call(t, newAPIs(true,
			model.LabelSet{"instance": "a"},
			model.LabelSet{"instance": "b"},
		))
		if !errors.Is(err, errReplicaDown) {
			t.Fatalf("expected the down replica to fail the whole query, got %v", err)
		}
	})
}

// TestErrorWrapDoesNotExposeKey guards the specific edit that would break
// TestMultiAPIToleratesDownReplica: giving ErrorWrap a Key(). See the type's
// doc comment for why it must not have one.
func TestErrorWrapDoesNotExposeKey(t *testing.T) {
	var api API = &ErrorWrap{A: &quorumAPI{key: model.LabelSet{"instance": "a"}}, Msg: "target"}
	if _, ok := api.(APILabels); ok {
		t.Fatal("ErrorWrap implements APILabels; this collapses server group HA -- see the ErrorWrap doc comment")
	}
}
