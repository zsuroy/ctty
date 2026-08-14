package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/i18n"
	"github.com/zsuroy/ctty/internal/ui"
)

var (
	// outputFormat defines the output format (table, json, simple)
	outputFormat string
	// tagsOnly limits search to tags only
	tagsOnly bool
	// namesOnly limits search to host names only
	namesOnly bool
)

var searchCmd = &cobra.Command{
	Use:   "search [query]",
	Short: "Search SSH hosts by name, hostname, or tags",
	Long: `Search through your SSH hosts configuration by name, hostname, or tags.
The search is case-insensitive and will match partial strings.

Examples:
  ctty search web          # Search for hosts containing "web"
  ctty search --tags dev   # Search only in tags for "dev"
  ctty search --names prod # Search only in host names for "prod"
  ctty search --format json server # Output results in JSON format`,
	Args: cobra.MaximumNArgs(1),
	Run:  runSearch,
}

func runSearch(cmd *cobra.Command, args []string) {
	// Register custom tag colors from configuration if available
	if appConfig, err := config.LoadAppConfig(); err == nil && len(appConfig.TagColors) > 0 {
		ui.SetCustomTagColors(appConfig.TagColors)
	}

	// Parse SSH configurations
	var hosts []config.SSHHost
	var err error

	if configFile != "" {
		hosts, err = config.ParseSSHConfigFile(configFile)
	} else {
		hosts, err = config.ParseSSHConfig()
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading SSH config file: %v\n", err)
		os.Exit(1)
	}

	if len(hosts) == 0 {
		fmt.Println(i18n.T("cli.no_hosts_in_config"))
		os.Exit(1)
	}

	// Filter out hidden hosts
	hosts = config.FilterVisibleHosts(hosts)

	// Get search query
	var query string
	if len(args) > 0 {
		query = args[0]
	}

	// Filter hosts based on search criteria
	filteredHosts := filterHosts(hosts, query, tagsOnly, namesOnly)

	// Display results
	if len(filteredHosts) == 0 {
		if query == "" {
			fmt.Println(i18n.T("cli.no_hosts_found"))
		} else {
			fmt.Println(i18n.T("cli.no_hosts_matching", query))
		}
		return
	}

	// Output results in specified format
	switch outputFormat {
	case "json":
		outputJSON(filteredHosts)
	case "simple":
		outputSimple(filteredHosts)
	default:
		outputTable(filteredHosts)
	}
}

// filterHosts filters hosts according to the search query and options
func filterHosts(hosts []config.SSHHost, query string, tagsOnly, namesOnly bool) []config.SSHHost {
	var filtered []config.SSHHost

	query = strings.TrimSpace(query)
	if query == "" {
		return hosts
	}

	words := strings.Fields(query)

	for _, host := range hosts {
		allMatch := true

		for _, word := range words {
			wordLower := strings.ToLower(word)
			cleanWord := strings.TrimPrefix(wordLower, "#")
			matched := false

			// Search in names if not tags-only
			if !tagsOnly {
				// Check the host name
				if strings.Contains(strings.ToLower(host.Name), wordLower) || strings.Contains(strings.ToLower(host.Name), cleanWord) {
					matched = true
				}

				// Check the hostname if not names-only
				if !namesOnly && !matched && (strings.Contains(strings.ToLower(host.Hostname), wordLower) || strings.Contains(strings.ToLower(host.Hostname), cleanWord)) {
					matched = true
				}
			}

			// Search in tags if not names-only
			if !namesOnly && !matched {
				for _, tag := range host.Tags {
					tagLower := strings.ToLower(tag)
					if strings.Contains(tagLower, cleanWord) || strings.Contains("#"+tagLower, wordLower) {
						matched = true
						break
					}
				}
			}

			if !matched {
				allMatch = false
				break
			}
		}

		if allMatch {
			filtered = append(filtered, host)
		}
	}

	return filtered
}

// outputTable displays results in a formatted table
func outputTable(hosts []config.SSHHost) {
	if len(hosts) == 0 {
		return
	}

	colName := i18n.T("table.col.name")
	colHost := i18n.T("table.col.hostname")
	colUser := i18n.T("table.col.user")
	colTags := i18n.T("table.col.tags")

	// Calculate column widths
	nameWidth := ansi.StringWidth(colName)
	hostWidth := ansi.StringWidth(colHost)
	userWidth := ansi.StringWidth(colUser)
	tagsWidth := ansi.StringWidth(colTags)

	for _, host := range hosts {
		if ansi.StringWidth(host.Name) > nameWidth {
			nameWidth = ansi.StringWidth(host.Name)
		}
		if ansi.StringWidth(host.Hostname) > hostWidth {
			hostWidth = ansi.StringWidth(host.Hostname)
		}
		if ansi.StringWidth(host.User) > userWidth {
			userWidth = ansi.StringWidth(host.User)
		}
		tagsLen := ui.CalculatePlainTagsWidth(host.Tags)
		if tagsLen > tagsWidth {
			tagsWidth = tagsLen
		}
	}

	// Add padding
	nameWidth += 2
	hostWidth += 2
	userWidth += 2
	tagsWidth += 2

	// Print header
	fmt.Printf("%-*s %-*s %-*s %-*s\n", nameWidth, colName, hostWidth, colHost, userWidth, colUser, tagsWidth, colTags)
	fmt.Printf("%s %s %s %s\n",
		strings.Repeat("-", nameWidth),
		strings.Repeat("-", hostWidth),
		strings.Repeat("-", userWidth),
		strings.Repeat("-", tagsWidth))

	// Print hosts
	for _, host := range hosts {
		user := host.User
		if user == "" {
			user = "-"
		}
		var tags string
		if len(host.Tags) == 0 {
			tags = "-"
		} else {
			tags = ui.FormatColoredTags(host.Tags)
		}
		fmt.Printf("%-*s %-*s %-*s %s\n", nameWidth, host.Name, hostWidth, host.Hostname, userWidth, user, tags)
	}

	fmt.Printf("\n%s\n", i18n.T("cli.found_hosts", len(hosts)))
}

// outputSimple displays results in simple format (one per line)
func outputSimple(hosts []config.SSHHost) {
	for _, host := range hosts {
		fmt.Println(host.Name)
	}
}

// outputJSON displays results in JSON format
func outputJSON(hosts []config.SSHHost) {
	fmt.Println("[")
	for i, host := range hosts {
		fmt.Printf("  {\n")
		fmt.Printf("    \"name\": \"%s\",\n", escapeJSON(host.Name))
		fmt.Printf("    \"hostname\": \"%s\",\n", escapeJSON(host.Hostname))
		fmt.Printf("    \"user\": \"%s\",\n", escapeJSON(host.User))
		fmt.Printf("    \"port\": \"%s\",\n", escapeJSON(host.Port))
		fmt.Printf("    \"identity\": \"%s\",\n", escapeJSON(host.Identity))
		fmt.Printf("    \"proxy_jump\": \"%s\",\n", escapeJSON(host.ProxyJump))
		fmt.Printf("    \"proxy_command\": \"%s\",\n", escapeJSON(host.ProxyCommand))
		fmt.Printf("    \"options\": \"%s\",\n", escapeJSON(host.Options))
		fmt.Printf("    \"tags\": [")
		for j, tag := range host.Tags {
			fmt.Printf("\"%s\"", escapeJSON(tag))
			if j < len(host.Tags)-1 {
				fmt.Printf(", ")
			}
		}
		fmt.Printf("]\n")
		if i < len(hosts)-1 {
			fmt.Printf("  },\n")
		} else {
			fmt.Printf("  }\n")
		}
	}
	fmt.Println("]")
}

// escapeJSON escapes special characters for JSON output
func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

func init() {
	// Add search command to root
	RootCmd.AddCommand(searchCmd)

	// Add flags
	searchCmd.Flags().StringVarP(&outputFormat, "format", "f", "table", "Output format (table, json, simple)")
	searchCmd.Flags().BoolVar(&tagsOnly, "tags", false, "Search only in tags")
	searchCmd.Flags().BoolVar(&namesOnly, "names", false, "Search only in host names")
}
