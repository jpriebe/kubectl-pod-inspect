package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	// Initialize all known client auth plugins.
	"k8s.io/client-go/kubernetes"
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"

	"github.com/logrusorgru/aurora"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type containerInfo struct {
	TypeCode     string
	Name         string
	Image        string
	State        string
	StateMessage string
	RestartCount int32
	Ready        bool
}

type dpodCommand struct {
	out       io.Writer
	f         cmdutil.Factory
	clientset *kubernetes.Clientset
	namespace string
}

// NewDpodCommand creates the command for rendering the Kubernetes server version.
func NewDpodCommand(streams genericclioptions.IOStreams) *cobra.Command {
	dpcmd := &dpodCommand{
		out: streams.Out,
	}

	ccmd := &cobra.Command{
		Use:          "dpod",
		Short:        "Lists pod containers' status",
		Long:         "Lists pod containers' status",
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			//fmt.Println("Echo: " + strings.Join(args, " "))
			return dpcmd.run(args)
		},
	}

	ccmd.AddCommand(newVersionCmd(streams.Out))

	fsets := ccmd.PersistentFlags()
	cfgFlags := genericclioptions.NewConfigFlags(true)
	cfgFlags.AddFlags(fsets)
	matchVersionFlags := cmdutil.NewMatchVersionFlags(cfgFlags)
	matchVersionFlags.AddFlags(fsets)

	dpcmd.f = cmdutil.NewFactory(matchVersionFlags)

	return ccmd
}

func (dp *dpodCommand) run(args []string) error {
	clientset, err := dp.f.KubernetesClientSet()
	if err != nil {
		return err
	}

	dp.clientset = clientset

	k8sCfg := dp.f.ToRawKubeConfigLoader()
	ns, _, err := k8sCfg.Namespace()
	if err != nil {
		return err
	}
	dp.namespace = ns

	if len(args) == 1 {
		err := dp.displayPod(args[0])
		return err
	}

	pods, err := dp.clientset.CoreV1().Pods(dp.namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	for _, pod := range pods.Items {
		dp.displayPod(pod.Name)
	}

	return nil
}

func (dp *dpodCommand) displayPod(podName string) error {
	pod, err := dp.clientset.CoreV1().Pods(dp.namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	cinfo := map[string]*containerInfo{}

	for _, c := range pod.Spec.InitContainers {
		// prefix with "0-" to ensure init containers show up first in the sorted list
		key := fmt.Sprintf("0-%s", c.Name)
		if _, ok := cinfo[key]; !ok {
			cinfo[key] = &containerInfo{}
		}

		cinfo[key].TypeCode = "IC"
		cinfo[key].Name = c.Name
		cinfo[key].Image = c.Image
	}

	for _, cs := range pod.Status.InitContainerStatuses {
		key := fmt.Sprintf("0-%s", cs.Name)
		if _, ok := cinfo[key]; !ok {
			return errors.New(fmt.Sprintf("Status found for init container '%s'; no corresponding container in spec.", cs.Name))
		}

		cstate, cmsg := getContainerStateInfo(cs.State)

		cinfo[key].State = cstate
		cinfo[key].StateMessage = cmsg
		cinfo[key].RestartCount = cs.RestartCount
		cinfo[key].Ready = cs.Ready
	}

	for _, c := range pod.Spec.Containers {
		// prefix with "1-" to ensure regular containers show up second in the sorted list
		key := fmt.Sprintf("1-%s", c.Name)
		if _, ok := cinfo[key]; !ok {
			cinfo[key] = &containerInfo{}
		}

		cinfo[key].Name = c.Name
		cinfo[key].TypeCode = "C"
		cinfo[key].Image = c.Image
	}

	for _, cs := range pod.Status.ContainerStatuses {
		key := fmt.Sprintf("1-%s", cs.Name)
		if _, ok := cinfo[key]; !ok {
			return errors.New(fmt.Sprintf("Status found for container '%s'; no corresponding container in spec.", cs.Name))
		}

		cstate, cmsg := getContainerStateInfo(cs.State)

		cinfo[key].State = cstate
		cinfo[key].StateMessage = cmsg
		cinfo[key].RestartCount = cs.RestartCount
		cinfo[key].Ready = cs.Ready
	}

	keys := make([]string, 0, len(cinfo))
	for k := range cinfo {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Printf("%s%s\n\n", aurora.Yellow("Pod: "), pod.Name)

	tw := dp.newTablewriter()

	tw.Append([]string{
		aurora.Yellow("Type").String(),
		aurora.Yellow("Name").String(),
		aurora.Yellow("State").String(),
		aurora.Yellow("RC").String(),
		aurora.Yellow("Ready").String(),
		aurora.Yellow("Image").String(),
	})
	for _, key := range keys {
		ci := cinfo[key]
		ready := aurora.Green("✔").String()
		if !ci.Ready {
			ready = aurora.Red("✖").String()
		}
		restartCount := fmt.Sprintf("%d", ci.RestartCount)
		if ci.RestartCount > 0 {
			restartCount = aurora.Red(restartCount).String()
		}

		tw.Append([]string{
			ci.TypeCode,
			ci.Name,
			ci.State,
			restartCount,
			ready,
			ci.Image,
		})
		if ci.StateMessage != "" {
			tw.Append([]string{"", "", "", "", "", ci.StateMessage})
		}
	}
	tw.Render()

	fmt.Printf("\n")

	return nil
}

func getContainerStateInfo(state v1.ContainerState) (string, string) {
	stateCode := ""
	reason := ""
	message := ""

	if state.Running != nil {
		stateCode = "R"
		reason = ""
		message = ""
	} else if state.Terminated != nil {
		stateCode = "T"
		reason = state.Terminated.Reason
		message = state.Terminated.Message
	} else if state.Waiting != nil {
		stateCode = "W"
		reason = state.Waiting.Reason
		message = state.Waiting.Message
	} else {
		return "n/a", ""
	}

	str1 := stateCode
	if reason != "" {
		str1 = fmt.Sprintf("%s (%s)", stateCode, reason)
	}

	str2 := ""
	if message != "" {
		str2 = message
	}

	return str1, str2
}

func (dp *dpodCommand) newTablewriter() *tablewriter.Table {
	tw := tablewriter.NewWriter(dp.out)
	tw.SetRowSeparator("")
	tw.SetCenterSeparator("")
	tw.SetColumnSeparator("")
	tw.SetBorder(false)
	tw.SetRowLine(false)
	tw.SetHeaderLine(false)
	tw.SetAutoWrapText(false)
	return tw
}
