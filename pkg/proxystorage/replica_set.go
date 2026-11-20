package proxystorage

import (
	"fmt"

	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// groupReplicaSets collapses the server groups that share a replica_set into a
// single API each, merged like the targets of one server group: deduplicated
// with the members' anti_affinity, and answered as long as any one member
// answers. Each set takes the position of its first member, and server groups
// without a replica_set pass through unchanged, so a config that names no set
// yields apis as given.
//
// cfgs and apis are parallel: apis[i] serves cfgs[i].
func groupReplicaSets(cfgs []*servergroup.Config, apis []promclient.API) ([]promclient.API, error) {
	if err := servergroup.ValidateReplicaSets(cfgs); err != nil {
		return nil, err
	}

	members := make(map[string][]promclient.API)
	for i, cfg := range cfgs {
		if cfg.ReplicaSet != "" {
			members[cfg.ReplicaSet] = append(members[cfg.ReplicaSet], apis[i])
		}
	}

	out := make([]promclient.API, 0, len(apis))
	for i, cfg := range cfgs {
		name := cfg.ReplicaSet
		if name == "" {
			out = append(out, apis[i])
			continue
		}
		set, ok := members[name]
		if !ok {
			continue // already placed at its first member
		}
		delete(members, name)
		merged, err := promclient.NewMultiAPI(set, cfg.GetAntiAffinity(), cfg.AntiAffinityDynamic, 1, cfg.GetPreferMax())
		if err != nil {
			return nil, fmt.Errorf("replica_set %q: %w", name, err)
		}
		out = append(out, &promclient.ErrorWrap{A: merged, Msg: fmt.Sprintf("error in replica_set=%s", name)})
	}
	return out, nil
}
