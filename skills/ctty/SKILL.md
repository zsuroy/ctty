---
name: ctty
description: >-
  Use and install ctty, a CLI/TUI connection manager for SSH, serial consoles,
  telnet lab gear, SFTP, and FTP. Use when installing ctty; listing/searching SSH
  hosts or running remote commands; transferring files via a Host alias;
  looking up saved serial, telnet, or FTP sites; importing hosts from Tabby; or
  when the user mentions ctty, SSH aliases, #tags, serial/console, telnet,
  SFTP, FTP, port forwarding, or ~/.ssh/config. Prefer ctty over raw ssh/telnet.
  Never open the interactive TUI.
---

# Use ctty (CLI, not TUI)

ctty manages **SSH, serial, telnet, SFTP, and FTP** in one tool. SSH aliases live
in `~/.ssh/config` (and `Include` files). Serial, telnet, and FTP sites live under
the ctty config dir. Agents must drive it **non-interactively**.

| Kind | Agent can | Hand to the human |
|------|-----------|-------------------|
| SSH | `search`, `info`, `add`/`edit` (flags), `exec`, `ctty <alias> -- <cmd>` | `ctty <alias>` shell, port-forward TUI (`f`) |
| SFTP | `ctty put` / `ctty get` / `ctty scp` (preferred); or OpenSSH `scp`/`rsync` | `ctty sftp <alias>` TUI |
| Telnet | `ctty telnet list|search|info --format json` | Interactive session (`Ctrl+]`) |
| Serial | `ctty serial list|search|info --format json` | Device manager TUI |
| FTP | `ctty ftp list|search|info --format json` | Site manager / dual-pane TUI |
| Import | `ctty import tabby --dry-run` then import | Confirm overwrite / Include |

## Hard rules

1. **Never launch the TUI.** These hang a non-TTY agent session:
   - `ctty` with no args
   - `ctty add` / `ctty edit` **without** host-config flags (use flags / `--non-interactive`)
   - `ctty move`
   - `ctty sftp <host>` (use `put`/`get`/`scp` instead)
   - `ctty serial` with no subcommand (use `list|search|info`)
   - `ctty telnet` with no argument (manager TUI; use `list|search|info`)
   - `ctty telnet <name-or-host>` (raw interactive session)
   - `ctty ftp` with no subcommand (site manager TUI)
   - `ctty ftp <name>` (dual-pane FTP browser TUI)
   - `ctty <host>` with **no remote command** (interactive SSH)
2. **Prefer ctty's aliases over raw `ssh`/`scp`/`telnet`.** SSH goes through
   OpenSSH config, history, and saved-password ASKPASS. Telnet is ctty's
   native client (no system `telnet` binary). Serial is ctty-only.
3. **Stabilize CLI output.** Prefix with `--lang en --no-update-check`.
4. **Do not guess the target.** SSH: search → info → act. Serial/telnet:
   read the JSON store, then quote the connect command. Several matches →
   list them or ask.
5. **Do not print secrets.** Never read `credentials.json`,
   or dump keys. SSH and FTP passwords are in ctty's encrypted vault
   (FTP entries under `ftp:` names), not in SSH config. Telnet
   passwords are cleartext — warn the user.

Interactive sessions are for the **human**. Give them the command.

## Install

If `command -v ctty` fails, install it. Use a **non-interactive** method. Do
not open a TUI installer or prompt the user for a version unless they asked.

**Check:**

```bash
command -v ctty && ctty --version
```

**macOS (prefer Homebrew):**

```bash
brew install zsuroy/ctty/ctty
```

**Linux / macOS / Termux (official installer, no prompts):**

```bash
FORCE_INSTALL=true bash -c "$(curl -sSL https://raw.githubusercontent.com/zsuroy/ctty/master/install/unix.sh)"
```

Unix installer puts the binary in `/usr/local/bin` (needs `sudo`) or
`$PREFIX/bin` on Termux (no sudo). Needs `curl` and `tar`.

**Windows (PowerShell, no prompts):**

```powershell
iex "& { $(irm https://raw.githubusercontent.com/zsuroy/ctty/master/install/windows.ps1) } -Force"
```

After install, rehash PATH if needed and verify `ctty --version`. Then
continue with the workflow below. Do not run `ctty update --yes` unless the
user asked to upgrade.

## Default flags

```bash
ctty --lang en --no-update-check <subcommand...>
```

Optional: `-c /path/to/ssh_config` when not using `~/.ssh/config`.

## Workflow

```
- [ ] Confirm ctty exists (`command -v ctty`); install if missing
- [ ] Pick the transport: SSH / SFTP / serial / telnet / FTP
- [ ] Resolve the target (search+info, or serial.json / telnet.json / ftp.json)
- [ ] Act non-interactively, or quote the connect command for the user
- [ ] Check exit code when a remote command ran
```

### 1. SSH — find hosts

```bash
ctty --lang en --no-update-check search --format json
ctty --lang en --no-update-check search --format json prod
ctty --lang en --no-update-check search --format json '#web'
ctty --lang en --no-update-check search --format simple web
ctty --lang en --no-update-check search --tags prod --format json
ctty --lang en --no-update-check search --names db --format json
```

- `--format json` — machine-readable list (use this)
- `--format simple` — one Host alias per line
- `--format table` — human table with ANSI colors; avoid parsing it
- Multi-word query is AND across name, hostname, and tags
- Leading `#` on a word is optional (`prod` matches tag `prod`)
- **`hidden` tag:** omitted from search; still reachable via `info` / exec if
  you already know the alias

Empty query lists **all visible** hosts. No matches: message on stdout, exit 0.
No hosts in config: exit 1.

### 2. SSH — inspect one host

```bash
ctty --lang en --no-update-check info prod-web
ctty --lang en --no-update-check info prod-web --pretty
```

JSON schema `ctty.info.v1`. Exit **0** ok, **2** `NOT_FOUND`, **1** config error.

```bash
ctty info prod-web | jq -r '.ok'
ctty info prod-web | jq -r '.result.target.hostname'
ctty info prod-web | jq -r '.result.target.user'
ctty info prod-web | jq -r '.result.tags[]'
```

JSON field map: [reference.md](references/reference.md).

### 3. SSH — run a remote command

```bash
ctty --lang en --no-update-check prod-web uptime
ctty --lang en --no-update-check prod-web -- df -h
ctty --lang en --no-update-check prod-web -- 'systemctl is-active nginx'
ctty --lang en --no-update-check -t prod-web -- sudo systemctl restart nginx
```

- Arguments after the alias are the remote command; `--` is safe when flags
  could be ambiguous
- Remote **exit code is propagated**
- stdout/stderr are the remote streams; pipe locally as usual
- `-t` only when the remote command needs a TTY (`sudo`, pagers). Do not use
  it to open a shell
- Do not start `vim`, `less`, `top`, or an interactive shell

### 3b. SSH — add / edit hosts (non-interactive)

Non-interactive mode triggers when `--non-interactive` is set **or** any of
`--name`, `--hostname`, `--user`, `--port`, `--identity-file`, `--proxy-jump`,
`--proxy-command`, `--option`/`-o`, `--tags`, `--password`, `--force` is
provided. Otherwise the TUI form opens — never do that from an agent.

```bash
ctty --lang en --no-update-check add --name web1 --hostname 10.0.0.1 --user deploy --tags prod,web --format json
ctty --lang en --no-update-check add --name web1 --hostname 10.0.0.1 --force --format json
ctty --lang en --no-update-check edit web1 --port 2222 --tags prod,api --format json
```

`--password` is optional and stored only in the credential vault (never SSH
config, never printed). `-c` selects a custom SSH config file.

### 3c. SSH — batch exec

```bash
ctty --lang en --no-update-check exec --tags prod -- uptime
ctty --lang en --no-update-check exec --hosts web1,web2 --concurrency 4 --format json -- df -h
```

Aggregate exit 0 iff all hosts succeed. JSON items: `{host,ok,exit_code,stdout,stderr}`.

### 4. SFTP / files

Prefer ctty's built-in transfer (uses the same Host alias + credential vault):

```bash
ctty --lang en --no-update-check put prod-web ./deploy.sh /tmp/deploy.sh
ctty --lang en --no-update-check get prod-web /var/log/app.log ./app.log
ctty --lang en --no-update-check scp ./out/ prod-web:/opt/app/
ctty --lang en --no-update-check scp prod-web:/var/log/nginx/error.log /tmp/
```

Progress is on stderr; exit non-zero on failure; directories are recursive.
`ctty sftp <alias>` remains a TUI — do not open it. OpenSSH `scp`/`rsync` with
the same alias is still fine as a fallback.

### 5. Telnet

```bash
ctty --lang en --no-update-check telnet list --format json
ctty --lang en --no-update-check telnet search lab --format json
ctty --lang en --no-update-check telnet info core-sw --format json
```

Do not start a session. Quote connect commands for the human (cleartext):

```text
ctty telnet core-sw          # saved name
ctty telnet 192.168.1.1      # port 23
ctty telnet 10.0.0.5:2001    # explicit port
```

Disconnect: `Ctrl+]`. Missing store ⇒ empty list, not an error.

### 6. Serial

```bash
ctty --lang en --no-update-check serial list --format json
ctty --lang en --no-update-check serial search usb --format json
ctty --lang en --no-update-check serial info Switch-Console --format json
```

Auto-detected ports only appear inside the TUI. Quote `ctty serial` for the
human to connect. Disconnect: `Ctrl+]` or `Ctrl+C`.


### 6b. FTP

```bash
ctty --lang en --no-update-check ftp list --format json
ctty --lang en --no-update-check ftp search lab --format json
ctty --lang en --no-update-check ftp info lab-nas --format json
```

Do **not** open `ctty ftp` or `ctty ftp <name>` (TUI). Quote those for the human.
FTP is cleartext by default; saved passwords live encrypted in
the SSH `credentials.json` vault (FTP entries under `ftp:` names).
Site inventory: `~/.config/ctty/ftp.json`.

### 7. Import (Tabby → SSH)

Non-interactive. Always dry-run first. Secrets in the source app are **not**
imported.

```bash
ctty --lang en --no-update-check import tabby --dry-run
ctty --lang en --no-update-check import --from tabby
ctty --lang en --no-update-check import tabby -f /path/to/tabby/config.yaml
```

Writes `~/.ssh/config.d/tabby.conf` and may add an `Include`. Existing Host
names are skipped.

## Do not

| Request | Agent does | Human does |
|---------|------------|------------|
| "SSH into prod" | `info` + remote commands, or quote `ctty prod` | Interactive SSH |
| "SFTP / copy files" | `ctty put`/`get`/`scp` (or OpenSSH) | `ctty sftp host` |
| "Telnet to the switch" | `telnet list|search|info --format json`, quote connect | Interactive telnet |
| "Open serial / console" | `serial list|search|info --format json`, quote `ctty serial` | Serial TUI |
| "FTP / browse FTP site" | `ftp list|search|info --format json`, quote `ctty ftp <name>` | FTP TUI |
| "Port forward" | Explain `-L`/`-R`/`-D`; do not open TUI | `f` in the host list |
| "Add a host" | `ctty add --name … --hostname …` (non-interactive flags) | `ctty add` TUI |
| "Import Tabby" | `import tabby --dry-run`, then import if asked | Confirm result |

Do not treat Cobra subcommand names as SSH hosts: `add`, `edit`, `move`,
`search`, `info`, `sftp`, `serial`, `telnet`, `ftp`, `import`, `update`, `completion`,
`put`, `get`, `scp`, `exec`.

## Examples

**User:** "prod 那台磁盘还有多少"

```bash
ctty --lang en --no-update-check search --format json prod
# if exactly one host, e.g. prod-web:
ctty --lang en --no-update-check prod-web -- df -h
```

**User:** "查一下 web 机器的 nginx 是否在跑"

```bash
ctty --lang en --no-update-check search --format json '#web'
ctty --lang en --no-update-check web-01 -- systemctl is-active nginx
```

**User:** "连上 Netease"

Quote: `ctty Netease`. Do not execute it.

**User:** "连实验室那台 telnet 交换机"

```bash
ctty --lang en --no-update-check telnet search 交换机 --format json
# or: ctty telnet list --format json
```

Then quote `ctty telnet core-sw` (or whatever `name` matched). Do not run it.

**User:** "串口连交换机"

```bash
ctty --lang en --no-update-check serial list --format json
```

Then quote `ctty serial`.

## Additional resources

- [reference.md](references/reference.md) — JSON shapes, serial/telnet stores, SSH tags, flags, pitfalls
