# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [1.0.0] - 2026-11-11

Initial release.

### Added

- File sharing over HTTP, designed for `curl`: metadata in SQLite, contents on disk.
- Upload under a generated slug (`POST /`) or under a given slug (`POST /<slug>`), the filename coming from `?filename=`, the `X-Filename` header, or generated; or to an explicit path (`POST /<slug>/<filename>`).
- Automatic versioning by filename: re-uploading a name creates version 2, 3…
- Download of the latest version or a given one (`?version=N`), with `Content-Disposition`, `ETag` (SHA-256), `X-File-Version` and range requests.
- Listing of a slug's files and versions as a text table, JSON or an HTML page with download links, delete buttons and an upload zone (drag and drop or file picker, several files, a progress bar per file).
- Deletion of a file (all versions) or a single version (`DELETE`, `?version=N`), on disk and in the database.
- Content type stored from the request, or detected from the content.
- JSON responses and errors with `Accept: application/json`.
- Documentation served on `/` (Markdown with curl, HTML in a browser).
- `/healthz` health check endpoint.
- Configuration through `ADDR`, `DATA_DIR`, `BASE_URL`, `MAX_UPLOAD_SIZE` and `SLUG_LENGTH`; public URL derived from `X-Forwarded-Host`/`X-Forwarded-Proto` when `BASE_URL` is unset.
- `ENV_FILE` variable: a `KEY=value` file read by the server itself, overriding the environment.
- Configuration reload on `SIGHUP`: `BASE_URL`, `MAX_UPLOAD_SIZE` and `SLUG_LENGTH` apply to new requests; `ADDR` and `DATA_DIR` need a restart. An invalid configuration is logged and the current one is kept.
- `--version` flag.
- Dockerfile, plus systemd and FreeBSD rc.d setup instructions in the README, with `reload` support.
- Unit, HTTP and end-to-end tests (`make test`, `make e2e`).

[Unreleased]: https://github.com/kol3rby/filedrop/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/kol3rby/filedrop/releases/tag/v1.0.0
