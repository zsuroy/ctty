package cmd

import (
	"testing"

	"github.com/zsuroy/ctty/internal/config"
)

func TestSelectHosts(t *testing.T) {
	hosts := []config.SSHHost{
		{Name: "a", Tags: []string{"prod", "web"}},
		{Name: "b", Tags: []string{"dev"}},
		{Name: "c", Tags: []string{"prod", "api"}},
		{Name: "d", Tags: nil},
	}

	got := selectHosts(hosts, []string{"prod"}, nil)
	if len(got) != 2 {
		t.Fatalf("tags prod: want 2, got %d", len(got))
	}

	got = selectHosts(hosts, nil, []string{"b", "d"})
	if len(got) != 2 {
		t.Fatalf("hosts b,d: want 2, got %d", len(got))
	}

	got = selectHosts(hosts, []string{"prod"}, []string{"b"})
	if len(got) != 3 {
		t.Fatalf("tags OR hosts: want 3, got %d (%v)", len(got), namesOf(got))
	}

	got = selectHosts(hosts, []string{"missing"}, nil)
	if len(got) != 0 {
		t.Fatalf("expected empty")
	}
}

func namesOf(hosts []config.SSHHost) []string {
	var n []string
	for _, h := range hosts {
		n = append(n, h.Name)
	}
	return n
}

func TestSplitCSV(t *testing.T) {
	got := splitCSV(" a, b ,c")
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("got %v", got)
	}
}
