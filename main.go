package main

import (
	"os"

	"github.com/jpriebe/kubectl-pod_inspect/cmd"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

var version = "undefined"

func main() {
	cmd.SetVersion(version)

	podInspectCmd := cmd.NewPodInspectCommand(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := podInspectCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
