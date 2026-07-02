package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func completionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Generate a shell completion script for mu.

To load completions:

Bash:
  $ source <(mu completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ mu completion bash > /etc/bash_completion.d/mu
  # macOS:
  $ mu completion bash > $(brew --prefix)/etc/bash_completion.d/mu

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc
  # To load completions for each session, execute once:
  $ mu completion zsh > "${fpath[1]}/_mu"
  # You will need to start a new shell for this setup to take effect.

Fish:
  $ mu completion fish | source
  # To load completions for each session, execute once:
  $ mu completion fish > ~/.config/fish/completions/mu.fish

PowerShell:
  PS> mu completion powershell | Out-String | Invoke-Expression
  # To load completions for every new session, run:
  PS> mu completion powershell > mu.ps1
  # and source this file from your PowerShell profile.
`,
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		Args:      cobra.ExactArgs(1),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return cmd.Root().GenBashCompletion(os.Stdout)
			case "zsh":
				return cmd.Root().GenZshCompletion(os.Stdout)
			case "fish":
				return cmd.Root().GenFishCompletion(os.Stdout, true)
			case "powershell":
				return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}
}
