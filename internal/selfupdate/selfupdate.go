// Package selfupdate implements in-place binary self-update from GitHub
// releases. It downloads the goreleaser release archive for the current
// platform (e.g. ctty_Darwin_arm64.tar.gz), verifies its sha256 against the
// release checksums.txt, extracts the binary in memory, and atomically swaps
// the running executable via minio/selfupdate's rename dance.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
)

const (
	githubRepo = "zsuroy/ctty"
	binaryName = "ctty"
)

// goreleaserArch maps runtime GOARCH to the naming used in release archive
// names: amd64 becomes x86_64 and 386 becomes i386, everything else passes
// through.
func goreleaserArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "x86_64"
	case "386":
		return "i386"
	}
	return goarch
}

// archiveAssetName returns the goreleaser archive asset for a platform,
// e.g. ctty_Darwin_arm64.tar.gz or ctty_Windows_x86_64.zip — exactly the
// assets published since v0.5.0, so no releaser changes are required.
func archiveAssetName(goos, goarch string) string {
	osName := strings.ToUpper(goos[:1]) + goos[1:] // darwin -> Darwin
	base := fmt.Sprintf("%s_%s_%s", binaryName, osName, goreleaserArch(goarch))
	if goos == "windows" {
		return base + ".zip"
	}
	return base + ".tar.gz"
}

// binaryEntryName is the file name of the executable inside the archive:
// ctty on unix, ctty.exe on windows.
func binaryEntryName(goos string) string {
	if goos == "windows" {
		return binaryName + ".exe"
	}
	return binaryName
}

// assetName returns the archive asset for the current platform.
func assetName() string { return archiveAssetName(runtime.GOOS, runtime.GOARCH) }

// AssetURL returns the download URL of the latest release's platform archive.
func AssetURL() string {
	return fmt.Sprintf("https://github.com/%s/releases/latest/download/%s",
		githubRepo, assetName())
}

// ChecksumsURL returns the URL of the release checksums.txt.
func ChecksumsURL() string {
	return fmt.Sprintf("https://github.com/%s/releases/latest/download/checksums.txt", githubRepo)
}

// Progress receives human-readable phase updates during Apply.
type Progress func(phase, message string)

// fetch downloads url into memory. Release archives are a few MB.
func fetch(ctx context.Context, url string) ([]byte, error) {
	return fetchProgress(ctx, url, nil)
}

// fetchProgress downloads url into memory, invoking onBytes with the count of
// bytes received so far (throttled by the caller). onTotal may be nil when the
// server omits Content-Length.
func fetchProgress(ctx context.Context, url string, onBytes func(received int64, total int64)) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", binaryName+"-selfupdate")

	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %s for %s", resp.Status, filepath.Base(url))
	}

	if onBytes == nil {
		return io.ReadAll(resp.Body)
	}
	total := resp.ContentLength // -1 when unknown
	var buf bytes.Buffer
	received := int64(0)
	if _, err = io.Copy(&buf, io.TeeReader(resp.Body, writerFunc(func(p []byte) {
		received += int64(len(p))
		onBytes(received, total)
	}))); err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	return buf.Bytes(), nil
}

// writerFunc adapts a func([]byte) to io.Writer.
type writerFunc func([]byte)

func (f writerFunc) Write(p []byte) (int, error) { f(p); return len(p), nil }

// expectedChecksum parses a goreleaser checksums.txt and returns the lowercase
// sha256 hex digest for the given asset name. Lines look like:
//
//	<64-hex>  <asset-name>
func expectedChecksum(checksums []byte, asset string) (string, error) {
	for _, line := range strings.Split(string(checksums), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == asset {
			return strings.ToLower(parts[0]), nil
		}
	}
	return "", fmt.Errorf("checksum for %s not found in checksums.txt", asset)
}

// extractBinary pulls the ctty[.exe] entry out of an in-memory release
// archive: tar.gz everywhere except windows (.zip).
func extractBinary(goos string, archive []byte) ([]byte, error) {
	entry := binaryEntryName(goos)

	if goos == "windows" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, fmt.Errorf("open zip: %w", err)
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == entry {
				rc, err := f.Open()
				if err != nil {
					return nil, fmt.Errorf("open %s: %w", f.Name, err)
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, fmt.Errorf("%s not found in archive", entry)
	}

	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("open gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == entry {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("%s not found in archive", entry)
}

// humanBytes renders a byte count as a compact human-readable string.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Apply downloads the latest release archive, verifies its sha256 against the
// published checksums.txt, extracts the binary, and atomically replaces the
// running executable. The process must be restarted afterwards to run the new
// version.
func Apply(ctx context.Context, cb Progress) error {
	if cb == nil {
		cb = func(string, string) {}
	}

	cb("downloading", fmt.Sprintf("Downloading %s ...", assetName()))
	// Throttle progress callbacks: report every 256 KB or 400 ms, whichever
	// comes first, so the UI shows steady motion without flooding the channel.
	var (
		lastReport time.Time
		lastBytes  int64
	)
	archive, err := fetchProgress(ctx, AssetURL(), func(received, total int64) {
		now := time.Now()
		if received-lastBytes < 256*1024 && now.Sub(lastReport) < 400*time.Millisecond {
			return
		}
		lastBytes, lastReport = received, now
		if total > 0 {
			cb("downloading", fmt.Sprintf("Downloading %s ... %s / %s (%d%%)",
				assetName(), humanBytes(received), humanBytes(total), received*100/total))
		} else {
			cb("downloading", fmt.Sprintf("Downloading %s ... %s", assetName(), humanBytes(received)))
		}
	})
	if err != nil {
		return err
	}

	cb("verifying", "Verifying checksum ...")
	checksums, err := fetch(ctx, ChecksumsURL())
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	wantHex, err := expectedChecksum(checksums, assetName())
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if gotHex := hex.EncodeToString(got[:]); gotHex != wantHex {
		return fmt.Errorf("checksum mismatch: got %s want %s", gotHex, wantHex)
	}
	cb("verified", "Checksum OK")

	cb("extracting", "Extracting binary ...")
	bin, err := extractBinary(runtime.GOOS, archive)
	if err != nil {
		return err
	}

	cb("applying", "Replacing binary ...")
	if err := selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{}); err != nil {
		msg := err.Error()
		if strings.Contains(msg, "permission denied") || strings.Contains(msg, "access is denied") {
			msg += " (hint: insufficient permissions to replace the binary)"
		}
		return fmt.Errorf("apply update: %s", msg)
	}

	cb("success", "Update applied — restart ctty to use the new version.")
	return nil
}
