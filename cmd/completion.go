package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion script",
	Long: `Generate shell completion script for ctty.

To load completions:

Bash:
  $ source <(ctty completion bash)
  
  # To load completions for each session, add to your ~/.bashrc:
  # echo 'source <(ctty completion bash)' >> ~/.bashrc

Zsh:
  $ source <(ctty completion zsh)
  
  # To load completions for each session, add to your ~/.zshrc:
  # echo 'source <(ctty completion zsh)' >> ~/.zshrc

Fish:
  $ ctty completion fish | source
  
  # To load completions for each session:
  $ ctty completion fish > ~/.config/fish/completions/ctty.fish

PowerShell:
  PS> ctty completion powershell | Out-String | Invoke-Expression
  
  # To load completions for each session, add to your PowerShell profile:
  # Add-Content $PROFILE 'ctty completion powershell | Out-String | Invoke-Expression'
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(completionCmd)
}
