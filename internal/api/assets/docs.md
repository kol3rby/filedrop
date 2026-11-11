# filedrop

A tiny file drop you drive with `curl`: upload a file, get a URL back, download it from anywhere.

## Overview

- Files are grouped under a **slug**, a short name that acts as a shared folder: `{{BASE_URL}}/<slug>/<filename>`.
- Uploading a file with a name that already exists in the slug creates a **new version**. Versions are numbered `1, 2, 3…`; downloads return the latest one unless a version is requested.
- There is **no authentication**: anyone who knows a slug can read, upload and delete its files. Use the generated slugs (random, hard to guess) and treat the URL as a secret.
- Maximum upload size: **{{MAX_UPLOAD_SIZE}}**.

## Endpoints

| Method | Path | Description | Success |
|---|---|---|---|
| `GET` | `/` | This documentation (HTML in a browser, Markdown with curl) | `200` |
| `POST` | `/` | Upload a file under a new, generated slug | `201` + file URL |
| `POST` | `/<slug>` | Upload a file under a given slug, named like with `POST /` | `201` + file URL |
| `POST` | `/<slug>/<filename>` | Upload a file (or a new version of it) under a given slug | `201` + file URL |
| `GET` | `/<slug>` | List the files of a slug with all their versions | `200` |
| `GET` | `/<slug>/<filename>` | Download the latest version, or `?version=N` | `200` + file content |
| `DELETE` | `/<slug>/<filename>` | Delete every version, or only `?version=N` | `204` |

## Usage with curl

Always send the raw content with `--data-binary` (`-d` would strip line breaks).

**Upload under a generated slug** — the response body is the URL of the file:

```sh
curl --data-binary @report.pdf "{{BASE_URL}}/?filename=report.pdf"
# {{BASE_URL}}/k3Fz9QpA/report.pdf
```

**Upload to a known slug** (sending the same filename again creates version 2, 3…):

```sh
curl --data-binary @report.pdf {{BASE_URL}}/k3Fz9QpA/report.pdf
```

Or let the filename follow the same rules as `POST /` (`?filename=`, `X-Filename`, or generated):

```sh
curl --data-binary @report.pdf "{{BASE_URL}}/k3Fz9QpA?filename=report.pdf"
```

**Pipe anything**:

```sh
tar cz ./project | curl --data-binary @- {{BASE_URL}}/k3Fz9QpA/project.tar.gz
```

**List the files of a slug**:

```sh
curl {{BASE_URL}}/k3Fz9QpA
```

**Download** the latest version, or a given one:

```sh
curl {{BASE_URL}}/k3Fz9QpA/report.pdf                    # to stdout
curl -OJ {{BASE_URL}}/k3Fz9QpA/report.pdf                # save as report.pdf
curl -o v1.pdf "{{BASE_URL}}/k3Fz9QpA/report.pdf?version=1"
```

**Delete** a file (all its versions), or a single version:

```sh
curl -X DELETE {{BASE_URL}}/k3Fz9QpA/report.pdf
curl -X DELETE "{{BASE_URL}}/k3Fz9QpA/report.pdf?version=1"
```

In a browser, `{{BASE_URL}}/<slug>` shows the same list with download links and delete buttons, and an upload zone: drop files on it or click it to choose them.

## Parameters

| Name | Where | Rules |
|---|---|---|
| `slug` | path | 1–64 characters among `A-Z a-z 0-9 _ -`. `healthz` is reserved. |
| `filename` | path | 1–255 bytes of UTF-8, no `/`, `\` or control characters, not `.` or `..`. URL-encode special characters (`my%20file.txt`). |
| `filename` | query, `POST /` and `POST /<slug>` | Name of the uploaded file. Can also be given with the `X-Filename` header. If both are missing, a name such as `file_2026-01-01_12-00-00.txt` is generated (UTC timestamp, extension guessed from the content type). |
| `version` | query | Positive integer. Selects a version for download and delete. |
| `Content-Type` | header, uploads | Stored and returned on download. If absent (or curl's default `application/x-www-form-urlencoded`), the type is detected from the content. |
| `Accept` | header | `application/json` switches uploads, listings and errors to JSON. |

## Responses

**Upload** returns `201 Created`, the file URL in the body and in the `Location` header, and the version number in `X-File-Version`. With `Accept: application/json`:

```json
{
  "slug": "k3Fz9QpA",
  "filename": "report.pdf",
  "version": 2,
  "size": 48213,
  "sha256": "9f86d081884c7d65…",
  "content_type": "application/pdf",
  "created_at": "2026-01-01T12:00:00Z",
  "url": "{{BASE_URL}}/k3Fz9QpA/report.pdf",
  "version_url": "{{BASE_URL}}/k3Fz9QpA/report.pdf?version=2"
}
```

**Download** returns the content with its `Content-Type`, a `Content-Disposition: attachment` header carrying the filename, an `ETag` (the SHA-256) and `X-File-Version`. Range requests (`curl -C -`) are supported.

**List** returns an aligned text table, a JSON array (`Accept: application/json`) or an HTML page (browsers).

## Errors

Errors come as `error: <message>` in plain text, or `{"error": "<message>"}` with `Accept: application/json`.

| Status | Meaning |
|---|---|
| `400` | Invalid slug, filename or version, or empty body |
| `404` | Unknown slug, file or version |
| `405` | Method not allowed on this path |
| `413` | Body larger than {{MAX_UPLOAD_SIZE}} |
| `500` | Server error |
