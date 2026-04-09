package main

import (
	"fmt"
	"os"

	"github.com/forge-metal/mailbox-service/internal/api"
)

const version = "dev"

func main() {
	spec, err := api.OpenAPIYAML(version, "127.0.0.1:4246")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
