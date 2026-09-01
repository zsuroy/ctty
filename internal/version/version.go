package version

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const GitHubRepo = "zsuroy/ctty"

// RepoURL is the canonical GitHub repository URL.
func RepoURL() string {
	return "https://github.com/" + GitHubRepo
}

// UpdateInfo contains information about available updates
type UpdateInfo struct {
	Available   bool
	CurrentVer  string
	LatestVer   string
	ReleaseURL  string
	ReleaseName string
}

// parseVersion extracts version numbers from a version string (e.g., "v1.2.3" -> [1, 2, 3])
func parseVersion(version string) []int {
	// Remove 'v' prefix if present
	version = strings.TrimPrefix(version, "v")

	parts := strings.Split(version, ".")
	nums := make([]int, len(parts))

	for i, part := range parts {
		// Remove any non-numeric suffixes (e.g., "1-beta", "2-rc1")
		numPart := strings.FieldsFunc(part, func(r rune) bool {
			return r == '-' || r == '+' || r == '_'
		})[0]

		if num, err := strconv.Atoi(numPart); err == nil {
			nums[i] = num
		}
	}

	return nums
}

// compareVersions compares two version strings
// Returns: -1 if v1 < v2, 0 if v1 == v2, 1 if v1 > v2
func compareVersions(v1, v2 string) int {
	nums1 := parseVersion(v1)
	nums2 := parseVersion(v2)

	// Pad with zeros to make lengths equal
	maxLen := len(nums1)
	if len(nums2) > maxLen {
		maxLen = len(nums2)
	}

	for len(nums1) < maxLen {
		nums1 = append(nums1, 0)
	}
	for len(nums2) < maxLen {
		nums2 = append(nums2, 0)
	}

	// Compare each part
	for i := 0; i < maxLen; i++ {
		if nums1[i] < nums2[i] {
			return -1
		}
		if nums1[i] > nums2[i] {
			return 1
		}
	}

	return 0
}

// CheckForUpdates checks GitHub for the latest release of ctty.
//
// Instead of the REST API (60 req/h unauthenticated, routinely exhausted on
// shared IPs), it resolves the releases/latest/download/ redirect and reads
// the tag from the Location header — same data, no rate-limit quota.
func CheckForUpdates(ctx context.Context, currentVersion string) (*UpdateInfo, error) {
	// Skip version check if current version is "dev"
	if currentVersion == "dev" {
		return &UpdateInfo{
			Available:  false,
			CurrentVer: currentVersion,
		}, nil
	}

	client := &http.Client{Timeout: 15 * time.Second, Transport: http.DefaultTransport}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		"https://github.com/"+GitHubRepo+"/releases/latest/download/checksums.txt", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "ctty/"+currentVersion)

	// Do NOT follow the final redirect to the CDN; the first hop carries
	// /releases/download/<tag>/ which is all we need.
	resp, err := client.Transport.RoundTrip(req.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &UpdateInfo{Available: false, CurrentVer: currentVersion}, nil
	}
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub returned status %d checking latest release", resp.StatusCode)
	}

	loc := resp.Header.Get("Location")
	tag := latestTagFromLocation(loc)
	if tag == "" {
		return nil, fmt.Errorf("could not determine latest version from redirect %q", loc)
	}

	updateAvailable := compareVersions(currentVersion, tag) < 0
	return &UpdateInfo{
		Available:   updateAvailable,
		CurrentVer:  currentVersion,
		LatestVer:   tag,
		ReleaseURL:  fmt.Sprintf("https://github.com/%s/releases/tag/%s", GitHubRepo, tag),
		ReleaseName: "ctty " + tag,
	}, nil
}

// latestTagFromLocation extracts "<tag>" from a GitHub asset redirect like
// https://github.com/<owner>/<repo>/releases/download/<tag>/<asset>.
func latestTagFromLocation(loc string) string {
	const marker = "/releases/download/"
	i := strings.Index(loc, marker)
	if i < 0 {
		return ""
	}
	rest := loc[i+len(marker):]
	if j := strings.IndexByte(rest, '/'); j >= 0 {
		rest = rest[:j]
	}
	return strings.TrimPrefix(rest, "v")
}
