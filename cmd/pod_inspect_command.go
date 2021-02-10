package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	// Initialize all known client auth plugins.
	"k8s.io/client-go/kubernetes"
	// add this, per krew best practices
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
	ReadyIcon    string
}

type podInspectCommand struct {
	out         io.Writer
	f           cmdutil.Factory
	clientset   *kubernetes.Clientset
	namespace   string
	numLogLines int
	numEvents   int
}

// NewPodInspectCommand creates the command for rendering the Kubernetes server version.
func NewPodInspectCommand(streams genericclioptions.IOStreams) *cobra.Command {
	dpcmd := &podInspectCommand{
		out: streams.Out,
	}

	ccmd := &cobra.Command{
		Use:          "kubectl pod-inspect <podname>",
		Short:        "Inspects a pod",
		Long:         "Provides detailed information about a pod, including its containers' statuses, pod events, and logs from non-ready containers.",
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return dpcmd.run(args)
		},
	}

	// we have to muck with the usage template because we're using "kubectl pod-inspect" for the
	// "Use" value.  Cobra really doesn't like when you use two tokens like that, but the
	// krew repo wants us to have the "kubectl" prepended to the usage info.
	oldLine := `{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}`
	newLine := `{{if .HasAvailableSubCommands}}
  kubectl pod-inspect [command]{{end}}`

	ccmd.SetUsageTemplate(strings.Replace(ccmd.UsageTemplate(), oldLine, newLine, 1))

	ccmd.Flags().IntVarP(&dpcmd.numEvents, "max-num-events", "e", 10, "Maximum number of events to display; 0 means display all")
	ccmd.Flags().IntVarP(&dpcmd.numLogLines, "max-num-log-lines", "l", 5, "Maximum number of log lines to display; 0 means display all")

	ccmd.AddCommand(newVersionCmd(streams.Out))

	fsets := ccmd.PersistentFlags()
	cfgFlags := genericclioptions.NewConfigFlags(true)
	cfgFlags.AddFlags(fsets)
	matchVersionFlags := cmdutil.NewMatchVersionFlags(cfgFlags)
	matchVersionFlags.AddFlags(fsets)

	dpcmd.f = cmdutil.NewFactory(matchVersionFlags)

	return ccmd
}

func (dp *podInspectCommand) run(args []string) error {
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

func (dp *podInspectCommand) displayPod(podName string) error {
	pod, err := dp.clientset.CoreV1().Pods(dp.namespace).Get(context.Background(), podName, metav1.GetOptions{})
	if err != nil {
		return err
	}

	cinfo := map[string]*containerInfo{}
	podLogs := map[string]string{}

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
			return fmt.Errorf("status found for init container '%s'; no corresponding container in spec", cs.Name)
		}

		cstate, cmsg, creadyicon := getContainerStateInfo(cs.State)

		cinfo[key].State = cstate
		cinfo[key].StateMessage = cmsg
		cinfo[key].RestartCount = cs.RestartCount
		cinfo[key].Ready = cs.Ready
		cinfo[key].ReadyIcon = creadyicon

		if !cs.Ready {
			logs, err := dp.getPodLogs(podName, cinfo[key].Name)
			if err != nil {
				return err
			}

			if logs != "" {
				podLogs[cinfo[key].Name] = logs
			}
		}
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
			return fmt.Errorf("status found for container '%s'; no corresponding container in spec", cs.Name)
		}

		cstate, cmsg, creadyicon := getContainerStateInfo(cs.State)

		cinfo[key].State = cstate
		cinfo[key].StateMessage = cmsg
		cinfo[key].RestartCount = cs.RestartCount
		cinfo[key].Ready = cs.Ready
		cinfo[key].ReadyIcon = creadyicon

		if !cs.Ready {
			logs, err := dp.getPodLogs(podName, cinfo[key].Name)
			if err != nil {
				return err
			}

			if logs != "" {
				podLogs[cinfo[key].Name] = logs
			}
		}
	}

	keys := make([]string, 0, len(cinfo))
	for k := range cinfo {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Printf("%s%s / %s\n\n", aurora.Cyan("Pod: "), pod.Namespace, pod.Name)

	fmt.Printf("%s\n\n", aurora.Cyan("Containers: "))

	tw := dp.newTablewriter(dp.out)

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
		restartCount := fmt.Sprintf("%d", ci.RestartCount)

		tw.Append([]string{
			ci.TypeCode,
			ci.Name,
			ci.State,
			restartCount,
			ci.ReadyIcon,
			ci.Image,
		})
		if ci.StateMessage != "" {
			tw.Append([]string{"", "", "", "", "", ci.StateMessage})
		}
	}
	tw.Render()

	podFailures, err := dp.getPodFailures(pod)
	if err != nil {
		return err
	}

	if podFailures != "" {
		fmt.Printf("\n")
		fmt.Printf("%s", podFailures)
	}

	podEvents, err := dp.getPodEvents(pod)
	if err != nil {
		return err
	}

	if podEvents != "" {
		fmt.Printf("\n")
		fmt.Printf("%s", podEvents)
	}

	for containerName, logs := range podLogs {
		logHeader := "logs:"
		if dp.numLogLines > 0 {
			if dp.numLogLines == 1 {
				logHeader = "logs (last line):"
			} else {
				logHeader = fmt.Sprintf("logs (last %d lines):", dp.numLogLines)
			}
		}
		fmt.Printf("\n%s %s %s\n\n%s", aurora.Cyan("Container"), containerName, aurora.Cyan(logHeader), logs)
	}

	fmt.Printf("\n")

	return nil
}

func (dp *podInspectCommand) getPodLogs(podName, containerName string) (string, error) {

	var tailLines int64
	tailLines = int64(dp.numLogLines)

	logOptions := v1.PodLogOptions{Container: containerName}

	if tailLines > 0 {
		logOptions.TailLines = &tailLines
	}

	req := dp.clientset.CoreV1().Pods(dp.namespace).GetLogs(podName, &logOptions)
	podLogs, err := req.Stream(context.Background())
	if err != nil {
		// ignore this error -- it could be that the container is in ImagePullBackoff, for example, and has no logs
		return "", nil
	}
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (dp *podInspectCommand) getPodFailures(pod *v1.Pod) (string, error) {
	retval := ""

	failedPodConditions := []v1.PodCondition{}

	for _, condition := range pod.Status.Conditions {
		if condition.Status != v1.ConditionTrue {
			failedPodConditions = append(failedPodConditions, condition)
		}
	}

	if len(failedPodConditions) != 0 {
		retval += aurora.Cyan(fmt.Sprintf("Failed Pod Conditions:\n\n")).String()

		sb := &strings.Builder{}
		tw := dp.newTablewriter(sb)

		tw.Append([]string{
			aurora.Yellow("Condition").String(),
			aurora.Yellow("Reason").String(),
			aurora.Yellow("Message").String(),
		})

		for _, condition := range failedPodConditions {
			tw.Append([]string{
				string(condition.Type),
				condition.Reason,
				condition.Message,
			})
		}

		tw.Render()
		retval += sb.String()
	}

	return retval, nil
}

func (dp *podInspectCommand) getPodEvents(pod *v1.Pod) (string, error) {
	retval := ""

	field := fmt.Sprintf("involvedObject.name=%s", pod.Name)
	eventList, err := dp.clientset.CoreV1().Events(dp.namespace).List(context.Background(), metav1.ListOptions{FieldSelector: field})
	if err != nil {
		return "", err
	}

	events := eventList.Items

	if len(events) == 0 {
		return "", nil
	}

	eventsTruncated := false
	if dp.numEvents > 0 {
		if len(events) > dp.numEvents {
			idxLast := len(events)
			idxFirst := idxLast - dp.numEvents

			events = events[idxFirst:idxLast]
			eventsTruncated = true
		}
	}

	sb := &strings.Builder{}
	tw := dp.newTablewriter(sb)

	tw.Append([]string{
		aurora.Yellow("Last Seen").String(),
		aurora.Yellow("Type").String(),
		aurora.Yellow("Reason").String(),
		aurora.Yellow("Message").String(),
	})

	for _, event := range events {
		timestamp := event.LastTimestamp
		if timestamp.IsZero() {
			timestamp = event.CreationTimestamp
		}
		tw.Append([]string{
			timestamp.String(),
			event.Type,
			event.Reason,
			event.Message,
		})
	}
	tw.Render()
	podEvents := sb.String()

	re := regexp.MustCompile(`\s+\n`)
	podEvents = re.ReplaceAllString(podEvents, "\n")

	if eventsTruncated {
		if len(events) == 1 {
			retval += aurora.Cyan(fmt.Sprintf("Last pod event:\n\n")).String()
		} else {
			retval += aurora.Cyan(fmt.Sprintf("Last %d pod events:\n\n", len(events))).String()
		}
	} else {
		retval += aurora.Cyan(fmt.Sprintf("Pod events:\n\n")).String()
	}
	retval += podEvents

	return retval, nil
}

func getContainerStateInfo(state v1.ContainerState) (string, string, string) {
	stateCode := ""
	reason := ""
	message := ""
	readyicon := ""

	if state.Running != nil {
		stateCode = "R"
		reason = ""
		message = ""
		readyicon = aurora.Green("✔").String()
	} else if state.Terminated != nil {
		stateCode = "T"
		reason = state.Terminated.Reason
		message = state.Terminated.Message
		readyicon = aurora.Red("✖").String()
	} else if state.Waiting != nil {
		stateCode = "W"
		reason = state.Waiting.Reason
		message = state.Waiting.Message
		readyicon = aurora.Yellow("…").String()
	} else {
		return "n/a", "", "?"
	}

	str1 := stateCode
	if reason != "" {
		str1 = fmt.Sprintf("%s (%s)", stateCode, reason)
	}

	return str1, message, readyicon
}

func (dp *podInspectCommand) newTablewriter(out io.Writer) *tablewriter.Table {
	tw := tablewriter.NewWriter(out)
	tw.SetRowSeparator("")
	tw.SetCenterSeparator("")
	tw.SetColumnSeparator("")
	tw.SetBorder(false)
	tw.SetRowLine(false)
	tw.SetHeaderLine(false)
	tw.SetAutoWrapText(false)
	return tw
}
