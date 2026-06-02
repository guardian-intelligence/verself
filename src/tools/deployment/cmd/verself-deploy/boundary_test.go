package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestDeployClientHasNoBootstrapOrRuntimeAuthority(t *testing.T) {
	for _, name := range []string{
		"main.go",
		"run.go",
		"check.go",
		"status.go",
		"validate.go",
	} {
		src, err := os.ReadFile(deployClientSourcePath(t, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := assertDeployClientBoundary(name, string(src)); err != nil {
			t.Fatal(err)
		}
	}
}

func deployClientSourcePath(t *testing.T, name string) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if ok {
		candidate := filepath.Join(filepath.Dir(current), name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	for _, base := range []string{
		".",
		filepath.Join("src", "tools", "deployment", "cmd", "verself-deploy"),
		filepath.Join(os.Getenv("TEST_SRCDIR"), "_main", "src", "tools", "deployment", "cmd", "verself-deploy"),
		filepath.Join(os.Getenv("TEST_SRCDIR"), "verself-sh", "src", "tools", "deployment", "cmd", "verself-deploy"),
	} {
		if base == "" {
			continue
		}
		candidate := filepath.Join(base, name)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	t.Fatalf("could not locate deploy client source file %s", name)
	return ""
}

func assertDeployClientBoundary(name string, src string) error {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, name, src, parser.ParseComments)
	if err != nil {
		return err
	}
	for _, imported := range file.Imports {
		path, err := strconv.Unquote(imported.Path.Value)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{
			"golang.org/x/crypto/ssh",
			"os/exec",
			"github.com/verself/deployment-tools/internal/sitebootstrap",
			"github.com/verself/deployment-tools/internal/sshtun",
			"github.com/verself/deployment-tools/internal/nomadclient",
			"github.com/verself/deployment-tools/internal/runtime",
			"github.com/verself/deployment-tools/deployengine",
		} {
			if path == forbidden || strings.HasPrefix(path, forbidden+"/") {
				return boundaryError(name, imported.Pos(), fset, "forbidden import "+path)
			}
		}
	}
	for _, needle := range []string{
		"BAO_TOKEN",
		"BAO_ADDR",
		"VAULT_TOKEN",
		"VAULT_ADDR",
		"NOMAD_ADDR",
		"VERSELF_NOMAD_ARTIFACTS_R2_ACCESS_KEY_ID",
		"VERSELF_NOMAD_ARTIFACTS_R2_SECRET_ACCESS_KEY",
		"sitebootstrap",
		"bootstrap-deploy",
	} {
		if strings.Contains(src, needle) {
			return boundaryError(name, token.Pos(strings.Index(src, needle)+1), fset, "forbidden deploy-client authority reference "+needle)
		}
	}
	return nil
}

func boundaryError(name string, pos token.Pos, fset *token.FileSet, msg string) error {
	location := fset.Position(pos)
	if location.Filename == "" {
		location.Filename = filepath.Base(name)
	}
	return &deployClientBoundaryError{location: location.String(), msg: msg}
}

type deployClientBoundaryError struct {
	location string
	msg      string
}

func (e *deployClientBoundaryError) Error() string {
	return e.location + ": " + e.msg
}

func TestDeployClientBoundaryGuardCatchesForbiddenImport(t *testing.T) {
	err := assertDeployClientBoundary("bad.go", `package main

import "os/exec"
`)
	if err == nil || !strings.Contains(err.Error(), "forbidden import os/exec") {
		t.Fatalf("boundary guard error = %v", err)
	}
}

func TestDeployClientBoundaryGuardCatchesForbiddenEnv(t *testing.T) {
	err := assertDeployClientBoundary("bad.go", `package main

const token = "BAO_TOKEN"
`)
	if err == nil || !strings.Contains(err.Error(), "BAO_TOKEN") {
		t.Fatalf("boundary guard error = %v", err)
	}
}
