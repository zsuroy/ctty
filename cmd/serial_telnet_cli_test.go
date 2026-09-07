package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/zsuroy/ctty/internal/serialconfig"
	"github.com/zsuroy/ctty/internal/telnetconfig"
)

func TestSerialListJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	dev := serialconfig.DefaultDevice()
	dev.Name = "console1"
	dev.Device = "/dev/ttyUSB0"
	if err := serialconfig.Add(dev); err != nil {
		t.Fatal(err)
	}

	serialFormat = "json"
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runSerialList()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	var devices []serialconfig.SerialDevice
	if err := json.Unmarshal(buf.Bytes(), &devices); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(devices) != 1 || devices[0].Name != "console1" {
		t.Fatalf("unexpected: %v", devices)
	}

	serialFormat = "json"
	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	runSerialInfo("console1")
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	var one serialconfig.SerialDevice
	if err := json.Unmarshal(buf.Bytes(), &one); err != nil {
		t.Fatal(err)
	}
	if one.Device != "/dev/ttyUSB0" {
		t.Fatalf("info: %+v", one)
	}

}

func TestTelnetListSearchJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	if err := telnetconfig.Add(telnetconfig.TelnetHost{
		Name: "core-sw", Host: "192.168.1.1", Port: 23, Tags: []string{"lab"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := telnetconfig.Add(telnetconfig.TelnetHost{
		Name: "edge", Host: "10.0.0.5", Port: 2001,
	}); err != nil {
		t.Fatal(err)
	}

	telnetFormat = "json"
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	runTelnetList()
	_ = w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	var hosts []telnetconfig.TelnetHost
	if err := json.Unmarshal(buf.Bytes(), &hosts); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(hosts) != 2 {
		t.Fatalf("want 2 hosts, got %d", len(hosts))
	}

	r2, w2, _ := os.Pipe()
	os.Stdout = w2
	runTelnetSearch("lab")
	_ = w2.Close()
	os.Stdout = old
	buf.Reset()
	_, _ = io.Copy(&buf, r2)
	hosts = nil
	if err := json.Unmarshal(buf.Bytes(), &hosts); err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0].Name != "core-sw" {
		t.Fatalf("search: %v", hosts)
	}
}
