package r2dav

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"cfui/internal/config"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/spf13/afero"
)

type Service struct {
	cfgMgr    *config.Manager
	newClient ClientFactory
	newFS     FSFactory
}

func NewService(cfgMgr *config.Manager) *Service {
	return &Service{
		cfgMgr:    cfgMgr,
		newClient: defaultClientFactory,
		newFS:     newR2FS,
	}
}

func NewServiceForTest(cfgMgr *config.Manager, newClient ClientFactory, newFS FSFactory) *Service {
	s := NewService(cfgMgr)
	if newClient != nil {
		s.newClient = newClient
	}
	if newFS != nil {
		s.newFS = newFS
	}
	return s
}

func (s *Service) Settings(ctx context.Context) SettingsResponse {
	cfg := s.effectiveConfig()
	return settingsResponse(cfg, s.Availability(ctx, cfg))
}

func (s *Service) SaveSettings(ctx context.Context, req SettingsRequest) (SettingsResponse, error) {
	appCfg := s.cfgMgr.Get()
	current := appCfg.R2WebDAV
	current.Enabled = req.Enabled
	current.AccountID = strings.TrimSpace(req.AccountID)
	current.BucketName = strings.TrimSpace(req.BucketName)
	current.Jurisdiction = normalizeJurisdiction(req.Jurisdiction)
	current.WebDAVUsername = strings.TrimSpace(req.WebDAVUsername)
	if strings.TrimSpace(req.WebDAVPassword) != "" {
		hash, err := HashPassword(req.WebDAVPassword)
		if err != nil {
			return SettingsResponse{}, err
		}
		current.WebDAVPasswordHash = hash
	}

	effective := s.withFallbackAccountID(current)
	if req.Enabled {
		availability := s.Availability(ctx, effective)
		if !availability.CanEnable {
			return SettingsResponse{}, fmt.Errorf("%s", availability.Message)
		}
	}

	appCfg.R2WebDAV = current
	if err := s.cfgMgr.Save(appCfg); err != nil {
		return SettingsResponse{}, err
	}
	return s.Settings(ctx), nil
}

func (s *Service) Availability(ctx context.Context, cfg config.R2WebDAVConfig) Availability {
	cfg = s.withFallbackAccountID(cfg)
	token := strings.TrimSpace(s.cfgMgr.Get().EffectiveTunnelManagement().APIToken)
	if token == "" {
		return availability(StatusAPITokenRequired, "R2 WebDAV requires Cloudflare API Token mode.", nil)
	}
	if strings.TrimSpace(cfg.AccountID) == "" {
		return availability(StatusAccountIDRequired, "Account ID is required for R2 WebDAV.", nil)
	}
	if strings.TrimSpace(cfg.BucketName) == "" {
		return availability(StatusBucketRequired, "Select or create an R2 bucket before enabling WebDAV.", nil)
	}
	if strings.TrimSpace(cfg.WebDAVUsername) == "" || strings.TrimSpace(cfg.WebDAVPasswordHash) == "" {
		return availability(StatusWebDAVCredentialsRequired, "Set a WebDAV username and password before enabling R2 WebDAV.", nil)
	}

	client, err := s.newClient(token)
	if err != nil {
		return availability(StatusAPITokenRequired, "Cloudflare API Token could not be used for R2 WebDAV.", nil)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	verify, err := client.VerifyAPIToken(ctx)
	if err != nil || verify.Status != "active" {
		return availability(StatusAPITokenRequired, "Cloudflare API Token is not active or could not be verified.", nil)
	}
	tokenDetails, err := client.GetAPIToken(ctx, verify.ID)
	if err != nil || !hasR2WritePermission(tokenDetails.Policies) {
		return availability(StatusR2PermissionDenied, "API Token needs R2 read/write permission.", []string{permR2StorageWrite})
	}
	if _, err := client.GetR2Bucket(ctx, cloudflare.AccountIdentifier(cfg.AccountID), cfg.BucketName); err != nil {
		return availability(StatusR2BucketNotFound, "Selected R2 bucket was not found or is not accessible.", nil)
	}
	return Availability{CanEnable: true, Status: StatusReady, Message: "R2 WebDAV is ready."}
}

func (s *Service) ListBuckets(ctx context.Context) (BucketsResponse, error) {
	cfg := s.effectiveConfig()
	token := strings.TrimSpace(s.cfgMgr.Get().EffectiveTunnelManagement().APIToken)
	if token == "" {
		return BucketsResponse{}, fmt.Errorf("R2 WebDAV requires Cloudflare API Token mode")
	}
	if strings.TrimSpace(cfg.AccountID) == "" {
		return BucketsResponse{}, fmt.Errorf("account id is required")
	}
	client, err := s.newClient(token)
	if err != nil {
		return BucketsResponse{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rows, err := client.ListR2Buckets(ctx, cloudflare.AccountIdentifier(cfg.AccountID), cloudflare.ListR2BucketsParams{})
	if err != nil {
		return BucketsResponse{}, err
	}
	buckets := make([]Bucket, 0, len(rows))
	for _, row := range rows {
		buckets = append(buckets, Bucket{Name: row.Name, CreationDate: row.CreationDate, Location: row.Location})
	}
	return BucketsResponse{Buckets: buckets}, nil
}

func (s *Service) CreateBucket(ctx context.Context, req CreateBucketRequest) (Bucket, error) {
	cfg := s.effectiveConfig()
	token := strings.TrimSpace(s.cfgMgr.Get().EffectiveTunnelManagement().APIToken)
	if token == "" {
		return Bucket{}, fmt.Errorf("R2 WebDAV requires Cloudflare API Token mode")
	}
	if strings.TrimSpace(cfg.AccountID) == "" {
		return Bucket{}, fmt.Errorf("account id is required")
	}
	name := strings.TrimSpace(req.Name)
	if err := validateBucketName(name); err != nil {
		return Bucket{}, err
	}
	client, err := s.newClient(token)
	if err != nil {
		return Bucket{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	row, err := client.CreateR2Bucket(ctx, cloudflare.AccountIdentifier(cfg.AccountID), cloudflare.CreateR2BucketParameters{
		Name:         name,
		LocationHint: strings.TrimSpace(req.LocationHint),
	})
	if err != nil {
		return Bucket{}, err
	}
	return Bucket{Name: row.Name, CreationDate: row.CreationDate, Location: row.Location}, nil
}

func (s *Service) ListFiles(ctx context.Context, rawPath string) (FilesResponse, error) {
	fs, err := s.Filesystem(ctx)
	if err != nil {
		return FilesResponse{}, err
	}
	return listFiles(fs, rawPath)
}

func (s *Service) WriteFile(ctx context.Context, rawPath string, body io.Reader) error {
	fs, err := s.Filesystem(ctx)
	if err != nil {
		return err
	}
	return writeFile(fs, rawPath, body)
}

func (s *Service) OpenFile(ctx context.Context, rawPath string) (afero.File, os.FileInfo, error) {
	fs, err := s.Filesystem(ctx)
	if err != nil {
		return nil, nil, err
	}
	file, info, err := openFile(fs, rawPath)
	if err != nil {
		return nil, nil, err
	}
	return file, info, nil
}

func (s *Service) Delete(ctx context.Context, rawPath string) error {
	fs, err := s.Filesystem(ctx)
	if err != nil {
		return err
	}
	return deletePath(fs, rawPath)
}

func (s *Service) Mkdir(ctx context.Context, rawPath string) error {
	fs, err := s.Filesystem(ctx)
	if err != nil {
		return err
	}
	return mkdir(fs, rawPath)
}

func (s *Service) Rename(ctx context.Context, from, to string) error {
	fs, err := s.Filesystem(ctx)
	if err != nil {
		return err
	}
	return renamePath(fs, from, to)
}

func (s *Service) Filesystem(ctx context.Context) (afero.Fs, error) {
	cfg := s.effectiveConfig()
	if !cfg.Enabled {
		return nil, fmt.Errorf("R2 WebDAV is disabled")
	}
	token := strings.TrimSpace(s.cfgMgr.Get().EffectiveTunnelManagement().APIToken)
	if token == "" {
		return nil, fmt.Errorf("R2 WebDAV requires Cloudflare API Token mode")
	}
	client, err := s.newClient(token)
	if err != nil {
		return nil, err
	}
	creds, _, err := deriveCredentials(ctx, token, client)
	if err != nil {
		return nil, err
	}
	return s.newFS(ctx, cfg.BucketName, endpointFor(cfg.AccountID, cfg.Jurisdiction), creds)
}

func (s *Service) WebDAVCredentials() (config.R2WebDAVConfig, bool) {
	cfg := s.effectiveConfig()
	return cfg, cfg.Enabled && strings.TrimSpace(cfg.WebDAVUsername) != "" && strings.TrimSpace(cfg.WebDAVPasswordHash) != ""
}

func (s *Service) effectiveConfig() config.R2WebDAVConfig {
	return s.withFallbackAccountID(s.cfgMgr.Get().R2WebDAV)
}

func (s *Service) withFallbackAccountID(cfg config.R2WebDAVConfig) config.R2WebDAVConfig {
	cfg.AccountID = strings.TrimSpace(cfg.AccountID)
	cfg.BucketName = strings.TrimSpace(cfg.BucketName)
	cfg.Jurisdiction = normalizeJurisdiction(cfg.Jurisdiction)
	cfg.WebDAVUsername = strings.TrimSpace(cfg.WebDAVUsername)
	if cfg.AccountID == "" {
		cfg.AccountID = strings.TrimSpace(s.cfgMgr.Get().EffectiveTunnelManagement().AccountID)
	}
	return cfg
}

func settingsResponse(cfg config.R2WebDAVConfig, availability Availability) SettingsResponse {
	return SettingsResponse{
		Enabled:        cfg.Enabled,
		AccountID:      cfg.AccountID,
		BucketName:     cfg.BucketName,
		Jurisdiction:   normalizeJurisdiction(cfg.Jurisdiction),
		WebDAVUsername: cfg.WebDAVUsername,
		PasswordSet:    strings.TrimSpace(cfg.WebDAVPasswordHash) != "",
		Endpoint:       EndpointPath,
		Availability:   availability,
	}
}

func availability(status, message string, missing []string) Availability {
	return Availability{CanEnable: false, Status: status, Message: message, MissingPermissions: missing}
}
