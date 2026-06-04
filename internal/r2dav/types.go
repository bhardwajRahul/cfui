package r2dav

import "time"

const EndpointPath = "/webdav/r2/"

const (
	StatusReady                     = "READY"
	StatusAPITokenRequired          = "API_TOKEN_REQUIRED"
	StatusAccountIDRequired         = "ACCOUNT_ID_REQUIRED"
	StatusR2PermissionDenied        = "R2_PERMISSION_DENIED"
	StatusBucketRequired            = "BUCKET_REQUIRED"
	StatusWebDAVCredentialsRequired = "WEBDAV_CREDENTIALS_REQUIRED"
	StatusR2BucketNotFound          = "R2_BUCKET_NOT_FOUND"
	StatusR2ConfigurationIncomplete = "R2_CONFIGURATION_INCOMPLETE"
	StatusR2FilesystemUnavailable   = "R2_FILESYSTEM_UNAVAILABLE"
)

type Availability struct {
	CanEnable          bool     `json:"can_enable"`
	Status             string   `json:"status"`
	Message            string   `json:"message"`
	MissingPermissions []string `json:"missing_permissions,omitempty"`
}

type SettingsRequest struct {
	Enabled        bool   `json:"enabled"`
	AccountID      string `json:"account_id"`
	BucketName     string `json:"bucket_name"`
	Jurisdiction   string `json:"jurisdiction"`
	WebDAVUsername string `json:"webdav_username"`
	WebDAVPassword string `json:"webdav_password"`
}

type SettingsResponse struct {
	Enabled        bool         `json:"enabled"`
	AccountID      string       `json:"account_id"`
	BucketName     string       `json:"bucket_name"`
	Jurisdiction   string       `json:"jurisdiction"`
	WebDAVUsername string       `json:"webdav_username"`
	PasswordSet    bool         `json:"password_set"`
	Endpoint       string       `json:"endpoint"`
	Availability   Availability `json:"availability"`
}

type Bucket struct {
	Name         string     `json:"name"`
	CreationDate *time.Time `json:"creation_date,omitempty"`
	Location     string     `json:"location,omitempty"`
}

type BucketsResponse struct {
	Buckets []Bucket `json:"buckets"`
}

type CreateBucketRequest struct {
	Name         string `json:"name"`
	LocationHint string `json:"location_hint"`
}

type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"`
	IsDir   bool      `json:"is_dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"mod_time"`
}

type FilesResponse struct {
	Path    string      `json:"path"`
	Parent  string      `json:"parent"`
	Entries []FileEntry `json:"entries"`
}

type MkdirRequest struct {
	Path string `json:"path"`
}

type RenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type Credentials struct {
	AccessKeyID     string
	SecretAccessKey string
}
