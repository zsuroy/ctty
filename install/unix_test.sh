#!/bin/bash
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
CTTY_INSTALL_TESTING=true
export CTTY_INSTALL_TESTING
# shellcheck source=unix.sh
. "$SCRIPT_DIR/unix.sh"

TEST_DIR=$(mktemp -d "${TMPDIR:-/tmp}/ctty-install-test.XXXXXX")
trap 'rm -rf -- "$TEST_DIR"' EXIT

ASSET="ctty_Linux_x86_64.tar.gz"
ARCHIVE="$TEST_DIR/$ASSET"
CHECKSUMS="$TEST_DIR/checksums.txt"
printf 'verified archive' > "$ARCHIVE"
DIGEST=$(sha256File "$ARCHIVE")
printf '%s  %s\n' "$DIGEST" "$ASSET" > "$CHECKSUMS"

verifyChecksum "$ARCHIVE" "$CHECKSUMS" "$ASSET" >/dev/null

printf '%s  %s\n%s  %s\n' "$DIGEST" "$ASSET" "$DIGEST" "$ASSET" > "$CHECKSUMS"
if verifyChecksum "$ARCHIVE" "$CHECKSUMS" "$ASSET" >/dev/null 2>&1; then
    printf 'duplicate checksums unexpectedly passed validation\n' >&2
    exit 1
fi

printf '%s  %s\n' "$DIGEST" "$ASSET" > "$CHECKSUMS"
printf 'tampered archive' > "$ARCHIVE"
if verifyChecksum "$ARCHIVE" "$CHECKSUMS" "$ASSET" >/dev/null 2>&1; then
    printf 'tampered archive unexpectedly passed checksum verification\n' >&2
    exit 1
fi

printf 'not-a-digest  %s\n' "$ASSET" > "$CHECKSUMS"
if verifyChecksum "$ARCHIVE" "$CHECKSUMS" "$ASSET" >/dev/null 2>&1; then
    printf 'invalid checksum unexpectedly passed validation\n' >&2
    exit 1
fi
