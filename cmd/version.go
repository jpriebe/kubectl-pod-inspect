package cmd

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

const appLabel = "kubectl pod-inspect"

var version string

// SetVersion set the application version for consumption in the output of the command.
func SetVersion(v string) {
	version = v
}

type versionCmd struct {
	out io.Writer
}

func newVersionCmd(out io.Writer) *cobra.Command {
	version := &versionCmd{
		out: out,
	}

	cmd := &cobra.Command{
		Use:   "version",
		Short: "print the version number and exit",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return errors.New("this command does not accept arguments")
			}
			return version.run()
		},
	}

	// we have to muck with the usage template because we're using "kubectl pod-inspect" for the
	// "Use" line in the root-level command.  Cobra really doesn't like when you use two tokens like
	// that, but the krew repo wants us to have the "kubectl" prepended to the usage info.
	oldLine := `{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}`
	newLine := `
  kubectl pod-inspect version{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}`

	cmd.SetUsageTemplate(strings.Replace(cmd.UsageTemplate(), oldLine, newLine, 1))

	return cmd
}

func (v *versionCmd) run() error {
	_, err := fmt.Fprintf(v.out, "%s %s\n", appLabel, version)
	if err != nil {
		return err
	}
	return nil
}
