#!/bin/bash

INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
EXECUTABLE_NAME=ctty
EXECUTABLE_PATH="$INSTALL_DIR/$EXECUTABLE_NAME"
USE_SUDO="false"
OS=""
ARCH=""
FORCE_INSTALL="${FORCE_INSTALL:-false}"
CTTY_VERSION="${CTTY_VERSION:-latest}"
TEMP_DIR=""
DOWNLOADED_BINARY=""

RED='\033[0;31m'
PURPLE='\033[0;35m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# detect Termux： make sure $PREFIX exist com.termux or $TERMUX_VERSION
if { [ -n "${PREFIX:-}" ] && [ -d "/data/data/com.termux" ]; } || [ -n "${TERMUX_VERSION:-}" ]; then
    IS_TERMUX="true"
    INSTALL_DIR="${PREFIX:-/data/data/com.termux/files/usr}/bin"
    EXECUTABLE_PATH="$INSTALL_DIR/$EXECUTABLE_NAME"
else
    IS_TERMUX="false"
fi

usage() {
    printf "${PURPLE}ctty Installation Script${NC}\n\n"
    printf "Usage:\n"
    printf "  Default (latest stable):     ${GREEN}bash install.sh${NC}\n"
    printf "  Specific version:            ${GREEN}CTTY_VERSION=v0.1.0 bash install.sh${NC}\n"
    printf "  Beta/pre-release:            ${GREEN}CTTY_VERSION=v0.1.0-beta bash install.sh${NC}\n"
    printf "  Force install:               ${GREEN}FORCE_INSTALL=true bash install.sh${NC}\n"
    printf "  Custom install directory:    ${GREEN}INSTALL_DIR=/opt/bin bash install.sh${NC}\n\n"
    printf "Environment variables:\n"
    printf "  CTTY_VERSION    - Version to install (default: latest)\n"
    printf "  FORCE_INSTALL   - Skip confirmation prompts (default: false)\n"
    printf "  INSTALL_DIR     - Installation directory (default: /usr/local/bin)\n\n"
}

setSystem() {
    ARCH=$(uname -m)
    case $ARCH in
        i386|i686) ARCH="amd64" ;;
        x86_64) ARCH="amd64";;
        armv6*) ARCH="armv6" ;;
        armv7*) ARCH="armv7" ;;
        aarch64*) ARCH="arm64" ;;
        arm64) ARCH="arm64" ;;
    esac

    OS=$(echo `uname`|tr '[:upper:]' '[:lower:]')
    
    # Determine if we need sudo
    if [ "$IS_TERMUX" = "true" ]; then
        USE_SUDO="false"
    elif [ "$OS" = "linux" ]; then
        USE_SUDO="true"
    elif [ "$OS" = "darwin" ]; then
        USE_SUDO="true"
    fi
}

runAsRoot() {
    if [ "$USE_SUDO" = "true" ]; then
        printf "${PURPLE}We need sudo access to install ctty to $INSTALL_DIR ${NC}\n"
        sudo "$@"
    else
        "$@"
    fi
}

sha256File() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | awk '{print $1}'
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | awk '{print $1}'
    elif command -v openssl >/dev/null 2>&1; then
        openssl dgst -sha256 "$1" | awk '{print $NF}'
    else
        printf "${RED}No SHA-256 tool found (sha256sum, shasum, or openssl required)${NC}\n" >&2
        return 1
    fi
}

verifyChecksum() {
    local ARCHIVE_PATH="$1"
    local CHECKSUMS_PATH="$2"
    local ASSET_NAME="$3"
    local EXPECTED
    local ACTUAL

    if ! EXPECTED=$(awk -v asset="$ASSET_NAME" '
        $2 == asset {
            if (found) {
                exit 2
            }
            expected = $1
            found = 1
        }
        END {
            if (!found) {
                exit 1
            }
            print expected
        }
    ' "$CHECKSUMS_PATH"); then
        printf "${RED}No unique checksum found for $ASSET_NAME${NC}\n" >&2
        return 1
    fi
    if ! printf '%s' "$EXPECTED" | grep -Eq '^[[:xdigit:]]{64}$'; then
        printf "${RED}No valid SHA-256 checksum found for $ASSET_NAME${NC}\n" >&2
        return 1
    fi
    ACTUAL=$(sha256File "$ARCHIVE_PATH") || return 1
    EXPECTED=$(printf '%s' "$EXPECTED" | tr '[:upper:]' '[:lower:]')
    ACTUAL=$(printf '%s' "$ACTUAL" | tr '[:upper:]' '[:lower:]')
    if [ "$ACTUAL" != "$EXPECTED" ]; then
        printf "${RED}Checksum verification failed for $ASSET_NAME${NC}\n" >&2
        return 1
    fi
    printf "${GREEN}Checksum verified.${NC}\n"
}

getLatestVersion() {
    if [ "$CTTY_VERSION" = "latest" ]; then
        printf "${YELLOW}Fetching latest stable version...${NC}\n"
        LATEST_VERSION=$(curl -s https://api.github.com/repos/zsuroy/ctty/releases/latest | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
        if [ -z "$LATEST_VERSION" ]; then
            printf "${RED}Failed to fetch latest version${NC}\n"
            exit 1
        fi
    else
        printf "${YELLOW}Using specified version: $CTTY_VERSION${NC}\n"
        # Validate that the specified version exists
        RELEASE_CHECK=$(curl -s "https://api.github.com/repos/zsuroy/ctty/releases/tags/$CTTY_VERSION" | grep '"tag_name":')
        if [ -z "$RELEASE_CHECK" ]; then
            printf "${RED}Version $CTTY_VERSION not found. Available versions:${NC}\n"
            curl -s https://api.github.com/repos/zsuroy/ctty/releases | grep '"tag_name":' | head -10 | sed -E 's/.*"([^"]+)".*/  - \1/'
            exit 1
        fi
        LATEST_VERSION="$CTTY_VERSION"
    fi
    printf "${GREEN}Installing version: $LATEST_VERSION${NC}\n"
}

downloadBinary() {
    # Map OS names to match GoReleaser format
    local GORELEASER_OS="$OS"
    case $OS in
        "darwin") GORELEASER_OS="Darwin" ;;
        "linux") GORELEASER_OS="Linux" ;;
        "windows") GORELEASER_OS="Windows" ;;
    esac
    
    # Map architecture names to match GoReleaser format  
    local GORELEASER_ARCH="$ARCH"
    case $ARCH in
        "amd64") GORELEASER_ARCH="x86_64" ;;
        "arm64") GORELEASER_ARCH="arm64" ;;
        "386") GORELEASER_ARCH="i386" ;;
        "armv6") GORELEASER_ARCH="armv6" ;;
        "armv7") GORELEASER_ARCH="armv7" ;;
    esac
    
    # GoReleaser format: ctty_Linux_armv7.tar.gz
    GITHUB_FILE="ctty_${GORELEASER_OS}_${GORELEASER_ARCH}.tar.gz"
    GITHUB_URL="https://github.com/zsuroy/ctty/releases/download/$LATEST_VERSION/$GITHUB_FILE"
    CHECKSUMS_URL="https://github.com/zsuroy/ctty/releases/download/$LATEST_VERSION/checksums.txt"
    ARCHIVE_PATH="$TEMP_DIR/$GITHUB_FILE"
    CHECKSUMS_PATH="$TEMP_DIR/checksums.txt"
    
    printf "${YELLOW}Downloading $GITHUB_FILE...${NC}\n"
    if ! curl --fail --location --proto '=https' --tlsv1.2 "$GITHUB_URL" --progress-bar --output "$ARCHIVE_PATH"; then
        printf "${RED}Failed to download binary${NC}\n"
        exit 1
    fi

    printf "${YELLOW}Downloading release checksums...${NC}\n"
    if ! curl --fail --location --proto '=https' --tlsv1.2 "$CHECKSUMS_URL" --silent --show-error --output "$CHECKSUMS_PATH"; then
        printf "${RED}Failed to download release checksums${NC}\n"
        exit 1
    fi
    if ! verifyChecksum "$ARCHIVE_PATH" "$CHECKSUMS_PATH" "$GITHUB_FILE"; then
        exit 1
    fi
    
    # Extract the binary
    if ! tar -xzf "$ARCHIVE_PATH" -C "$TEMP_DIR"; then
        printf "${RED}Failed to extract binary${NC}\n"
        exit 1
    fi
    
    # GoReleaser extracts the binary as just "ctty", not with the platform suffix
    EXTRACTED_BINARY="$TEMP_DIR/ctty"
    if [ ! -f "$EXTRACTED_BINARY" ]; then
        printf "${RED}Could not find extracted binary: $EXTRACTED_BINARY${NC}\n"
        exit 1
    fi
    DOWNLOADED_BINARY="$EXTRACTED_BINARY"
}

install() {
    printf "${YELLOW}Installing ctty...${NC}\n"
    
    # Backup old version if it exists to prevent interference during installation
    OLD_BACKUP=""
    if [ -f "$EXECUTABLE_PATH" ]; then
        OLD_BACKUP="$EXECUTABLE_PATH.backup.$$"
        runAsRoot mv "$EXECUTABLE_PATH" "$OLD_BACKUP"
    fi
    
    if ! chmod +x "$DOWNLOADED_BINARY"; then
        printf "${RED}Failed to set permissions${NC}\n"
        # Restore backup if installation fails
        if [ -n "$OLD_BACKUP" ] && [ -f "$OLD_BACKUP" ]; then
            runAsRoot mv "$OLD_BACKUP" "$EXECUTABLE_PATH"
        fi
        exit 1
    fi

    if ! runAsRoot mv "$DOWNLOADED_BINARY" "$EXECUTABLE_PATH"; then
        printf "${RED}Failed to install binary${NC}\n"
        # Restore backup if installation fails
        if [ -n "$OLD_BACKUP" ] && [ -f "$OLD_BACKUP" ]; then
            runAsRoot mv "$OLD_BACKUP" "$EXECUTABLE_PATH"
        fi
        exit 1
    fi
    
    # Clean up backup if installation succeeded
    if [ -n "$OLD_BACKUP" ] && [ -f "$OLD_BACKUP" ]; then
        runAsRoot rm -f "$OLD_BACKUP"
    fi
}

cleanup() {
    if [ -n "$TEMP_DIR" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf -- "$TEMP_DIR"
    fi
}

checkExisting() {
    if command -v ctty >/dev/null 2>&1; then
        CURRENT_VERSION=$(ctty --version 2>/dev/null | grep -o 'version.*' | cut -d' ' -f2 || echo "unknown")
        printf "${YELLOW}ctty is already installed (version: $CURRENT_VERSION)${NC}\n"
        
        # Check if FORCE_INSTALL is set
        if [ "$FORCE_INSTALL" = "true" ]; then
            printf "${GREEN}Force install enabled, proceeding with installation...${NC}\n"
            return
        fi
        
        # Check if running via pipe (stdin is not a terminal)
        if [ ! -t 0 ]; then
            printf "${YELLOW}Running via pipe - automatically proceeding with installation...${NC}\n"
            printf "${YELLOW}Use 'FORCE_INSTALL=false bash -c \"\$(curl -sSL ...)\"' to disable auto-install${NC}\n"
            return
        fi
        
        printf "${YELLOW}Do you want to overwrite it? [y/N]: ${NC}"
        read -r response
        case "$response" in
            [yY][eE][sS]|[yY]) 
                printf "${GREEN}Proceeding with installation...${NC}\n"
                ;;
            *)
                printf "${GREEN}Installation cancelled.${NC}\n"
                exit 0
                ;;
        esac
    fi
}

main() {
    # Check for help argument
    if [ "$1" = "-h" ] || [ "$1" = "--help" ] || [ "$1" = "help" ]; then
        usage
        exit 0
    fi
    
    printf "${PURPLE}Installing ctty - Connection Manager${NC}\n\n"
    
    # Set up system detection
    setSystem
    printf "${GREEN}Detected system: $OS ($ARCH)${NC}\n"

    if [ "$IS_TERMUX" = "true" ]; then
        printf "Running in Termux, installing to: $INSTALL_DIR\n"
    fi
    
    # Get and validate version FIRST (this can fail early)
    getLatestVersion
    
    # Check if already installed (this might prompt user)
    checkExisting

    TEMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ctty-install.XXXXXX") || {
        printf "${RED}Failed to create a temporary directory${NC}\n"
        exit 1
    }
    trap cleanup EXIT
    
    # Download and install
    downloadBinary
    install
    cleanup
    
    printf "\n${GREEN}✅ ctty was installed successfully to: ${NC}$EXECUTABLE_PATH\n"
    printf "${GREEN}You can now use 'ctty' command to manage your SSH connections!${NC}\n\n"
    
    # Show version
    printf "${YELLOW}Verifying installation...${NC}\n"
    if command -v ctty >/dev/null 2>&1; then
        # Use the full path to ensure we're using the newly installed version
        "$EXECUTABLE_PATH" --version 2>/dev/null || echo "Version check failed, but installation completed"
    else
        printf "${RED}Warning: 'ctty' command not found in PATH. You may need to restart your terminal or add $INSTALL_DIR to your PATH.${NC}\n"
    fi
}

if [ "${CTTY_INSTALL_TESTING:-false}" != "true" ]; then
    main "$@"
fi
