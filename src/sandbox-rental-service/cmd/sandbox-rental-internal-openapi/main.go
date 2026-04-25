package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/forge-metal/sandbox-rental-service/internal/api"
)

const (
	version           = "1.0.0"
	internalServerURL = "https://127.0.0.1:4263"
)

func main() {
	format := flag.String("format", "3.0", "OpenAPI output format: 3.0 or 3.1")
	check := flag.Bool("check", false, "Compare generated output against the committed file")
	flag.Parse()

	var (
		spec []byte
		err  error
	)
	switch *format {
	case "3.0":
		spec, err = api.InternalOpenAPIDowngradeYAML(version, internalServerURL)
	case "3.1":
		spec, err = api.InternalOpenAPIYAML(version, internalServerURL)
	default:
		fmt.Fprintln(os.Stderr, "invalid -format value, expected 3.0 or 3.1")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !bytes.HasSuffix(spec, []byte("\n")) {
		spec = append(spec, '\n')
	}

	if *check {
		specPath := filepath.Join("openapi", "internal-openapi-"+*format+".yaml")
		current, readErr := os.ReadFile(specPath)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, readErr)
			os.Exit(1)
		}
		if !bytes.Equal(current, spec) {
			fmt.Fprintln(os.Stderr, "openapi spec drift:", specPath)
			os.Exit(1)
		}
		return
	}

	if _, err := os.Stdout.Write(spec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
