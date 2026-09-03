# ctty CLI reference

ctty is SSH + serial + telnet + SFTP. Only SSH `search` / `info` / remote-exec
and `import` are non-interactive CLIs. Serial/telnet inventories are JSON files.

## Persistent flags (all commands)

| Flag | Meaning |
|------|---------|
| `--lang en` | English messages (also `zh`, `auto`) |
| `--no-update-check` | Skip GitHub version check |
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
