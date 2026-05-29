package main

import (
	"fmt"
	"strings"

	"github.com/verself/deployment-tools/internal/nomadclient"
)

func printFailurePacket(packet nomadclient.FailurePacket) {
	fmt.Println()
	fmt.Printf("verself-deploy: nomad failure packet job=%s deployment=%s eval=%s\n", packet.JobID, packet.DeploymentID, packet.EvalID)
	if packet.DeploymentStatus != "" || packet.StatusDescription != "" {
		fmt.Printf("  deployment status=%s description=%s\n", packet.DeploymentStatus, packet.StatusDescription)
	}
	for _, eval := range packet.Evaluations {
		fmt.Printf("  eval id=%s status=%s queued=%d failed_task_groups=%d description=%s\n",
			eval.ID, eval.Status, eval.QueuedAllocs, len(eval.FailedTaskGroups), eval.StatusDescription)
		for _, failure := range eval.FailedTaskGroups {
			fmt.Printf("    failed_tg %s\n", failure.Summary())
		}
	}
	for _, alloc := range packet.Allocations {
		fmt.Printf("  alloc id=%s group=%s status=%s node=%s description=%s\n",
			alloc.ID, alloc.TaskGroup, alloc.ClientStatus, firstPacketValue(alloc.NodeName, alloc.NodeID), alloc.ClientDescription)
		for _, task := range alloc.Tasks {
			fmt.Printf("    task=%s state=%s restarts=%d event=%s exit=%d message=%s\n",
				task.Name, task.State, task.Restarts, task.EventType, task.ExitCode, firstPacketValue(task.EventMessage, task.DriverError, task.SetupError, task.DownloadError))
		}
		if strings.TrimSpace(alloc.StderrTail) != "" {
			fmt.Println("    stderr tail:")
			fmt.Println(indentPacket(alloc.StderrTail))
		}
		if strings.TrimSpace(alloc.StdoutTail) != "" {
			fmt.Println("    stdout tail:")
			fmt.Println(indentPacket(alloc.StdoutTail))
		}
	}
	for _, inspectErr := range packet.InspectErrors {
		fmt.Printf("  inspect_error %s\n", inspectErr)
	}
}

func firstPacketValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func indentPacket(value string) string {
	lines := strings.Split(strings.TrimRight(value, "\n"), "\n")
	for i, line := range lines {
		lines[i] = "      " + line
	}
	return strings.Join(lines, "\n")
}
