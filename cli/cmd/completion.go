package cmd

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

func newCmdCompletion() *cobra.Command {
	example := `  # bash <= 3.2
  source /dev/stdin <<< "$(linkerd completion bash)"

  # bash >= 4.0
  source <(linkerd completion bash)

  # bash <= 3.2 on osx
  brew install bash-completion # ensure you have bash-completion 1.3+
  linkerd completion bash > $(brew --prefix)/etc/bash_completion.d/linkerd

  # bash >= 4.0 on osx
  brew install bash-completion@2
  linkerd completion bash > $(brew --prefix)/etc/bash_completion.d/linkerd

  # zsh
  source <(linkerd completion zsh)

  # zsh on osx / oh-my-zsh
  linkerd completion zsh > "${fpath[1]}/_linkerd"`

	cmd := &cobra.Command{
		Use:       "completion [bash|zsh]",
		Short:     "Output shell completion code for the specified shell (bash or zsh)",
		Long:      "Output shell completion code for the specified shell (bash or zsh).",
		Example:   example,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh"},
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

func getCompletion(sh string, parent *cobra.Command) (string, error) {
	var err error
	var buf bytes.Buffer

	switch sh {
	case "bash":
		err = parent.GenBashCompletion(&buf)
	case "zsh":
		err = parent.GenZshCompletion(&buf)
	default:
		err = errors.New("unsupported shell type (must be bash or zsh): " + sh)
	}

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
