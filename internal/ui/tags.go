package ui

import (
	"fmt"
	"hash/fnv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// TagPalette contains a curated set of terminal-friendly, high-contrast, distinct hex colors
var TagPalette = []string{
	"#38BDF8", // Sky Blue
	"#34D399", // Emerald Green
	"#F472B6", // Rose Pink
	"#FB923C", // Orange
	"#A78BFA", // Light Purple
	"#FBBF24", // Amber Yellow
	"#2DD4BF", // Teal
	"#F87171", // Soft Red
	"#818CF8", // Indigo
	"#C084FC", // Violet
	"#A3E635", // Lime Green
	"#22D3EE", // Cyan
	"#E879F9", // Fuchsia
	"#FB7185", // Bright Rose
	"#4ADE80", // Light Green
	"#60A5FA", // Royal Blue
}

// SemanticTagColors maps common tag keywords to intuitive colors
var SemanticTagColors = map[string]string{
	// Production / Critical
	"prod":       "#EF4444",
	"production": "#EF4444",
	"prd":        "#EF4444",
	"live":       "#EF4444",
	"critical":   "#EF4444",

	// Staging / Testing
	"stage":   "#F59E0B",
	"staging": "#F59E0B",
	"stg":     "#F59E0B",
	"preprod": "#F59E0B",
	"pre":     "#F59E0B",
	"test":    "#EAB308",
	"testing": "#EAB308",
	"qa":      "#EAB308",
	"uat":     "#EAB308",

	// Development / Local
	"dev":         "#10B981",
	"development": "#10B981",
	"local":       "#10B981",
	"debug":       "#10B981",

	// Databases & Storage
	"db":       "#8B5CF6",
	"database": "#8B5CF6",
	"sql":      "#8B5CF6",
	"mysql":    "#8B5CF6",
	"postgres": "#8B5CF6",
	"redis":    "#8B5CF6",
	"mongo":    "#8B5CF6",

	// Web / Frontend / Proxy
	"web":      "#06B6D4",
	"frontend": "#06B6D4",
	"ui":       "#06B6D4",
	"nginx":    "#06B6D4",
	"gateway":  "#06B6D4",

	// API / Backend / Services
	"api":          "#3B82F6",
	"backend":      "#3B82F6",
	"server":       "#3B82F6",
	"app":          "#3B82F6",
	"microservice": "#3B82F6",

	// Infrastructure & Cloud
	"k8s":        "#6366F1",
	"kubernetes": "#6366F1",
	"docker":     "#6366F1",
	"aws":        "#F97316",
	"gcp":        "#4285F4",
	"azure":      "#0089D6",
	"cloud":      "#0EA5E9",
	"infra":      "#6366F1",

	// Work & Personal
	"work":     "#818CF8",
	"office":   "#818CF8",
	"corp":     "#818CF8",
	"personal": "#EC4899",
	"home":     "#EC4899",
	"lab":      "#EC4899",
	"homelab":  "#EC4899",
	"vpn":      "#14B8A6",

	// Hidden tag (ctty special tag)
	"hidden": "#6B7280",
}

var (
	customTagColorsMu sync.RWMutex
	customTagColors   map[string]string
)

// SetCustomTagColors registers user-configured custom tag colors
func SetCustomTagColors(colors map[string]string) {
	customTagColorsMu.Lock()
	defer customTagColorsMu.Unlock()
	if colors == nil {
		customTagColors = nil
		return
	}
	customTagColors = make(map[string]string, len(colors))
	for k, v := range colors {
		customTagColors[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(k), "#"))] = v
	}
}

func hashTag(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// GetTagHexColor returns the hex color string (e.g. "#EF4444") for a given tag
func GetTagHexColor(tag string) string {
	clean := strings.ToLower(strings.TrimSpace(tag))
	clean = strings.TrimPrefix(clean, "#")

	// 1. Check user custom colors
	customTagColorsMu.RLock()
	if customTagColors != nil {
		if hex, ok := customTagColors[clean]; ok && hex != "" {
			customTagColorsMu.RUnlock()
			return hex
		}
	}
	customTagColorsMu.RUnlock()

	// 2. Check semantic presets
	if hex, ok := SemanticTagColors[clean]; ok {
		return hex
	}

	// 3. Fallback to deterministic hash over curated palette
	idx := hashTag(clean) % uint32(len(TagPalette))
	return TagPalette[idx]
}

// GetTagColor returns the lipgloss color for a given tag
func GetTagColor(tag string) lipgloss.Color {
	return lipgloss.Color(GetTagHexColor(tag))
}

// FormatColoredTag renders a single tag prefixed with '#' dynamically adapted
// to the terminal's color capabilities (TrueColor -> ANSI256 -> ANSI16 -> Plain ASCII)
// using \x1b[39m (reset foreground only, preserving row background)
func FormatColoredTag(tag string) string {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return ""
	}
	clean := strings.TrimPrefix(tag, "#")
	hex := GetTagHexColor(clean)

	profile := lipgloss.ColorProfile()
	if profile == termenv.Ascii {
		return "#" + clean
	}

	c := profile.Color(hex)
	if c == nil {
		return "#" + clean
	}
	seq := c.Sequence(false)
	if seq == "" {
		return "#" + clean
	}
	return fmt.Sprintf("\x1b[%sm#%s\x1b[39m", seq, clean)
}

// FormatColoredTags renders a slice of tags with individual colors, joined by spaces
func FormatColoredTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	var formatted []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		formatted = append(formatted, FormatColoredTag(tag))
	}
	return strings.Join(formatted, " ")
}

// FormatPlainTags formats tags as "#tag1 #tag2" without ANSI escape codes
func FormatPlainTags(tags []string) string {
	if len(tags) == 0 {
		return ""
	}
	var formatted []string
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		clean := strings.TrimPrefix(tag, "#")
		formatted = append(formatted, "#"+clean)
	}
	return strings.Join(formatted, " ")
}

// CalculatePlainTagsWidth calculates the visible character width of formatted tags ("#tag1 #tag2")
func CalculatePlainTagsWidth(tags []string) int {
	return ansi.StringWidth(FormatPlainTags(tags))
}
