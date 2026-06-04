package s3dav

import (
	"context"
	"strings"

	cloudflare "github.com/cloudflare/cloudflare-go"
)

type CloudflareClient interface {
	ListR2Buckets(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.ListR2BucketsParams) ([]cloudflare.R2Bucket, error)
	CreateR2Bucket(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.CreateR2BucketParameters) (cloudflare.R2Bucket, error)
}

type ClientFactory func(token string) (CloudflareClient, error)

func defaultClientFactory(token string) (CloudflareClient, error) {
	return cloudflare.NewWithAPIToken(strings.TrimSpace(token))
}
