package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/verself/deployment-tools/internal/sitebootstrap"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "site-bootstrap: "+err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: site-bootstrap bootstrap-deploy|seed-template|seed-validate|seed-materialize|inventory-write|root-handoff")
	}
	switch args[0] {
	case "bootstrap-deploy":
		return bootstrapDeploy(args[1:])
	case "seed-template":
		return seedTemplate(args[1:])
	case "seed-validate":
		return seedValidate(args[1:])
	case "seed-materialize":
		return seedMaterialize(args[1:])
	case "inventory-write":
		return inventoryWrite(args[1:])
	case "root-handoff":
		return rootHandoff(args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func bootstrapDeploy(args []string) error {
	fs := flag.NewFlagSet("bootstrap-deploy", flag.ContinueOnError)
	site := fs.String("site", "prod", "Deployment site.")
	sha := fs.String("sha", "", "Git SHA to bootstrap deploy.")
	repoRoot := fs.String("repo-root", "", "Repository root.")
	inventory := fs.String("inventory", "", "Site inventory path.")
	bootstrapVars := fs.String("bootstrap-vars-file", "", "Generated Ansible bootstrap vars path.")
	sshTransport := fs.String("ssh-transport", "recovery", "SSH transport for the Nomad tunnel: recovery or inventory.")
	r2ControlPlaneBinary := fs.String("r2-control-plane-binary", "", "Bazel-resolved cloudflare-r2-control-plane binary.")
	cloudflareBinary := fs.String("cloudflare-control-plane-binary", "", "Bazel-resolved cloudflare-control-plane binary.")
	timeout := fs.Duration("timeout", 15*time.Minute, "Bootstrap deploy timeout.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root := *repoRoot
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("resolve repo root: %w", err)
		}
	}
	inventoryPath := *inventory
	if inventoryPath == "" {
		inventoryPath = defaultInventoryPath(*site)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	return sitebootstrap.RunBootstrapDeploy(ctx, sitebootstrap.BootstrapDeployOptions{
		Site:                 *site,
		SHA:                  *sha,
		RepoRoot:             root,
		InventoryPath:        inventoryPath,
		BootstrapVarsPath:    *bootstrapVars,
		SSHTransport:         *sshTransport,
		R2ControlPlaneBinary: *r2ControlPlaneBinary,
		CloudflareBinary:     *cloudflareBinary,
		Timeout:              *timeout,
	})
}

func seedTemplate(args []string) error {
	fs := flag.NewFlagSet("seed-template", flag.ContinueOnError)
	site := fs.String("site", "prod", "Deployment site.")
	out := fs.String("out", "", "Seed template output path.")
	repoRoot := fs.String("repo-root", "", "Repository root for owner-local deployment declarations.")
	force := fs.Bool("force", false, "Overwrite an existing output file.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	output := *out
	if output == "" {
		output = defaultSeedBundlePath(*site)
	}
	if err := sitebootstrap.WriteSeedTemplate(sitebootstrap.SeedTemplateOptions{
		Site:       *site,
		OutputPath: output,
		RepoRoot:   *repoRoot,
		ForceWrite: *force,
	}); err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func seedValidate(args []string) error {
	fs := flag.NewFlagSet("seed-validate", flag.ContinueOnError)
	site := fs.String("site", "prod", "Deployment site.")
	seed := fs.String("seed-bundle", "", "Operator-provided seed bundle path.")
	repoRoot := fs.String("repo-root", "", "Repository root for owner-local deployment declarations.")
	format := fs.String("format", "text", "Output format: text or json.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	seedPath := *seed
	if seedPath == "" {
		seedPath = defaultSeedBundlePath(*site)
	}
	evidence, err := sitebootstrap.ValidateSeedBundle(*site, seedPath, *repoRoot)
	if err != nil {
		return err
	}
	return printEvidence(evidence, *format)
}

func seedMaterialize(args []string) error {
	fs := flag.NewFlagSet("seed-materialize", flag.ContinueOnError)
	site := fs.String("site", "prod", "Deployment site.")
	seed := fs.String("seed-bundle", "", "Operator-provided seed bundle path.")
	repoRoot := fs.String("repo-root", "", "Repository root for owner-local deployment declarations.")
	vars := fs.String("out-vars", "", "Ansible vars output path.")
	evidence := fs.String("out-evidence", "", "Fingerprint evidence output path.")
	force := fs.Bool("force", false, "Overwrite existing generated outputs.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	seedPath := *seed
	if seedPath == "" {
		seedPath = defaultSeedBundlePath(*site)
	}
	varsPath := *vars
	if varsPath == "" {
		varsPath = defaultVarsPath(*site)
	}
	evidencePath := *evidence
	if evidencePath == "" {
		evidencePath = defaultEvidencePath(*site)
	}
	report, err := sitebootstrap.MaterializeSeedBundle(sitebootstrap.MaterializeOptions{
		Site:       *site,
		SeedPath:   seedPath,
		VarsPath:   varsPath,
		Evidence:   evidencePath,
		RepoRoot:   *repoRoot,
		ForceWrite: *force,
	})
	if err != nil {
		return err
	}
	return printEvidence(report, "text")
}

func inventoryWrite(args []string) error {
	fs := flag.NewFlagSet("inventory-write", flag.ContinueOnError)
	site := fs.String("site", "prod", "Deployment site.")
	host := fs.String("host", "", "Public host IP address or DNS name.")
	alias := fs.String("host-alias", "", "Ansible host alias.")
	user := fs.String("user", "ubuntu", "Ansible SSH user.")
	out := fs.String("out", "", "Inventory output path.")
	force := fs.Bool("force", false, "Overwrite an existing inventory.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	output := *out
	if output == "" {
		output = defaultInventoryPath(*site)
	}
	if err := sitebootstrap.WriteInventory(sitebootstrap.InventoryOptions{
		Site:       *site,
		Host:       *host,
		HostAlias:  *alias,
		User:       *user,
		OutputPath: output,
		ForceWrite: *force,
	}); err != nil {
		return err
	}
	fmt.Println(output)
	return nil
}

func rootHandoff(args []string) error {
	fs := flag.NewFlagSet("root-handoff", flag.ContinueOnError)
	site := fs.String("site", "prod", "Deployment site.")
	host := fs.String("host", "", "Fresh host IP address or DNS name.")
	port := fs.Int("port", 22, "SSH port.")
	rootPassword := fs.String("root-password-file", "", "File containing the Latitude root password.")
	publicKey := fs.String("public-key-file", defaultHomePath(".ssh/id_ed25519.pub"), "Operator SSH public key.")
	privateKey := fs.String("private-key-file", defaultHomePath(".ssh/id_ed25519"), "Operator SSH private key matching --public-key-file.")
	user := fs.String("user", "ubuntu", "Bootstrap SSH user to create.")
	alias := fs.String("host-alias", "", "Ansible host alias.")
	inventory := fs.String("inventory", "", "Inventory output path.")
	hostKey := fs.String("host-key-sha256", "", "Expected server host key SHA256 fingerprint.")
	trustFirstUse := fs.Bool("trust-first-use", false, "Allow first connection without an expected host key fingerprint.")
	timeout := fs.Duration("timeout", 30*time.Second, "SSH connection timeout.")
	noInventory := fs.Bool("no-inventory", false, "Do not write the site inventory.")
	forceInventory := fs.Bool("force-inventory", false, "Overwrite an existing inventory file.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	inventoryPath := *inventory
	if inventoryPath == "" {
		inventoryPath = defaultInventoryPath(*site)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	return sitebootstrap.RunRootHandoff(ctx, sitebootstrap.RootHandoffOptions{
		Site:               *site,
		Host:               *host,
		Port:               *port,
		RootPasswordFile:   *rootPassword,
		PublicKeyFile:      *publicKey,
		PrivateKeyFile:     *privateKey,
		BootstrapUser:      *user,
		HostAlias:          *alias,
		InventoryPath:      inventoryPath,
		HostKeySHA256:      *hostKey,
		TrustFirstUse:      *trustFirstUse,
		Timeout:            *timeout,
		WriteInventoryFile: !*noInventory,
		ForceInventory:     *forceInventory,
	})
}

func printEvidence(evidence sitebootstrap.Evidence, format string) error {
	switch format {
	case "json":
		body, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			return fmt.Errorf("encode evidence: %w", err)
		}
		fmt.Println(string(body))
	case "text":
		fmt.Printf("validated site bootstrap seed: site=%s values=%d\n", evidence.Site, len(evidence.Values))
	default:
		return fmt.Errorf("--format must be text or json")
	}
	return nil
}

func defaultSeedBundlePath(site string) string {
	return filepath.Join(".verself", "site-bootstrap", site, "seed.yml")
}

func defaultVarsPath(site string) string {
	return filepath.Join(".verself", "site-bootstrap", site, "bootstrap-vars.json")
}

func defaultEvidencePath(site string) string {
	return filepath.Join(".verself", "site-bootstrap", site, "seed-fingerprints.json")
}

func defaultInventoryPath(site string) string {
	return filepath.Join("src", "host", "sites", site, "inventory.ini")
}

func defaultHomePath(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return rel
	}
	return filepath.Join(home, rel)
}
