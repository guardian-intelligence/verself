package githubintegration

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestGithubProblemCatalogMatchesSmithy(t *testing.T) {
	raw, err := readGithubIntegrationSmithy()
	if err != nil {
		t.Fatal(err)
	}

	smithyCatalog, err := parseSmithyProblemCatalog(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(smithyCatalog) == 0 {
		t.Fatal("Smithy GithubIntegrationProblemCode catalog is empty")
	}
	if len(githubProblemCatalog) != len(smithyCatalog) {
		t.Fatalf("runtime catalog has %d codes, Smithy catalog has %d codes", len(githubProblemCatalog), len(smithyCatalog))
	}

	for code, want := range smithyCatalog {
		got, ok := githubProblemCatalog[code]
		if !ok {
			t.Fatalf("runtime catalog missing Smithy problem code %q", code)
		}
		if got.Type != want.Type {
			t.Fatalf("%s type = %q, want %q", code, got.Type, want.Type)
		}
		if got.Title != want.Title {
			t.Fatalf("%s title = %q, want %q", code, got.Title, want.Title)
		}
		if got.Detail != want.Detail {
			t.Fatalf("%s detail = %q, want %q", code, got.Detail, want.Detail)
		}
		if got.Status != want.Status {
			t.Fatalf("%s status = %d, want %d", code, got.Status, want.Status)
		}
		if got.Phase != want.Phase {
			t.Fatalf("%s phase = %q, want %q", code, got.Phase, want.Phase)
		}
		if got.Retryable != want.Retryable {
			t.Fatalf("%s retryable = %t, want %t", code, got.Retryable, want.Retryable)
		}
		if got.DocsURL != want.DocsURL {
			t.Fatalf("%s docs URL = %q, want %q", code, got.DocsURL, want.DocsURL)
		}
		if got.DocsURL != githubProblemDocsURL(code) {
			t.Fatalf("%s docs URL = %q, want derived URL %q", code, got.DocsURL, githubProblemDocsURL(code))
		}
	}

	for code := range githubProblemCatalog {
		if _, ok := smithyCatalog[code]; !ok {
			t.Fatalf("runtime catalog contains code %q not modeled in Smithy", code)
		}
	}
}

func readGithubIntegrationSmithy() ([]byte, error) {
	for _, path := range []string{
		"src/smithy/models/verself/github_integration.smithy",
		"_main/src/smithy/models/verself/github_integration.smithy",
		"../../../../smithy/models/verself/github_integration.smithy",
	} {
		if root := strings.TrimSpace(os.Getenv("TEST_SRCDIR")); root != "" {
			path = root + "/" + path
		}
		if raw, err := os.ReadFile(path); err == nil {
			return raw, nil
		}
	}
	return nil, fmt.Errorf("read github_integration.smithy from runfiles or source tree")
}

func parseSmithyProblemCatalog(model string) (map[githubProblemCode]githubProblemDefinition, error) {
	traitRE := regexp.MustCompile(`(?s)@githubIntegrationProblem\((.*?)\)\s+[A-Z0-9_]+\s*=\s*"([^"]+)"`)
	fieldRE := regexp.MustCompile(`([a-z_]+):\s*("[^"]*"|[0-9]+|true|false)`)
	matches := traitRE.FindAllStringSubmatch(model, -1)
	catalog := make(map[githubProblemCode]githubProblemDefinition, len(matches))
	for _, match := range matches {
		code := githubProblemCode(match[2])
		fields := map[string]string{}
		for _, field := range fieldRE.FindAllStringSubmatch(match[1], -1) {
			fields[field[1]] = strings.Trim(field[2], `"`)
		}
		definition, err := smithyProblemDefinition(code, fields)
		if err != nil {
			return nil, err
		}
		catalog[code] = definition
	}
	return catalog, nil
}

func smithyProblemDefinition(code githubProblemCode, fields map[string]string) (githubProblemDefinition, error) {
	required := []string{"type", "title", "status", "phase", "retryable", "detail", "docs_url"}
	for _, field := range required {
		if strings.TrimSpace(fields[field]) == "" && field != "status" {
			return githubProblemDefinition{}, fmt.Errorf("%s missing Smithy problem metadata field %q", code, field)
		}
	}
	status, err := strconv.ParseInt(fields["status"], 10, 32)
	if err != nil {
		return githubProblemDefinition{}, fmt.Errorf("%s invalid Smithy status %q: %w", code, fields["status"], err)
	}
	retryable, err := strconv.ParseBool(fields["retryable"])
	if err != nil {
		return githubProblemDefinition{}, fmt.Errorf("%s invalid Smithy retryable %q: %w", code, fields["retryable"], err)
	}
	return githubProblemDefinition{
		Type:      fields["type"],
		Code:      code,
		Title:     fields["title"],
		Detail:    fields["detail"],
		Status:    int32(status),
		Phase:     fields["phase"],
		Retryable: retryable,
		DocsURL:   fields["docs_url"],
	}, nil
}
