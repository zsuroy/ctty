package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/ftpconfig"
	"github.com/zsuroy/ctty/internal/ui"
)

var ftpFormat string

// ftpCmd opens the FTP site manager / dual-pane browser, or queries saved sites.
//
// Forms:
//
//	ctty ftp                         → site manager TUI (humans)
//	ctty ftp list|search|info …      → non-interactive JSON/human query (agents)
//	ctty ftp <name>                  → dual-pane FTP browser for a saved site
//
// Agents must use list|search|info --format json and never open the TUI.
// FTP passwords live in the encrypted vault (credentials.json, ftp: prefix).
var ftpCmd = &cobra.Command{
	Use:   "ftp [list|search|info|name]",
	Short: "Open FTP site manager / browser, or query saved sites",
	Long: `Open the FTP site manager TUI, query saved sites, or browse a site.

FTP traffic (including passwords) is cleartext unless FTPS is used (follow-up).
Site inventory: ~/.config/ctty/ftp.json
Passwords (encrypted vault): ~/.config/ctty/credentials.json under ftp: names.

Forms:
  ctty ftp                          List and manage saved FTP sites (TUI)
  ctty ftp list [--format json]     List saved sites
  ctty ftp search [query] [--format json]
  ctty ftp info <name> [--format json]
  ctty ftp lab-nas                  Open dual-pane local|remote browser for a site

Agents: use list|search|info --format json only; never open the FTP TUI.`,
	Args: cobra.ArbitraryArgs,
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) >= 1 {
			if args[0] == "info" && len(args) == 1 {
				return completeFTPNames(toComplete), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		base := []string{"list", "search", "info"}
		names := completeFTPNames(toComplete)
		var out []string
		lower := strings.ToLower(toComplete)
		for _, b := range base {
			if strings.HasPrefix(b, lower) {
				out = append(out, b)
			}
		}
		out = append(out, names...)
		return out, cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) == 0 {
			if err := ui.RunFTPMode(AppVersion, noUpdateCheck); err != nil {
				log.Fatalf("Error running FTP mode: %v", err)
			}
			fmt.Println()
			return
		}

		switch args[0] {
		case "list":
			runFTPList()
			return
		case "search":
			q := ""
			if len(args) > 1 {
				q = strings.Join(args[1:], " ")
			}
			runFTPSearch(q)
			return
		case "info":
			if len(args) < 2 {
				fmt.Fprintf(os.Stderr, "Error: ftp info requires a site name\n")
				os.Exit(1)
			}
			runFTPInfo(args[1])
			return
		}

		name := args[0]
		if _, ok := ftpconfig.Find(name); !ok {
			fmt.Fprintf(os.Stderr, "Error: ftp site %q not found\n", name)
			os.Exit(2)
		}
		if err := ui.RunFTPBrowserMode(name, AppVersion, noUpdateCheck); err != nil {
			log.Fatalf("Error running FTP browser: %v", err)
		}
		fmt.Println()
	},
}

func completeFTPNames(toComplete string) []string {
	sites, err := ftpconfig.Load()
	if err != nil {
		return nil
	}
	lower := strings.ToLower(toComplete)
	var out []string
	for _, s := range sites {
		if strings.HasPrefix(strings.ToLower(s.Name), lower) {
			out = append(out, s.Name)
		}
	}
	return out
}

func runFTPList() {
	sites, err := ftpconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	outputFTPSites(sites)
}

func runFTPSearch(query string) {
	sites, err := ftpconfig.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	query = strings.TrimSpace(query)
	if query == "" {
		outputFTPSites(sites)
		return
	}
	words := strings.Fields(strings.ToLower(query))
	var matched []ftpconfig.FTPSite
	for _, s := range sites {
		hay := strings.ToLower(s.Name + " " + s.Host + " " + s.User + " " + strings.Join(s.Tags, " "))
		ok := true
		for _, w := range words {
			w = strings.TrimPrefix(w, "#")
			if !strings.Contains(hay, w) {
				ok = false
				break
			}
		}
		if ok {
			matched = append(matched, s)
		}
	}
	outputFTPSites(matched)
}

func runFTPInfo(name string) {
	site, ok := ftpconfig.Find(name)
	if !ok {
		if ftpFormat == "json" {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
				"ok": false, "error": "NOT_FOUND", "name": name,
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: ftp site %q not found\n", name)
		}
		os.Exit(2)
	}
	if ftpFormat == "json" {
		_ = json.NewEncoder(os.Stdout).Encode(site)
		return
	}
	fmt.Printf("Name: %s\n", site.Name)
	fmt.Printf("Host: %s\n", site.Host)
	fmt.Printf("Port: %d\n", site.Port)
	fmt.Printf("User: %s\n", site.User)
	if len(site.Tags) > 0 {
		fmt.Printf("Tags: %s\n", strings.Join(site.Tags, ", "))
	}
}

func outputFTPSites(sites []ftpconfig.FTPSite) {
	if ftpFormat == "json" {
		if sites == nil {
			sites = []ftpconfig.FTPSite{}
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sites)
		return
	}
	if len(sites) == 0 {
		fmt.Println("No FTP sites found.")
		return
	}
	for _, s := range sites {
		tags := ""
		if len(s.Tags) > 0 {
			tags = " [" + strings.Join(s.Tags, ", ") + "]"
		}
		user := s.User
		if user == "" {
			user = "anonymous"
		}
		fmt.Printf("%-20s %s@%s:%d%s\n", s.Name, user, s.Host, s.Port, tags)
	}
}

func init() {
	RootCmd.AddCommand(ftpCmd)
	ftpCmd.Flags().StringVar(&ftpFormat, "format", "", "Output format: json (for list/search/info)")
}
