# S3 WebDAV Design

Date: 2026-06-04

## Goal

Add an optional S3 WebDAV feature to cfui. Users can configure one or more S3-compatible bucket mounts, expose each mount under a custom WebDAV path, and manage files from the web UI.

Cloudflare R2 is supported as one provider preset, but object access always uses S3-compatible credentials: Access Key ID and Secret Access Key. Cloudflare API Token permissions are not required to enable S3 WebDAV or to read/write files.

## Decisions

- Feature flag: `s3_webdav`.
- WebDAV routes: custom paths under `/webdav/`, for example `/webdav/my_r2/` and `/webdav/qiniu/`.
- API routes: `/api/s3/*`.
- Do not keep old `/api/r2/*`, `/webdav/r2/`, or `internal/r2dav` compatibility.
- Persist mounts in a dedicated `s3_webdav_settings` table. Do not store mount definitions as a JSON blob.
- Store only global `s3_webdav_enabled` and `s3_webdav_active_key` in `app_settings`.
- Treat Access Key ID, Secret Access Key, and WebDAV password hash as sensitive fields.
- Support multiple mounts, each with its own S3 endpoint, bucket, root prefix, WebDAV path, WebDAV username, and WebDAV password.
- Cloudflare R2 bucket list/create is optional and uses the existing Remote Tunnel Manager Cloudflare API Token only when the selected mount provider is Cloudflare R2.

## Data Model

`s3_webdav_settings` stores one row per mount:

- `key`, `name`, `sort_order`, `enabled`
- `provider`
- `endpoint_url`, `region`, `path_style`
- `account_id`, `jurisdiction`
- `bucket_name`, `root_prefix`, `mount_path`
- `access_key_id`, `secret_access_key`
- `webdav_username`, `webdav_password_hash`
- timestamps

`app_settings` stores:

- `s3_webdav_enabled`
- `s3_webdav_active_key`

## API

- `GET /api/s3/settings`
- `POST /api/s3/settings`
- `POST /api/s3/mounts`
- `PUT /api/s3/mounts/{key}`
- `DELETE /api/s3/mounts/{key}`
- `POST /api/s3/test`
- `GET /api/s3/buckets`
- `POST /api/s3/buckets`
- `GET /api/s3/files`
- `GET /api/s3/files/download`
- `PUT /api/s3/files/*`
- `DELETE /api/s3/files/*`
- `POST /api/s3/files/mkdir`
- `POST /api/s3/files/rename`

File and bucket APIs accept `mount_key` so the UI can manage multiple mounts.

## UX

- Hide the S3 WebDAV tab while the feature flag is off.
- Show S3 WebDAV in the Features page with a short reason/status line.
- In the S3 tab, show a mount list on the left and the selected mount's settings/files on the right.
- Provider choices:
  - Generic S3: user manages bucket outside cfui.
  - Cloudflare R2: cfui shows R2 credential guidance and optional bucket list/create controls.
- Explain that WebDAV login credentials are separate from S3 object credentials.
- Use an icon-only copy button for the WebDAV endpoint with accessible label/title.
- Provide a connection test button for the selected mount.

## Verification

- Go unit tests cover config persistence, mount validation, API handlers, and WebDAV path routing.
- JS syntax checks cover the frontend files.
- Full release verification should include `go test ./...`, `go build ./...`, and `git diff --check`.
