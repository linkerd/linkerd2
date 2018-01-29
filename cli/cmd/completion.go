package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

var example = `  # bash <= 3.2
  source /dev/stdin <<< "$(conduit completion bash)"

  # bash >= 4.0
  source <(conduit completion bash)

  # bash <= 3.2 on osx
  brew install bash-completion # ensure you have bash-completion 1.3+
  conduit completion bash > $(brew --prefix)/etc/bash_completion.d/conduit

  # bash >= 4.0 on osx
  brew install bash-completion@2
  conduit completion bash > $(brew --prefix)/etc/bash_completion.d/conduit

  # zsh
  source <(conduit completion zsh)

  # zsh on osx / oh-my-zsh
  conduit completion zsh > "${fpath[1]}/_conduit"`

var completionCmd = &cobra.Command{
	Use:       "completion [bash|zsh]",
	Short:     "Shell completion",
	Long:      "Output completion code for the specified shell (bash or zsh).",
	Example:   example,
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"bash", "zsh"},
	Run: func(cmd *cobra.Command, args []string) {
		out, err := getCompletion(args[0])
		if err != nil {
			log.Fatal(err.Error())
		} else {
			fmt.Printf(out)
		}
	},
}

func init() {
	RootCmd.AddCommand(completionCmd)
}

func getCompletion(sh string) (string, error) {
	var err error
	var buf bytes.Buffer

	switch sh {
	case "bash":
		err = RootCmd.GenBashCompletion(&buf)
	case "zsh":
		err = RootCmd.GenZshCompletion(&buf)
	default:
		err = errors.New("unsupported shell type (must be bash or zsh): " + sh)
	}

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
