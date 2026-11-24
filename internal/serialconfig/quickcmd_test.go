package serialconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQuickCmdDefaults(t *testing.T) {
	cmds := DefaultQuickCmds()
	if len(cmds) != 8 {
		t.Fatalf("default quick cmds = %d, want 8", len(cmds))
	}
	if cmds[4].Label != "MODBUS" || !cmds[4].Hex {
		t.Fatalf("slot 5 should be a HEX Modbus frame, got %+v", cmds[4])
	}
	if cmds[0].Value != "AT" || cmds[0].Hex {
		t.Fatalf("slot 1 should be plain ASCII AT, got %+v", cmds[0])
	}
}

func TestQuickCmdLoadSaveRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	// Missing file returns defaults without creating anything.
	cmds, err := LoadQuickCmds()
	if err != nil {
		t.Fatalf("load defaults: %v", err)
	}
	if len(cmds) != 8 {
		t.Fatalf("defaults = %d, want 8", len(cmds))
	}
	path := filepath.Join(dir, "ctty", "serial-quick.json")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("missing quick file should not be created on load")
	}

	cmds[0] = QuickCmd{Label: "PING", Value: "PING", Hex: false}
	if err := SaveQuickCmds(cmds); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("quick file not created after save: %v", err)
	}

	got, err := LoadQuickCmds()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got[0].Label != "PING" || got[4].Label != "MODBUS" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestQuickCmdPayloadAndByteCount(t *testing.T) {
	cases := []struct {
		cmd  QuickCmd
		want []byte
	}{
		{QuickCmd{Value: "AT+GMR"}, []byte("AT+GMR")},
		{QuickCmd{Value: "01 03 00 00 84 0A", Hex: true}, []byte{1, 3, 0, 0, 0x84, 0x0A}},
		{QuickCmd{Value: "0x0103", Hex: true}, []byte{1, 3}},
		// Label-only slot: the add flow fills the label first, so the label
		// has to be what gets sent instead of an empty payload.
		{QuickCmd{Label: "AT+CFUN=1"}, []byte("AT+CFUN=1")},
		// An explicit value still wins over the label.
		{QuickCmd{Label: "RST", Value: "AT+RST"}, []byte("AT+RST")},
	}
	for _, c := range cases {
		b, err := c.cmd.Payload()
		if err != nil || !equalQuickBytes(b, c.want) {
			t.Fatalf("payload %+v = %v,%v want %v", c.cmd, b, err, c.want)
		}
		if n := c.cmd.ByteCount(); n != len(c.want) {
			t.Fatalf("byte count = %d, want %d", n, len(c.want))
		}
	}
}

func equalQuickBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// modbusCRC is the CRC-16/MODBUS the RTU frame carries, low byte first.
func modbusCRC(data []byte) [2]byte {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}
	return [2]byte{byte(crc & 0xFF), byte(crc >> 8)}
}

// The built-in slot used to ship "01 03 00 00 84 0A": it dropped the
// register-quantity field while keeping the CRC computed over the complete
// six-byte request, so every click sent a frame a slave must reject.
func TestQuickCmdModbusFrameIsCompleteAndValid(t *testing.T) {
	var slot *QuickCmd
	for _, c := range DefaultQuickCmds() {
		if c.Label == "MODBUS" {
			cc := c
			slot = &cc
		}
	}
	if slot == nil {
		t.Fatal("no MODBUS slot in the defaults")
	}
	b, err := slot.Payload()
	if err != nil {
		t.Fatalf("payload: %v", err)
	}
	if len(b) != 8 {
		t.Fatalf("frame = % X (%d bytes), want 8", b, len(b))
	}
	if b[0] != 0x01 || b[1] != 0x03 || b[2] != 0x00 || b[3] != 0x00 || b[4] != 0x00 || b[5] != 0x01 {
		t.Fatalf("request = % X, want 01 03 00 00 00 01", b[:6])
	}
	want := modbusCRC(b[:6])
	if b[6] != want[0] || b[7] != want[1] {
		t.Fatalf("crc = %02X %02X, want %02X %02X", b[6], b[7], want[0], want[1])
	}
	if want != [2]byte{0x84, 0x0A} {
		t.Fatalf("crc drift: %02X %02X, want 84 0A", want[0], want[1])
	}
}

// A file written by an older build still holds the truncated frame; loading
// it must repair the slot (and persist the repair) instead of leaving Alt+5
// broken forever.
func TestQuickCmdLoadMigratesLegacyModbusFrame(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfgDir := filepath.Join(dir, "ctty")
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := `[{"label":"MODBUS","value":"01 03 00 00 84 0A","hex":true},{"label":"AT","value":"AT","hex":false}]`
	path := filepath.Join(cfgDir, "serial-quick.json")
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}

	cmds, err := LoadQuickCmds()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cmds[0].Value != "01 03 00 00 00 01 84 0A" {
		t.Fatalf("legacy frame not migrated: %q", cmds[0].Value)
	}
	if cmds[1].Value != "AT" {
		t.Fatalf("unrelated slot changed: %+v", cmds[1])
	}

	// The repair is written back, so it survives without re-running.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "00 01 84 0A") {
		t.Fatalf("migration not persisted: %s", data)
	}

	// A user-edited hex slot is left alone.
	custom := `[{"label":"X","value":"01 03 00 05","hex":true}]`
	if err := os.WriteFile(path, []byte(custom), 0600); err != nil {
		t.Fatal(err)
	}
	cmds, err = LoadQuickCmds()
	if err != nil {
		t.Fatalf("load custom: %v", err)
	}
	if cmds[0].Value != "01 03 00 05" {
		t.Fatalf("custom hex value rewritten: %q", cmds[0].Value)
	}
}

// A hex slot accepts the same spellings the send box does: a frame pasted
// from a datasheet should not need hand-editing to decode.
func TestQuickCmdHexAcceptsCommonSpellings(t *testing.T) {
	want := []byte{0x01, 0x03}
	for _, v := range []string{"01 03", "01,03", "0x01,0x03", "01-03", "01:03", `\x01\x03`, "01_03"} {
		got, err := (QuickCmd{Value: v, Hex: true}).Payload()
		if err != nil {
			t.Fatalf("Payload(%q) error: %v", v, err)
		}
		if string(got) != string(want) {
			t.Fatalf("Payload(%q) = % X, want % X", v, got, want)
		}
	}
}
