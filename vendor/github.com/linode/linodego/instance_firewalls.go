package linodego

import (
	"context"
)

// ListInstanceFirewalls returns a paginated list of Cloud Firewalls for linodeID
func (c *Client) ListInstanceFirewalls(ctx context.Context, linodeID int, opts *ListOptions) ([]Firewall, error) {
	return getPaginatedResults[Firewall](ctx, c, formatAPIPath("linode/instances/%d/firewalls", linodeID), opts)
}
