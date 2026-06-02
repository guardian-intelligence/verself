// Command reconcile-cloudflare-dns drives Cloudflare zone state to match the
// site-owned DNS record inventory.
//
// This binary makes one list call per zone, diffs, and applies in parallel.
//
// The reconciler is a fast idempotent diff/apply. It is run explicitly through
// `aspect integrations cloudflare-dns` from the prod Cloudflare control plane
// when site DNS inputs change.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "reconcile-cloudflare-dns: "+err.Error())
		os.Exit(1)
	}
}

type config struct {
	site        string
	ansibleDir  string
	inventory   string
	secretEnv   string
	tokenFile   string
	adminSlot   string
	timeout     time.Duration
	concurrency int
	dryRun      bool
}

func run(args []string) error {
	fs := flag.NewFlagSet("reconcile-cloudflare-dns", flag.ContinueOnError)
	cfg := config{}
	fs.StringVar(&cfg.site, "site", "prod", "Deployment site.")
	fs.StringVar(&cfg.ansibleDir, "ansible-dir", "", "Path to authored host Ansible root (defaults to src/host/ansible).")
	fs.StringVar(&cfg.inventory, "inventory", "", "Path to the site Ansible inventory (defaults to src/host/sites/<site>/inventory.ini).")
	fs.StringVar(&cfg.secretEnv, "secret-env-file", "", "Ingress-only env file containing the prod Cloudflare account-admin pair.")
	fs.StringVar(&cfg.tokenFile, "account-admin-token-file", "", "File containing one prod Cloudflare account-admin token.")
	fs.StringVar(&cfg.adminSlot, "account-admin-slot", "a", "Account-admin slot to read from --secret-env-file: a or b.")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Total timeout for the Cloudflare API.")
	fs.IntVar(&cfg.concurrency, "concurrency", 8, "Maximum parallel Cloudflare write requests.")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Print the diff without applying.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.ansibleDir == "" {
		cfg.ansibleDir = "src/host/ansible"
	}
	if cfg.inventory == "" {
		cfg.inventory = filepath.Clean(filepath.Join(cfg.ansibleDir, "..", "sites", cfg.site, "inventory.ini"))
	}
	desired, err := loadDesired(cfg.ansibleDir, cfg.site, cfg.inventory)
	if err != nil {
		return err
	}
	token, err := loadCloudflareToken(cfg)
	if err != nil {
		return fmt.Errorf("load Cloudflare account-admin token: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	cf := newCloudflareClient(token)
	zones, err := cf.zonesByName(ctx, desired.zoneNames())
	if err != nil {
		return fmt.Errorf("list cloudflare zones: %w", err)
	}

	type planEntry struct {
		zoneID  string
		desired desiredRecord
		actual  *cfDNSRecord
	}
	plan := []planEntry{}
	for zoneName, zoneID := range zones {
		actual, err := cf.listARecords(ctx, zoneID)
		if err != nil {
			return fmt.Errorf("list A records for zone %s: %w", zoneName, err)
		}
		actualByName := map[string]cfDNSRecord{}
		for _, rec := range actual {
			actualByName[rec.Name] = rec
		}
		for _, want := range desired.byZone(zoneName) {
			if cur, ok := actualByName[want.fqdn]; ok {
				cp := cur
				plan = append(plan, planEntry{zoneID: zoneID, desired: want, actual: &cp})
			} else {
				plan = append(plan, planEntry{zoneID: zoneID, desired: want, actual: nil})
			}
		}
	}

	stats := struct {
		seen    int
		diffed  int
		applied int
	}{seen: len(plan)}

	type writeJob struct {
		entry planEntry
		op    string // "create" or "update"
	}
	var jobs []writeJob
	for _, p := range plan {
		if p.actual == nil {
			jobs = append(jobs, writeJob{entry: p, op: "create"})
			continue
		}
		if p.actual.Content == p.desired.targetIP &&
			p.actual.TTL == p.desired.ttl &&
			p.actual.Proxied == p.desired.proxied {
			continue // matches desired
		}
		jobs = append(jobs, writeJob{entry: p, op: "update"})
	}
	stats.diffed = len(jobs)

	if cfg.dryRun {
		for _, j := range jobs {
			fmt.Printf("[dry-run] %s %s.%s → %s (ttl=%d proxied=%v)\n",
				j.op, j.entry.desired.record, j.entry.desired.zoneName,
				j.entry.desired.targetIP, j.entry.desired.ttl, j.entry.desired.proxied)
		}
		fmt.Printf("seen=%d diffed=%d applied=0 (dry-run)\n", stats.seen, stats.diffed)
		return nil
	}

	// Parallel apply, bounded by concurrency.
	sem := make(chan struct{}, cfg.concurrency)
	var (
		wg        sync.WaitGroup
		applyErr  error
		errMu     sync.Mutex
		applied   int
		appliedMu sync.Mutex
	)
	for _, j := range jobs {
		j := j
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			var err error
			switch j.op {
			case "create":
				err = cf.createARecord(ctx, j.entry.zoneID, j.entry.desired)
			case "update":
				err = cf.updateARecord(ctx, j.entry.zoneID, j.entry.actual.ID, j.entry.desired)
			}
			if err != nil {
				errMu.Lock()
				applyErr = errors.Join(applyErr, fmt.Errorf("%s %s: %w", j.op, j.entry.desired.fqdn, err))
				errMu.Unlock()
				return
			}
			appliedMu.Lock()
			applied++
			appliedMu.Unlock()
		}()
	}
	wg.Wait()
	stats.applied = applied

	fmt.Printf("seen=%d diffed=%d applied=%d\n", stats.seen, stats.diffed, stats.applied)
	if applyErr != nil {
		return applyErr
	}
	return nil
}

// ---- desired-state loading -------------------------------------------------

type desiredRecord struct {
	zoneName string // Cloudflare hosted zone, for example verself.sh.
	record   string // Name relative to the hosted zone, for example deployments.api.gamma.
	fqdn     string // Public DNS name, for example deployments.api.gamma.verself.sh.
	targetIP string // inventory/site public IP
	ttl      int    // 1 = Cloudflare automatic
	proxied  bool
}

type desiredState struct {
	records []desiredRecord
}

func (d *desiredState) zoneNames() []string {
	seen := map[string]struct{}{}
	for _, r := range d.records {
		seen[r.zoneName] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (d *desiredState) byZone(zone string) []desiredRecord {
	out := []desiredRecord{}
	for _, r := range d.records {
		if r.zoneName == zone {
			out = append(out, r)
		}
	}
	return out
}

// loadDesired reads site vars that drive Cloudflare DNS shape.
// The public IP follows site inventory so reprovisioning cannot strand DNS on a
// previous server.
func loadDesired(ansibleDir, site, inventoryPath string) (*desiredState, error) {
	siteVarsPath := filepath.Clean(filepath.Join(ansibleDir, "..", "sites", site, "vars.yml"))

	var siteVars struct {
		VerselfDomain         string `yaml:"verself_domain"`
		CompanyDomain         string `yaml:"company_domain"`
		CloudflareProductZone string `yaml:"cloudflare_product_zone"`
		CloudflareCompanyZone string `yaml:"cloudflare_company_zone"`
		BareMetalPublicIPv4   string `yaml:"bare_metal_public_ipv4"`
		Records               []struct {
			Kind   string `yaml:"kind"`
			Record string `yaml:"record"`
			Zone   string `yaml:"zone"` // "product" | "company"
		} `yaml:"cloudflare_dns_records"`
	}
	if err := readYAML(siteVarsPath, &siteVars); err != nil {
		return nil, fmt.Errorf("read %s: %w", siteVarsPath, err)
	}
	verself := strings.TrimSpace(siteVars.VerselfDomain)
	company := strings.TrimSpace(siteVars.CompanyDomain)
	publicIP := strings.TrimSpace(siteVars.BareMetalPublicIPv4)
	if publicIP == "" || publicIP == "0.0.0.0" {
		var err error
		publicIP, err = inventoryInfraHost(inventoryPath)
		if err != nil {
			return nil, err
		}
	}
	if verself == "" || company == "" || publicIP == "" {
		return nil, fmt.Errorf("missing verself_domain / company_domain / inventory public IP")
	}
	productZone := firstNonEmpty(siteVars.CloudflareProductZone, verself)
	companyZone := firstNonEmpty(siteVars.CloudflareCompanyZone, company)

	out := &desiredState{}
	seen := map[string]struct{}{}
	for _, r := range siteVars.Records {
		var publicDomain string
		var hostedZone string
		switch r.Zone {
		case "product":
			publicDomain = verself
			hostedZone = productZone
		case "company":
			publicDomain = company
			hostedZone = companyZone
		default:
			return nil, fmt.Errorf("unknown cloudflare_dns_records[].zone: %q", r.Zone)
		}
		fqdn := publicFQDN(publicDomain, r.Record)
		record, err := recordNameForHostedZone(fqdn, hostedZone)
		if err != nil {
			return nil, err
		}
		key := hostedZone + "|" + fqdn
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out.records = append(out.records, desiredRecord{
			zoneName: hostedZone,
			record:   record,
			fqdn:     fqdn,
			targetIP: publicIP,
			ttl:      1,
			proxied:  false,
		})
	}
	return out, nil
}

func publicFQDN(publicDomain, record string) string {
	publicDomain = strings.Trim(strings.TrimSpace(publicDomain), ".")
	record = strings.Trim(strings.TrimSpace(record), ".")
	if record == "" || record == "@" {
		return publicDomain
	}
	return record + "." + publicDomain
}

func recordNameForHostedZone(fqdn, hostedZone string) (string, error) {
	fqdn = strings.Trim(strings.TrimSpace(fqdn), ".")
	hostedZone = strings.Trim(strings.TrimSpace(hostedZone), ".")
	if hostedZone == "" || strings.Contains(hostedZone, "{{") {
		return "", fmt.Errorf("invalid Cloudflare hosted zone %q", hostedZone)
	}
	if fqdn == hostedZone {
		return "@", nil
	}
	suffix := "." + hostedZone
	if !strings.HasSuffix(fqdn, suffix) {
		return "", fmt.Errorf("DNS name %s is not inside Cloudflare hosted zone %s", fqdn, hostedZone)
	}
	return strings.TrimSuffix(fqdn, suffix), nil
}

func inventoryInfraHost(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open inventory %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	section := ""
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "infra" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		host := fields[0]
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if ok && key == "ansible_host" {
				host = value
				break
			}
		}
		if host == "" {
			return "", fmt.Errorf("inventory %s has an empty [infra] host", path)
		}
		return host, nil
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read inventory %s: %w", path, err)
	}
	return "", fmt.Errorf("inventory %s has no [infra] host", path)
}

func readYAML(path string, into any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, into)
}

func loadCloudflareToken(cfg config) (string, error) {
	sources := 0
	for _, value := range []string{cfg.secretEnv, cfg.tokenFile} {
		if strings.TrimSpace(value) != "" {
			sources++
		}
	}
	if sources != 1 {
		return "", fmt.Errorf("declare exactly one token source: --secret-env-file or --account-admin-token-file")
	}
	switch {
	case cfg.secretEnv != "":
		return readAccountAdminTokenFromSecretEnv(cfg.secretEnv, cfg.adminSlot)
	default:
		return readPlainToken(cfg.tokenFile)
	}
}

func readAccountAdminTokenFromSecretEnv(path, slot string) (string, error) {
	slot = strings.TrimSpace(slot)
	switch slot {
	case "a", "b":
	default:
		return "", fmt.Errorf("--account-admin-slot must be a or b")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	values, err := parseEnvFile(body)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}
	token := strings.TrimSpace(values["account-admin-"+slot])
	if token == "" {
		return "", fmt.Errorf("%s has no account-admin-%s", path, slot)
	}
	return token, nil
}

func parseEnvFile(body []byte) (map[string]string, error) {
	out := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %q is missing '='", scanner.Text())
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %q has an empty key", scanner.Text())
		}
		out[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func readPlainToken(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", fmt.Errorf("%s is empty", path)
	}
	return token, nil
}

// ---- helpers used by main / cf client ------------------------------------

func encodeQuery(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	return "?" + values.Encode()
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
