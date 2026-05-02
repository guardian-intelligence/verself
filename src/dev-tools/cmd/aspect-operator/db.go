package main

import (
	"fmt"
)

func cmdDB(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("db: missing subcommand (try `pg`, `ch`, or `tb`)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "pg":
		return cmdDBPG(rest)
	case "ch":
		return cmdDBCH(rest)
	case "tb":
		return cmdDBTB(rest)
	default:
		return fmt.Errorf("db: unknown subcommand: %s", sub)
	}
}
