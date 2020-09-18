package main

import (
	"os"

	"github.com/jpriebe/kubectl-dpod/cmd"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var version = "undefined"

func main() {
	cmd.SetVersion(version)

	dpodCmd := cmd.NewDpodCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := dpodCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
