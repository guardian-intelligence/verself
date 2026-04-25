package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	identityapi "github.com/verself/identity-service/internal/api"
)

const publicServerURL = "https://identity.api.anveio.com"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	format := flag.String("format", "3.1", "OpenAPI format: 3.0 or 3.1")
	check := flag.Bool("check", false, "verify the committed OpenAPI file is up to date")
	flag.Parse()

	var (
		data []byte
		err  error
		path string
	)
	switch *format {
	case "3.0":
		path = "openapi/openapi-3.0.yaml"
		data, err = identityapi.OpenAPIDowngradeYAML("1.0.0", publicServerURL)
	case "3.1":
		path = "openapi/openapi-3.1.yaml"
		data, err = identityapi.OpenAPIYAML("1.0.0", publicServerURL)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
	if err != nil {
		return err
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	if !*check {
		_, err = os.Stdout.Write(data)
		return err
	}
	existing, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(existing, data) {
		return fmt.Errorf("%s is out of date; run make openapi", path)
	}
	return nil
}
