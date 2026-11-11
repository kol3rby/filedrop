#!/usr/bin/env bash
# End-to-end test: starts the real binary and replays the documented curl commands.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$ROOT/bin/filedrop"
WORK="$ROOT/.tmp/e2e"
PORT="${PORT:-18080}"
URL="http://127.0.0.1:$PORT"

rm -rf "$WORK" && mkdir -p "$WORK"
ENV_FILE="$WORK/filedrop.env" DATA_DIR="$WORK/data" ADDR="127.0.0.1:$PORT" MAX_UPLOAD_SIZE=1MB "$BIN" 2>"$WORK/server.log" &
PID=$!
trap 'kill $PID 2>/dev/null || true' EXIT

for _ in $(seq 50); do curl -fs "$URL/healthz" >/dev/null && break; sleep 0.1; done

pass=0
check() { # check <description> <expected> <actual>
  if [[ "$2" == "$3" ]]; then
    pass=$((pass + 1)); echo "ok   $1"
  else
    echo "FAIL $1: expected [$2], got [$3]"; echo "--- server log"; cat "$WORK/server.log"; exit 1
  fi
}
status() { curl -s -o /dev/null -w '%{http_code}' "$@"; }

printf 'first version\n'  > "$WORK/notes.txt"
head -c 4096 /dev/urandom > "$WORK/data.bin"

# Docs
check "GET / (curl) returns markdown" "# filedrop" "$(curl -s "$URL/" | head -1)"
check "GET / (browser) returns html" "200" "$(status -H 'Accept: text/html' "$URL/")"

# Upload with generated slug
FILE_URL="$(curl -s --data-binary @"$WORK/notes.txt" "$URL/?filename=notes.txt")"
SLUG="$(echo "$FILE_URL" | sed -E "s#^$URL/([^/]+)/notes.txt\$#\1#")"
check "POST / returns the file URL" "$URL/$SLUG/notes.txt" "$FILE_URL"

# New version in the same slug
printf 'second version\n' > "$WORK/notes.txt"
check "POST /{slug}/{filename} new version" "201" "$(status --data-binary @"$WORK/notes.txt" "$FILE_URL")"
curl -s --data-binary @"$WORK/data.bin" "$URL/$SLUG/data.bin" >/dev/null

# List
check "GET /{slug} lists 3 versions" "3" "$(curl -s "$URL/$SLUG" | tail -n +2 | wc -l | tr -d ' ')"

# Download
check "download latest" "second version" "$(curl -s "$FILE_URL")"
check "download ?version=1" "first version" "$(curl -s "$FILE_URL?version=1")"
(cd "$WORK" && mkdir -p dl && cd dl && curl -sOJ "$URL/$SLUG/data.bin")
check "binary roundtrip with curl -OJ" "$(sha256sum < "$WORK/data.bin")" "$(sha256sum < "$WORK/dl/data.bin")"

# Delete
check "DELETE one version" "204" "$(status -X DELETE "$FILE_URL?version=1")"
check "deleted version is gone" "404" "$(status "$FILE_URL?version=1")"
check "DELETE file" "204" "$(status -X DELETE "$FILE_URL")"
check "deleted file is gone" "404" "$(status "$FILE_URL")"
check "blob on disk remains only for data.bin" "1" "$(ls "$WORK/data/blobs" | wc -l | tr -d ' ')"
check "DB rows remaining" "1" "$(curl -s -H 'Accept: application/json' "$URL/$SLUG" | grep -c '"filename"')"

# Limits
head -c 2000000 /dev/zero > "$WORK/big.bin"
check "upload over limit" "413" "$(status --data-binary @"$WORK/big.bin" "$URL/$SLUG/big.bin")"

# Reload on SIGHUP: ENV_FILE overrides the environment
printf 'MAX_UPLOAD_SIZE=4MB\n' > "$WORK/filedrop.env"
kill -HUP "$PID"
for _ in $(seq 50); do grep -q "configuration reloaded" "$WORK/server.log" && break; sleep 0.1; done
check "upload allowed after SIGHUP reload" "201" "$(status --data-binary @"$WORK/big.bin" "$URL/$SLUG/big.bin")"

echo "all $pass checks passed"
