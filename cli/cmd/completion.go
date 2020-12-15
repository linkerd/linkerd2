package cmd

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

// newCmdCompletion creates a new cobra command `completion` which contains commands for
// enabling linkerd auto completion
func newCmdCompletion() *cobra.Command {
	example := `  # bash <= 3.2:
  source /dev/stdin <<< "$(linkerd completion bash)"

  # bash >= 4.0:
  source <(linkerd completion bash)

  # bash <= 3.2 on osx:
  brew install bash-completion # ensure you have bash-completion 1.3+
  linkerd completion bash > $(brew --prefix)/etc/bash_completion.d/linkerd

  # bash >= 4.0 on osx:
  brew install bash-completion@2
  linkerd completion bash > $(brew --prefix)/etc/bash_completion.d/linkerd

  # zsh:
  # If shell completion is not already enabled in your environment you will need
  # to enable it.  You can execute the following once:

  echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  linkerd completion zsh > "${fpath[1]}/_linkerd"

  # You will need to start a new shell for this setup to take effect.

  # fish:
  linkerd completion fish | source

  # To load fish shell completions for each session, execute once:
  linkerd completion fish > ~/.config/fish/completions/linkerd.fish`

	cmd := &cobra.Command{
		Use:       "completion [bash|zsh|fish]",
		Short:     "Output shell completion code for the specified shell (bash, zsh or fish)",
		Long:      "Output shell completion code for the specified shell (bash, zsh or fish).",
		Example:   example,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			out, err := getCompletion(args[0], cmd.Parent())
			if err != nil {
				return err
			}

			fmt.Print(out)
			return nil
		},
	}

	return cmd
}

// getCompletion will return the auto completion shell script, if supported
func getCompletion(sh string, parent *cobra.Command) (string, error) {
	var err error
	var buf bytes.Buffer

	switch sh {
	case "bash":
		err = parent.GenBashCompletion(&buf)
	case "zsh":
		err = parent.GenZshCompletion(&buf)
	case "fish":
		err = parent.GenFishCompletion(&buf, true)

	default:
		err = errors.New("unsupported shell type (must be bash, zsh or fish): " + sh)
	}

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
