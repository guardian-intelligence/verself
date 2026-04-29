// openapi-wire-check exits non-zero on any apiwire numeric-safety
// violation in the OpenAPI 3.1 specs passed as arguments. Phase 4
// retires this binary; in the meantime aspect codegen check still uses
// it for services not yet covered by a per-service Bazel test.
package main

import (
	"fmt"
	"os"

	"github.com/verself/apiwire/wirecheck"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: openapi-wire-check <openapi-3.1.yaml>...")
		os.Exit(2)
	}

	var violations []wirecheck.Violation
	for _, path := range os.Args[1:] {
		found, err := wirecheck.CheckFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", path, err)
			os.Exit(2)
		}
		violations = append(violations, found...)
	}

	if len(violations) == 0 {
		return
	}
	for _, v := range violations {
		fmt.Fprintln(os.Stderr, v)
	}
	os.Exit(1)
}
