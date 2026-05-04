// Renders the billing-service OpenAPI spec for one format. Bazel captures
// stdout into the declared output; --output remains for direct operator use.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/verself/billing-service/internal/billingapi"
)

func main() {
	format := flag.String("format", "", "OpenAPI output format: 3.0 or 3.1 (required)")
	output := flag.String("output", "", "Path to write the rendered YAML to (required)")
	flag.Parse()
	var spec []byte
	var err error
	switch *format {
	case "3.0":
		spec, err = billingapi.OpenAPIDowngradeYAML()
	case "3.1":
		spec, err = billingapi.OpenAPIYAML()
	default:
		fmt.Fprintln(os.Stderr, "missing or invalid --format value, expected 3.0 or 3.1")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *output == "" {
		_, err = os.Stdout.Write(spec)
	} else {
		err = os.WriteFile(*output, spec, 0o644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
