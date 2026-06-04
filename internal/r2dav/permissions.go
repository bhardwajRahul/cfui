package r2dav

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	cloudflare "github.com/cloudflare/cloudflare-go"
)

const (
	permR2StorageWrite    = "Workers R2 Storage Write"
	permR2BucketItemWrite = "Workers R2 Storage Bucket Item Write"
)

type CloudflareClient interface {
	VerifyAPIToken(ctx context.Context) (cloudflare.APITokenVerifyBody, error)
	GetAPIToken(ctx context.Context, tokenID string) (cloudflare.APIToken, error)
	ListR2Buckets(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.ListR2BucketsParams) ([]cloudflare.R2Bucket, error)
	CreateR2Bucket(ctx context.Context, rc *cloudflare.ResourceContainer, params cloudflare.CreateR2BucketParameters) (cloudflare.R2Bucket, error)
	GetR2Bucket(ctx context.Context, rc *cloudflare.ResourceContainer, bucketName string) (cloudflare.R2Bucket, error)
}

type ClientFactory func(token string) (CloudflareClient, error)

func defaultClientFactory(token string) (CloudflareClient, error) {
	return cloudflare.NewWithAPIToken(strings.TrimSpace(token))
}

func deriveCredentials(ctx context.Context, token string, client CloudflareClient) (Credentials, cloudflare.APITokenVerifyBody, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	verify, err := client.VerifyAPIToken(ctx)
	if err != nil {
		return Credentials{}, cloudflare.APITokenVerifyBody{}, fmt.Errorf("verify api token: %w", err)
	}
	if verify.Status != "active" {
		return Credentials{}, verify, fmt.Errorf("api token is %s", verify.Status)
	}
	if verify.ID == "" {
		return Credentials{}, verify, fmt.Errorf("api token id is empty")
	}
	sum := sha256.Sum256([]byte(token))
	return Credentials{
		AccessKeyID:     verify.ID,
		SecretAccessKey: hex.EncodeToString(sum[:]),
	}, verify, nil
}

func hasR2WritePermission(policies []cloudflare.APITokenPolicies) bool {
	for _, policy := range policies {
		if !strings.EqualFold(policy.Effect, "allow") {
			continue
		}
		for _, group := range policy.PermissionGroups {
			switch group.Name {
			case permR2StorageWrite, permR2BucketItemWrite:
				return true
			}
		}
	}
	return false
}
