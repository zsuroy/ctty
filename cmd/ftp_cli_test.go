package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/zsuroy/ctty/internal/ftpconfig"
)

func TestFTPListSearchInfoJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := ftpconfig.Add(ftpconfig.FTPSite{
		Name: "lab-nas", Host: "nas.example.test", Port: 21, User: "ftpuser", Tags: []string{"lab"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ftpconfig.Add(ftpconfig.FTPSite{
		Name: "edge-ftp", Host: "edge.example.test", Port: 2121, User: "anon",
	}); err != nil {
		t.Fatal(err)
	}

	ftpFormat = "json"
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runFTPList()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	var sites []ftpconfig.FTPSite
	if err := json.Unmarshal(buf.Bytes(), &sites); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(sites) != 2 {
		t.Fatalf("want 2 sites, got %d", len(sites))
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	runFTPSearch("lab")
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	sites = nil
	if err := json.Unmarshal(buf.Bytes(), &sites); err != nil {
		t.Fatal(err)
	}
	if len(sites) != 1 || sites[0].Name != "lab-nas" {
		t.Fatalf("search: %v", sites)
	}

	r3, w3, _ := os.Pipe()
	os.Stdout = w3
	runFTPInfo("lab-nas")
	_ = w3.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r3)
	var one ftpconfig.FTPSite
	if err := json.Unmarshal(buf.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Host != "nas.example.test" || one.User != "ftpuser" {
		t.Fatalf("info: %+v", one)
	}
}
