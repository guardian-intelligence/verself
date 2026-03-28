package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	root := &cobra.Command{
		Use:     "bmci",
		Short:   "forge-metal: bare-metal CI platform",
		Version: version,
	}

	root.AddCommand(controllerCmd())
	root.AddCommand(agentCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func controllerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Run the job scheduler and node registry",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("controller: not yet implemented")
			return nil
		},
	}
	return cmd
}

func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Run the worker agent on a bare-metal node",
	}
	cmd.AddCommand(agentJoinCmd())
	cmd.AddCommand(agentRunCmd())
	return cmd
}

func agentJoinCmd() *cobra.Command {
	var controllerAddr string
	var token string

	cmd := &cobra.Command{
		Use:   "join",
		Short: "Register this node with the controller",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("agent join: controller=%s (not yet implemented)\n", controllerAddr)
			return nil
		},
	}
	cmd.Flags().StringVar(&controllerAddr, "controller", "", "Controller address (e.g. https://10.0.0.1:8443)")
	cmd.Flags().StringVar(&token, "token", "", "Bootstrap token for registration")
	_ = cmd.MarkFlagRequired("controller")
	_ = cmd.MarkFlagRequired("token")
	return cmd
}

func agentRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Start the agent daemon (heartbeat + job execution)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("agent run: not yet implemented")
			return nil
		},
	}
}
