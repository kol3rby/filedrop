# filedrop

A small Go web app to share files with `curl`: metadata in SQLite, contents on disk, automatic versioning by filename.

## Quick start

```sh
make build && ./bin/filedrop          # listens on :8080, stores data in ./data
curl http://localhost:8080/           # full documentation (Markdown; HTML in a browser)
./bin/filedrop --version              # prints "filedrop 1.0.0"
```

```sh
curl --data-binary @report.pdf "http://localhost:8080/?filename=report.pdf"   # → http://localhost:8080/<slug>/report.pdf
curl --data-binary @report.pdf http://localhost:8080/<slug>/report.pdf        # version 2
curl http://localhost:8080/<slug>                                             # list
curl -OJ http://localhost:8080/<slug>/report.pdf                              # download latest
curl -X DELETE http://localhost:8080/<slug>/report.pdf                        # delete (disk + DB)
```

## Configuration

| Variable | Default | Description |
|---|---|---|
| `ADDR` | `:8080` | Listen address |
| `DATA_DIR` | `./data` | Holds `filedrop.db` and `blobs/` |
| `BASE_URL` | *(derived from the request)* | Public URL used in responses, e.g. `https://files.example.com`. Set it behind a proxy you do not control. |
| `MAX_UPLOAD_SIZE` | `100MB` | Maximum body size (`KB`, `MB`, `GB` are binary units) |
| `SLUG_LENGTH` | `8` | Length of generated slugs (4–64) |
| `ENV_FILE` | *(none)* | File of `KEY=value` lines (`#` comments allowed) whose values override the environment. A missing file is ignored. |

Without `BASE_URL`, the host is taken from `X-Forwarded-Host`/`X-Forwarded-Proto`, then from the request.

### Reloading

On `SIGHUP`, the server re-reads its configuration (from `ENV_FILE` and its environment) without dropping connections. The environment of a running process can't change, so put the values to reload in `ENV_FILE`.

- `BASE_URL`, `MAX_UPLOAD_SIZE` and `SLUG_LENGTH` take effect for new requests.
- `ADDR` and `DATA_DIR` need a restart; a change is logged and ignored.
- If the new configuration is invalid, the error is logged and the current one is kept.

```sh
kill -HUP "$(pgrep -x filedrop)"
```

## Development

```sh
make test     # unit + HTTP tests
make lint     # go vet + gofmt
make e2e      # builds and runs scripts/e2e.sh against the real binary
```

Layout: `cmd/server` (entry point), `internal/store` (SQLite), `internal/blob` (disk), `internal/slug` (validation), `internal/api` (handlers, `assets/docs.md` is the documentation served on `/`).

## Docker

```sh
docker build -t filedrop .
docker run -p 8080:8080 -v filedrop-data:/data filedrop
```

To reload settings, mount an env file and send `SIGHUP`:

```sh
docker run -d --name filedrop -p 8080:8080 -v filedrop-data:/data \
  -v "$PWD/filedrop.env:/etc/filedrop.env:ro" -e ENV_FILE=/etc/filedrop.env filedrop
docker kill -s HUP filedrop
```

Mount the directory rather than the file if your editor replaces files on save; a single-file bind mount keeps pointing at the old one.

## systemd

Install the binary, then generate the service file:

```sh
make build && sudo install -m 755 bin/filedrop /usr/local/bin/filedrop

sudo tee /etc/systemd/system/filedrop.service > /dev/null <<'EOF'
[Unit]
Description=filedrop file sharing server
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/filedrop
ExecReload=/bin/kill -HUP $MAINPID
Environment=ADDR=:8080
Environment=DATA_DIR=/var/lib/filedrop
# Optional overrides (KEY=value per line), read by filedrop itself so that
# "systemctl reload" picks up changes; a missing file is ignored
Environment=ENV_FILE=/etc/filedrop/filedrop.env
DynamicUser=yes
StateDirectory=filedrop
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now filedrop
```

Put any variable from [Configuration](#configuration) in `/etc/filedrop/filedrop.env`, for example:

```sh
BASE_URL=https://files.example.com
MAX_UPLOAD_SIZE=1GB
```

Then apply it with `sudo systemctl reload filedrop` (`restart` for `ADDR` or `DATA_DIR`, see [Reloading](#reloading)), and follow the logs with `journalctl -u filedrop -f`.

`DynamicUser` runs the service as a temporary unprivileged user. `StateDirectory` creates `/var/lib/filedrop` and gives that user ownership of it, so you don't need to create a system account.

## FreeBSD (rc.d)

Build the FreeBSD binary. You can cross-compile it from any OS (use `GOARCH=arm64` for ARM), or run `gmake build` on FreeBSD itself:

```sh
GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/filedrop ./cmd/server
```

On the FreeBSD host, as root, install the binary, create a system user, then generate the rc.d script:

```sh
install -m 755 filedrop /usr/local/bin/filedrop
pw useradd filedrop -d /nonexistent -s /usr/sbin/nologin -c "filedrop"

cat > /usr/local/etc/rc.d/filedrop <<'EOF'
#!/bin/sh

# PROVIDE: filedrop
# REQUIRE: LOGIN NETWORKING
# KEYWORD: shutdown

. /etc/rc.subr

name=filedrop
rcvar=filedrop_enable

load_rc_config $name

: ${filedrop_enable:="NO"}
: ${filedrop_user:="filedrop"}
: ${filedrop_addr:=":8080"}
: ${filedrop_data_dir:="/var/db/filedrop"}
: ${filedrop_env_file:="/usr/local/etc/filedrop.env"}

pidfile="/var/run/${name}/${name}.pid"
child_pidfile="/var/run/${name}/${name}.child.pid"
command="/usr/sbin/daemon"
command_args="-r -S -T ${name} -P ${pidfile} -p ${child_pidfile} /usr/local/bin/filedrop"
start_precmd="${name}_prestart"
extra_commands="reload"
reload_cmd="${name}_reload"

filedrop_prestart()
{
	install -d -o "${filedrop_user}" -m 750 "${filedrop_data_dir}" "/var/run/${name}"
	export ADDR="${filedrop_addr}" DATA_DIR="${filedrop_data_dir}" ENV_FILE="${filedrop_env_file}"
}

# daemon(8) does not forward SIGHUP: signal the server itself
filedrop_reload()
{
	kill -HUP "$(cat "${child_pidfile}")"
}

run_rc_command "$1"
EOF
chmod 555 /usr/local/etc/rc.d/filedrop

sysrc filedrop_enable=YES
service filedrop start
```

`filedrop_addr` and `filedrop_data_dir` can be set in `rc.conf` (e.g. `sysrc filedrop_addr=127.0.0.1:8080`). For any other variable from [Configuration](#configuration), use `/usr/local/etc/filedrop.env` (`KEY=value` per line, values there override the `rc.conf` ones), then `service filedrop reload` (`restart` for `ADDR` or `DATA_DIR`, see [Reloading](#reloading)).

`daemon(8)` restarts the server if it crashes (`-r`) and sends its output to syslog (`-S`, visible in `/var/log/messages`).

## License

[MIT](LICENSE)
