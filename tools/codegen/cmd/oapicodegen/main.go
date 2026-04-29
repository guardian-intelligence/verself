// oapicodegen is the Bazel-friendly entry point for verself OpenAPI Go
// client generation. The upstream `oapi-codegen` binary shells out to
// the Go toolchain at format time, which the Bazel action sandbox does
// not provide on PATH; we drive the codegen library in process and
// then strip oapi-codegen's union of all-template imports down to the
// ones the emitted source actually references using go/ast — keeping
// the OAPICodegen action self-contained, deterministic, and cacheable.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"

	"github.com/oapi-codegen/oapi-codegen/v2/pkg/codegen"
	"github.com/oapi-codegen/oapi-codegen/v2/pkg/util"
)

func main() {
	spec := flag.String("spec", "", "Path to the OpenAPI YAML spec (required)")
	pkg := flag.String("package", "", "Go package name for the generated file (required)")
	out := flag.String("output", "", "Path to write the generated Go source (required)")
	flag.Parse()

	if *spec == "" || *pkg == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: oapicodegen --spec=<yaml> --package=<name> --output=<go>")
		os.Exit(2)
	}

	swagger, err := util.LoadSwagger(*spec)
	if err != nil {
		fail("load spec %s: %v", *spec, err)
	}

	cfg := codegen.Configuration{
		PackageName: *pkg,
		Generate: codegen.GenerateOptions{
			Models: true,
			Client: true,
		},
		OutputOptions: codegen.OutputOptions{
			SkipFmt: true,
		},
	}.UpdateDefaults()

	if err := cfg.Validate(); err != nil {
		fail("validate config: %v", err)
	}

	src, err := codegen.Generate(swagger, cfg)
	if err != nil {
		fail("generate: %v", err)
	}

	formatted, err := pruneAndFormat(*out, []byte(src))
	if err != nil {
		fail("format: %v", err)
	}

	if err := os.WriteFile(*out, formatted, 0o644); err != nil {
		fail("write %s: %v", *out, err)
	}
}

// pruneAndFormat parses src as Go, removes any import not referenced by
// an identifier in the file body, and runs go/format. Equivalent to the
// goimports prune step but pure-AST so we never need `go list`.
func pruneAndFormat(filename string, src []byte) ([]byte, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, src, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	used := map[string]bool{}
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok {
			return true
		}
		used[id.Name] = true
		return true
	})

	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.IMPORT {
			continue
		}
		kept := gen.Specs[:0]
		for _, spec := range gen.Specs {
			imp := spec.(*ast.ImportSpec)
			if importIsUsed(imp, used) {
				kept = append(kept, spec)
			}
		}
		gen.Specs = kept
		// An empty import block must be removed: otherwise go/format
		// emits a bare `import ()` that goimports would have stripped.
		if len(gen.Specs) == 0 {
			gen.Lparen = token.NoPos
			gen.Rparen = token.NoPos
		}
	}

	// Drop any GenDecl that is now an empty import block.
	live := file.Decls[:0]
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if ok && gen.Tok == token.IMPORT && len(gen.Specs) == 0 {
			continue
		}
		live = append(live, decl)
	}
	file.Decls = live

	var buf bytes.Buffer
	if err := format.Node(&buf, fset, file); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func importIsUsed(imp *ast.ImportSpec, used map[string]bool) bool {
	if imp.Name != nil {
		switch imp.Name.Name {
		case "_", ".":
			// Side-effect / dot imports are not referenced by name; we
			// trust oapi-codegen not to emit either spuriously.
			return true
		default:
			return used[imp.Name.Name]
		}
	}
	path, err := strconv.Unquote(imp.Path.Value)
	if err != nil {
		return true
	}
	return used[defaultImportName(path)]
}

// defaultImportName returns the package name a Go import declaration
// implicitly uses when no rename is provided. It handles two
// well-known suffix conventions oapi-codegen output exercises:
// SemVer major suffixes (.../v2) and the gopkg.in (.../foo.v2) idiom.
func defaultImportName(path string) string {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(last) >= 2 && last[0] == 'v' {
		if _, err := strconv.Atoi(last[1:]); err == nil && len(parts) >= 2 {
			last = parts[len(parts)-2]
		}
	}
	if idx := strings.Index(last, "."); idx > 0 {
		last = last[:idx]
	}
	return last
}

func fail(formatStr string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, formatStr+"\n", args...)
	os.Exit(1)
}
