# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [0.2.0] - 2026-08-14

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