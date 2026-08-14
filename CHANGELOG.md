# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.4.0] - 2026-08-15

### Added

- **SSH Password Storage & Zero-Touch Auto-Login** — Save passwords securely for SSH hosts with automated, hands-free login:
  - Added optional masked `Password` (`••••••`) field in Add & Edit Host forms with instant toggle and validation
  - Local AES-256-GCM encrypted credential vault stored at `~/.config/ctty/credentials.json` with strict `0600` permissions (never pollutes or modifies standard `~/.ssh/config`)
  - Seamless OpenSSH `SSH_ASKPASS` protocol bridge: connects automatically without installing `sshpass` or third-party binaries, fully compatible with macOS, Linux, Windows, and Termux
  - Integrated with SFTP client: hosts with saved passwords automatically connect to SFTP with 0 password prompts
  - Host details modal (`i` key) indicates whether a password is saved (`•••••••• (Saved)`)
- **Port Forwarding Localization & Enhancements (`f` key)** — Complete bilingual localization for port forwarding setup forms (Local, Remote, Dynamic SOCKS proxy), including input placeholders, descriptions, and localized validation errors
- **Seamless Zero-Host TUI Support** — Removed blocking CLI prompts when `~/.ssh/config` has no hosts; users can directly enter the TUI to use serial management, change settings, or press `a` to add hosts with friendly empty-state guidance
- **Universal Tab Navigation** — `Tab` / `Shift+Tab` key now smoothly switches between Remote and Local file browsers in SFTP mode, toggles between search input and list in Serial mode, and cycles focus across all tables
- **Multi-Line Main Help Bar** — Split bottom help footer into two clean, well-spaced lines displaying all core operations and tools (`a`, `e`, `d`, `s`, `p`, `f`, `i`, `o`, `t`, `S`, `H`, `h`, `q`)

### Fixed

- **Port Forwarding Input Validation** — Added robust validation for local and remote ports in port forwarding forms with localized error messaging
- **CJK Settings Form Alignment** — Fixed visual misalignment in Settings modal under Chinese mode by replacing byte-padding with `ansi.StringWidth` column calculation
- **Search Mode Escape Key** — Pressing `Esc` while searching now properly cancels search and refocuses the table instead of immediately quitting the app

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