package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// debugDumpSections splits a debug-view dump into its "===== NAME ====="
// sections, keyed by section name ("LIST", "TAG PICKER", "FTP LIST", ...),
// with the "REPORT …" self-measurement lines stripped from the frame body.
func debugDumpSections(out string) map[string]string {
	sections := map[string]string{}
	for _, part := range strings.Split(out, "===== ")[1:] {
		idx := strings.Index(part, " =====\n")
		if idx < 0 {
			continue
		}
		var frame []string
		for _, ln := range strings.Split(part[idx+len(" =====\n"):], "\n") {
			if !strings.HasPrefix(ln, "REPORT ") {
				frame = append(frame, ln)
			}
		}
		sections[part[:idx]] = strings.Join(frame, "\n")
	}
	return sections
}

func TestDebugViewDumpsListAndTagPicker(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	cfg := `# Tags: web, prod
Host web-1
    HostName 10.0.1.1
    User deploy

# Tags: db
Host db-1
    HostName 10.0.2.1
    User admin
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	if err := runDebugView(buf, cfgPath, 100, 30, "ssh"); err != nil {
		t.Fatalf("runDebugView: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"===== LIST =====", "===== TAG PICKER =====", "Tag Filter", "web-1", "#prod"} {
		if !strings.Contains(out, want) {
			t.Fatalf("dump missing %q:\n%s", want, out)
		}
	}
}

func TestDebugViewDefaultDrawerIsSSH(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	cfg := `# Tags: web
Host web-1
    HostName 10.0.1.1
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}

	buf := new(bytes.Buffer)
	if err := runDebugView(buf, cfgPath, 100, 30, ""); err != nil {
		t.Fatalf("runDebugView: %v", err)
	}
	if !strings.Contains(buf.String(), "===== LIST =====") {
		t.Fatalf("empty drawer should dump the ssh list:\n%s", buf.String())
	}
}

func TestDebugViewManagerDrawers(t *testing.T) {
	const width, height = 100, 24
	seedContent := map[string]string{
		"ftp":    "lab-nas",
		"s3":     "minio-lab",
		"webdav": "nextcloud",
		"telnet": "core-switch",
	}
	for _, manager := range []string{"ftp", "s3", "webdav", "telnet"} {
		t.Run(manager, func(t *testing.T) {
			buf := new(bytes.Buffer)
			if err := runDebugView(buf, "", width, height, manager); err != nil {
				t.Fatalf("runDebugView(%s): %v", manager, err)
			}
			out := buf.String()
			label := strings.ToUpper(manager)
			for _, want := range []string{
				"===== " + label + " LIST =====",
				"===== " + label + " TAG PICKER =====",
				seedContent[manager],
				"Tag Filter",
			} {
				if !strings.Contains(out, want) {
					t.Fatalf("%s dump missing %q:\n%s", manager, want, out)
				}
			}

			sections := debugDumpSections(out)
			if len(sections) != 2 {
				t.Fatalf("%s dump sections=%d want 2", manager, len(sections))
			}
			for name, frame := range sections {
				lines := strings.Split(strings.TrimRight(frame, "\n"), "\n")
				if len(lines) != height {
					t.Fatalf("%s %s frame rows=%d want %d", manager, name, len(lines), height)
				}
			}
		})
	}
}

func TestDebugViewUnknownDrawer(t *testing.T) {
	buf := new(bytes.Buffer)
	if err := runDebugView(buf, "", 100, 30, "bogus"); err == nil {
		t.Fatal("expected error for unknown drawer")
	}
}
