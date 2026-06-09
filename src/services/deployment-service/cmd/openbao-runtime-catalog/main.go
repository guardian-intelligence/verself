package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/verself/deployment-service/deploycontract"
)

func main() {
	var out string
	fs := flag.NewFlagSet("openbao-runtime-catalog", flag.ExitOnError)
	fs.StringVar(&out, "out", "", "output JSON file")
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "openbao-runtime-catalog: %v\n", err)
		os.Exit(2)
	}
	if out == "" {
		fmt.Fprintln(os.Stderr, "openbao-runtime-catalog: --out is required")
		os.Exit(2)
	}
	catalog, err := deploycontract.OpenBaoRuntimeCatalogFromFiles(fs.Args())
	if err != nil {
		fmt.Fprintf(os.Stderr, "openbao-runtime-catalog: %v\n", err)
		os.Exit(1)
	}
	body, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "openbao-runtime-catalog: encode catalog: %v\n", err)
		os.Exit(1)
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "openbao-runtime-catalog: create output dir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "openbao-runtime-catalog: write %s: %v\n", out, err)
		os.Exit(1)
	}
}
