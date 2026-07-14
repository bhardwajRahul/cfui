# Dependency Refresh and Configuration Backup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Merge the four open dependency PRs, update the embedded cloudflared module, and add selectable, optionally encrypted configuration export and replacement import.

**Architecture:** Keep backup parsing and section application in a pure `internal/configbackup` package. The HTTP server owns bounded upload/download endpoints and runtime reconciliation, while a dedicated browser module renders the backup dialogs in the Features panel. Existing `config.Manager.Save` remains the only persistence boundary so selected imports commit atomically through the current SQLite transaction.

**Tech Stack:** Go 1.26, SQLite/ent, `encoding/json`, `crypto/aes`, `crypto/cipher`, `crypto/rand`, `golang.org/x/crypto/scrypt`, `net/http`, embedded vanilla HTML/CSS/JavaScript, TOML localization, GitHub CLI.

## Global Constraints

- Backup envelope format is `cfui-config-backup`, envelope version 1, payload schema version 1.
- Import replaces selected normal sections and preserves every unselected section.
- Passwords are optional; a non-empty password encrypts the complete payload with scrypt N=32768, r=8, p=1 and AES-256-GCM.
- Sensitive credentials are never exported or imported unless the Sensitive credentials option is explicitly selected.
- OAuth sessions, PKCE state, MCP tokens, validation reports, logs, and runtime state are never included.
- Plaintext export with sensitive credentials requires a second browser confirmation.
- The server requires `confirm_plaintext_sensitive: true` for a sensitive plaintext export; a browser-only confirmation is not treated as a security boundary.
- Inspection and import accept at most 8 MiB and never persist the uploaded backup.
- Surviving running tunnels are not restarted automatically; deleted profiles receive an asynchronous stop request.
- Keep English, Chinese, and Japanese locale keys in parity.
- Merge commits are the repository’s established PR strategy.

---

## File Map

- Create `internal/configbackup/types.go`: stable envelope, section DTOs, options, inspection, and apply result types.
- Create `internal/configbackup/codec.go`: strict JSON decoding, optional scrypt/AES-GCM encryption, and envelope validation.
- Create `internal/configbackup/sections.go`: convert `config.Config` into selected backup sections.
- Create `internal/configbackup/apply.go`: validate and apply selected sections, preserve unselected data, and compute removed/changed profile keys.
- Create `internal/configbackup/codec_test.go`: envelope, encryption, tamper, version, and strict JSON tests.
- Create `internal/configbackup/apply_test.go`: section replacement, credential preservation, limits, and warnings.
- Create `internal/server/config_backup.go`: export, inspect, import, bounded multipart parsing, and runtime reconciliation.
- Create `internal/server/config_backup_test.go`: HTTP contract, size limits, persistence, and runtime hook tests.
- Modify `internal/server/server.go`: register the three endpoints and hold testable runtime hooks.
- Create `web/dist/js/app-backup.js`: dialog state, export download, inspection preview, import, confirmation, and refresh behavior.
- Modify `web/dist/index.html`: Features card, export/import dialogs, and `app-backup.js` script tag.
- Modify `web/dist/js/app-init.js`: wire the backup UI at startup.
- Modify `web/dist/style.css`: compact backup option list, metadata grid, warning, and summary styles.
- Modify `locales/en.toml`, `locales/zh.toml`, `locales/ja.toml`: all backup UI and error strings.
- Modify `web_init_test.go` and `i18n_parity_test.go`: embedded asset and locale parity assertions.
- Modify `README.md` and `README.zh-CN.md`: user-facing backup and restore documentation.
- Modify `go.mod` and `go.sum`: merged dependency PR results and cloudflared update.

---

### Task 1: Review and merge all open dependency PRs

**Files:**
- Remote merge commits: GitHub PR #37, #34, #36, and #38
- Local branch: rebase the existing design commit onto the resulting `origin/main`

**Interfaces:**
- Consumes: clean local `main` with the committed design specification
- Produces: `origin/main` containing all four PR merge commits; local `main` rebased on that remote state

- [ ] **Step 1: Record the protected local state**

Run:

~~~bash
git status --short --branch -uall
git rev-parse HEAD
git remote -v
~~~

Expected: only `main` ahead of `origin/main` by the design-document commit, with no modified or untracked files.

- [ ] **Step 2: Verify and merge independent PR #37**

Run:

~~~bash
gh pr view 37 --json number,title,mergeable,mergeStateStatus,statusCheckRollup,headRefOid,url
git fetch origin pull/37/head:refs/tmp/cfui-pr-37
WT=$(mktemp -d)
git worktree add --detach "$WT" refs/tmp/cfui-pr-37
go -C "$WT" test ./...
git worktree remove "$WT"
HEAD=$(gh pr view 37 --json headRefOid --jq .headRefOid)
gh pr merge 37 --merge --match-head-commit "$HEAD"
gh pr view 37 --json state,mergedAt,mergeCommit,url
~~~

Expected: tests pass; PR #37 reports state `MERGED` and a merge commit.

- [ ] **Step 3: Refresh, verify, and merge PR #34**

Run:

~~~bash
gh pr update-branch 34
gh pr view 34 --json mergeable,mergeStateStatus,headRefOid
git fetch origin pull/34/head:refs/tmp/cfui-pr-34
WT=$(mktemp -d)
git worktree add --detach "$WT" refs/tmp/cfui-pr-34
go -C "$WT" test ./...
git worktree remove "$WT"
HEAD=$(gh pr view 34 --json headRefOid --jq .headRefOid)
gh pr merge 34 --merge --match-head-commit "$HEAD"
gh pr view 34 --json state,mergedAt,mergeCommit,url
~~~

Expected: PR #34 is refreshed against the #37 merge, tests pass, and PR #34 reports `MERGED`.

- [ ] **Step 4: Refresh, verify, and merge PR #36**

Run:

~~~bash
gh pr update-branch 36
gh pr view 36 --json mergeable,mergeStateStatus,headRefOid
git fetch origin pull/36/head:refs/tmp/cfui-pr-36
WT=$(mktemp -d)
git worktree add --detach "$WT" refs/tmp/cfui-pr-36
go -C "$WT" test ./...
git worktree remove "$WT"
HEAD=$(gh pr view 36 --json headRefOid --jq .headRefOid)
gh pr merge 36 --merge --match-head-commit "$HEAD"
gh pr view 36 --json state,mergedAt,mergeCommit,url
~~~

Expected: PR #36 reports `CLEAN` before merge, tests pass, and PR #36 reports `MERGED`.

- [ ] **Step 5: Refresh, verify, and merge PR #38**

Run:

~~~bash
gh pr update-branch 38
gh pr view 38 --json mergeable,mergeStateStatus,headRefOid
git fetch origin pull/38/head:refs/tmp/cfui-pr-38
WT=$(mktemp -d)
git worktree add --detach "$WT" refs/tmp/cfui-pr-38
go -C "$WT" test ./...
git worktree remove "$WT"
HEAD=$(gh pr view 38 --json headRefOid --jq .headRefOid)
gh pr merge 38 --merge --match-head-commit "$HEAD"
gh pr view 38 --json state,mergedAt,mergeCommit,url
~~~

Expected: PR #38 reports `CLEAN` before merge, tests pass, and PR #38 reports `MERGED`.

- [ ] **Step 6: Confirm the remote queue and rebase the local design commit once**

Run:

~~~bash
gh pr list --state open --limit 20
git fetch origin
git status --short --branch -uall
git rebase origin/main
git status --short --branch -uall
go test ./...
~~~

Expected: none of #34, #36, #37, or #38 remains open; local `main` is ahead only by the rebased design-document commit; tests pass.

---

### Task 2: Update the embedded cloudflared module

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: dependency graph after all four PR merges
- Produces: `github.com/cloudflare/cloudflared` pinned to `v0.0.0-20260713102814-2601f87b5728` with a tidy, buildable module graph

- [ ] **Step 1: Verify the current cloudflared baseline**

Run:

~~~bash
go list -m github.com/cloudflare/cloudflared
go test ./internal/cloudflared ./internal/service ./internal/server
~~~

Expected: the module still reports `v0.0.0-20260618133902-81a53555aa82` and targeted tests pass before the bump.

- [ ] **Step 2: Apply the exact latest revision and tidy**

Run:

~~~bash
go get github.com/cloudflare/cloudflared@v0.0.0-20260713102814-2601f87b5728
go mod tidy
go list -m github.com/cloudflare/cloudflared
git diff -- go.mod go.sum
~~~

Expected: cloudflared reports `v0.0.0-20260713102814-2601f87b5728`; every other dependency change is a transitive consequence of the four PRs or cloudflared.

- [ ] **Step 3: Verify cloudflared integration**

Run:

~~~bash
go test ./internal/cloudflared ./internal/service ./internal/server
go test ./...
go vet ./...
~~~

Expected: all commands exit zero.

- [ ] **Step 4: Commit the dependency update**

Run:

~~~bash
git status --short --branch -uall
git rev-parse HEAD
git add go.mod go.sum
git commit -m "build(deps): update cloudflared"
~~~

Expected: one commit containing only `go.mod` and `go.sum` changes owned by this task.

---

### Task 3: Define and encode the stable backup format

**Files:**
- Create: `internal/configbackup/types.go`
- Create: `internal/configbackup/codec.go`
- Create: `internal/configbackup/codec_test.go`

**Interfaces:**
- Consumes: `config.Config` section types and an `io.Reader` for cryptographic randomness
- Produces:
  - `Encode(payload Payload, password string, random io.Reader) ([]byte, error)`
  - `Decode(data []byte, password string) (Decoded, error)`
  - `Inspect(decoded Decoded) Inspection`

- [ ] **Step 1: Write failing codec tests**

Create `internal/configbackup/codec_test.go` with table-driven tests covering plaintext, encrypted, wrong password, ciphertext mutation, unknown fields, duplicate members, trailing JSON, unsupported version, malformed base64, and string-size validation.

Use these fixtures and assertions:

~~~go
func testPayload() Payload {
	return Payload{
		SchemaVersion: PayloadVersion,
		CreatedAt:     time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
		AppVersion:    "v0.9.6",
		Sections:      []Section{SectionApplication},
		Application:   &ApplicationSection{MCPEnabled: true, OAuthClientID: "client", OAuthRelayCallbackURL: "https://relay.example/oauth/callback"},
	}
}

func TestEncodeDecodePlaintext(t *testing.T) {
	data, err := Encode(testPayload(), "", bytes.NewReader(bytes.Repeat([]byte{1}, 64)))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := Decode(data, "")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if decoded.Encrypted || !reflect.DeepEqual(decoded.Payload, testPayload()) {
		t.Fatalf("unexpected decoded payload: %#v", decoded)
	}
}

func TestEncodeDecodeEncrypted(t *testing.T) {
	data, err := Encode(testPayload(), "backup password", bytes.NewReader(bytes.Repeat([]byte{2}, 64)))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if bytes.Contains(data, []byte("client")) {
		t.Fatal("encrypted envelope leaked plaintext")
	}
	decoded, err := Decode(data, "backup password")
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !decoded.Encrypted || !reflect.DeepEqual(decoded.Payload, testPayload()) {
		t.Fatalf("unexpected decoded payload: %#v", decoded)
	}
}

func TestDecodeRejectsWrongPassword(t *testing.T) {
	data, err := Encode(testPayload(), "correct", rand.Reader)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	_, err = Decode(data, "wrong")
	if !errors.Is(err, ErrInvalidPasswordOrTampered) {
		t.Fatalf("expected credential error, got %v", err)
	}
}
~~~

Also assert these inputs fail with `ErrInvalidBackup` or `ErrUnsupportedVersion`:

~~~json
{"format":"cfui-config-backup","format":"duplicate","version":1,"encrypted":false,"payload":{}}
{"format":"cfui-config-backup","version":2,"encrypted":false,"payload":{}}
{"format":"cfui-config-backup","version":1,"encrypted":false,"payload":{}} {}
~~~

- [ ] **Step 2: Run the codec tests and confirm the package is absent**

Run:

~~~bash
go test ./internal/configbackup -run 'TestEncode|TestDecode' -v
~~~

Expected: FAIL because `internal/configbackup` and its exported types do not exist.

- [ ] **Step 3: Add the complete format types**

Create `internal/configbackup/types.go` with these constants and public shapes:

~~~go
package configbackup

import (
	"errors"
	"time"

	"cfui/internal/config"
)

const (
	Format            = "cfui-config-backup"
	EnvelopeVersion   = 1
	PayloadVersion    = 1
	MaxBackupBytes    = 8 << 20
	MaxStringBytes    = 64 << 10
	MaxTunnelProfiles = 256
	MaxS3Mounts       = 256
	MaxDDNSSources    = 64
	MaxDDNSRecords    = 1024
)

type Section string

const (
	SectionTunnels          Section = "tunnels"
	SectionRemoteManagement Section = "remote_management"
	SectionDDNS             Section = "ddns"
	SectionS3WebDAV         Section = "s3_webdav"
	SectionApplication      Section = "application"
	SectionSensitive        Section = "sensitive"
)

var (
	ErrInvalidBackup             = errors.New("invalid backup")
	ErrUnsupportedVersion        = errors.New("unsupported backup version")
	ErrPasswordRequired          = errors.New("backup password required")
	ErrInvalidPasswordOrTampered = errors.New("invalid password or tampered backup")
)

type Envelope struct {
	Format      string      `json:"format"`
	Version     int         `json:"version"`
	Encrypted   bool        `json:"encrypted"`
	Payload     *Payload    `json:"payload,omitempty"`
	Encryption  *Encryption `json:"encryption,omitempty"`
	Ciphertext  string      `json:"ciphertext,omitempty"`
}

type Encryption struct {
	Algorithm string `json:"algorithm"`
	KDF       string `json:"kdf"`
	N         int    `json:"n"`
	R         int    `json:"r"`
	P         int    `json:"p"`
	Salt      string `json:"salt"`
	Nonce     string `json:"nonce"`
}

type Payload struct {
	SchemaVersion    int                      `json:"schema_version"`
	CreatedAt        time.Time                `json:"created_at"`
	AppVersion       string                   `json:"app_version"`
	Sections         []Section                `json:"sections"`
	Tunnels          *TunnelSection           `json:"tunnels,omitempty"`
	RemoteManagement *RemoteManagementSection `json:"remote_management,omitempty"`
	DDNS             *config.DDNSConfig       `json:"ddns,omitempty"`
	S3WebDAV         *S3WebDAVSection         `json:"s3_webdav,omitempty"`
	Application      *ApplicationSection      `json:"application,omitempty"`
	Sensitive        *SensitiveSection        `json:"sensitive,omitempty"`
}

type TunnelSection struct {
	ActiveKey string          `json:"active_key"`
	Profiles  []TunnelProfile `json:"profiles"`
}

type TunnelProfile struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	LocalEnabled    bool   `json:"local_enabled"`
	AutoStart       bool   `json:"auto_start"`
	AutoRestart     bool   `json:"auto_restart"`
	CustomTag       string `json:"custom_tag"`
	SoftwareName    string `json:"software_name"`
	Protocol        string `json:"protocol"`
	GracePeriod     string `json:"grace_period"`
	Region          string `json:"region"`
	Retries         int    `json:"retries"`
	MetricsEnable   bool   `json:"metrics_enable"`
	MetricsPort     int    `json:"metrics_port"`
	LogLevel        string `json:"log_level"`
	LogFile         string `json:"log_file"`
	LogJSON         bool   `json:"log_json"`
	EdgeIPVersion   string `json:"edge_ip_version"`
	EdgeBindAddress string `json:"edge_bind_address"`
	PostQuantum     bool   `json:"post_quantum"`
	NoTLSVerify     bool   `json:"no_tls_verify"`
	ExtraArgs       string `json:"extra_args"`
}

type RemoteManagementSection struct {
	Profiles []RemoteProfile `json:"profiles"`
	APIEmail string          `json:"api_email"`
}

type RemoteProfile struct {
	Key       string `json:"key"`
	Enabled   bool   `json:"enabled"`
	AccountID string `json:"account_id"`
	TunnelID  string `json:"tunnel_id"`
}

type S3WebDAVSection struct {
	Enabled                 bool      `json:"enabled"`
	ActiveKey               string    `json:"active_key"`
	WebDAVAccessMode        string    `json:"webdav_access_mode"`
	DedicatedBindHost       string    `json:"dedicated_bind_host"`
	DedicatedPort           int       `json:"dedicated_port"`
	DedicatedAutoStart      bool      `json:"dedicated_auto_start"`
	DedicatedDomainMode     string    `json:"dedicated_domain_mode"`
	DedicatedCustomDomain   string    `json:"dedicated_custom_domain"`
	DedicatedTunnelHostname string    `json:"dedicated_tunnel_hostname"`
	Mounts                  []S3Mount `json:"mounts"`
}

type S3Mount struct {
	Key               string `json:"key"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	WebDAVEnabled     bool   `json:"webdav_enabled"`
	WebDAVAuthEnabled bool   `json:"webdav_auth_enabled"`
	MountType         string `json:"mount_type"`
	Provider          string `json:"provider"`
	EndpointURL       string `json:"endpoint_url"`
	Region            string `json:"region"`
	PathStyle         bool   `json:"path_style"`
	AccountID         string `json:"account_id"`
	BucketName        string `json:"bucket_name"`
	RootPrefix        string `json:"root_prefix"`
	MountPath         string `json:"mount_path"`
	Jurisdiction      string `json:"jurisdiction"`
	WebDAVUsername    string `json:"webdav_username"`
}

type ApplicationSection struct {
	MCPEnabled            bool   `json:"mcp_enabled"`
	OAuthClientID         string `json:"oauth_client_id"`
	OAuthRelayCallbackURL string `json:"oauth_relay_callback_url"`
}

type SensitiveSection struct {
	TunnelTokens map[string]string        `json:"tunnel_tokens,omitempty"`
	APIToken     string                   `json:"api_token,omitempty"`
	APIKey       string                   `json:"api_key,omitempty"`
	S3           map[string]S3Credentials `json:"s3,omitempty"`
}

type S3Credentials struct {
	AccessKeyID        string `json:"access_key_id,omitempty"`
	SecretAccessKey    string `json:"secret_access_key,omitempty"`
	WebDAVPasswordHash string `json:"webdav_password_hash,omitempty"`
}

type Decoded struct {
	Payload    Payload
	Encrypted bool
}

type Inspection struct {
	CreatedAt         time.Time `json:"created_at"`
	AppVersion        string    `json:"app_version"`
	Encrypted         bool      `json:"encrypted"`
	Sections          []Section `json:"sections"`
	ContainsSensitive bool      `json:"contains_sensitive"`
	TunnelProfiles    int       `json:"tunnel_profiles"`
	DDNSSources       int       `json:"ddns_sources"`
	DDNSRecords       int       `json:"ddns_records"`
	S3Mounts          int       `json:"s3_mounts"`
	Warnings          []string  `json:"warnings,omitempty"`
	RemovedTunnels    []string  `json:"removed_tunnels,omitempty"`
	RestartRequired   []string  `json:"restart_required,omitempty"`
}
~~~

- [ ] **Step 4: Implement strict envelope encoding and decoding**

Create `internal/configbackup/codec.go`. Implement:

- `rejectDuplicateJSONKeys` by walking `json.Decoder.Token` recursively and rejecting a repeated key within the same object;
- `decodeStrict` with `DisallowUnknownFields` and a mandatory EOF after the first value;
- plaintext envelope output when password is empty;
- encrypted output using a 16-byte random salt, 12-byte random nonce, `scrypt.Key(password, salt, 32768, 8, 1, 32)`, `aes.NewCipher`, `cipher.NewGCM`, and additional data `[]byte("cfui-config-backup:v1")`;
- generic `ErrInvalidPasswordOrTampered` for GCM authentication failure;
- exact encryption metadata validation before allocating or deriving a key;
- `validatePayload` checks section/pointer consistency, canonical section order, duplicate keys, the exact object-count limits, and the 64 KiB limit on every string field;
- `Inspect` counts derived only from the decoded payload.

The public encoder must follow this control flow:

~~~go
func Encode(payload Payload, password string, random io.Reader) ([]byte, error) {
	if err := validatePayload(payload); err != nil {
		return nil, err
	}
	if password == "" {
		return json.MarshalIndent(Envelope{Format: Format, Version: EnvelopeVersion, Encrypted: false, Payload: &payload}, "", "  ")
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("%w: encode payload", ErrInvalidBackup)
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(random, salt); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(random, nonce); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(password), salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := gcm.Seal(nil, nonce, plain, []byte("cfui-config-backup:v1"))
	env := Envelope{
		Format: Format, Version: EnvelopeVersion, Encrypted: true,
		Encryption: &Encryption{
			Algorithm: "AES-256-GCM", KDF: "scrypt", N: 32768, R: 8, P: 1,
			Salt: base64.StdEncoding.EncodeToString(salt),
			Nonce: base64.StdEncoding.EncodeToString(nonce),
		},
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	return json.MarshalIndent(env, "", "  ")
}
~~~

- [ ] **Step 5: Run codec tests**

Run:

~~~bash
gofmt -w internal/configbackup/types.go internal/configbackup/codec.go internal/configbackup/codec_test.go
go test ./internal/configbackup -run 'TestEncode|TestDecode' -v
~~~

Expected: all codec tests pass.

- [ ] **Step 6: Commit the format layer**

Run:

~~~bash
git add internal/configbackup/types.go internal/configbackup/codec.go internal/configbackup/codec_test.go
git commit -m "feat: add versioned config backup format"
~~~

---

### Task 4: Build, validate, and apply selectable configuration sections

**Files:**
- Create: `internal/configbackup/sections.go`
- Create: `internal/configbackup/apply.go`
- Create: `internal/configbackup/apply_test.go`

**Interfaces:**
- Consumes:
  - `Build(cfg config.Config, options ExportOptions, appVersion string, now time.Time) (Payload, error)`
  - `Apply(current config.Config, payload Payload, selected []Section) (ApplyResult, error)`
- Produces:
  - `ApplyResult.Config`: complete post-import configuration
  - `ApplyResult.ChangedSections`: selected sections whose effective data changed
  - `ApplyResult.Warnings`: ignored credential or remote-profile keys
  - `ApplyResult.RemovedTunnelKeys`: profiles absent after replacement
  - `ApplyResult.ChangedTunnelKeys`: surviving profiles whose local settings or token changed

- [ ] **Step 1: Write failing section and apply tests**

Create `internal/configbackup/apply_test.go` with:

1. Build with Tunnels only omits tokens and remote fields.
2. Build with Tunnels, S3 WebDAV, Remote Tunnel Manager, and Sensitive includes only those credentials.
3. Import Tunnels without Sensitive preserves tokens by matching key and leaves new profile tokens empty.
4. Import Tunnels without Remote preserves matching remote fields and defaults new remote fields.
5. Import S3 WebDAV without Sensitive preserves matching mount credentials.
6. Import Sensitive alone updates existing matching tunnel and mount credentials and warns for unknown keys.
7. DDNS replacement removes old sources and records.
8. Application replacement changes `MCPEnabled`, `OAuthClientID`, and `OAuthRelayCallbackURL` only.
9. Duplicate keys and all exact count/string limits fail before Apply returns a config.
10. Removed and changed tunnel key lists are sorted for deterministic responses.

Use a fixture containing two tunnels, two mounts, one DDNS source, one DDNS record, API credentials, OAuth base settings, and distinct secrets. Assert complete structs with `reflect.DeepEqual`; never compare only counts.

- [ ] **Step 2: Run the apply tests and confirm failure**

Run:

~~~bash
go test ./internal/configbackup -run 'TestBuild|TestApply|TestValidate' -v
~~~

Expected: FAIL because `Build`, `Apply`, `ExportOptions`, and `ApplyResult` do not exist.

- [ ] **Step 3: Add options and result types**

Append these shapes to `internal/configbackup/types.go`:

~~~go
type ExportOptions struct {
	Sections         []Section
	IncludeSensitive bool
}

type ApplyResult struct {
	Config            config.Config
	ChangedSections   []Section
	Warnings          []string
	RemovedTunnelKeys []string
	ChangedTunnelKeys []string
}

type TunnelDiff struct {
	RemovedKeys []string
	ChangedKeys []string
}

var normalSectionOrder = []Section{
	SectionTunnels,
	SectionRemoteManagement,
	SectionDDNS,
	SectionS3WebDAV,
	SectionApplication,
}
~~~

- [ ] **Step 4: Implement export section construction**

Create `internal/configbackup/sections.go`.

Implement one explicit converter in each direction. Do not marshal `config.Config` and delete fields. The converters must assign every field declared in `TunnelProfile` and `S3Mount` directly.

`Build` must:

- reject zero selected normal sections;
- reject unknown or duplicate section names;
- preserve canonical section order regardless of request order;
- include Sensitive only when `IncludeSensitive` is true;
- include tunnel tokens only when Tunnels is selected;
- include API token/key only when Remote Tunnel Manager is selected;
- include S3 credentials only when S3 WebDAV is selected;
- stamp `PayloadVersion`, `now.UTC()`, and `appVersion`.

Use these function signatures:

~~~go
func Build(cfg config.Config, options ExportOptions, appVersion string, now time.Time) (Payload, error)
func tunnelSection(cfg config.Config) TunnelSection
func remoteSection(cfg config.Config) RemoteManagementSection
func s3Section(cfg config.Config) S3WebDAVSection
func sensitiveSection(cfg config.Config, selected map[Section]bool) *SensitiveSection
~~~

- [ ] **Step 5: Implement replacement and preservation semantics**

Create `internal/configbackup/apply.go`.

`Apply` must start from a deep copy of current. Use keyed maps only for preserving fields; retain the imported order for tunnel profiles, remote profiles, DDNS items, and S3 mounts.

The exact order is:

1. Validate payload and selected sections.
2. Replace local tunnel fields if Tunnels is selected.
3. Preserve current token and remote fields for matching imported tunnel keys.
4. Apply Remote Tunnel Manager fields to matching resulting tunnel keys and replace shared `APIEmail`.
5. Replace DDNS.
6. Replace non-secret S3 WebDAV fields and preserve matching credentials.
7. Replace Application fields.
8. Apply Sensitive fields last to existing matching keys.
9. Rebuild top-level legacy mirrors by saving through `config.Manager` later; do not duplicate config package normalization inside `configbackup`.
10. Compute deterministic changed, removed, and warning lists.

Use helpers with these exact signatures:

~~~go
func Apply(current config.Config, payload Payload, selected []Section) (ApplyResult, error)
func applyTunnelSection(current config.Config, imported TunnelSection) config.Config
func applyRemoteSection(cfg config.Config, imported RemoteManagementSection) (config.Config, []string)
func applyS3Section(current config.Config, imported S3WebDAVSection) config.Config
func applySensitive(cfg config.Config, imported SensitiveSection) (config.Config, []string)
func removedTunnelKeys(before, after config.Config) []string
func changedTunnelKeys(before, after config.Config) []string
func DiffTunnels(before, after config.Config) TunnelDiff
~~~

`changedTunnelKeys` compares all local runner fields and Token, but excludes remote-management-only fields.
`DiffTunnels` is the exported wrapper used after `config.Manager.Save` so server responses reflect normalized persisted keys.

- [ ] **Step 6: Run package tests**

Run:

~~~bash
gofmt -w internal/configbackup/types.go internal/configbackup/sections.go internal/configbackup/apply.go internal/configbackup/apply_test.go
go test ./internal/configbackup -v
~~~

Expected: all backup package tests pass.

- [ ] **Step 7: Commit selectable section behavior**

Run:

~~~bash
git add internal/configbackup
git commit -m "feat: apply selectable backup sections"
~~~

---

### Task 5: Add bounded HTTP endpoints and runtime reconciliation

**Files:**
- Create: `internal/server/config_backup.go`
- Create: `internal/server/config_backup_test.go`
- Modify: `internal/server/server.go`

**Interfaces:**
- Consumes: `configbackup.Build`, `Encode`, `Decode`, `Inspect`, and `Apply`
- Produces:
  - `POST /api/config-backup/export`
  - `POST /api/config-backup/inspect`
  - `POST /api/config-backup/import`

- [ ] **Step 1: Write failing handler tests**

Create `internal/server/config_backup_test.go` with:

- export rejects GET and empty section selection;
- export rejects sensitive plaintext output unless `confirm_plaintext_sensitive` is true;
- export returns `application/json`, `Content-Disposition: attachment`, and no sensitive strings by default;
- encrypted export does not expose tokens or OAuth client data;
- inspect returns metadata and accepts selected sections for preview;
- inspect rejects files over 8 MiB without writing them under `cfgMgr.Dir()`;
- import replaces DDNS while preserving Tunnels when only DDNS is selected;
- import sensitive-only updates matching credentials;
- import with a wrong password returns status 400 and code `invalid_password_or_tampered`;
- import persistence failure does not call runtime hooks;
- successful import calls DDNS, S3, and OAuth hooks only when their effective values changed;
- removed profiles invoke asynchronous `RemoveProfile` and surviving running changed profiles appear in `restart_required`.

Build multipart requests in memory with `mime/multipart.Writer`. Add a channel-backed fake remove hook so the test waits for the asynchronous call without sleeping.

- [ ] **Step 2: Run handler tests and confirm failure**

Run:

~~~bash
go test ./internal/server -run 'TestConfigBackup' -v
~~~

Expected: FAIL because the handlers and routes do not exist.

- [ ] **Step 3: Register routes and add testable hooks**

Modify `Server` in `internal/server/server.go`:

~~~go
type configBackupRuntimeHooks struct {
	removeProfile func(string) error
	profileStatus func(string) (cloudflared.Status, bool)
	restartDDNS   func()
	restartS3     func(context.Context)
	resetOAuth    func()
}
~~~

Add `backupHooks configBackupRuntimeHooks` to `Server`.

Register:

~~~go
mux.HandleFunc("/api/config-backup/export", s.handleConfigBackupExport)
mux.HandleFunc("/api/config-backup/inspect", s.handleConfigBackupInspect)
mux.HandleFunc("/api/config-backup/import", s.handleConfigBackupImport)
~~~

Add a lazy `s.configBackupRuntime()` helper that fills only nil functions from `s.runner`, `s.ddnsSvc`, `s.reloadS3WebDAVAfterImport`, and `s.resetOAuthService`. Tests can pre-fill individual functions.

`reloadS3WebDAVAfterImport` records whether the dedicated server is currently running. If it is running, call `restartS3WebDAVDedicated` so the new address/settings take effect. If it is not running, call `reconcileS3WebDAVDedicated(ctx, false)` so a newly imported `DedicatedAutoStart` setting is honored without starting a manually disabled server.

- [ ] **Step 4: Implement bounded request parsing and error mapping**

Create `internal/server/config_backup.go`.

Export request:

~~~go
type configBackupExportRequest struct {
	Sections                  []configbackup.Section `json:"sections"`
	IncludeSensitive          bool                   `json:"include_sensitive"`
	Password                  string                 `json:"password"`
	ConfirmPlaintextSensitive bool                   `json:"confirm_plaintext_sensitive"`
}
~~~

Import response:

~~~go
type configBackupImportResponse struct {
	ChangedSections []configbackup.Section `json:"changed_sections"`
	Warnings        []string               `json:"warnings,omitempty"`
	StopRequested   []string               `json:"stop_requested,omitempty"`
	RestartRequired []string               `json:"restart_required,omitempty"`
}
~~~

Use `http.MaxBytesReader` with 64 KiB for export JSON. For inspect/import, use `r.MultipartReader` and read parts directly into memory. Accept only `file`, `password`, and `sections` fields; reject duplicate file/password/sections parts, unknown parts, more than 8 MiB of file data, and more than 64 KiB of combined text fields. Never call `ParseMultipartForm`.

Return JSON errors with:

~~~go
type configBackupErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
~~~

Map errors to `invalid_backup`, `unsupported_version`, `password_required`, `invalid_password_or_tampered`, `plaintext_sensitive_confirmation_required`, `too_large`, `invalid_selection`, and `save_failed`. Do not include wrapped secret-bearing errors.

- [ ] **Step 5: Implement export, inspect preview, and import**

`handleConfigBackupExport`:

1. POST only.
2. Decode bounded JSON.
3. Reject `IncludeSensitive && Password == "" && !ConfirmPlaintextSensitive`.
4. Build from `s.cfgMgr.Get()`, `version.Version`, and `time.Now()`.
5. Encode with `crypto/rand.Reader`.
6. Set `Content-Type: application/json` and Content-Disposition using `mime.FormatMediaType`.
7. Write filename `cfui-backup-YYYYMMDDTHHMMSSZ.json`.

`handleConfigBackupInspect`:

1. POST only.
2. Read multipart data.
3. Decode with the optional password.
4. Inspect payload.
5. If sections were supplied, call Apply against current config without saving and add `RemovedTunnels` and the running subset of `ChangedTunnelKeys`.

`handleConfigBackupImport`:

1. POST only.
2. Re-read and decode the file independently.
3. Apply selected sections to current config.
4. Save `result.Config` once through `s.cfgMgr.Save`.
5. Read the normalized saved config.
6. Recompute changed/removed keys with `configbackup.DiffTunnels(before, saved)`.
7. Start one goroutine per removed key calling `removeProfile` and logging only the key plus generic error.
8. Call `restartDDNS` only when DDNS differs.
9. Call `restartS3` only when S3WebDAV differs.
10. Call `resetOAuth` only when OAuthClientID or OAuthRelayCallbackURL differs.
11. Return `StopRequested` and the pre-save running subset of changed tunnel keys.

- [ ] **Step 6: Run server tests**

Run:

~~~bash
gofmt -w internal/server/server.go internal/server/config_backup.go internal/server/config_backup_test.go
go test ./internal/server -run 'TestConfigBackup' -v
go test ./internal/server -v
~~~

Expected: all server tests pass.

- [ ] **Step 7: Commit server support**

Run:

~~~bash
git add internal/server/server.go internal/server/config_backup.go internal/server/config_backup_test.go
git commit -m "feat: add config backup API"
~~~

---

### Task 6: Add the export/import user interface and translations

**Files:**
- Create: `web/dist/js/app-backup.js`
- Modify: `web/dist/index.html`
- Modify: `web/dist/js/app-init.js`
- Modify: `web/dist/style.css`
- Modify: `locales/en.toml`
- Modify: `locales/zh.toml`
- Modify: `locales/ja.toml`
- Modify: `web_init_test.go`
- Modify: `i18n_parity_test.go`

**Interfaces:**
- Consumes: the three config-backup endpoints and existing cfui dialog, toast, API, `fetchConfig`, and `fetchFeatures` helpers
- Produces: `window.cfui.wireBackup` and a complete Features-panel backup workflow

- [ ] **Step 1: Add failing embedded-asset and markup tests**

Extend `web_init_test.go`:

~~~go
func TestBackupUIAssetsAreWired(t *testing.T) {
	index, err := os.ReadFile("web/dist/index.html")
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	src := string(index)
	for _, marker := range []string{
		`id="config-backup-export"`,
		`id="config-backup-import"`,
		`id="config-backup-export-dialog"`,
		`id="config-backup-import-dialog"`,
		`src="/js/app-backup.js"`,
	} {
		if !strings.Contains(src, marker) {
			t.Fatalf("index missing %s", marker)
		}
	}
	initJS, err := os.ReadFile("web/dist/js/app-init.js")
	if err != nil {
		t.Fatalf("read app-init.js: %v", err)
	}
	if !strings.Contains(string(initJS), "wireBackup") {
		t.Fatal("app-init.js does not wire backup UI")
	}
}
~~~

Run:

~~~bash
go test . -run 'TestBackupUIAssetsAreWired|TestEmbeddedLocaleKeyParity' -v
~~~

Expected: FAIL because the markup, script, wiring, and locale keys do not exist.

- [ ] **Step 2: Add the Features card and dialogs**

In `web/dist/index.html`, append a second card under the existing Features card with:

- title and description;
- Export configuration button with id `config-backup-export`;
- Import configuration button with id `config-backup-import`;
- hidden file input accepting `application/json` and `.json`;
- export modal with six checkboxes, optional password, plaintext-sensitive warning, cancel, and download;
- import modal with file metadata, encrypted password prompt, six available-section checkboxes, counts, preview list, cancel, and replace button.

Use existing `modal-backdrop`, `modal`, `form-field`, `toggle`, `btn-row`, `alert`, and `help-text` classes. Every visible string and aria-label uses `data-i18n` or `data-i18n-attr`.

Insert:

~~~html
<script src="/js/app-backup.js"></script>
~~~

after `app-services.js` and before `app-oauth-data.js` so `app-init` can consume `window.cfui.wireBackup`.

- [ ] **Step 3: Implement browser behavior**

Create `web/dist/js/app-backup.js`.

The module must:

- define the exact section order `tunnels`, `remote_management`, `ddns`, `s3_webdav`, `application`, `sensitive`;
- default all normal export options on and Sensitive off;
- require at least one normal export section;
- call `window.cfui.confirm` when Sensitive is selected with an empty password and send `confirm_plaintext_sensitive: true` only after the user accepts;
- send export JSON with `fetch`, read `response.blob()`, derive the filename from Content-Disposition when present, and download through a temporary object URL;
- retain the selected File object only in browser memory;
- inspect through FormData containing file, password, and JSON-encoded selected sections;
- show password input only when the server returns `password_required`;
- render counts through `textContent`, never `innerHTML` with server data;
- keep Sensitive off after inspection;
- repeat inspection after import selection changes so removed and restart-required profile keys stay accurate;
- require replacement confirmation before import;
- submit import with a fresh FormData;
- after success call `fetchFeatures`, `fetchConfig`, `fetchStatus`, `refreshDDNS` when visible, and `fetchS3Settings` when visible;
- show stop_requested, restart_required, and warning summaries through localized toast text;
- clear password fields, file references, and object URLs when dialogs close.

Export:

~~~js
window.cfui.wireBackup = wireBackup;
~~~

Modify `app-init.js` to destructure `wireBackup` and call it after `wireServices`.

- [ ] **Step 4: Add translations**

Add the same keys to `locales/en.toml`, `locales/zh.toml`, and `locales/ja.toml`:

- `config_backup_title`
- `config_backup_subtitle`
- `config_backup_export`
- `config_backup_import`
- `config_backup_export_title`
- `config_backup_import_title`
- `config_backup_section_tunnels`
- `config_backup_section_remote`
- `config_backup_section_ddns`
- `config_backup_section_s3`
- `config_backup_section_application`
- `config_backup_section_sensitive`
- `config_backup_password_optional`
- `config_backup_password_help`
- `config_backup_plaintext_sensitive_title`
- `config_backup_plaintext_sensitive_message`
- `config_backup_choose_file`
- `config_backup_encrypted`
- `config_backup_plaintext`
- `config_backup_source_version`
- `config_backup_created_at`
- `config_backup_tunnel_count`
- `config_backup_ddns_source_count`
- `config_backup_ddns_record_count`
- `config_backup_s3_mount_count`
- `config_backup_replace_warning`
- `config_backup_removed_tunnels`
- `config_backup_restart_required`
- `config_backup_download`
- `config_backup_replace`
- `config_backup_exported`
- `config_backup_imported`
- `config_backup_password_required`
- `config_backup_invalid_password`
- `config_backup_invalid_file`
- `config_backup_unsupported_version`
- `config_backup_too_large`
- `config_backup_select_section`
- `config_backup_contains_sensitive`
- `config_backup_stop_requested`

Use natural native-language copy. Do not translate product names, AES-GCM, scrypt, Tunnel, DDNS, S3 WebDAV, OAuth, or MCP inconsistently with existing locale usage.

- [ ] **Step 5: Add focused styles**

Append styles for:

- `backup-action-row`: wrapping button row;
- `backup-option-list`: two-column grid above 720px and one column below;
- `backup-option`: bordered checkbox row using current surface and border variables;
- `backup-meta-grid`: compact key/value grid;
- `backup-summary-list`: monospace profile keys with wrapping;
- `backup-sensitive-warning`: existing warning colors without a new palette.

Do not introduce gradients, new font imports, or new global button styles.

- [ ] **Step 6: Verify UI assets and locale parity**

Run:

~~~bash
go test . -run 'TestBackupUIAssetsAreWired|TestEmbeddedLocaleKeyParity' -v
go test ./internal/server -run 'TestI18n' -v
~~~

Expected: all tests pass.

- [ ] **Step 7: Commit the UI**

Run:

~~~bash
git add web/dist/index.html web/dist/js/app-backup.js web/dist/js/app-init.js web/dist/style.css locales/en.toml locales/zh.toml locales/ja.toml web_init_test.go i18n_parity_test.go
git commit -m "feat: add config backup interface"
~~~

---

### Task 7: Document behavior and run release-grade verification

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`
- Modify: `docs/superpowers/specs/2026-07-14-config-backup-restore-design.md`
- Verify: all changed source, tests, assets, dependency files, and documentation

**Interfaces:**
- Consumes: completed dependency and feature commits
- Produces: documented behavior, reviewed diff, clean test/build evidence, and a final local commit

- [ ] **Step 1: Update user documentation**

Add a Configuration Backup and Restore section to both READMEs containing:

- location in the Features tab;
- six selectable categories;
- default exclusion of sensitive credentials;
- optional password behavior;
- explicit warning that sensitive plaintext exports are readable files;
- selected-section replacement semantics;
- exclusions for OAuth sessions, PKCE, MCP tokens, validation reports, logs, and runtime state;
- DDNS/S3/OAuth reload behavior;
- removed-tunnel asynchronous stop and surviving-tunnel restart prompt;
- 8 MiB import limit.

Update the design wording from “stopped profiles” to “profiles whose asynchronous stop was requested” everywhere it appears.

- [ ] **Step 2: Run formatting and focused tests**

Run:

~~~bash
gofmt -w internal/configbackup internal/server
go test ./internal/configbackup -v
go test ./internal/server -run 'TestConfigBackup|TestI18n|TestFeatures' -v
go test . -run 'TestBackupUIAssetsAreWired|TestEmbeddedLocaleKeyParity' -v
~~~

Expected: every command exits zero with non-skipped tests.

- [ ] **Step 3: Run complete verification**

Run:

~~~bash
go test ./...
go vet ./...
make build
git diff --check
~~~

Expected: all tests pass, vet reports no findings, `make build` exits zero, and `git diff --check` prints nothing.

- [ ] **Step 4: Review security-sensitive output**

Run:

~~~bash
git diff --stat
git diff
git diff --cached
rg -n 'password|secret|api_token|api_key|access_token|refresh_token|tunnel_tokens|secret_access_key' internal/configbackup internal/server web/dist/js/app-backup.js README.md README.zh-CN.md
~~~

Confirm:

- no real credentials exist;
- error/log paths never format secret values;
- DOM rendering of server strings uses `textContent`;
- upload size and string/count limits are enforced before persistence;
- plaintext sensitive export requires confirmation;
- encrypted payload does not retain plaintext metadata beyond format/version/encrypted state.

- [ ] **Step 5: Request code review and address findings**

Use `requesting-code-review` with:

- description: merged dependency PRs, updated cloudflared, and added selectable optionally encrypted configuration backup/restore;
- requirements: `docs/superpowers/specs/2026-07-14-config-backup-restore-design.md`;
- base SHA: `origin/main` after the four PR merges;
- head SHA: current HEAD.

Fix every Critical and Important finding, rerun the affected focused tests, then rerun `go test ./...`, `go vet ./...`, and `make build`.

- [ ] **Step 6: Commit documentation and review fixes**

Run:

~~~bash
git status --short --branch -uall
git rev-parse HEAD
git add README.md README.zh-CN.md docs/superpowers/specs/2026-07-14-config-backup-restore-design.md
git diff --cached --check
git commit -m "docs: explain config backup and restore"
~~~

If review fixes are already committed separately and no documentation diff remains, do not create an empty commit.

- [ ] **Step 7: Final state check**

Run:

~~~bash
git status --short --branch -uall
git log --oneline --decorate -12
gh pr view 34 --json state,mergedAt,mergeCommit,url
gh pr view 36 --json state,mergedAt,mergeCommit,url
gh pr view 37 --json state,mergedAt,mergeCommit,url
gh pr view 38 --json state,mergedAt,mergeCommit,url
go list -m github.com/cloudflare/cloudflared
~~~

Expected: worktree clean; all four PRs merged; cloudflared pinned to `v0.0.0-20260713102814-2601f87b5728`; local feature commits present. Do not push the local feature commits unless the user explicitly authorizes a push.

---

## Self-Review Checklist

- Every design requirement maps to a task above.
- Tunnel, remote, DDNS, S3, application, and sensitive ownership are explicit.
- The password is optional and plaintext-sensitive export is gated.
- Import is bounded, strict, and atomic before runtime reconciliation.
- Runtime behavior distinguishes asynchronous stop requests from confirmed stops.
- PR merge operations verify exact head commits and refresh dependent PR branches.
- No implementation step relies on OAuth/MCP runtime data being exportable.
- All newly named functions and types are defined in this plan before use.
- Final verification includes focused tests, full tests, vet, build, diff checks, security review, and an independent code review.
