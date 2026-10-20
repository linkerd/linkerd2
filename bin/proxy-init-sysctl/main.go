// Command sysctl provides the single kernel setting needed by Linkerd's
// proxy-init without adding a general-purpose system utilities package.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	closeWaitParameter = "net.netfilter.nf_conntrack_tcp_timeout_close_wait"
	procSysRoot        = "/proc/sys"
)

func main() {
	if err := run(os.Args[1:], procSysRoot, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, root string, output io.Writer) error {
	if len(args) != 2 || args[0] != "-w" {
		return fmt.Errorf("usage: sysctl -w %s=<seconds>", closeWaitParameter)
	}

	key, value, ok := strings.Cut(args[1], "=")
	if !ok || key != closeWaitParameter {
		return fmt.Errorf("unsupported sysctl setting %q", args[1])
	}

	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", closeWaitParameter, err)
	}

	path := filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(key, ".", "/")))
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := fmt.Fprintln(file, seconds); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}

	_, err = fmt.Fprintf(output, "%s = %d\n", key, seconds)
	return err
}
