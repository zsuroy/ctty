package cmd

import "testing"

func TestParseTransferTarget(t *testing.T) {
	tests := []struct {
		in         string
		wantHost   string
		wantRemote string
		wantOK     bool
	}{
		{"web1:/tmp/a", "web1", "/tmp/a", true},
		{"web1:relative/path", "web1", "relative/path", true},
		{"web1:", "web1", ".", true},
		{"./local", "", "", false},
		{"/abs/path", "", "", false},
		{"C:/windows/path", "", "", false},
		{"C:\\windows\\path", "", "", false},
		{":/nohost", "", "", false},
		{"path/with:colon", "", "", false}, // host contains slash → local
		{"user@bastion:/tmp/x", "user@bastion", "/tmp/x", true},
	}
	for _, tt := range tests {
		host, remote, ok := ParseTransferTarget(tt.in)
		if ok != tt.wantOK || host != tt.wantHost || remote != tt.wantRemote {
			t.Errorf("ParseTransferTarget(%q) = (%q,%q,%v); want (%q,%q,%v)",
				tt.in, host, remote, ok, tt.wantHost, tt.wantRemote, tt.wantOK)
		}
	}
}
