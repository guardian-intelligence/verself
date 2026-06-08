package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

type siteFacts struct {
	path          string
	site          string
	productDomain string
	companyDomain string
	productZone   string
	companyZone   string
	publicIPv4    string
	dnsRecords    []siteFactsDNSRecord
}

type siteFactsDNSRecord struct {
	Kind   string `json:"kind"`
	Record string `json:"record"`
	Zone   string `json:"zone"`
}

func loadSiteFacts(repoRoot, site string) (siteFacts, error) {
	path := filepath.Join(repoRoot, "src", "guardian-specification", "examples", site, "facts.cue")
	body, err := os.ReadFile(path)
	if err != nil {
		return siteFacts{}, fmt.Errorf("read %s: %w", path, err)
	}
	ctx := cuecontext.New()
	value := ctx.CompileBytes(body, cue.Filename(path)).FillPath(cue.ParsePath("site"), site)
	if err := value.Validate(cue.Concrete(true)); err != nil {
		return siteFacts{}, fmt.Errorf("%s: site facts must be concrete: %w", path, err)
	}
	facts := siteFacts{
		path:          path,
		site:          requiredCueString(value, cue.ParsePath("site")),
		productDomain: requiredCueString(value, cue.ParsePath("domains.product")),
		companyDomain: requiredCueString(value, cue.ParsePath("domains.company")),
		productZone:   requiredCueString(value, cue.ParsePath("cloudflare.productZone")),
		companyZone:   requiredCueString(value, cue.ParsePath("cloudflare.companyZone")),
		publicIPv4:    optionalCueString(value, cue.ParsePath("bareMetal.publicIPv4")),
	}
	if err := factsRequired(value, path, map[string]cue.Path{
		"site":                   cue.ParsePath("site"),
		"domains.product":        cue.ParsePath("domains.product"),
		"domains.company":        cue.ParsePath("domains.company"),
		"cloudflare.productZone": cue.ParsePath("cloudflare.productZone"),
		"cloudflare.companyZone": cue.ParsePath("cloudflare.companyZone"),
	}); err != nil {
		return siteFacts{}, err
	}
	if facts.site != site {
		return siteFacts{}, fmt.Errorf("%s: site=%q does not match selected site %q", path, facts.site, site)
	}
	if err := value.LookupPath(cue.ParsePath("cloudflare.dnsRecords")).Decode(&facts.dnsRecords); err != nil {
		return siteFacts{}, fmt.Errorf("%s: cloudflare.dnsRecords must be concrete DNS records: %w", path, err)
	}
	return facts, nil
}

func requiredCueString(root cue.Value, path cue.Path) string {
	text, err := root.LookupPath(path).String()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

func optionalCueString(root cue.Value, path cue.Path) string {
	value := root.LookupPath(path)
	if !value.Exists() {
		return ""
	}
	text, err := value.String()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(text)
}

func factsRequired(root cue.Value, file string, required map[string]cue.Path) error {
	for label, path := range required {
		value := root.LookupPath(path)
		if !value.Exists() {
			return fmt.Errorf("%s: %s is required", file, label)
		}
		text, err := value.String()
		if err != nil {
			return fmt.Errorf("%s: %s must be a string: %w", file, label, err)
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Errorf("%s: %s is required", file, label)
		}
	}
	return nil
}
