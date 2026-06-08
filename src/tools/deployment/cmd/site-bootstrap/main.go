package main

import (
	"context"
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
		return fmt.Errorf("usage: site-bootstrap bootstrap-deploy|inventory-write|root-handoff")
	}
	switch args[0] {
	case "bootstrap-deploy":
		return bootstrapDeploy(args[1:])
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
	sshTransport := fs.String("ssh-transport", "recovery", "SSH transport for the Nomad tunnel: recovery or inventory.")
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
		Site:          *site,
		SHA:           *sha,
		RepoRoot:      root,
		InventoryPath: inventoryPath,
		SSHTransport:  *sshTransport,
		Timeout:       *timeout,
	})
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

func defaultInventoryPath(site string) string {
	return filepath.Join("src", "sites", site, "inventory.ini")
}

func defaultHomePath(rel string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return rel
	}
	return filepath.Join(home, rel)
}
