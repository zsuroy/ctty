# ctty CLI reference

ctty is SSH + serial + telnet + SFTP + FTP + WebDAV + S3. SSH `search` / `info` / remote-exec,
`import`, and serial/telnet/ftp/webdav/s3 `list|search|info` are non-interactive CLIs.
Serial/telnet/FTP/WebDAV/S3 inventories are JSON files under the ctty config dir.

## Persistent flags (all commands)

| Flag | Meaning |
|------|---------|
| `--lang en` | English messages (also `zh`, `auto`) |
| `--no-update-check` | Skip GitHub version check |
| `-q, --quiet` | Suppress ctty's own informational output |
| `-c path` | SSH config file instead of `~/.ssh/config` |

Root also has `-t` / `--tty` (force TTY on `ctty <host> <cmd>`) and
`-s` / `--search` (TUI — do not use).

## `ctty search`

```bash
ctty search [query] [--format json|simple|table] [--tags] [--names]
```

JSON is an array of objects:

```json
[
  {
    "name": "prod-web",
    "hostname": "10.0.0.10",
    "user": "deploy",
    "port": "2222",
    "identity": "~/.ssh/id_prod",
    "proxy_jump": "bastion",
    "proxy_command": "",
    "options": "",
    "tags": ["prod", "web"]
  }
]
```

`port` is a **string** here (may be empty). Empty strings mean unset.

`--tags` searches tags only; `--names` searches Host aliases only.
Default searches name + hostname + tags. Query words are AND.
Case-insensitive. `#tag` and `tag` both match.

Hidden hosts are filtered out before search.

## `ctty info <alias>`

One JSON object, schema `ctty.info.v1`.

Success (`ok: true`, exit 0):

```json
{
  "schema": "ctty.info.v1",
  "ok": true,
  "hostname": "prod-web",
  "result": {
    "canonical_name": "prod-web",
    "target": {
      "host": "prod-web",
      "hostname": "10.0.0.10",
      "user": "deploy",
      "port": 2222
    },
    "identity_file": "~/.ssh/id_prod",
    "proxy_jump": "bastion",
    "proxy_command": null,
    "options": "ServerAliveInterval 60",
    "tags": ["prod", "web"],
    "remote_command": null,
    "request_tty": null,
    "source": { "file": "/Users/you/.ssh/config", "line": 12 }
  },
  "error": null
}
```

`target.port` is a **number** or omitted. Optional strings are `null` when unset.

Not found (`ok: false`, exit 2):

```json
{
  "schema": "ctty.info.v1",
  "ok": false,
  "hostname": "missing",
  "result": null,
  "error": { "code": "NOT_FOUND", "message": "...", "details": null }
}
```

`error.code`: `NOT_FOUND` | `CONFIG_ERROR`.

`info` **does** resolve hidden hosts. Use it when search omitted an alias
the user named explicitly.

## `ctty ping`

Check network connectivity and SSH port reachability.

```bash
ctty ping [hosts...] [--all] [--tags tag1,tag2] [--format table|json] [--json] [--timeout 5s] [--concurrency 16]
```

JSON is an array of objects:

```json
[
  {
    "host": "prod-web",
    "ok": true,
    "status": "online",
    "latency_ms": 24.5,
    "error": ""
  },
  {
    "host": "dev-offline",
    "ok": false,
    "status": "offline",
    "latency_ms": 0,
    "error": "connection refused"
  }
]
```

Exit code: 0 if all pinged hosts are online; 1 if any host is offline or not found.

## `ctty peek`

Inspect real-time system metrics (uptime, CPU load averages, memory %, root disk %) without opening an interactive shell.

```bash
ctty peek [hosts...] [--all] [--tags tag1,tag2] [--format table|json] [--json] [--timeout 7s] [--concurrency 8]
```

JSON is an array of objects:

```json
[
  {
    "host": "prod-web",
    "ok": true,
    "stats": {
      "uptime": "15 days, 4:20",
      "users": "2",
      "load1": "0.15",
      "load5": "0.20",
      "load15": "0.18",
      "mem_total_mb": 8192,
      "mem_used_mb": 4096,
      "mem_percent": 50.0,
      "disk_total": "100G",
      "disk_used": "25G",
      "disk_avail": "75G",
      "disk_percent": 25.0
    },
    "error": ""
  }
]
```

Exit code: 0 if all hosts probed successfully; 1 if any host failed or was not found.

## Remote exec

```
ctty [global flags] [-t] <alias> [--] <remote command...>
```

Uses `ssh` under the hood with the user's config (`-F` if `-c` was passed).
If a password is stored in ctty's vault, ASKPASS is injected automatically.

Exit code = remote process exit code (or 1 if the alias does not exist).

Unknown Cobra "commands" are treated as host aliases (`ctty mybox` tries to
SSH to `mybox`). That is why a bare alias is interactive — never run it
unattended.

## Serial store (`serial.json`)

```json
{
  "devices": [
    {
      "name": "Switch-Console",
      "device": "/dev/cu.usbserial-1420",
      "baud_rate": 115200,
      "data_bits": 8,
      "parity": "none",
      "stop_bits": 1,
      "flow_control": "none"
    }
  ]
}
```

No CLI to list or connect by name. Missing file = empty list. Newly plugged
ports that are not saved only show up in `ctty serial`.

## Telnet store (`telnet.json`)

```json
{
  "hosts": [
    {
      "name": "core-sw",
      "host": "192.168.1.1",
      "port": 23,
      "tags": ["lab"]
    }
  ]
}
```

`ctty telnet <name>` matches `name` first, else parses `host[:port]`
(default 23; bracketed IPv6 ok). Still an interactive bridge — do not run it
unattended. Missing file = empty list.

## Config files (read, don't rummage secrets)

| Path | Role |
|------|------|
| `~/.ssh/config` and `Include` | SSH Host aliases |
| `~/.config/ctty/config.json` | language, updates, keybindings, tag colors |
| `~/.config/ctty/telnet.json` | saved telnet devices |
| `~/.config/ctty/serial.json` | saved serial devices |
| `~/.config/ctty/snippets.json` | TUI remote-exec snippets (not used by CLI exec) |
| `~/.config/ctty/credentials.json` | **encrypted SSH passwords — do not read or copy** |
| `~/.config/ctty/backups/` | last SSH config backup after a ctty mutation |

Windows: `%APPDATA%\ctty\` instead of `~/.config/ctty/`.

### SSH Host block (for adding a host without TUI)

Write above the `Host` line. Tags are comments, not SSH keywords.
The special tag `hidden` hides the host from TUI/search.

```ssh
# Tags: prod, web
Host prod-web
    HostName 10.0.0.10
    User deploy
    Port 22
    IdentityFile ~/.ssh/id_prod
    ProxyJump bastion
```

Prefer appending to an `Include`d file (e.g. `~/.ssh/config.d/`) if one
exists, so the main config stays small. Do not store passwords here.

ctty backups the file it rewrites; a manual edit does not. Copy first if
you are changing a live config.

## Import / update (rarely for agents)

```bash
ctty import tabby --dry-run
ctty import --from tabby
ctty update          # check only
```

`import` writes `~/.ssh/config.d/tabby.conf` and may add an `Include`.
Always `--dry-run` first. Do not run `ctty update --yes` unless asked.

## Pitfalls

- Search JSON `port` is a string; info JSON `target.port` is a number.
- Table search output is localized and colorized — not for scripts.
- `ctty search` with zero config hosts exits 1; zero *matches* exits 0.
- Alias `info` / `search` / `add` / `serial` / `telnet` / … cannot be used as
  `ctty <host>`.
- First SSH to a new host from TUI uses `StrictHostKeyChecking=accept-new`.
  CLI `ctty <host> cmd` uses stock OpenSSH host-key behavior — a new host
  may prompt; in a non-TTY that fails. Don't loop on that; report it.
- `ctty telnet <arg>` is never a one-shot probe; it attaches a raw terminal.
- Serial has no named CLI connect; `ctty serial` is always the manager TUI.


## `ctty ftp list|search|info`

```bash
ctty ftp list --format json
ctty ftp search lab --format json
ctty ftp info lab-nas --format json
```

JSON array / object of `{name,host,port,user,tags}`. Site file:
`~/.config/ctty/ftp.json` (0600). Passwords encrypted in SSH
`credentials.json` vault under `ftp:` names.
Agents must not open `ctty ftp` / `ctty ftp <name>` TUI.

## `ctty webdav list|search|info`

```bash
ctty webdav list --format json
ctty webdav search cloud --format json
ctty webdav info nextcloud --format json
```

JSON array / object of `{name,url,user,tags}`. Site file:
`~/.config/ctty/webdav.json` (0600). Passwords encrypted in SSH
`credentials.json` vault under `webdav:` names.
Headless transfers mirror FTP: `webdav ls/get/put/mkdir/rm/rmdir/rename`
(dirs recursive, progress on stderr).
Agents must not open `ctty webdav` / `ctty webdav <name>` TUI.
Auth is Basic/Digest only — `NoAuthenticator ... 401` means the server
demands NTLM/Negotiate; report it, do not retry.

## `ctty s3 list|search|info`

```bash
ctty s3 list --format json
ctty s3 search backups --format json
ctty s3 info minio-local --format json
```

JSON array / object of `{name,endpoint,bucket,region,access_key,use_ssl,path_style,insecure_tls,tags}`. Site file:
`~/.config/ctty/s3.json` (0600). Secret access keys encrypted in SSH
`credentials.json` vault under `s3:` names.
Headless object operations: `s3 ls/get/put/mkdir/rm/rename`
(dirs recursive, progress on stderr).
Agents must not open `ctty s3` / `ctty s3 <name>` TUI.
Supports AWS S3, MinIO, Cloudflare R2, Aliyun OSS, Ceph, and S3-compatible endpoints.

## `ctty backup` and `ctty restore`

Backup and restore all ctty configurations, devices, sites, and credentials.

```bash
# Create unencrypted backup archive (.tar.gz)
ctty backup -o backup.tar.gz --format json

# Create encrypted backup (.ctty) with AES-256-GCM
ctty backup -p "passphrase" -o backup.ctty --format json

# Restore archive (--dry-run preview, --overwrite to replace existing files)
ctty restore backup.tar.gz --dry-run --format json
ctty restore backup.ctty -p "passphrase" --overwrite --format json
```

## `ctty export`

Export connection profiles to JSON or OpenSSH config format.

```bash
ctty export [--format json|ssh] [--tags tag1,tag2]
```

JSON export bundle contains `{schema, exported_at, hosts, ftp_sites, webdav_sites, serial_devices, telnet_hosts}`.
`--format ssh` outputs standard OpenSSH configuration blocks to stdout.

## Generic JSON import (`ctty import json`)

Import SSH profiles from any generic JSON array or wrapped object:

```bash
ctty import json -f /path/to/hosts.json --dry-run
```

Accepted structure (array or wrapped under `hosts` / `servers` / `nodes`):

```json
[
  {
    "name": "web-01",
    "hostname": "10.0.0.1",
    "user": "ubuntu",
    "port": 22,
    "identity_file": "~/.ssh/id_rsa",
    "proxy_jump": "bastion",
    "tags": ["prod", "web"]
  }
]
```

Field alias support:
- Name: `name`, `label`, `alias`, `title`, `id`
- Hostname: `hostname`, `host`, `ip`, `address`, `server`
- User: `user`, `username`, `user_name`, `login`
- Port: `port` (number or string, default 22)
- Identity: `identity`, `identity_file`, `key_path`, `private_key`
- ProxyJump: `proxy_jump`, `jump_host`, `bastion`
- Tags: `tags`, `tag`, `group`, `groups` (array or comma-separated string)
