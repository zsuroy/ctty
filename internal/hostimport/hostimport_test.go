package hostimport

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zsuroy/ctty/internal/config"
)

func TestLookupTabby(t *testing.T) {
	src, ok := Lookup("Tabby")
	if !ok {
		t.Fatal("tabby should be registered")
	}
	if src.Name() != "tabby" || src.DestFileName() != "tabby.conf" {
		t.Errorf("got name=%q dest=%q", src.Name(), src.DestFileName())
	}
	names := Names()
	if len(names) == 0 || names[0] != "tabby" {
		t.Errorf("Names() = %v", names)
	}
	if _, ok := Lookup("putty"); ok {
		t.Fatal("putty is not registered")
	}
}

type stubSource struct{}

func (stubSource) Name() string                 { return "stub" }
func (stubSource) DestFileName() string         { return "stub.conf" }
func (stubSource) DefaultPath() (string, error) { return "/tmp/stub", nil }
func (stubSource) Parse([]byte) ([]config.SSHHost, error) {
	return []config.SSHHost{{Name: "from-stub", Hostname: "stub.example", Port: "22"}}, nil
}

func TestApplyUsesAnySource(t *testing.T) {
	dir := t.TempDir()
	mainCfg := filepath.Join(dir, "config")
	if err := os.WriteFile(mainCfg, []byte("Host already\n    HostName 1.2.3.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(Options{
		Source:     stubSource{},
		MainConfig: mainCfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedCount != 1 {
		t.Fatalf("added=%d", res.AddedCount)
	}
	if !strings.HasSuffix(res.DestFile, "stub.conf") {
		t.Errorf("dest = %q, want stub.conf", res.DestFile)
	}
	body, err := os.ReadFile(res.DestFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "from stub") && !strings.Contains(string(body), "Host from-stub") {
		t.Errorf("dest:\n%s", body)
	}
}

func TestParseTabbySSHProfiles(t *testing.T) {
	data, err := os.ReadFile("testdata/tabby/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	hosts, err := Tabby{}.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	byName := map[string]int{}
	for i, h := range hosts {
		byName[h.Name] = i
	}

	if _, ok := byName["skip-local"]; ok {
		t.Fatal("local profiles must be skipped")
	}
	if _, ok := byName["no-host"]; ok {
		t.Fatal("profiles without host must be skipped")
	}
	if len(hosts) != 4 {
		t.Fatalf("got %d hosts, want 4 (ssh with hostname)", len(hosts))
	}

	web := hosts[byName["web-prod"]]
	if web.Hostname != "10.0.0.1" || web.User != "deploy" || web.Port != "2222" {
		t.Errorf("web-prod fields: %+v", web)
	}
	if web.Identity != "/home/me/.ssh/id_ed25519" {
		t.Errorf("Identity = %q", web.Identity)
	}
	if !strings.Contains(web.Options, "IdentityFile /home/me/.ssh/id_rsa") {
		t.Errorf("extra IdentityFile missing from Options: %q", web.Options)
	}
	if web.ProxyCommand != "ssh jump -W %h:%p" {
		t.Errorf("ProxyCommand = %q", web.ProxyCommand)
	}
	if len(web.Tags) != 1 || web.Tags[0] != "production" {
		t.Errorf("tags = %v, want [production]", web.Tags)
	}

	if _, ok := byName["db-prod"]; !ok {
		t.Errorf("space in name should become db-prod, got %v", keys(byName))
	}

	behind := hosts[byName["behind-jump"]]
	if behind.ProxyJump != "jumper" {
		t.Errorf("ProxyJump = %q, want jumper (resolved from profile id)", behind.ProxyJump)
	}
}

func TestParseTabbyIgnoresPasswords(t *testing.T) {
	yaml := []byte(`
profiles:
  - name: secret
    type: ssh
    options:
      host: example.com
      user: root
      password: s3cret
`)
	hosts, err := Tabby{}.Parse(yaml)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("len=%d", len(hosts))
	}
	if strings.Contains(hosts[0].Options, "s3cret") || strings.Contains(hosts[0].Identity, "s3cret") {
		t.Fatal("password leaked into SSH host")
	}
}

func TestImportDryRunAndWrite(t *testing.T) {
	data, err := os.ReadFile("testdata/tabby/config.yaml")
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	mainCfg := filepath.Join(dir, "config")
	dest := filepath.Join(dir, "config.d", "tabby.conf")
	if err := os.WriteFile(mainCfg, []byte("Host already\n    HostName 1.2.3.4\n"), 0600); err != nil {
		t.Fatal(err)
	}

	src := Tabby{}
	res, err := Apply(Options{
		Source:     src,
		Data:       data,
		MainConfig: mainCfg,
		DestFile:   dest,
		DryRun:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedCount != 4 {
		t.Errorf("dry-run added=%d, want 4", res.AddedCount)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("dry-run must not write dest file")
	}

	res, err = Apply(Options{
		Source:     src,
		Data:       data,
		MainConfig: mainCfg,
		DestFile:   dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.AddedCount != 4 {
		t.Errorf("added=%d, want 4", res.AddedCount)
	}

	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "Host web-prod") {
		t.Errorf("dest missing web-prod:\n%s", text)
	}
	if !strings.Contains(text, "# Tags: production") {
		t.Errorf("dest missing tags:\n%s", text)
	}

	mainBody, err := os.ReadFile(mainCfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(mainBody), "Include") {
		t.Errorf("main config missing Include:\n%s", mainBody)
	}

	res2, err := Apply(Options{
		Source:     src,
		Data:       data,
		MainConfig: mainCfg,
		DestFile:   dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.AddedCount != 0 {
		t.Errorf("second import added=%d, want 0", res2.AddedCount)
	}
	if res2.SkippedCount < 4 {
		t.Errorf("skipped=%d, want >= 4", res2.SkippedCount)
	}
}

func TestImportDoesNotDuplicateInclude(t *testing.T) {
	data, err := os.ReadFile("testdata/tabby/config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	mainCfg := filepath.Join(dir, "config")
	dest := filepath.Join(dir, "config.d", "tabby.conf")
	if err := os.WriteFile(mainCfg, []byte("Include config.d/*\n\nHost already\n    HostName 1.2.3.4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := Apply(Options{Source: Tabby{}, Data: data, MainConfig: mainCfg, DestFile: dest})
	if err != nil {
		t.Fatal(err)
	}
	if res.IncludeAdded {
		t.Fatal("Include already covers dest; should not add another")
	}
	body, err := os.ReadFile(mainCfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "Include") != 1 {
		t.Errorf("duplicate Include:\n%s", body)
	}
}

func keys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
