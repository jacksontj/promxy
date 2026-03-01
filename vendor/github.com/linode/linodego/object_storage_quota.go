package linodego

import (
	"context"
)

// ObjectStorageQuota represents a Object Storage related quota information on your account.
type ObjectStorageQuota struct {
	QuotaID        string `json:"quota_id"`
	QuotaName      string `json:"quota_name"`
	EndpointType   string `json:"endpoint_type"`
	S3Endpoint     string `json:"s3_endpoint"`
	Description    string `json:"description"`
	QuotaLimit     int    `json:"quota_limit"`
	ResourceMetric string `json:"resource_metric"`
}

// ObjectStorageQuotaUsage is the usage data for a specific Object Storage related quota on your account.
type ObjectStorageQuotaUsage struct {
	QuotaLimit int  `json:"quota_limit"`
	Usage      *int `json:"usage"`
}

// ListObjectStorageQuotas lists the active ObjectStorage-related quotas applied to your account.
func (c *Client) ListObjectStorageQuotas(ctx context.Context, opts *ListOptions) ([]ObjectStorageQuota, error) {
	return getPaginatedResults[ObjectStorageQuota](ctx, c, formatAPIPath("object-storage/quotas"), opts)
}

// GetObjectStorageQuota gets information about a specific ObjectStorage-related quota on your account.
func (c *Client) GetObjectStorageQuota(ctx context.Context, quotaID string) (*ObjectStorageQuota, error) {
	e := formatAPIPath("object-storage/quotas/%s", quotaID)
	return doGETRequest[ObjectStorageQuota](ctx, c, e)
}

// GetObjectStorageQuotaUsage gets usage data for a specific ObjectStorage Quota resource you can have on your account and the current usage for that resource.
func (c *Client) GetObjectStorageQuotaUsage(ctx context.Context, quotaID string) (*ObjectStorageQuotaUsage, error) {
	e := formatAPIPath("object-storage/quotas/%s/usage", quotaID)
	return doGETRequest[ObjectStorageQuotaUsage](ctx, c, e)
}
