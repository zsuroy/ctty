# Installation Scripts

This directory contains installation scripts for ctty.

## Unix/Linux/macOS Installation

### Quick Install (Recommended)

```bash
curl -sSL https://raw.githubusercontent.com/zsuroy/ctty/master/install/unix.sh | bash
```

**Note:** When using the pipe method, the installer will automatically proceed with installation if ctty is already installed.

## Windows Installation

### Quick Install (Recommended)

```powershell
irm https://raw.githubusercontent.com/zsuroy/ctty/master/install/windows.ps1 | iex
```

### Install Options

**Force install without prompts:**
```powershell
iex "& { $(irm https://raw.githubusercontent.com/zsuroy/ctty/master/install/windows.ps1) } -Force"
```

**Custom installation directory:**
```powershell
iex "& { $(irm https://raw.githubusercontent.com/zsuroy/ctty/master/install/windows.ps1) } -InstallDir 'C:\tools'"
```

## Unix/Linux/macOS Advanced Options

**Force install without prompts:**
```bash
FORCE_INSTALL=true bash -c "$(curl -sSL https://raw.githubusercontent.com/zsuroy/ctty/master/install/unix.sh)"
```

**Disable auto-install when using pipe:**
```bash
FORCE_INSTALL=false bash -c "$(curl -sSL https://raw.githubusercontent.com/zsuroy/ctty/master/install/unix.sh)"
```

### Manual Install

1. Download the script:
```bash
curl -O https://raw.githubusercontent.com/zsuroy/ctty/master/install/unix.sh
```

2. Make it executable:
```bash
chmod +x unix.sh
```

3. Run the installer:
```bash
./unix.sh
```

## What the installer does

1. **Detects your system** - Automatically detects your OS (Linux/macOS) and architecture (AMD64/ARM64)
2. **Fetches latest version** - Gets the latest release from GitHub
3. **Downloads binary** - Downloads the appropriate binary for your system
4. **Installs to /usr/local/bin** - Installs the binary with proper permissions
5. **Verifies installation** - Checks that the installation was successful

## Supported Platforms

- **Linux**: AMD64, ARM64
- **macOS**: AMD64 (Intel), ARM64 (Apple Silicon)

## Requirements

- `curl` - for downloading
- `tar` - for extracting archives
- `sudo` access - for installing to `/usr/local/bin`

## Uninstall

To uninstall ctty:

```bash
sudo rm /usr/local/bin/ctty
```
