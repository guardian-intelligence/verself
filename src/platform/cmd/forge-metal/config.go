package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/forge-metal/forge-metal/internal/config"
	"github.com/spf13/cobra"
)

func configCmd(paths config.Paths) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Get and set static configuration values",
	}

	cmd.AddCommand(configGetCmd(paths))
	cmd.AddCommand(configSetCmd(paths))
	cmd.AddCommand(configUnsetCmd(paths))
	cmd.AddCommand(configListCmd(paths))
	cmd.AddCommand(configPathCmd(paths))
	cmd.AddCommand(configEditCmd(paths))

	return cmd
}

func configGetCmd(paths config.Paths) *cobra.Command {
	var (
		local      bool
		global     bool
		system     bool
		showOrigin bool
	)

	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a config value",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := config.ParseScopeFlags(local, global, system)
			if err != nil {
				return err
			}

			entry, err := config.Get(paths, args[0], scope)
			if err != nil {
				return err
			}

			if showOrigin {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", entry.Origin, entry.DisplayValue())
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout(), entry.DisplayValue())
			return nil
		},
	}

	addScopeFlags(cmd, &local, &global, &system)
	cmd.Flags().BoolVar(&showOrigin, "show-origin", false, "Show the origin of the winning value")

	return cmd
}

func configSetCmd(paths config.Paths) *cobra.Command {
	var (
		local  bool
		global bool
		system bool
	)

	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a static config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := config.ParseScopeFlags(local, global, system)
			if err != nil {
				return err
			}
			if scope == config.ScopeEffective {
				scope = config.ScopeLocal
			}

			return config.Set(paths, scope, args[0], args[1])
		},
	}

	addScopeFlags(cmd, &local, &global, &system)
	return cmd
}

func configUnsetCmd(paths config.Paths) *cobra.Command {
	var (
		local  bool
		global bool
		system bool
	)

	cmd := &cobra.Command{
		Use:   "unset <key>",
		Short: "Unset a config value in one scope",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := config.ParseScopeFlags(local, global, system)
			if err != nil {
				return err
			}
			if scope == config.ScopeEffective {
				scope = config.ScopeLocal
			}

			return config.Unset(paths, scope, args[0])
		},
	}

	addScopeFlags(cmd, &local, &global, &system)
	return cmd
}

func configListCmd(paths config.Paths) *cobra.Command {
	var (
		local      bool
		global     bool
		system     bool
		showOrigin bool
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List config values",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := config.ParseScopeFlags(local, global, system)
			if err != nil {
				return err
			}

			entries, err := config.List(paths, scope)
			if err != nil {
				return err
			}

			for _, entry := range entries {
				if showOrigin {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s=%s\n", entry.Origin, entry.Key, entry.DisplayValue())
					continue
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s=%s\n", entry.Key, entry.DisplayValue())
			}
			return nil
		},
	}

	addScopeFlags(cmd, &local, &global, &system)
	cmd.Flags().BoolVar(&showOrigin, "show-origin", false, "Show the origin of each value")

	return cmd
}

func configPathCmd(paths config.Paths) *cobra.Command {
	var (
		local  bool
		global bool
		system bool
	)

	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the config file path for a scope",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := config.ParseScopeFlags(local, global, system)
			if err != nil {
				return err
			}
			if scope == config.ScopeEffective {
				scope = config.ScopeLocal
			}

			path, err := pathForScope(paths, scope)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	}

	addScopeFlags(cmd, &local, &global, &system)
	return cmd
}

func configEditCmd(paths config.Paths) *cobra.Command {
	var (
		local  bool
		global bool
		system bool
	)

	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit a config file in your editor",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			scope, err := config.ParseScopeFlags(local, global, system)
			if err != nil {
				return err
			}
			if scope == config.ScopeEffective {
				scope = config.ScopeLocal
			}

			path, err := pathForScope(paths, scope)
			if err != nil {
				return err
			}
			if err := ensureConfigFile(path); err != nil {
				return err
			}

			editor := os.Getenv("VISUAL")
			if editor == "" {
				editor = os.Getenv("EDITOR")
			}
			if editor == "" {
				return fmt.Errorf("VISUAL or EDITOR must be set")
			}

			edit := exec.Command(editor, path)
			edit.Stdin = os.Stdin
			edit.Stdout = cmd.OutOrStdout()
			edit.Stderr = cmd.ErrOrStderr()
			return edit.Run()
		},
	}

	addScopeFlags(cmd, &local, &global, &system)
	return cmd
}

func addScopeFlags(cmd *cobra.Command, local, global, system *bool) {
	cmd.Flags().BoolVar(local, "local", false, "Use the repo-local config scope")
	cmd.Flags().BoolVar(global, "global", false, "Use the per-user config scope")
	cmd.Flags().BoolVar(system, "system", false, "Use the system-wide config scope")
}

func pathForScope(paths config.Paths, scope config.Scope) (string, error) {
	switch scope {
	case config.ScopeLocal:
		return paths.Local, nil
	case config.ScopeGlobal:
		return paths.Global, nil
	case config.ScopeSystem:
		return paths.System, nil
	default:
		return "", fmt.Errorf("invalid scope %q", scope)
	}
}

func ensureConfigFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("create config file: %w", err)
	}
	return file.Close()
}
