// Command reconcile-cloudflare-dns drives Cloudflare zone state to match the
// authored substrate topology.
//
// Replaces the cloudflare_dns Ansible role, which sequentially called
// community.general.cloudflare_dns once per record (~870ms each, ~40s total
// for the prod record set). This binary makes one list call per zone, diffs,
// and applies in parallel — typical wall time is under one second.
//
// The reconciler is a fast idempotent diff/apply, so it has no input-hash
// gate; aspect deploy invokes it on every converge. The ClickHouse ledger
// row in verself.reconciler_runs is the verifiable artifact.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"sort"
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
	secretsPath string
	timeout     time.Duration
	concurrency int
	dryRun      bool
}

func run(args []string) error {
	fs := flag.NewFlagSet("reconcile-cloudflare-dns", flag.ContinueOnError)
	cfg := config{}
	fs.StringVar(&cfg.site, "site", "prod", "Deployment site.")
	fs.StringVar(&cfg.ansibleDir, "ansible-dir", "", "Path to authored host-configuration Ansible root (defaults to src/host-configuration/ansible).")
	fs.StringVar(&cfg.secretsPath, "secrets", "", "Path to SOPS-encrypted secrets.yml (defaults to <ansible-dir>/group_vars/all/secrets.sops.yml).")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "Total timeout for the Cloudflare API.")
	fs.IntVar(&cfg.concurrency, "concurrency", 8, "Maximum parallel Cloudflare write requests.")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Print the diff without applying.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.ansibleDir == "" {
		cfg.ansibleDir = "src/host-configuration/ansible"
	}
	if cfg.secretsPath == "" {
		cfg.secretsPath = cfg.ansibleDir + "/group_vars/all/secrets.sops.yml"
	}

	desired, err := loadDesired(cfg.ansibleDir)
	if err != nil {
		return err
	}
	token, err := decryptSopsToken(cfg.secretsPath)
	if err != nil {
		return fmt.Errorf("decrypt cloudflare_api_token: %w", err)
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
	zoneName string // verself.sh / guardianintelligence.org
	record   string // "@" or "billing.api"
	fqdn     string // record + "." + zoneName, or zoneName when record == "@"
	targetIP string // bare_metal_public_ipv4
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

// loadDesired reads authored substrate vars that drive Cloudflare DNS shape:
// generated/dns.yml (topology_dns_records) and generated/ops.yml
// (verself_domain, company_domain, bare_metal_public_ipv4).
func loadDesired(ansibleDir string) (*desiredState, error) {
	dnsPath := ansibleDir + "/group_vars/all/generated/dns.yml"
	opsPath := ansibleDir + "/group_vars/all/generated/ops.yml"

	var dnsDoc struct {
		Records []struct {
			Kind   string `yaml:"kind"`
			Record string `yaml:"record"`
			Zone   string `yaml:"zone"` // "product" | "company"
		} `yaml:"topology_dns_records"`
	}
	if err := readYAML(dnsPath, &dnsDoc); err != nil {
		return nil, fmt.Errorf("read %s: %w", dnsPath, err)
	}
	var opsDoc map[string]any
	if err := readYAML(opsPath, &opsDoc); err != nil {
		return nil, fmt.Errorf("read %s: %w", opsPath, err)
	}
	verself, _ := opsDoc["verself_domain"].(string)
	company, _ := opsDoc["company_domain"].(string)
	publicIP, _ := opsDoc["bare_metal_public_ipv4"].(string)
	if verself == "" || company == "" || publicIP == "" {
		return nil, fmt.Errorf("missing verself_domain / company_domain / bare_metal_public_ipv4 in %s", opsPath)
	}

	out := &desiredState{}
	seen := map[string]struct{}{}
	for _, r := range dnsDoc.Records {
		var zone string
		switch r.Zone {
		case "product":
			zone = verself
		case "company":
			zone = company
		default:
			return nil, fmt.Errorf("unknown topology_dns_records[].zone: %q", r.Zone)
		}
		fqdn := zone
		if r.Record != "@" {
			fqdn = r.Record + "." + zone
		}
		key := zone + "|" + fqdn
		if _, dup := seen[key]; dup {
			continue // ansible role used `topology_dns_records | unique`
		}
		seen[key] = struct{}{}
		out.records = append(out.records, desiredRecord{
			zoneName: zone,
			record:   r.Record,
			fqdn:     fqdn,
			targetIP: publicIP,
			ttl:      1,
			proxied:  false,
		})
	}
	return out, nil
}

func readYAML(path string, into any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, into)
}

// decryptSopsToken shells out to `sops -d --extract '["cloudflare_api_token"]'`
// to read the single ciphertext we care about; doing it ourselves would
// require linking against the SOPS Go library and decrypting age/PGP/KMS
// recipients we don't manage here.
func decryptSopsToken(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("stat %s: %w", path, err)
	}
	cmd := exec.Command("sops", "-d", "--extract", `["cloudflare_api_token"]`, path)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("sops -d %s: %w (%s)", path, err, string(ee.Stderr))
		}
		return "", fmt.Errorf("sops -d %s: %w", path, err)
	}
	tok := string(out)
	for len(tok) > 0 && (tok[len(tok)-1] == '\n' || tok[len(tok)-1] == '\r') {
		tok = tok[:len(tok)-1]
	}
	if tok == "" {
		return "", fmt.Errorf("sops -d %s returned empty value for cloudflare_api_token", path)
	}
	return tok, nil
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
