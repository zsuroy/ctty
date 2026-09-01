package cmd

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zsuroy/ctty/internal/selfupdate"
	"github.com/zsuroy/ctty/internal/version"
)

// updateCmd checks for a newer release and (with --yes) applies it in place.
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates and self-update the ctty binary",
	Long: `Check GitHub for a newer ctty release.

Without flags, prints whether an update is available.
With --yes, downloads the latest release archive for this platform,
verifies its sha256 checksum against checksums.txt, and atomically
replaces the running binary. Restart ctty afterwards.`,
	Run: func(cmd *cobra.Command, args []string) {
		yes, _ := cmd.Flags().GetBool("yes")

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		info, err := version.CheckForUpdates(ctx, AppVersion)
		if err != nil {
			log.Fatalf("Update check failed: %v", err)
		}
		if !info.Available {
			fmt.Printf("ctty %s is up to date.\n", info.CurrentVer)
			return
		}

		fmt.Printf("Update available: %s → %s\n", info.CurrentVer, info.LatestVer)
		if info.ReleaseURL != "" {
			fmt.Println(info.ReleaseURL)
		}
		if !yes {
			fmt.Println("Run 'ctty update --yes' to download and install it.")
			return
		}

		uxCtx, uxCancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer uxCancel()
		lastLen := 0
		err = selfupdate.Apply(uxCtx, func(phase, msg string) {
			if phase == "downloading" {
				// Carriage-return progress: overwrite the same line while
				// bytes flow, then leave it behind once the phase changes.
				line := "\r" + msg
				pad := lastLen - len([]rune(msg))
				if pad > 0 {
					line += strings.Repeat(" ", pad)
				}
				fmt.Print(line)
				lastLen = len([]rune(msg))
				return
			}
			if lastLen > 0 {
				fmt.Println()
				lastLen = 0
			}
			fmt.Println(msg)
		})
		if err != nil {
			log.Fatalf("Update failed: %v", err)
		}
	},
}

func init() {
	updateCmd.Flags().BoolP("yes", "y", false, "Download and install the update without prompting")
	RootCmd.AddCommand(updateCmd)
}
