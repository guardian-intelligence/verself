package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/verself/deployment-tools/internal/deploycontract"
)

func main() {
	repoRoot := flag.String("repo-root", "", "verself-sh checkout root")
	format := flag.String("format", "text", "output format: text or json")
	flag.Parse()

	root := *repoRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "deploy-contract-validate: cwd: %v\n", err)
			os.Exit(1)
		}
		root = cwd
	}
	report, err := deploycontract.ValidateRepo(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "deploy-contract-validate: %v\n", err)
		os.Exit(1)
	}
	switch *format {
	case "text":
		fmt.Printf(
			"validated deployment contracts: deploy_files=%d integration_files=%d postgres_files=%d runtime_secrets=%d credstore_files=%d public_routes=%d public_apis=%d clickhouse_credentials=%d promotion_gates=%d\n",
			report.DeployFiles,
			report.IntegrationFiles,
			report.PostgresFiles,
			report.RuntimeSecrets,
			report.CredstoreFiles,
			report.PublicRoutes,
			report.PublicAPIs,
			report.ClickHouseCreds,
			report.Gates,
		)
	case "json":
		body, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "deploy-contract-validate: encode report: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(body))
	default:
		fmt.Fprintf(os.Stderr, "deploy-contract-validate: --format must be text or json\n")
		os.Exit(2)
	}
}
