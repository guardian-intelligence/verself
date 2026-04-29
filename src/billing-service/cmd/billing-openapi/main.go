// Renders the billing-service OpenAPI spec for one format and writes
// it to --output. Invoked by the verself_openapi_yaml Bazel rule —
// stdout is not used because run_binary cannot capture it.
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
		fmt.Fprintln(os.Stderr, "missing --output path")
		os.Exit(1)
	}
	if err := os.WriteFile(*output, spec, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
