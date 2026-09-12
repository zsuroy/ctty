# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.7.1] - 2026-09-12

### Fixed

- **FTP site list panic on narrow terminals** — Switching between 3-column (narrow) and 4-column (wide) layouts on a populated table panicked (`index out of range`) because bubbles `renderRow` indexes columns by row length. Rows are now drained before the column swap. Affected e.g. phones in portrait (`ctty ftp` crashed on launch).

## [0.7.0] - 2026-09-12

### Added

- **FTP support** — Separate FTP transport (github.com/jlaffaye/ftp), not SFTP/SSH:
  - Site inventory in `~/.config/ctty/ftp.json` (0600) with tags; passwords encrypted in the shared `credentials.json` vault under `ftp:` names.
  - Agent CLI: `ctty ftp list|search|info --format json` (non-interactive); `ctty ftp` site manager, `ctty ftp <name>` browser.
  - Human TUI: `ctty ftp` site manager (add/edit/delete/search/info, tags with SSH-style colors); dual-pane local|remote browser (list/chdir/upload/download, refresh status).
  - SFTP-aligned keybindings: `Enter` download confirm, `d` delete, `n` mkdir, `R` rename, `i` file details — in both panes; `v` toggles single/dual layout (persisted, narrow terminals force single).
  - Frame height fits the terminal so the header is never scrolled away; bilingual help and footer.
  - Agent skill updated: agents use JSON CLI only; never open FTP TUI; warn cleartext traffic. FTPS/TLS follow-up.

## [0.6.4] - 2026-09-11

### Added

- **SSH `IdentityAgent` Support** — Full support for `IdentityAgent` directives in `~/.ssh/config` (including global inheritance from `Host *` and per-host declarations). Supports custom UNIX domain socket paths with `~` tilde expansion and quoted path handling (e.g. 1Password SSH Agent at `~/Library/Group Containers/XXXXX.com.1password/t/agent.sock`), `none` to disable the agent, and `SSH_AUTH_SOCK` fallback per OpenSSH specification.

### Fixed

- **SFTP standalone CLI hang (`ctty sftp <host>`)** — Fixed an issue where running `ctty sftp <host>` directly from the command line would hang indefinitely on `"正在建立 SFTP 连接..."`. `Model.Init()` now properly dispatches `sftpForm.Init()` when started in `ViewSFTP` mode, scheduling the asynchronous SSH/SFTP connection immediately (#20).

## [0.6.3] - 2026-09-07

### Fixed

- **Host list border alignment** — Host-list table uses a manual ASCII box + JetBrains gray-circle (U+26AB) padding; search keeps rounded chrome. On JetBrains/JediTerm, only the gray status circle advances as one column; green/red/yellow ping circles keep width 2 so ping results do not overflow; VS Code unchanged. Original status circles kept; no ellipsis from width hacks.

- **SFTP upload local browse (Windows)** — Upload picker starts in the process working directory (where ctty was launched) instead of the user home. At a drive root (`C:\`), ← opens a drive list so other volumes are reachable; it no longer jumps back to the remote browser. Tab/Esc still return to remote.
- **SFTP upload local browse (Unix)** — At filesystem root `/`, ← stays on the local picker instead of switching to remote (Tab/Esc still leave).
- **SFTP connecting / password / error layout** — Those full-page SFTP states use `renderFormPage` so the bordered box tracks terminal width on resize; long error text wraps inside the box.

## [0.6.2] - 2026-09-07

### Added

- **Agent CLI: non-interactive `add` / `edit`** — Skip Bubble Tea forms when `--non-interactive` is set or any host-config flag is provided (`--name`, `--hostname`, `--user`, `--port`, `--identity-file`, `--proxy-jump`, `--proxy-command`, `--option`/`-o`, `--tags`, `--password`, `--force`). Writes OpenSSH config via the existing backup/validation path; optional `--password` goes to the credential vault only; `--format json` emits a success payload for agents.
- **Agent CLI: file transfer** — `ctty put <host> <local> <remote>`, `ctty get <host> <remote> <local>`, and `ctty scp` (host:path notation) using the built-in SFTP client; recursive directories; progress on stderr; non-zero exit on failure.
- **Agent CLI: serial / telnet JSON** — `ctty serial list|search|info` and `ctty telnet list|search|info` with `--format json`. No-args still opens the TUI; `ctty telnet <name>` connect is unchanged.
- **Agent CLI: batch exec** — `ctty exec --tags prod -- uptime` and/or `--hosts a,b` with `--concurrency` (default 8) and `--format json` array of `{host,ok,exit_code,stdout,stderr}`; aggregate exit 0 iff all succeed.

### Fixed

- **SFTP host key verify (post-#11)** — Trusted hosts verify against `known_hosts` read-only; the file is created/appended only for unknown keys (accept-new). Setup no longer fails solely because `~/.ssh` is not writable or `chmod` is denied.
- **SFTP host key algorithms** — Prefer host key types already recorded in `known_hosts` for the dial target and Host alias (OpenSSH-compatible). Avoids false `knownhosts: key mismatch` when the server offers a different algorithm than the one OpenSSH previously trusted (golang/go#29286).
- **SFTP error details** — The SFTP error screen shows the underlying Dial/host-key error text instead of only the generic “failed to start SFTP session” message.

## [0.6.1] - 2026-09-03

### Added

- **Agent Skill (`skills/ctty/`)** — Cursor / Claude Code / Codex agent skill with full non-interactive usage guide, install commands, workflow checklists, and JSON reference. Lets AI agents drive ctty's CLI (search, info, remote exec, import) without launching the TUI.

### Fixed

- **SFTP transfer cancel deadlock** — Pressing ESC during an upload/download now truly cancels the in-flight goroutine via `context.WithCancel`, releasing the SFTP session mutex immediately. Previously the goroutine kept running and held the lock, so subsequent ESC (exit) or any directory operation would deadlock the entire TUI.

## [0.6.0] - 2026-09-02

### Added

- **Full Telnet Support** — Native RFC 854 telnet client for lab equipment, console servers, and legacy network gear (no system `telnet` binary required — macOS ≥ 10.13 ships without one, and Windows/Termux need optional extras):
  - `ctty telnet` opens a dedicated device-manager TUI mirroring the serial workflow: list, connect, add, edit, delete, search (`/`), info (`i`), and reachability probe (`p` — concurrent TCP dial with 3s timeout, 🟢/🔴 indicators). List columns are Name / Host / Port / Tags (host no longer repeats `:port`); help wraps onto two lines; delete confirm stays on the list instead of replacing the whole screen. Add/edit/info form borders track the terminal width; field prompts align by display width (Chinese labels included). Add/edit save from any field with Ctrl+S. Tags use the same per-tag colors as the SSH host list.
  - `ctty telnet <name>` connects to a saved device; `ctty telnet <host[:port]>` dials directly (bracketed IPv6 supported); shell completions offer saved names
  - `T` key from the main SSH host list opens the same manager
  - Conservative negotiation: accepts ECHO / SGA / BINARY, refuses everything else with WONT/DONT; answers NAWS window-size queries with 80×24 (IAC-escaped); swallows subnegotiations; CR-NUL handled per RFC
  - Interactive bridge with raw terminal mode via `golang.org/x/term`; disconnect with `Ctrl+]` or SIGINT/SIGTERM
  - Saved devices in `~/.config/ctty/telnet.json` (0600, atomic temp+rename writes), separate from SSH config
  - Full bilingual i18n (English / 简体中文) for all new UI strings
  - Unit tests: IAC parser state machine (split sequences across chunks, IAC unescaping, subnegotiation framing), negotiation replies, NAWS encoding, `ParseHostPort` edge cases, config CRUD round-trip
- **Shared Raw-Mode Package (`internal/rawterm`)** — Terminal raw-mode enter/restore unified on `golang.org/x/term`, replacing hand-rolled termios on Unix and fixing the previous no-op Windows stub; used by both serial and telnet sessions

## [0.5.3] - 2026-09-02

### Fixed

- **SFTP multi-file transfer** — Sequential queue so a second Enter no longer overlaps the first transfer. Progress shows one file at a time (`[1/2]`, `[2/2]`) instead of flickering names. The SFTP subsystem is reused and serialized; transfer or refresh errors stay in the browser instead of dumping the session.
- **SFTP delete confirm** — The `[Y/n]` prompt closes as soon as you confirm, so it no longer sits under the “Deleted successfully” toast.

## [0.5.2] - 2026-09-02

### Added

- **Host Import** — From Tabby: `ctty import --from tabby` reads Tabby's `config.yaml`, converts `type: ssh` profiles into OpenSSH host blocks, and appends them to `~/.ssh/config.d/tabby.conf` (adding an `Include` to the main SSH config if needed). Existing host names are skipped; Tabby vault passwords are not imported. Use `--dry-run` to preview. Import sources are registered behind a `Source` interface so additional apps can be added without changing the CLI.

## [0.5.1] - 2026-09-02

### Added

- **In-App Self-Update** — Check for and install new releases directly from ctty:
  - `ctty update` checks GitHub for a newer release; `ctty update --yes` downloads and installs it
  - From the TUI, press `U` when an update banner is visible to open the update modal (confirm with `y`)
  - Downloads the raw `ctty-<os>-<arch>` asset from GitHub Releases, sha256-verifies it against the published `checksums.txt`, and atomically replaces the running binary (via [minio/selfupdate](https://github.com/minio/selfupdate))
  - Progress phases (downloading → verifying → applying) rendered live in the TUI; success prompts a restart
  - goreleaser config now publishes raw binaries for darwin/linux/windows × amd64/arm64 alongside the existing archives

## [0.5.0] - 2026-08-18

### Added

- **Remote Command Execution (`x` key)** — Execute commands on remote hosts directly from the TUI without opening an interactive SSH session:
  - Press `x` on a selected host to open the command input form
  - Built-in common command snippets (`docker ps`, `df -h`, `free -m`, `uptime`, `top`, `last`, `ps`, `du`)
  - Custom snippets manageable directly from the TUI (`n` to add, `D` to delete), persisted to `~/.config/ctty/snippets.json` with `0600` permissions
  - User snippets marked with ★ in the list; built-ins cannot be deleted
  - `Tab` to fill input with selected snippet, `↑/↓` to browse, `Enter` to run
  - SSH auto-login (saved passwords) supported via `SSH_ASKPASS` injection
- **SSH First-Connection Host Key Auto-Accept** — Automatically accepts and saves new host keys on first connection (`StrictHostKeyChecking=accept-new`), eliminating the interactive `Are you sure you want to continue connecting (yes/no)?` prompt that blocked TUI connections

## [0.4.3] - 2026-08-16

### Fixed

- **Serial Table Height on Empty Device List** — Fixed table filling the entire terminal window with blank rows when no serial devices are detected (e.g. Termux on Android without serial hardware); table height is now capped to actual content (header + data rows)
- **SSH Host Table Not Expanding** — Fixed SSH host list table staying at content width when the terminal window is widened; table now fills the full available width by distributing extra space proportionally across all visible columns
- **SSH Auto-Login from TUI** — Fixed saved SSH passwords not being used when connecting via the TUI (pressing Enter on a host); the TUI now injects `SSH_ASKPASS` environment variables into the SSH process, matching the CLI `ctty <host>` behavior that already worked correctly

## [0.4.2] - 2026-08-16

### Fixed

- **Narrow Terminal Overflow (SSH / Serial / SFTP)** — Search bars, host tables, and help text no longer overflow the screen width on narrow terminals (e.g. Termux on phones, tmux splits):
  - SSH host list: progressive column hiding on narrow terminals (4 cols → 3 cols hiding Tags → 2 cols hiding Tags + Last Login); fixed `distributeWidths` return-value mapping bug that caused Last Login column to silently get width 0 in 3-column mode
  - Search bars (all views): `textinput.View()` always renders `Width+1` display cols (cursor char); `searchInputWidth` now accounts for this; content truncated via `ansi.Truncate` to prevent CJK placeholder text from pushing borders off-screen
  - Serial table: fixed `getColumns` to account for bubbles table's per-cell `Padding(0,1)` (2 extra cols per cell) so table width matches search bar
  - SFTP table: same per-cell padding fix in `getColumns`
  - Help text: `MaxWidth` truncation to prevent long help strings from inflating `JoinVertical` width and padding all lines
  - Empty hosts message: truncated to table content width to prevent it from inflating the table beyond terminal width

## [0.4.1] - 2026-08-16

### Fixed

- **Empty Host List Navigation Panic** — Fixed panic when pressing arrow keys (↑/↓/←/→) on empty SSH host list by preventing table navigation updates when no hosts are available and ensuring proper table height for empty states

## [0.4.0] - 2026-08-15

### Added

- **SSH Password Storage & Zero-Touch Auto-Login** — Save passwords securely for SSH hosts with automated, hands-free login:
  - Added optional masked `Password` (`••••••`) field in Add & Edit Host forms with instant toggle and validation
  - Local AES-256-GCM encrypted credential vault stored at `~/.config/ctty/credentials.json` with strict `0600` permissions (never pollutes or modifies standard `~/.ssh/config`)
  - Seamless OpenSSH `SSH_ASKPASS` protocol bridge: connects automatically without installing `sshpass` or third-party binaries, fully compatible with macOS, Linux, Windows, and Termux
  - Integrated with SFTP client: hosts with saved passwords automatically connect to SFTP with 0 password prompts
  - Host details modal (`i` key) indicates whether a password is saved (`•••••••• (Saved)`)
- **Responsive Auto-Scrolling Across All Views & Modals** — Completely eliminated height restrictions and overflow clipping across the entire TUI:
  - **Add & Edit Host Forms (`a`/`e`)**: Dynamic focus-following viewport scrolling with fixed headers/footers, removing `Terminal height is too small!` entirely
  - **Help Menu (`h`/`?`)**: Multi-column responsive layout with dedicated page and line scrolling (`↑`/`↓`/`PgUp`/`PgDn`/`j`/`k`)
  - **Host Information Modal (`i`)**: Dynamic viewport scrolling for host parameters with compact padding on small terminals
  - **Port Forwarding Form (`f`)**: Focus-following auto-scroll viewport for Local, Remote, and Dynamic forwarding setup
  - **Config File Selector (`m`)**: Windowed viewport list scrolling when multiple include configs exist
- **Port Forwarding Localization & Enhancements (`f` key)** — Complete bilingual localization for port forwarding setup forms (Local, Remote, Dynamic SOCKS proxy), including input placeholders, descriptions, and localized validation errors
- **Seamless Zero-Host TUI Support** — Removed blocking CLI prompts when `~/.ssh/config` has no hosts; users can directly enter the TUI to use serial management, change settings, or press `a` to add hosts with friendly empty-state guidance
- **Universal Tab Navigation** — `Tab` / `Shift+Tab` key now smoothly switches between Remote and Local file browsers in SFTP mode, toggles between search input and list in Serial mode, and cycles focus across all tables
- **Multi-Line Main Help Bar** — Split bottom help footer into two clean, well-spaced lines displaying all core operations and tools (`a`, `e`, `d`, `s`, `p`, `f`, `i`, `o`, `t`, `S`, `H`, `h`, `q`)

### Fixed

- **Port Forwarding Input Validation** — Added robust validation for local and remote ports in port forwarding forms with localized error messaging
- **CJK Settings Form Alignment** — Fixed visual misalignment in Settings modal under Chinese mode by replacing byte-padding with `ansi.StringWidth` column calculation
- **Search Mode Escape Key** — Pressing `Esc` while searching now properly cancels search and refocuses the table instead of immediately quitting the app
- **Empty Host List Navigation Panic** — Fixed panic when pressing arrow keys (↑/↓/←/→) on empty SSH host list by preventing table navigation updates when no hosts are available and ensuring proper table height for empty states

## [0.3.0] - 2026-08-14

### Added

- **SSH Tag Color Highlighting** — Automatic color-coding for SSH host tags across the TUI host list, host info popup, and `ctty search` CLI output:
  - Built-in semantic color mapping for common tags (`prod`, `staging`, `dev`, `db`, `web`, `api`, `k8s`, `work`, `personal`, `home`, `hidden`, etc.)
  - Deterministic hash-based palette for arbitrary custom tags (same tag consistently gets the same vibrant color)
  - Custom tag color overrides supported via `tag_colors` field in `~/.config/ctty/config.json`
  - Dynamic color profile adaptation (TrueColor ➔ ANSI 256 ➔ ANSI 16 ➔ plain ASCII) for seamless compatibility across macOS, Windows, Linux, and Termux
  - Precise ANSI-aware character width and safe truncation via `x/ansi` to prevent escape sequence corruption and preserve selection backgrounds
- **i18n Internationalization Support** — Full bilingual (English & Simplified Chinese) support across TUI and CLI:
  - Automatic language detection from system environment (`$LC_ALL`, `$LC_MESSAGES`, `$LANG`, `CTTY_LANG`, macOS `AppleLocale`, Windows Win32 API, Android `getprop`)
  - Configurable in `~/.config/ctty/config.json` via `"language": "zh_CN"` (or `"en"`, `"auto"`)
  - Command-line override via `--lang zh` or `--lang en`
  - Complete translations for SSH host list, Serial Device Manager, SFTP file browser, search bars, help menus, status/relative time strings, host details, and delete confirmation dialogs
- **Interactive Settings Modal (`S` key)** — Dedicated settings & preferences screen in TUI to configure Interface Language (Auto / 简体中文 / English), Automatic Update Checking, and ESC Key Behavior, with instant auto-save to `~/.config/ctty/config.json`
- **CLI `ctty sftp <host>` Command** — Direct entrypoint to open the SFTP file browser TUI for a specified host with full shell tab completion
- **Termux (Android) install support** — `install/unix.sh` auto-detects Termux (via `$PREFIX`/`$TERMUX_VERSION`), installs to `$PREFIX/bin`, and skips `sudo`

### Fixed

- **TUI Sort Mode Cycling** — Fixed `s` key cycling to loop through all 4 columns (`Name ↓` ➔ `Hostname ↓` ➔ `Tags ↓` ➔ `Last Login ↓`) instead of only toggling between first and last; added column sort indicators for Hostname and Tags
- **TUI & CLI Search Filtering** — Fixed an issue where viewport height calculation truncated matching search results; improved multi-word search intersection and added `#` prefix support for tag searches

## [0.2.0]  - 2026-08-14

### Added

- **SFTP file transfer support** — full-featured SFTP browser with:
  - Remote and local file browsing
  - Upload/download with progress and cancel
  - **Search functionality (`/` key) for both remote and local files**
  - Two-line help for better readability
  - Clear [LOCAL] and [REMOTE] labels (no confusing emoji)
  - Error handling with friendly messages
  - **Upload progress display (same as download)**
  - **Cancel message shows filename instead of generic "Download cancelled"**
  - **Search input with real-time filtering**
  - **Consistent search behavior across SSH, Serial, and SFTP**

## [0.1.0] - 2026-08-13

### Added

- **Serial connection support** — new `serialconfig` package with device config storage (`~/.config/ctty/serial.json`), port enumeration, and serial connection bridge via `tea.ExecCommand`
- **Serial TUI device list** — auto-detected available ports with saved devices at top; detected ports listed with default settings (115200 8N1)
- **Serial device add form** — name, device path (`←/→` to pick from detected ports), baud rate, data bits, parity, stop bits
- **Serial parameter edit form** — tweak parameters before connecting; baud rate supports direct input or `←/→` to cycle presets (9600/19200/38400/57600/115200/230400/460800/921600)
- **`t` keybind** — open serial device manager from SSH host list
- **Help page updated** — serial shortcuts added to help screen
- **Chinese README** — `README_CN.md` with full translation
