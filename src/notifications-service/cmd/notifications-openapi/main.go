package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/forge-metal/notifications-service/internal/api"
)

func main() {
	var format string
	var check bool
	flag.StringVar(&format, "format", "3.1", "OpenAPI version to emit: 3.0 or 3.1")
	flag.BoolVar(&check, "check", false, "verify committed spec is up to date")
	flag.Parse()

	var (
		data []byte
		err  error
	)
	switch format {
	case "3.0":
		data, err = api.OpenAPIDowngradeYAML("1.0.0", "https://notifications.api.anveio.com")
	case "3.1":
		data, err = api.OpenAPIYAML("1.0.0", "https://notifications.api.anveio.com")
	default:
		err = fmt.Errorf("unsupported format %q", format)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !bytes.HasSuffix(data, []byte("\n")) {
		data = append(data, '\n')
	}
	if !check {
		_, _ = os.Stdout.Write(data)
		return
	}
	path := filepath.Join("openapi", "openapi-"+format+".yaml")
	current, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if !bytes.Equal(current, data) {
		fmt.Fprintf(os.Stderr, "%s is out of date; run make openapi\n", path)
		os.Exit(1)
	}
}
