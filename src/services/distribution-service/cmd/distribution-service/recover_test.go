package main

import (
	"slices"
	"strings"
	"testing"
)

func TestLocalIdentityCommandsUseNamesNotAllocatedIDs(t *testing.T) {
	commands := []rootCommand{
		createLocalUserCommand(distributionServiceUser, distributionSPIREWorkloadGroup),
		appendSupplementaryGroupCommand(distributionServiceUser, distributionSPIREWorkloadGroup),
	}
	commands = append(commands, reconcileLocalUserCommands(distributionServiceUser)...)

	for _, command := range commands {
		if slices.Contains(command.args, "--uid") {
			t.Fatalf("%s args include --uid: %v", command.name, command.args)
		}
		for i, arg := range command.args {
			if arg == "--gid" || arg == "--groups" {
				if i+1 >= len(command.args) {
					t.Fatalf("%s %s missing value: %v", command.name, arg, command.args)
				}
				value := command.args[i+1]
				if strings.Trim(value, "0123456789") == "" {
					t.Fatalf("%s %s uses numeric identity %q: %v", command.name, arg, value, command.args)
				}
			}
		}
	}
}
