// selfupdate_test.go covers asset naming, checksum parsing, and archive
// extraction — the pure logic of the self-update flow. Download/apply paths
// need network access and a real release; they are exercised manually via
// `ctty update --yes`.
package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"
)

func TestArchiveAssetName(t *testing.T) {
	tests := []struct{ goos, goarch, want string }{
		{"darwin", "arm64", "ctty_Darwin_arm64.tar.gz"},
		{"darwin", "amd64", "ctty_Darwin_x86_64.tar.gz"},
		{"linux", "amd64", "ctty_Linux_x86_64.tar.gz"},
		{"linux", "arm64", "ctty_Linux_arm64.tar.gz"},
		{"linux", "386", "ctty_Linux_i386.tar.gz"},
		{"windows", "amd64", "ctty_Windows_x86_64.zip"},
		{"windows", "386", "ctty_Windows_i386.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.goos+"/"+tt.goarch, func(t *testing.T) {
			if got := archiveAssetName(tt.goos, tt.goarch); got != tt.want {
				t.Fatalf("archiveAssetName(%s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

// The names generated for this platform must match the assets actually
// published in v0.5.0 (verified 2026-08-26 against the live release).
func TestAssetURLs(t *testing.T) {
	if !strings.Contains(AssetURL(), githubRepo+"/releases/latest/download/") {
		t.Errorf("AssetURL %q lacks latest-release prefix", AssetURL())
	}
	if !strings.Contains(ChecksumsURL(), "checksums.txt") {
		t.Errorf("ChecksumsURL %q wrong", ChecksumsURL())
	}
}

func TestExpectedChecksum(t *testing.T) {
	const full = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	checksums := []byte(strings.Join([]string{
		"1111111111111111111111111111111111111111111111111111111111111111  ctty_Darwin_arm64.tar.gz",
		full + "  ctty_Linux_x86_64.tar.gz",
		"",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  ctty_Windows_x86_64.zip",
	}, "\n"))

	t.Run("found", func(t *testing.T) {
		got, err := expectedChecksum(checksums, "ctty_Darwin_arm64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "1111111111111111111111111111111111111111111111111111111111111111" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("found-full", func(t *testing.T) {
		got, err := expectedChecksum(checksums, "ctty_Linux_x86_64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != full {
			t.Fatalf("got %q want %q", got, full)
		}
	})
	t.Run("uppercase-normalized", func(t *testing.T) {
		up := []byte("ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789  ctty_Linux_arm64.tar.gz\n")
		got, err := expectedChecksum(up, "ctty_Linux_arm64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := strings.ToLower("ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789")
		if got != want {
			t.Fatalf("want lowercase digest, got %q", got)
		}
	})
	t.Run("missing", func(t *testing.T) {
		if _, err := expectedChecksum(checksums, "ctty_Solaris_sparc"); err == nil {
			t.Fatal("expected error for unknown asset")
		}
	})
}

// buildFakeTarGz produces an in-memory tar.gz containing the given files.
func buildFakeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinaryTarGz(t *testing.T) {
	fake := buildFakeTarGz(t, map[string]string{
		"LICENSE":   "mit",
		"README.md": "# x",
		"ctty":      "\x7fELFfakebinary",
	})
	got, err := extractBinary("linux", fake)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if string(got) != "\x7fELFfakebinary" {
		t.Fatalf("wrong payload %q", got)
	}
}

func TestExtractBinaryZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range []struct{ name, body string }{
		{"LICENSE", "mit"}, {"README.md", "# x"}, {"ctty.exe", "MZfakebinary"},
	} {
		w, err := zw.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := extractBinary("windows", buf.Bytes())
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if string(got) != "MZfakebinary" {
		t.Fatalf("wrong payload %q", got)
	}
}

func TestExtractBinaryMissingEntry(t *testing.T) {
	fake := buildFakeTarGz(t, map[string]string{"LICENSE": "mit"})
	if _, err := extractBinary("linux", fake); err == nil {
		t.Fatal("expected error when binary entry absent")
	}
}
