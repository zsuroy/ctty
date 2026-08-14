# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- **SSH Tag Color Highlighting** — Automatic color-coding for SSH host tags across the TUI host list, host info popup, and `ctty search` CLI output:
  - Built-in semantic color mapping for common tags (`prod`, `staging`, `dev`, `db`, `web`, `api`, `k8s`, `work`, `personal`, `home`, `hidden`, etc.)
  - Deterministic hash-based palette for arbitrary custom tags (same tag consistently gets the same vibrant color)
  - Custom tag color overrides supported via `tag_colors` field in `~/.ctty/config.json`
  - Dynamic color profile adaptation (TrueColor ➔ ANSI 256 ➔ ANSI 16 ➔ plain ASCII) for seamless compatibility across macOS, Windows, Linux, and Termux
  - Precise ANSI-aware character width and safe truncation via `x/ansi` to prevent escape sequence corruption and preserve selection backgrounds
- **i18n Internationalization Support** — Full bilingual (English & Simplified Chinese) support across TUI and CLI:
  - Automatic language detection from system environment (`$LC_ALL`, `$LC_MESSAGES`, `$LANG`, `CTTY_LANG`, macOS `AppleLocale`, Windows Win32 API, Android `getprop`)
  - Configurable in `~/.ctty/config.json` via `"language": "zh_CN"` (or `"en"`, `"auto"`)
  - Command-line override via `--lang zh` or `--lang en`
  - Complete translations for SSH host list, Serial Device Manager, SFTP file browser, search bars, help menus, status/relative time strings, host details, and delete confirmation dialogs
- **Interactive Settings Modal (`S` key)** — Dedicated settings & preferences screen in TUI to configure Interface Language (Auto / 简体中文 / English), Automatic Update Checking, and ESC Key Behavior, with instant auto-save to `~/.ctty/config.json`
- **CLI `ctty sftp <host>` Command** — Direct entrypoint to open the SFTP file browser TUI for a specified host with full shell tab completion
- **Termux (Android) install support** — `install/unix.sh` auto-detects Termux (via `$PREFIX`/`$TERMUX_VERSION`), installs to `$PREFIX/bin`, and skips `sudo`

### Fixed

- **TUI Sort Mode Cycling** — Fixed `s` key cycling to loop through all 4 columns (`Name ↓` ➔ `Hostname ↓` ➔ `Tags ↓` ➔ `Last Login ↓`) instead of only toggling between first and last; added column sort indicators for Hostname and Tags
- **TUI & CLI Search Filtering** — Fixed an issue where viewport height calculation truncated matching search results; improved multi-word search intersection and added `#` prefix support for tag searches

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