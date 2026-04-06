package main

import (
	"fmt"
	"os"

	"github.com/forge-metal/billing-service/internal/billingapi"
)

func main() {
	spec, err := billingapi.OpenAPIDowngradeYAML()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if _, err := os.Stdout.Write(spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
