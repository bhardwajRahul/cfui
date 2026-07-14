# Configuration Backup and Restore Design

Date: 2026-07-14

## Goal

Add a safe, versioned configuration export and import workflow to cfui. Users can choose which configuration sections to export or replace during import. Backup encryption is optional, and sensitive credentials are never included unless explicitly selected.

The same delivery batch will also review and merge all currently open Dependabot pull requests, update github.com/cloudflare/cloudflared to the latest compatible revision, and correct Cloudflare API Token verification so Zone Read never substitutes for DNS Write. Those dependency and verification operations are implementation and release tasks rather than part of the backup file format.

## Decisions

- Use a logical, versioned JSON backup instead of copying the SQLite database.
- Both export and import show a section checklist.
- Import replaces each selected section; unselected sections remain unchanged.
- A password is optional. A non-empty password encrypts the complete payload. An empty password creates a plaintext backup.
- Sensitive credentials are a separate, default-off option.
- Plaintext export containing sensitive credentials requires an explicit second confirmation.
- OAuth sessions, OAuth PKCE state, MCP access tokens, OAuth validation reports, logs, and runtime state are never exported.
- Existing running tunnels are not automatically restarted after import. Removed tunnel profiles receive an asynchronous stop-and-forget request. Surviving affected tunnels are reported as requiring restart.

## Alternatives Considered

### Raw SQLite backup

This preserves every row but cannot provide a reliable per-section selection workflow. A live SQLite database also has WAL consistency concerns, and database-schema compatibility would unnecessarily couple backups to a specific cfui release.

### Raw API configuration snapshot

Serializing the current configuration response would be quick, but its public API shape is not a stable backup contract. Some stored secrets are intentionally omitted from JSON, and future API changes could silently make older backups incomplete.

### Versioned logical JSON backup

This approach provides explicit ownership of fields, section-level replacement, stable validation, migrations for future versions, and controlled handling of secrets. It is the selected design.

## User Interface

Add a Configuration Backup and Restore card to the Features panel because the feature applies to the whole local cfui workspace rather than one tunnel profile.

### Export dialog

The export dialog contains these options:

1. Tunnel configuration
2. Remote Tunnel Manager
3. DDNS
4. S3 WebDAV
5. Application and OAuth base settings
6. Sensitive credentials

At least one non-sensitive section must be selected for export. During export, Sensitive credentials are additive: selecting them includes credentials belonging to the selected sections only.

The dialog also contains an optional password field. When it is empty, the downloaded envelope contains a plaintext payload. When it is non-empty, the complete payload is encrypted. If sensitive credentials are selected without a password, a confirmation dialog explains that Tunnel tokens, Cloudflare API credentials, or storage credentials will be written to a readable local file. The export request carries an explicit plaintext-sensitive acknowledgement only after that confirmation; the server rejects sensitive plaintext export without it.

The downloaded filename is cfui-backup-YYYYMMDDTHHMMSSZ.json.

### Import dialog

The user selects a backup file. cfui inspects the envelope without persisting the uploaded file. If the envelope is encrypted, the dialog asks for its password and retries inspection.

After successful inspection, the dialog shows:

- backup creation time and source cfui version;
- whether the payload is encrypted;
- sections available in the file;
- counts of tunnel profiles, DDNS sources and records, and S3 mounts;
- whether sensitive credentials are present;
- a checklist containing only sections present in the file.

The inspection endpoint accepts the currently selected import sections and returns a replacement preview. The frontend repeats inspection after the selection changes so the confirmation can name removed profiles and running profiles that will require restart.

All available non-sensitive sections are selected by default. Sensitive credentials remain unselected by default. During import, the credential section may be selected by itself to update matching objects already present in cfui. Before import, the UI states that selected normal sections will replace their current counterparts and that the credential section only updates matching credential fields. If running or removed tunnel profiles are affected, the confirmation also states which profiles will receive an asynchronous stop request or require restart.

After import, the UI refreshes configuration and feature state and displays the changed sections, warnings, profiles whose asynchronous stop was requested, and profiles that require restart.

## Section Ownership

The backup payload does not serialize config.Config directly. It uses dedicated data-transfer types so field ownership and secret handling remain explicit.

### Tunnel configuration

Owns the ordered tunnel profile list, active tunnel key, profile key, name, local-enabled state, and local cloudflared runner settings. It excludes Tunnel tokens unless sensitive credentials are selected.

Remote-management fields are not owned by this section. If Tunnel configuration is imported without Remote Tunnel Manager, current remote-management fields are preserved for matching profile keys. Newly imported profile keys receive disabled and empty remote-management defaults.

If sensitive credentials are not imported, existing Tunnel tokens are preserved for matching profile keys. Newly imported profile keys have no token.

### Remote Tunnel Manager

Owns each profile's remote-management enabled flag, Account ID, and Tunnel ID, plus shared Cloudflare API authentication metadata. The API token and API key are excluded unless sensitive credentials are selected. API email is included with this section because it is required metadata for Global API Key authentication.

When imported without Tunnel configuration, remote settings are applied only to existing matching profile keys. Unknown profile keys are ignored and returned as warnings.

### DDNS

Owns enabled state, interval, only-on-change behavior, retry count, ordered IP sources, and ordered DNS record definitions. Import replaces the complete DDNS section.

### S3 WebDAV

Owns global S3 WebDAV state and the ordered mount list, excluding Access Key ID, Secret Access Key, and WebDAV password hash unless sensitive credentials are selected. WebDAV usernames remain in the normal S3 section.

If sensitive credentials are not imported, current credentials are preserved for matching mount keys. Newly imported mount keys have empty credentials.

### Application and OAuth base settings

Owns the MCP feature switch, OAuth Client ID, and OAuth relay callback URL. It does not include OAuth identities, access tokens, refresh tokens, PKCE state, or validation reports.

Module-specific enabled flags remain with their modules: Remote Tunnel Manager, DDNS, and S3 WebDAV.

### Sensitive credentials

Stores credentials in maps keyed by tunnel or mount key and stores the shared Remote Tunnel Manager authentication fields. It can update credentials for existing matching objects even when the corresponding normal section is not selected. Credential entries whose target key does not exist after the selected imports are ignored and returned as warnings.

The section may contain:

- Tunnel tokens;
- Cloudflare API token and Global API key;
- S3 Access Key ID and Secret Access Key;
- WebDAV password hashes.

OAuth session tokens and MCP token hashes are explicitly unsupported.

## Backup Envelope

The outer document has a stable format marker, envelope version, encrypted flag, and either a plaintext payload or encrypted payload metadata.

Plaintext envelope fields:

- format: cfui-config-backup
- version: 1
- encrypted: false
- payload: the version 1 backup payload

Encrypted envelope fields:

- format: cfui-config-backup
- version: 1
- encrypted: true
- encryption algorithm: AES-256-GCM
- key derivation: scrypt with N 32768, r 8, p 1, a 16-byte random salt, and a 32-byte key
- nonce: a 12-byte cryptographically random nonce
- ciphertext: base64-encoded encrypted payload

The authenticated additional data is the constant cfui-config-backup:v1. The password, derived key, plaintext payload, and decrypted credentials are never logged or persisted outside the configuration transaction.

The inner payload contains:

- schema version 1;
- UTC creation timestamp;
- source cfui version;
- list of included sections;
- selected section objects;
- optional sensitive credential object.

Future formats add explicit migrations. A backup with a newer unsupported envelope or payload version is rejected without changing configuration.

## HTTP API

Add these local API endpoints:

### POST /api/config-backup/export

Accepts a JSON request containing selected sections, include-sensitive flag, optional password, and an explicit plaintext-sensitive acknowledgement. The acknowledgement is required only when sensitive credentials are selected and the password is empty. Returns the JSON envelope as an attachment.

### POST /api/config-backup/inspect

Accepts multipart form data containing the file, optional password, and optional selected section list. Returns envelope metadata, available sections, counts, encryption state, sensitive-data presence, import warnings, removed profile keys, and running profile keys that require restart for the supplied selection. It never writes the uploaded file to the data directory.

### POST /api/config-backup/import

Accepts the file, optional password, and selected section list as multipart form data. It reparses and revalidates the file rather than trusting the inspection response. On success it returns changed sections, warnings, tunnel keys whose asynchronous stop was requested, and tunnel keys requiring restart.

All three endpoints allow only POST. Import and inspection use http.MaxBytesReader with an 8 MiB limit.

## Validation and Abuse Resistance

The backup parser is a trust boundary. Version 1 decoding is strict and rejects unknown fields, duplicate JSON members, trailing JSON values, invalid base64, unsupported cryptographic parameters, malformed timestamps, and absent required sections.

Limits are:

- 8 MiB complete upload;
- 256 tunnel profiles;
- 256 S3 mounts;
- 64 DDNS IP sources;
- 1024 DDNS records;
- 64 KiB maximum for any individual string field.

Existing configuration normalization remains the final canonicalization step. Additional validation rejects duplicate or empty section names, duplicate object keys, invalid section combinations, and an import with no selected section.

No request log, error response, or audit message includes backup content, passwords, derived keys, tokens, access keys, password hashes, or decrypted values. User-facing failures distinguish invalid format, unsupported version, password required, wrong password or tampered file, size limit, and configuration-save failure without echoing secret material.

## Import Transaction and Runtime Reconciliation

The import service follows this order:

1. Read the current configuration snapshot.
2. Parse, decrypt when required, and validate the complete backup.
3. Apply only selected sections to an in-memory copy.
4. Preserve unselected modules and matching credentials according to section ownership.
5. Run normalization and cross-section validation.
6. Save through config.Manager.Save, which writes the structured configuration in one SQLite transaction.
7. Compare the saved configuration with the pre-import snapshot.
8. Start asynchronous stop-and-forget operations for removed tunnel instances through Runner.RemoveProfile so an import performed through the affected tunnel can return its HTTP response.
9. Restart DDNS when the DDNS section changed.
10. Restart the dedicated S3 WebDAV service when the S3 section changed.
11. Reset the OAuth service when application and OAuth base settings changed.
12. Return runtime warnings and restart-required tunnel keys.

No runtime service is changed before the configuration transaction commits. If runtime reconciliation produces a warning after persistence succeeds, the response reports the persisted success and the specific runtime warning instead of pretending the transaction rolled back.

Surviving running tunnels continue with their existing process configuration. They are listed as requiring restart when any of their local runner settings or token changed.

## Testing

### Backup package tests

- plaintext round trip for every section;
- encrypted round trip with the exact scrypt and AES-GCM parameters;
- wrong password and tampered ciphertext rejection;
- sensitive credentials omitted by default;
- credentials included only for selected modules;
- version, duplicate field, trailing data, count, string-length, and upload-size rejection;
- deterministic section replacement and preservation of unselected fields;
- matching-key credential preservation and unknown-key warnings.

### Configuration and server tests

- import save is atomic when validation or persistence fails;
- removed running profiles call Runner.RemoveProfile;
- surviving changed profiles are returned as restart-required;
- DDNS, S3 WebDAV, and OAuth runtime hooks run only for changed selected sections;
- endpoint method checks, attachment headers, multipart parsing, size limits, and generic error responses;
- import inspection never writes an uploaded backup to disk.

### Frontend and localization tests

- export checklist, optional password, and plaintext-secret confirmation;
- encrypted import password flow and section inspection;
- replacement confirmation and post-import refresh;
- English, Chinese, and Japanese translation parity;
- embedded web asset initialization test.

### Final verification

Run gofmt on changed Go files, go test ./..., go vet ./..., and make build. Review the complete diff for accidental credential exposure before commit and push.

## Documentation

Update README.md and README.zh-CN.md with the backup scope, excluded runtime data, optional encryption behavior, plaintext credential warning, replacement semantics, and restart behavior.

## Out of Scope

- raw database backup or restore;
- exporting logs, runtime status, OAuth sessions, PKCE state, MCP tokens, or validation reports;
- fetching a backup from a URL;
- scheduled or remote backup storage;
- automatically restarting surviving cloudflared tunnels;
- importing or exporting Cloudflare remote resources such as DNS records beyond local DDNS definitions, remote ingress configuration, Workers, R2 objects, D1 data, or KV values.
