package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/hostimport"
	"github.com/zsuroy/ctty/internal/i18n"
)

var importCmd = &cobra.Command{
	Use:   "import [--from SOURCE] [SOURCE]",
	Short: "Import SSH hosts from another app",
	Long: `Import SSH profiles from another connection manager into OpenSSH config.

Each source writes to ~/.ssh/config.d/<source>.conf and adds an Include to
the main SSH config if needed. Existing Host names are skipped. Passwords
and vault secrets from the source app are not imported.

Examples:
  ctty import --from tabby
  ctty import tabby --dry-run
  ctty import tabby -f /path/to/config.yaml`,
	Args: cobra.MaximumNArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return hostimport.Names(), cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		from, _ := cmd.Flags().GetString("from")
		if from == "" && len(args) > 0 {
			from = args[0]
		}
		supported := strings.Join(hostimport.Names(), ", ")
		if from == "" {
			log.Fatal(i18n.T("import.err_source", supported))
		}
		src, ok := hostimport.Lookup(from)
		if !ok {
			log.Fatal(i18n.T("import.err_unsupported", from, supported))
		}

		file, _ := cmd.Flags().GetString("file")
		if file == "" {
			var err error
			file, err = src.DefaultPath()
			if err != nil {
				log.Fatalf("%s: %v", i18n.T("import.err_read"), err)
			}
		}
		data, err := os.ReadFile(file)
		if err != nil {
			log.Fatalf("%s: %v", i18n.T("import.err_no_file", src.Name(), file), err)
		}

		mainCfg := configFile
		if mainCfg == "" {
			mainCfg, err = config.GetDefaultSSHConfigPath()
			if err != nil {
				log.Fatalf("%s: %v", i18n.T("import.err_read"), err)
			}
		}

		dryRun, _ := cmd.Flags().GetBool("dry-run")
		res, err := hostimport.Apply(hostimport.Options{
			Source:     src,
			Data:       data,
			MainConfig: mainCfg,
			DestFile:   hostimport.DefaultDestFile(mainCfg, src.DestFileName()),
			DryRun:     dryRun,
		})
		if err != nil {
			log.Fatalf("%s: %v", i18n.T("import.err_read"), err)
		}

		if dryRun {
			fmt.Println(i18n.T("import.dry_run_header", res.AddedCount, res.SkippedCount))
			if res.Preview != "" {
				fmt.Print(res.Preview)
			}
			fmt.Println(i18n.T("import.passwords_note"))
			return
		}

		if res.AddedCount == 0 && res.SkippedCount == 0 {
			fmt.Println(i18n.T("import.none"))
			return
		}
		fmt.Println(i18n.T("import.added", res.AddedCount))
		fmt.Println(i18n.T("import.skipped", res.SkippedCount))
		fmt.Println(i18n.T("import.wrote", res.DestFile))
		if res.IncludeAdded {
			fmt.Println(i18n.T("import.include_added", mainCfg))
		}
		fmt.Println(i18n.T("import.passwords_note"))
	},
}

func init() {
	importCmd.Flags().String("from", "", "Source application ("+strings.Join(hostimport.Names(), ", ")+")")
	importCmd.Flags().StringP("file", "f", "", "Path to the source config file (auto-detected if omitted)")
	importCmd.Flags().Bool("dry-run", false, "Print converted hosts without writing files")
	RootCmd.AddCommand(importCmd)
}
