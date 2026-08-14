package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/zsuroy/ctty/internal/config"
)

func TestGetTagColor(t *testing.T) {
	// Semantic colors
	if GetTagColor("prod") != lipgloss.Color("#EF4444") {
		t.Errorf("Expected prod to be #EF4444, got %v", GetTagColor("prod"))
	}
	if GetTagColor("production") != lipgloss.Color("#EF4444") {
		t.Errorf("Expected production to be #EF4444, got %v", GetTagColor("production"))
	}
	if GetTagColor("dev") != lipgloss.Color("#10B981") {
		t.Errorf("Expected dev to be #10B981, got %v", GetTagColor("dev"))
	}
	if GetTagColor("database") != lipgloss.Color("#8B5CF6") {
		t.Errorf("Expected database to be #8B5CF6, got %v", GetTagColor("database"))
	}
	if GetTagColor("hidden") != lipgloss.Color("#6B7280") {
		t.Errorf("Expected hidden to be #6B7280, got %v", GetTagColor("hidden"))
	}

	// Case-insensitivity and leading #
	if GetTagColor("#PROD") != lipgloss.Color("#EF4444") {
		t.Errorf("Expected #PROD to be #EF4444, got %v", GetTagColor("#PROD"))
	}
	if GetTagColor("  Dev  ") != lipgloss.Color("#10B981") {
		t.Errorf("Expected '  Dev  ' to be #10B981, got %v", GetTagColor("  Dev  "))
	}

	// Deterministic hashing for arbitrary tags
	color1 := GetTagColor("my-custom-cluster-1")
	color2 := GetTagColor("my-custom-cluster-1")
	if color1 != color2 {
		t.Errorf("Expected same tag to produce same color, got %v vs %v", color1, color2)
	}

	// Different arbitrary tags should get valid palette colors
	color3 := GetTagColor("another-tag-xyz")
	if color3 == "" {
		t.Errorf("Expected non-empty color for tag")
	}
}

func TestCustomTagColors(t *testing.T) {
	defer SetCustomTagColors(nil)

	SetCustomTagColors(map[string]string{
		"my-special-tag": "#123456",
		"prod":           "#654321", // Override semantic color
	})

	if GetTagColor("my-special-tag") != lipgloss.Color("#123456") {
		t.Errorf("Expected custom tag color #123456, got %v", GetTagColor("my-special-tag"))
	}
	if GetTagColor("prod") != lipgloss.Color("#654321") {
		t.Errorf("Expected custom override #654321 for prod, got %v", GetTagColor("prod"))
	}
}

func TestFormatColoredTags(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	// Empty tags
	if FormatColoredTags(nil) != "" {
		t.Errorf("Expected empty string for nil tags")
	}
	if FormatColoredTags([]string{}) != "" {
		t.Errorf("Expected empty string for empty tags")
	}

	// Single tag
	res := FormatColoredTags([]string{"prod"})
	if !strings.Contains(res, "#prod") {
		t.Errorf("Expected formatted tag to contain #prod, got %v", res)
	}

	// Multiple tags
	multiRes := FormatColoredTags([]string{"prod", "web", "db"})
	if !strings.Contains(multiRes, "#prod") || !strings.Contains(multiRes, "#web") || !strings.Contains(multiRes, "#db") {
		t.Errorf("Expected formatted tags to contain all tags, got %v", multiRes)
	}
}

func TestCalculatePlainTagsWidth(t *testing.T) {
	if CalculatePlainTagsWidth(nil) != 0 {
		t.Errorf("Expected 0 for nil tags")
	}
	if CalculatePlainTagsWidth([]string{}) != 0 {
		t.Errorf("Expected 0 for empty tags")
	}

	// "#prod" -> 5 characters
	if CalculatePlainTagsWidth([]string{"prod"}) != 5 {
		t.Errorf("Expected 5 for ['prod'], got %d", CalculatePlainTagsWidth([]string{"prod"}))
	}

	// "#prod #web" -> 5 + 1 + 4 = 10 characters
	if CalculatePlainTagsWidth([]string{"prod", "web"}) != 10 {
		t.Errorf("Expected 10 for ['prod', 'web'], got %d", CalculatePlainTagsWidth([]string{"prod", "web"}))
	}

	// Tag already having # prefix: "#prod" -> "#prod" (5)
	if CalculatePlainTagsWidth([]string{"#prod"}) != 5 {
		t.Errorf("Expected 5 for ['#prod'], got %d", CalculatePlainTagsWidth([]string{"#prod"}))
	}
}

func TestTableRenderingWithColoredTags(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)

	m := createTestModel()
	m.hosts = []config.SSHHost{
		{Name: "server1", Hostname: "server1.example.com", Tags: []string{"prod", "db"}},
		{Name: "server2", Hostname: "server2.example.com", Tags: []string{"dev"}},
	}
	m.filteredHosts = m.hosts
	m.updateTableRows()

	view := m.renderTableView()
	if !strings.Contains(view, "server1") || !strings.Contains(view, "server2") {
		t.Errorf("Expected table view to contain host names")
	}
	if !strings.Contains(view, "#prod") || !strings.Contains(view, "#dev") {
		t.Errorf("Expected table view to contain colored tag names")
	}
}

func TestColorProfilesCompatibility(t *testing.T) {
	profiles := []struct {
		name    string
		profile termenv.Profile
	}{
		{"TrueColor", termenv.TrueColor},
		{"ANSI256", termenv.ANSI256},
		{"ANSI", termenv.ANSI},
		{"Ascii", termenv.Ascii},
	}

	for _, p := range profiles {
		t.Run(p.name, func(t *testing.T) {
			lipgloss.SetColorProfile(p.profile)
			res := FormatColoredTag("prod")
			if !strings.Contains(res, "#prod") {
				t.Errorf("Profile %s: expected result to contain '#prod', got %q", p.name, res)
			}
			if p.profile == termenv.Ascii && res != "#prod" {
				t.Errorf("Profile Ascii: expected plain '#prod', got %q", res)
			}
		})
	}
}
