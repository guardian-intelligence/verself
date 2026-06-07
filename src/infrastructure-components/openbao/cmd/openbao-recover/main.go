package main

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	// Some OpenPGP keys advertise RIPEMD160; link it so encryption can negotiate.
	_ "golang.org/x/crypto/ripemd160"
)

const (
	defaultRepoRoot    = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultRuntimeRoot = "/var/lib/openbao/runtime"
	defaultDataDir     = "/var/lib/openbao/raft"
	defaultConfigPath  = "/etc/openbao/openbao.hcl"
	defaultAddr        = "https://127.0.0.1:8200"
	defaultCACert      = "/etc/verself/openbao/ca.pem"
	defaultReportPath  = "/run/verself/recovery/openbao/report.json"
	defaultKeyShares   = 3
	defaultThreshold   = 2

	baselineReconcileRetryWindow = 60 * time.Second
	baselineReconcileRetryDelay  = 1 * time.Second
)

type config struct {
	repoRoot                    string
	runtimeRoot                 string
	dataDir                     string
	configPath                  string
	reportPath                  string
	addr                        string
	caCert                      string
	bao                         string
	keyShares                   int
	threshold                   int
	pgpKeys                     stringList
	initOutputPath              string
	snapshotPath                string
	snapshotManifest            string
	snapshotOut                 string
	manifestOut                 string
	tokenStdin                  bool
	unsealStdin                 bool
	breakglassGenerateRootStdin bool
	loop                        bool
	loopInterval                time.Duration
	nomadWorkloadJWTFile        string
	nomadWorkloadRole           string
	resourceGraph               string
	resourceName                string
	pgpKeyDir                   string
	baseline                    openBaoBaselineSpec
}

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value != "" {
		*l = append(*l, value)
	}
	return nil
}

type condition struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Resource string `json:"resource"`
	Message  string `json:"message,omitempty"`
}

type report struct {
	Action     string            `json:"action"`
	State      string            `json:"state"`
	Address    string            `json:"address,omitempty"`
	Snapshot   *snapshotManifest `json:"snapshot,omitempty"`
	Conditions []condition       `json:"conditions"`
	Evidence   map[string]string `json:"evidence,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type snapshotManifest struct {
	APIVersion string               `json:"apiVersion"`
	Kind       string               `json:"kind"`
	Metadata   snapshotManifestMeta `json:"metadata"`
	Spec       snapshotManifestSpec `json:"spec"`
}

type snapshotManifestMeta struct {
	Name string `json:"name"`
}

type snapshotManifestSpec struct {
	CreatedAt      string `json:"createdAt"`
	SnapshotSHA256 string `json:"snapshotSHA256"`
	SnapshotBytes  int64  `json:"snapshotBytes"`
	OpenBaoVersion string `json:"openBaoVersion,omitempty"`
	SealType       string `json:"sealType,omitempty"`
	ClusterID      string `json:"clusterID,omitempty"`
	ClusterName    string `json:"clusterName,omitempty"`
	SourceAddress  string `json:"sourceAddress,omitempty"`
}

type baoStatus struct {
	Initialized bool   `json:"initialized"`
	Sealed      bool   `json:"sealed"`
	Version     string `json:"version"`
	SealType    string `json:"type"`
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
	Progress    int    `json:"progress"`
	Threshold   int    `json:"t"`
	Shares      int    `json:"n"`
}

type initOptions struct {
	KeyShares int
	Threshold int
}

type initResponse struct {
	RootToken       string   `json:"root_token"`
	UnsealKeysB64   []string `json:"unseal_keys_b64"`
	KeysBase64      []string `json:"keys_base64"`
	RecoveryKeysB64 []string `json:"recovery_keys_b64"`
}

type generateRootAttempt struct {
	Started          bool   `json:"started"`
	Nonce            string `json:"nonce"`
	Progress         int    `json:"progress"`
	Required         int    `json:"required"`
	Complete         bool   `json:"complete"`
	EncodedToken     string `json:"encoded_token"`
	EncodedRootToken string `json:"encoded_root_token"`
	PGPFingerprint   string `json:"pgp_fingerprint"`
	OTP              string `json:"otp"`
	OTPLength        int    `json:"otp_length"`
}

type encryptedInitMaterial struct {
	APIVersion string                    `json:"apiVersion"`
	Kind       string                    `json:"kind"`
	Metadata   encryptedInitMaterialMeta `json:"metadata"`
	Spec       encryptedInitMaterialSpec `json:"spec"`
}

type encryptedInitMaterialMeta struct {
	Name string `json:"name"`
}

type encryptedInitMaterialSpec struct {
	CreatedAt                  string                                 `json:"createdAt"`
	KeyShares                  int                                    `json:"keyShares"`
	KeyThreshold               int                                    `json:"keyThreshold"`
	PGPRecipientCount          int                                    `json:"pgpRecipientCount"`
	EncryptedUnsealSharesB64   []string                               `json:"encryptedUnsealSharesB64"`
	EncryptedRecoverySharesB64 []string                               `json:"encryptedRecoverySharesB64,omitempty"`
	OperatorImportTokens       []encryptedOperatorImportTokenMaterial `json:"operatorImportTokens,omitempty"`
}

type encryptedOperatorImportTokenMaterial struct {
	Name               string   `json:"name"`
	Policy             string   `json:"policy"`
	TTL                string   `json:"ttl"`
	Uses               int      `json:"uses,omitempty"`
	EncryptedTokensB64 []string `json:"encryptedTokensB64"`
}

type operatorImportTokenHandoff struct {
	Name   string
	Policy string
	TTL    string
	Uses   int
	Token  string
}

type openBaoClient interface {
	Status(context.Context) (baoStatus, error)
	Init(context.Context, initOptions) (initResponse, error)
	Unseal(context.Context, string) (baoStatus, error)
	RestoreSnapshot(context.Context, string, string) error
	SaveSnapshot(context.Context, string) ([]byte, error)
	ReconcileBaseline(context.Context, string, openBaoBaselineSpec) error
	RevokeSelf(context.Context, string) error
	LoginJWT(context.Context, string, string, string) (string, error)
	CreateToken(context.Context, string, openBaoOperatorImportTokenSpec) (string, error)
	GenerateRootInit(context.Context) (generateRootAttempt, error)
	GenerateRootUpdate(context.Context, string, string) (generateRootAttempt, error)
	GenerateRootCancel(context.Context) error
	DecodeGeneratedRootToken(context.Context, string, string) (string, error)
}

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stdin); err != nil {
		fmt.Fprintf(os.Stderr, "openbao-recover: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout io.Writer, stdin io.Reader) error {
	switch args[0] {
	case "prepare":
		cfg, err := parseConfig("openbao-recover prepare", args[1:], false, false)
		if err != nil {
			return err
		}
		return prepare(ctx, cfg, prepareOptions{})
	case "recover":
		cfg, err := parseConfig("openbao-recover recover", args[1:], true, false)
		if err != nil {
			return err
		}
		if cfg.loop {
			return recoverLoop(ctx, cfg, stdout, stdin)
		}
		client, err := newRealOpenBaoClient(cfg)
		if err != nil {
			return err
		}
		rep := recoverOnce(ctx, cfg, client, stdin)
		return writeRecoveryReport(stdout, cfg.reportPath, rep)
	case "status":
		cfg, err := parseConfig("openbao-recover status", args[1:], false, false)
		if err != nil {
			return err
		}
		client, err := newRealOpenBaoClient(cfg)
		if err != nil {
			return err
		}
		status, err := client.Status(ctx)
		rep := statusReport(cfg, status, err)
		return writeReport(stdout, cfg.reportPath, rep)
	case "snapshot":
		return runSnapshot(ctx, args[1:], stdout, stdin)
	case "-h", "--help", "help":
		usage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func runSnapshot(ctx context.Context, args []string, stdout io.Writer, stdin io.Reader) error {
	if len(args) < 1 {
		return errors.New("snapshot subcommand is required")
	}
	switch args[0] {
	case "save":
		cfg, err := parseConfig("openbao-recover snapshot save", args[1:], false, true)
		if err != nil {
			return err
		}
		client, err := newRealOpenBaoClient(cfg)
		if err != nil {
			return err
		}
		token, err := readToken(stdin, cfg.tokenStdin)
		if err != nil {
			return err
		}
		rep := saveSnapshot(ctx, cfg, client, token)
		return writeReport(stdout, cfg.reportPath, rep)
	case "verify":
		cfg, err := parseConfig("openbao-recover snapshot verify", args[1:], false, true)
		if err != nil {
			return err
		}
		manifest, err := readSnapshotManifest(cfg.snapshotManifest)
		if err != nil {
			return err
		}
		if err := verifySnapshotDigest(cfg.snapshotPath, manifest); err != nil {
			rep := report{
				Action:     "snapshot.verify",
				State:      "SnapshotInvalid",
				Snapshot:   &manifest,
				Conditions: []condition{conditionFalse("OpenBaoSnapshotVerified", "DigestMismatch", "openbao", err.Error())},
			}
			return writeReport(stdout, cfg.reportPath, rep)
		}
		rep := report{
			Action:     "snapshot.verify",
			State:      "SnapshotVerified",
			Snapshot:   &manifest,
			Conditions: []condition{conditionTrue("OpenBaoSnapshotVerified", "DigestVerified", "openbao", "snapshot digest matches manifest")},
		}
		return writeReport(stdout, cfg.reportPath, rep)
	default:
		return fmt.Errorf("unknown snapshot subcommand %q", args[0])
	}
}

func parseConfig(name string, args []string, recoveryFlags bool, snapshotFlags bool) (config, error) {
	cfg := config{
		repoRoot:     defaultRepoRoot,
		runtimeRoot:  defaultRuntimeRoot,
		dataDir:      defaultDataDir,
		configPath:   defaultConfigPath,
		reportPath:   defaultReportPath,
		addr:         defaultAddr,
		caCert:       defaultCACert,
		keyShares:    defaultKeyShares,
		threshold:    defaultThreshold,
		loopInterval: 5 * time.Second,
		resourceName: "openbao",
		pgpKeyDir:    "/run/verself/recovery/openbao/pgp",
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.StringVar(&cfg.repoRoot, "repo-root", cfg.repoRoot, "boarded repo root")
	fs.StringVar(&cfg.runtimeRoot, "runtime-root", cfg.runtimeRoot, "OpenBao runtime root")
	fs.StringVar(&cfg.dataDir, "data-dir", cfg.dataDir, "OpenBao Raft data directory")
	fs.StringVar(&cfg.configPath, "config", cfg.configPath, "OpenBao config path")
	fs.StringVar(&cfg.reportPath, "report", cfg.reportPath, "nonsecret recovery report path")
	fs.StringVar(&cfg.addr, "addr", cfg.addr, "OpenBao API address")
	fs.StringVar(&cfg.caCert, "ca-cert", cfg.caCert, "OpenBao CA certificate path")
	fs.StringVar(&cfg.bao, "bao", "", "bao binary path")
	fs.StringVar(&cfg.resourceGraph, "resource-graph", "", "Guardian resource graph document path")
	fs.StringVar(&cfg.resourceName, "resource-name", cfg.resourceName, "OpenBaoCluster resource name")
	fs.StringVar(&cfg.pgpKeyDir, "pgp-key-dir", cfg.pgpKeyDir, "directory for public PGP key files derived from the resource graph")
	if recoveryFlags {
		fs.IntVar(&cfg.keyShares, "key-shares", cfg.keyShares, "fresh init key shares")
		fs.IntVar(&cfg.threshold, "key-threshold", cfg.threshold, "fresh init key threshold")
		fs.Var(&cfg.pgpKeys, "pgp-key", "PGP public recipient for encrypted init output; repeat")
		fs.StringVar(&cfg.initOutputPath, "init-output", "", "non-durable path for PGP-encrypted init material")
		fs.StringVar(&cfg.snapshotPath, "snapshot", "", "verified Raft snapshot path to restore")
		fs.StringVar(&cfg.snapshotManifest, "snapshot-manifest", "", "snapshot manifest path")
		fs.BoolVar(&cfg.unsealStdin, "unseal-stdin", false, "read unseal shares from stdin, one per line")
		fs.BoolVar(&cfg.tokenStdin, "operator-token-stdin", false, "read an operator token from stdin")
		fs.BoolVar(&cfg.breakglassGenerateRootStdin, "breakglass-generate-root-token-stdin", false, "BREAKGLASS: use deprecated OpenBao generate-root endpoints with stdin unseal shares")
		fs.BoolVar(&cfg.loop, "loop", false, "run recover as a level-triggered controller")
		fs.DurationVar(&cfg.loopInterval, "loop-interval", cfg.loopInterval, "recover controller observation interval")
		fs.StringVar(&cfg.nomadWorkloadJWTFile, "nomad-workload-jwt-file", "", "Nomad workload identity JWT file used for baseline reconciliation")
		fs.StringVar(&cfg.nomadWorkloadRole, "nomad-workload-role", "", "OpenBao JWT role used for baseline reconciliation")
	}
	if snapshotFlags {
		fs.StringVar(&cfg.snapshotPath, "snapshot", "", "snapshot path to verify")
		fs.StringVar(&cfg.snapshotManifest, "snapshot-manifest", "", "snapshot manifest path")
		fs.StringVar(&cfg.snapshotOut, "snapshot-out", "", "snapshot output path")
		fs.StringVar(&cfg.manifestOut, "manifest-out", "", "snapshot manifest output path")
		fs.BoolVar(&cfg.tokenStdin, "token-stdin", false, "read OpenBao token from stdin")
	}
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	baoExplicit := flagProvided(args, "bao")
	reportExplicit := flagProvided(args, "report")
	reportPath := cfg.reportPath
	cfg = normalizeConfig(cfg)
	if cfg.resourceGraph != "" {
		next, err := applyResourceGraphConfig(cfg)
		if err != nil {
			return config{}, err
		}
		if !baoExplicit {
			next.bao = ""
		}
		if reportExplicit {
			next.reportPath = reportPath
		}
		cfg = normalizeConfig(next)
	}
	if cfg.tokenStdin && cfg.breakglassGenerateRootStdin {
		return config{}, errors.New("--operator-token-stdin and --breakglass-generate-root-token-stdin are mutually exclusive")
	}
	if cfg.loop && (cfg.tokenStdin || cfg.unsealStdin || cfg.breakglassGenerateRootStdin) {
		return config{}, errors.New("--loop cannot read operator material from stdin")
	}
	if (cfg.nomadWorkloadJWTFile == "") != (cfg.nomadWorkloadRole == "") {
		return config{}, errors.New("--nomad-workload-jwt-file and --nomad-workload-role must be provided together")
	}
	if cfg.loopInterval <= 0 {
		return config{}, errors.New("--loop-interval must be positive")
	}
	return cfg, nil
}

func flagProvided(args []string, name string) bool {
	short := "-" + name
	long := "--" + name
	for _, arg := range args {
		if arg == short || arg == long || strings.HasPrefix(arg, short+"=") || strings.HasPrefix(arg, long+"=") {
			return true
		}
	}
	return false
}

func normalizeConfig(cfg config) config {
	cfg.repoRoot = strings.TrimSpace(cfg.repoRoot)
	cfg.runtimeRoot = strings.TrimSpace(cfg.runtimeRoot)
	cfg.dataDir = strings.TrimSpace(cfg.dataDir)
	cfg.configPath = strings.TrimSpace(cfg.configPath)
	cfg.reportPath = strings.TrimSpace(cfg.reportPath)
	cfg.addr = strings.TrimSpace(cfg.addr)
	cfg.caCert = strings.TrimSpace(cfg.caCert)
	cfg.bao = strings.TrimSpace(cfg.bao)
	cfg.snapshotPath = strings.TrimSpace(cfg.snapshotPath)
	cfg.snapshotManifest = strings.TrimSpace(cfg.snapshotManifest)
	cfg.snapshotOut = strings.TrimSpace(cfg.snapshotOut)
	cfg.manifestOut = strings.TrimSpace(cfg.manifestOut)
	cfg.initOutputPath = strings.TrimSpace(cfg.initOutputPath)
	cfg.nomadWorkloadJWTFile = strings.TrimSpace(cfg.nomadWorkloadJWTFile)
	cfg.nomadWorkloadRole = strings.TrimSpace(cfg.nomadWorkloadRole)
	cfg.resourceGraph = strings.TrimSpace(cfg.resourceGraph)
	cfg.resourceName = strings.TrimSpace(cfg.resourceName)
	cfg.pgpKeyDir = strings.TrimSpace(cfg.pgpKeyDir)
	if cfg.bao == "" {
		cfg.bao = filepath.Join(cfg.runtimeRoot, "current", "bin", "bao")
	}
	return cfg
}

type guardianDocument struct {
	Entrypoint json.RawMessage    `json:"entrypoint"`
	Resources  []guardianResource `json:"resources"`
}

type guardianResource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   resourceMeta    `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type resourceMeta struct {
	Name string `json:"name"`
}

type objectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

type openBaoClusterSpec struct {
	Address          string              `json:"address"`
	CACert           string              `json:"caCert"`
	RuntimeRoot      string              `json:"runtimeRoot"`
	DataDir          string              `json:"dataDir"`
	ConfigPath       string              `json:"configPath"`
	ReportPath       string              `json:"reportPath"`
	InitMaterialPath string              `json:"initMaterialPath"`
	Seal             openBaoSealSpec     `json:"seal"`
	Snapshots        openBaoSnapshotSpec `json:"snapshots"`
	Baseline         openBaoBaselineSpec `json:"baseline"`
}

type openBaoSealSpec struct {
	Shamir openBaoShamirSpec `json:"shamir"`
}

type openBaoShamirSpec struct {
	KeyShares        int         `json:"keyShares"`
	KeyThreshold     int         `json:"keyThreshold"`
	PGPRecipientRefs []objectRef `json:"pgpRecipientRefs"`
}

type openBaoSnapshotSpec struct {
	Restore *openBaoSnapshotRestoreSpec `json:"restore"`
	Save    *openBaoSnapshotSaveSpec    `json:"save"`
}

type openBaoSnapshotRestoreSpec struct {
	ManifestPath string    `json:"manifestPath"`
	SnapshotPath string    `json:"snapshotPath"`
	SourceRef    objectRef `json:"sourceRef"`
}

type openBaoSnapshotSaveSpec struct {
	ManifestPath   string    `json:"manifestPath"`
	SnapshotPath   string    `json:"snapshotPath"`
	DestinationRef objectRef `json:"destinationRef"`
}

type openBaoBaselineSpec struct {
	Reconcile            bool                             `json:"reconcile"`
	Mounts               []openBaoMountSpec               `json:"mounts"`
	Policies             []openBaoPolicySpec              `json:"policies"`
	JWTAuths             []openBaoJWTAuthSpec             `json:"jwtAuths"`
	OperatorImportTokens []openBaoOperatorImportTokenSpec `json:"operatorImportTokens"`
	SecretPaths          []openBaoSecretPathSpec          `json:"-"`
}

type openBaoMountSpec struct {
	Path        string            `json:"path"`
	Type        string            `json:"type"`
	Description string            `json:"description"`
	Options     map[string]string `json:"options"`
}

type openBaoPolicySpec struct {
	Name string `json:"name"`
	HCL  string `json:"hcl"`
}

type openBaoOperatorImportTokenSpec struct {
	Name   string `json:"name"`
	Policy string `json:"policy"`
	TTL    string `json:"ttl"`
	Uses   int    `json:"uses"`
}

type openBaoJWTAuthSpec struct {
	Path          string                  `json:"path"`
	Description   string                  `json:"description"`
	JWKSURL       string                  `json:"jwksURL"`
	SPIREBundle   *openBaoSPIREBundleSpec `json:"spireBundle"`
	SupportedAlgs []string                `json:"supportedAlgs"`
	Roles         []openBaoJWTRoleSpec    `json:"roles"`
}

type openBaoSPIREBundleSpec struct {
	SPIREServerPath string `json:"spireServerPath"`
	SocketPath      string `json:"socketPath"`
}

type openBaoJWTRoleSpec struct {
	Name                 string            `json:"name"`
	RoleType             string            `json:"roleType"`
	BoundAudiences       []string          `json:"boundAudiences"`
	BoundSubject         string            `json:"boundSubject"`
	BoundClaims          map[string]string `json:"boundClaims"`
	UserClaim            string            `json:"userClaim"`
	UserClaimJSONPointer bool              `json:"userClaimJSONPointer"`
	ClaimMappings        map[string]string `json:"claimMappings"`
	TokenType            string            `json:"tokenType"`
	TokenPolicies        []string          `json:"tokenPolicies"`
	TokenPeriod          string            `json:"tokenPeriod"`
	TokenExplicitMaxTTL  int               `json:"tokenExplicitMaxTTL"`
}

type openBaoSecretPathSpec struct {
	Name        string
	Path        string               `json:"path"`
	Key         string               `json:"key"`
	Source      string               `json:"source"`
	Generate    *openBaoGenerateSpec `json:"generate"`
	ProducerRef *objectRef           `json:"producerRef"`
}

type openBaoGenerateSpec struct {
	Bytes    int    `json:"bytes"`
	Encoding string `json:"encoding"`
}

type pgpRecipientSpec struct {
	PublicKeyBase64 string `json:"publicKeyBase64"`
}

func applyResourceGraphConfig(cfg config) (config, error) {
	body, err := os.ReadFile(cfg.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc guardianDocument
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	resources := map[string]guardianResource{}
	for _, resource := range doc.Resources {
		key := resourceKey(resource.APIVersion, resource.Kind, resource.Metadata.Name)
		if _, exists := resources[key]; exists {
			return config{}, fmt.Errorf("Guardian resource graph duplicates %s", key)
		}
		resources[key] = resource
	}
	name := defaultIfEmpty(cfg.resourceName, "openbao")
	cluster, ok := resources[resourceKey("openbao.guardianintelligence.org/v1alpha1", "OpenBaoCluster", name)]
	if !ok {
		return config{}, fmt.Errorf("Guardian resource graph missing OpenBaoCluster %q", name)
	}
	var spec openBaoClusterSpec
	clusterDecoder := json.NewDecoder(bytes.NewReader(cluster.Spec))
	clusterDecoder.DisallowUnknownFields()
	if err := clusterDecoder.Decode(&spec); err != nil {
		return config{}, fmt.Errorf("decode OpenBaoCluster %q: %w", name, err)
	}
	cfg.addr = spec.Address
	cfg.caCert = spec.CACert
	cfg.runtimeRoot = spec.RuntimeRoot
	cfg.dataDir = spec.DataDir
	cfg.configPath = spec.ConfigPath
	cfg.reportPath = spec.ReportPath
	cfg.initOutputPath = spec.InitMaterialPath
	cfg.keyShares = spec.Seal.Shamir.KeyShares
	cfg.threshold = spec.Seal.Shamir.KeyThreshold
	cfg.baseline = spec.Baseline
	secretPaths, err := loadOpenBaoSecretPaths(resources)
	if err != nil {
		return config{}, err
	}
	cfg.baseline.SecretPaths = secretPaths
	if spec.Snapshots.Restore != nil {
		cfg.snapshotPath = spec.Snapshots.Restore.SnapshotPath
		cfg.snapshotManifest = spec.Snapshots.Restore.ManifestPath
	}
	pgpFiles, err := materializePGPRecipients(cfg, resources, spec.Seal.Shamir.PGPRecipientRefs)
	if err != nil {
		return config{}, err
	}
	cfg.pgpKeys = stringList(pgpFiles)
	return cfg, nil
}

func materializePGPRecipients(cfg config, resources map[string]guardianResource, refs []objectRef) ([]string, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	if cfg.pgpKeyDir == "" {
		return nil, errors.New("pgp-key-dir is required when OpenBao PGP recipient refs are configured")
	}
	if err := os.MkdirAll(cfg.pgpKeyDir, 0o700); err != nil {
		return nil, fmt.Errorf("create OpenBao PGP recipient dir: %w", err)
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		resource, ok := resources[resourceKey(ref.APIVersion, ref.Kind, ref.Name)]
		if !ok {
			return nil, fmt.Errorf("missing OpenBao PGP recipient %s/%s/%s", ref.APIVersion, ref.Kind, ref.Name)
		}
		if resource.APIVersion != "openbao.guardianintelligence.org/v1alpha1" || resource.Kind != "PGPRecipient" {
			return nil, fmt.Errorf("OpenBao PGP recipient ref %s/%s/%s must target openbao.guardianintelligence.org/v1alpha1/PGPRecipient", ref.APIVersion, ref.Kind, ref.Name)
		}
		var spec pgpRecipientSpec
		decoder := json.NewDecoder(bytes.NewReader(resource.Spec))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&spec); err != nil {
			return nil, fmt.Errorf("decode PGPRecipient %q: %w", ref.Name, err)
		}
		publicKey := strings.TrimSpace(spec.PublicKeyBase64)
		if publicKey == "" {
			return nil, fmt.Errorf("PGPRecipient %q spec.publicKeyBase64 is required", ref.Name)
		}
		path := filepath.Join(cfg.pgpKeyDir, ref.Name+".pgp.b64")
		if err := writeFileAtomic(path, []byte(publicKey+"\n"), 0o644); err != nil {
			return nil, err
		}
		out = append(out, path)
	}
	return out, nil
}

func loadOpenBaoSecretPaths(resources map[string]guardianResource) ([]openBaoSecretPathSpec, error) {
	var paths []openBaoSecretPathSpec
	for _, resource := range resources {
		if resource.APIVersion != "openbao.guardianintelligence.org/v1alpha1" || resource.Kind != "SecretPath" {
			continue
		}
		var spec openBaoSecretPathSpec
		decoder := json.NewDecoder(bytes.NewReader(resource.Spec))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&spec); err != nil {
			return nil, fmt.Errorf("decode SecretPath %q: %w", resource.Metadata.Name, err)
		}
		spec.Name = resource.Metadata.Name
		if err := validateSecretPath(spec); err != nil {
			return nil, fmt.Errorf("SecretPath %q: %w", resource.Metadata.Name, err)
		}
		paths = append(paths, spec)
	}
	return paths, nil
}

func validateSecretPath(spec openBaoSecretPathSpec) error {
	if strings.TrimSpace(spec.Name) == "" || strings.Trim(strings.TrimSpace(spec.Path), "/") == "" || strings.TrimSpace(spec.Key) == "" {
		return errors.New("metadata.name, spec.path, and spec.key are required")
	}
	switch spec.Source {
	case "generated":
		if spec.Generate == nil {
			return errors.New("spec.generate is required when spec.source is generated")
		}
		if spec.Generate.Bytes <= 0 {
			return errors.New("spec.generate.bytes must be positive")
		}
		switch spec.Generate.Encoding {
		case "hex", "base64url", "alphanumeric", "password":
		default:
			return errors.New("spec.generate.encoding must be hex, base64url, alphanumeric, or password")
		}
	case "producedBy":
		if spec.ProducerRef == nil {
			return errors.New("spec.producerRef is required when spec.source is producedBy")
		}
	case "operatorImport":
	default:
		return errors.New("spec.source must be generated, producedBy, or operatorImport")
	}
	return nil
}

func resourceKey(apiVersion string, kind string, name string) string {
	return apiVersion + "/" + kind + "/" + name
}

func defaultIfEmpty(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func usage(w io.Writer) {
	fmt.Fprint(w, `openbao-recover

usage:
  openbao-recover prepare [flags]
  openbao-recover recover [flags]
  openbao-recover status [flags]
  openbao-recover snapshot save --snapshot-out <path> --manifest-out <path> --token-stdin [flags]
  openbao-recover snapshot verify --snapshot <path> --snapshot-manifest <path> [flags]

Recovery reports contain conditions and fingerprints only. Unseal shares,
recovery shares, and operator tokens must be provided through ephemeral operator
paths, not host files. Generate-root support is breakglass-only; autonomous
recovery should use auto-unseal and scoped workload identity. Nomad-managed
reconciliation should run recover --loop with --nomad-workload-jwt-file and
--nomad-workload-role.
`)
}

func recoverOnce(ctx context.Context, cfg config, client openBaoClient, stdin io.Reader) report {
	if err := prepare(ctx, cfg, prepareOptions{}); err != nil {
		return report{
			Action:  "recover",
			State:   "PrepareFailed",
			Address: cfg.addr,
			Conditions: []condition{
				conditionFalse("OpenBaoRuntimePrepared", "PrepareFailed", "openbao", err.Error()),
				conditionFalse("OpenBaoRecoveryComplete", "RuntimePrepareFailed", "openbao", "OpenBao runtime preparation failed"),
			},
		}
	}
	status, err := client.Status(ctx)
	if err != nil {
		return statusReport(cfg, status, err)
	}
	rep := report{
		Action:   "recover",
		State:    classify(status),
		Address:  cfg.addr,
		Evidence: statusEvidence(status),
		Conditions: []condition{
			conditionTrue("OpenBaoRuntimePrepared", "RuntimeReady", "openbao", "OpenBao runtime and config are prepared"),
			conditionTrue("OpenBaoServerReady", "StatusReadable", "openbao", "OpenBao status is readable"),
		},
	}
	switch classify(status) {
	case "InitializedUnsealed":
		if cfg.baseline.Reconcile && !baselineAuthorityConfigured(cfg) {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires operator authority"),
				conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
			)
			return rep
		}
		if !baselineAuthorityConfigured(cfg) {
			rep.Conditions = append(rep.Conditions,
				conditionTrue("OpenBaoRecoveryComplete", "Available", "openbao", "OpenBao is initialized, unsealed, and available"),
			)
			return rep
		}
		if cfg.breakglassGenerateRootStdin {
			shares, err := readUnsealShares(stdin, true)
			if err != nil {
				rep.Conditions = append(rep.Conditions,
					conditionFalse("OpenBaoBreakglassRootToken", "UnsealQuorumIncomplete", "openbao", "threshold unseal material is required to generate a transient root token"),
					conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires operator authority"),
					conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
				)
				return rep
			}
			token, err := generateRootTokenFromShares(ctx, client, shares)
			if err != nil {
				rep.Conditions = append(rep.Conditions,
					conditionFalse("OpenBaoBreakglassRootToken", "BreakglassGenerateRootFailed", "openbao", err.Error()),
					conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires operator authority"),
					conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
				)
				return rep
			}
			extra := []condition{conditionTrue("OpenBaoBreakglassRootToken", "BreakglassGenerated", "openbao", "breakglass transient root token was generated from threshold unseal material")}
			return reconcileBaselineWithToken(ctx, cfg.baseline, client, rep, token, extra)
		}
		if hasNomadWorkloadAuthority(cfg) {
			return reconcileBaselineWithNomadWorkload(ctx, cfg, client, rep, nil)
		}
		token, err := readToken(stdin, true)
		if err != nil {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires an operator token"),
				conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
			)
			return rep
		}
		return reconcileBaselineWithToken(ctx, cfg.baseline, client, rep, token, nil)
	case "InitializedSealed":
		shares, err := readUnsealShares(stdin, cfg.unsealStdin || cfg.breakglassGenerateRootStdin)
		if err != nil {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("OpenBaoUnsealed", "UnsealQuorumIncomplete", "openbao", "threshold unseal material is required"),
				conditionFalse("OpenBaoRecoveryComplete", "WaitingForUnseal", "openbao", "OpenBao is sealed"),
			)
			return rep
		}
		for _, share := range shares {
			status, err = client.Unseal(ctx, share)
			if err != nil {
				rep.Conditions = append(rep.Conditions,
					conditionFalse("OpenBaoUnsealed", "UnsealFailed", "openbao", "unseal material was rejected"),
					conditionFalse("OpenBaoRecoveryComplete", "UnsealFailed", "openbao", "OpenBao remains sealed"),
				)
				return rep
			}
			if !status.Sealed {
				break
			}
		}
		if status.Sealed {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("OpenBaoUnsealed", "UnsealQuorumIncomplete", "openbao", "not enough unseal shares were presented"),
				conditionFalse("OpenBaoRecoveryComplete", "WaitingForUnseal", "openbao", "OpenBao remains sealed"),
			)
			return rep
		}
		rep.State = "InitializedUnsealed"
		rep.Evidence = statusEvidence(status)
		if cfg.baseline.Reconcile {
			rep.Conditions = append(rep.Conditions, conditionTrue("OpenBaoUnsealed", "UnsealComplete", "openbao", "OpenBao was unsealed"))
			if cfg.breakglassGenerateRootStdin {
				token, err := generateRootTokenFromShares(ctx, client, shares)
				if err != nil {
					rep.Conditions = append(rep.Conditions,
						conditionFalse("OpenBaoBreakglassRootToken", "BreakglassGenerateRootFailed", "openbao", err.Error()),
						conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires operator authority"),
						conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
					)
					return rep
				}
				extra := []condition{conditionTrue("OpenBaoBreakglassRootToken", "BreakglassGenerated", "openbao", "breakglass transient root token was generated from threshold unseal material")}
				return reconcileBaselineWithToken(ctx, cfg.baseline, client, rep, token, extra)
			}
			if hasNomadWorkloadAuthority(cfg) {
				return reconcileBaselineWithNomadWorkload(ctx, cfg, client, rep, nil)
			}
			rep.Conditions = append(rep.Conditions,
				conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires operator authority"),
				conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
			)
			return rep
		}
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoUnsealed", "UnsealComplete", "openbao", "OpenBao was unsealed"),
			conditionTrue("OpenBaoRecoveryComplete", "Recovered", "openbao", "OpenBao is initialized, unsealed, and available"),
		)
		return rep
	case "Uninitialized":
		if cfg.snapshotPath != "" || cfg.snapshotManifest != "" {
			return restoreSnapshot(ctx, cfg, client, rep)
		}
		return freshInit(ctx, cfg, client, rep)
	default:
		rep.Conditions = append(rep.Conditions, conditionFalse("OpenBaoRecoveryComplete", "UnknownState", "openbao", "OpenBao status did not map to a recovery state"))
		return rep
	}
}

func recoverLoop(ctx context.Context, cfg config, stdout io.Writer, stdin io.Reader) error {
	for {
		loopCfg := cfg
		if loopCfg.resourceGraph != "" {
			next, err := applyResourceGraphConfig(loopCfg)
			if err != nil {
				return err
			}
			loopCfg = normalizeConfig(next)
		}
		client, err := newRealOpenBaoClient(loopCfg)
		if err != nil {
			return err
		}
		rep := recoverOnce(ctx, loopCfg, client, stdin)
		if err := writeReport(stdout, loopCfg.reportPath, rep); err != nil {
			return err
		}
		timer := time.NewTimer(loopCfg.loopInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return nil
		case <-timer.C:
		}
	}
}

func baselineAuthorityConfigured(cfg config) bool {
	return cfg.tokenStdin || cfg.breakglassGenerateRootStdin || hasNomadWorkloadAuthority(cfg)
}

func hasNomadWorkloadAuthority(cfg config) bool {
	return cfg.nomadWorkloadJWTFile != "" && cfg.nomadWorkloadRole != ""
}

func reconcileBaselineWithNomadWorkload(ctx context.Context, cfg config, client openBaoClient, rep report, extra []condition) report {
	authPath, err := jwtAuthPathForRole(cfg.baseline.JWTAuths, cfg.nomadWorkloadRole)
	if err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoWorkloadToken", "JWTAuthUnavailable", "openbao", err.Error()),
			conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires workload authority"),
			conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
		)
		return rep
	}
	jwt, err := readNomadWorkloadJWT(cfg.nomadWorkloadJWTFile)
	if err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoWorkloadToken", "JWTUnavailable", "openbao", err.Error()),
			conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires workload authority"),
			conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
		)
		return rep
	}
	token, err := client.LoginJWT(ctx, authPath, cfg.nomadWorkloadRole, jwt)
	if err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoWorkloadToken", "JWTLoginFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityRequired", "openbao", "baseline reconciliation requires workload authority"),
			conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
		)
		return rep
	}
	extra = append(extra, conditionTrue("OpenBaoWorkloadToken", "JWTAccepted", "openbao", "Nomad workload identity issued a scoped OpenBao token"))
	if handoffCondition, ok := operatorImportTokenObservedCondition(cfg); ok {
		extra = append(extra, handoffCondition)
	}
	return reconcileBaselineWithToken(ctx, cfg.baseline, client, rep, token, extra)
}

func jwtAuthPathForRole(auths []openBaoJWTAuthSpec, role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		return "", errors.New("Nomad workload JWT role is required")
	}
	var matches []string
	for _, auth := range auths {
		for _, candidate := range auth.Roles {
			if strings.TrimSpace(candidate.Name) == role {
				matches = append(matches, strings.Trim(strings.TrimSpace(auth.Path), "/"))
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("OpenBao JWT auth role %q is not configured", role)
	case 1:
		if matches[0] == "" {
			return "", fmt.Errorf("OpenBao JWT auth role %q has an empty auth path", role)
		}
		return matches[0], nil
	default:
		return "", fmt.Errorf("OpenBao JWT auth role %q is configured on multiple auth paths", role)
	}
}

func readNomadWorkloadJWT(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Nomad workload JWT: %w", err)
	}
	jwt := strings.TrimSpace(string(body))
	if jwt == "" {
		return "", errors.New("Nomad workload JWT file is empty")
	}
	return jwt, nil
}

func reconcileBaselineWithToken(ctx context.Context, baseline openBaoBaselineSpec, client openBaoClient, rep report, token string, extra []condition) report {
	if err := reconcileBaselineWithRetry(ctx, baseline, client, token); err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions, baselineReconcileFailureConditions(err)...)
		rep.Conditions = append(rep.Conditions, revokePresentedToken(ctx, client, token))
		return rep
	}
	revokeCondition := revokePresentedToken(ctx, client, token)
	if revokeCondition.Status != "True" {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
			revokeCondition,
			conditionFalse("OpenBaoRecoveryComplete", "TransientTokenRevocationFailed", "openbao", "baseline was reconciled but the transient token could not be revoked"),
		)
		return rep
	}
	rep.Conditions = append(rep.Conditions, extra...)
	rep.Conditions = append(rep.Conditions,
		conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
		revokeCondition,
		conditionTrue("OpenBaoRecoveryComplete", "Recovered", "openbao", "OpenBao is unsealed and baseline is reconciled"),
	)
	return rep
}

func reconcileBaselineWithRetry(ctx context.Context, baseline openBaoBaselineSpec, client openBaoClient, token string) error {
	deadline := time.Now().Add(baselineReconcileRetryWindow)
	var lastErr error
	for {
		err := client.ReconcileBaseline(ctx, token, baseline)
		if err == nil {
			return nil
		}
		lastErr = err
		if !isOpenBaoTransient(err) || time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(baselineReconcileRetryDelay):
		}
	}
}

func baselineReconcileFailureConditions(err error) []condition {
	if isOpenBaoPermissionDenied(err) {
		return []condition{
			conditionFalse("OpenBaoBaselineReconciled", "BaselineAuthorityInsufficient", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
		}
	}
	return []condition{
		conditionFalse("OpenBaoBaselineReconciled", "ReconcileFailed", "openbao", err.Error()),
		conditionFalse("OpenBaoRecoveryComplete", "BaselineFailed", "openbao", "baseline reconciliation failed"),
	}
}

func revokePresentedToken(ctx context.Context, client openBaoClient, token string) condition {
	if err := client.RevokeSelf(ctx, token); err != nil {
		reason := "RevokeSelfFailed"
		if isOpenBaoPermissionDenied(err) {
			reason = "RevokeSelfPermissionDenied"
		}
		return conditionFalse("OpenBaoTransientTokenRevoked", reason, "openbao", err.Error())
	}
	return conditionTrue("OpenBaoTransientTokenRevoked", "Revoked", "openbao", "transient recovery token was revoked")
}

func generateRootTokenFromShares(ctx context.Context, client openBaoClient, shares []string) (string, error) {
	if len(shares) == 0 {
		return "", errors.New("at least one unseal share is required to generate a root token")
	}
	init, err := client.GenerateRootInit(ctx)
	if err != nil {
		return "", err
	}
	complete := false
	defer func() {
		if !complete {
			_ = client.GenerateRootCancel(context.Background())
		}
	}()
	if !init.Started || init.Nonce == "" || init.OTP == "" {
		return "", errors.New("OpenBao generate-root did not start with a nonce and OTP")
	}
	attempt := init
	for _, share := range shares {
		attempt, err = client.GenerateRootUpdate(ctx, share, init.Nonce)
		if err != nil {
			return "", err
		}
		if attempt.Complete {
			complete = true
			break
		}
	}
	if !complete {
		return "", fmt.Errorf("OpenBao generate-root incomplete after %d shares; required %d", attempt.Progress, attempt.Required)
	}
	encoded := strings.TrimSpace(attempt.EncodedToken)
	if encoded == "" {
		encoded = strings.TrimSpace(attempt.EncodedRootToken)
	}
	if encoded == "" {
		return "", errors.New("OpenBao generate-root completed without an encoded token")
	}
	return client.DecodeGeneratedRootToken(ctx, encoded, init.OTP)
}

func isOpenBaoPermissionDenied(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "status 403") || strings.Contains(message, "permission denied")
}

func isOpenBaoTransient(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, status := range []string{"status 500", "status 502", "status 503", "status 504"} {
		if strings.Contains(message, status) {
			return true
		}
	}
	return strings.Contains(message, "connection refused") || strings.Contains(message, "connection reset")
}

func restoreSnapshot(ctx context.Context, cfg config, client openBaoClient, rep report) report {
	if cfg.snapshotPath == "" || cfg.snapshotManifest == "" {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoSnapshotRestored", "SnapshotInputRequired", "openbao", "snapshot and manifest are required for restore"),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotMissing", "openbao", "snapshot restore cannot start"),
		)
		return rep
	}
	manifest, err := readSnapshotManifest(cfg.snapshotManifest)
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoSnapshotRestored", "SnapshotManifestInvalid", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotManifestInvalid", "openbao", "snapshot manifest is not usable"),
		)
		return rep
	}
	rep.Snapshot = &manifest
	if err := verifySnapshotDigest(cfg.snapshotPath, manifest); err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoSnapshotVerified", "DigestMismatch", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotInvalid", "openbao", "snapshot digest did not match manifest"),
		)
		return rep
	}
	init, err := client.Init(ctx, initOptions{KeyShares: 1, Threshold: 1})
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoSnapshotRestored", "TemporaryInitFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotRestoreBlocked", "openbao", "temporary restore target init failed"),
		)
		return rep
	}
	rootToken := strings.TrimSpace(init.RootToken)
	if rootToken == "" {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoSnapshotRestored", "TemporaryRootTokenMissing", "openbao", "temporary init did not return a root token"),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotRestoreBlocked", "openbao", "snapshot restore requires temporary root token"),
		)
		return rep
	}
	tempStatus, err := unsealTemporaryRestoreTarget(ctx, client, init)
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoSnapshotVerified", "DigestVerified", "openbao", "snapshot digest matches manifest"),
			conditionFalse("OpenBaoSnapshotRestored", "TemporaryUnsealFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotRestoreBlocked", "openbao", "temporary restore target could not be unsealed"),
		)
		return rep
	}
	if tempStatus.Sealed {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoSnapshotVerified", "DigestVerified", "openbao", "snapshot digest matches manifest"),
			conditionFalse("OpenBaoSnapshotRestored", "TemporaryUnsealIncomplete", "openbao", "temporary restore target remains sealed"),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotRestoreBlocked", "openbao", "temporary restore target could not accept authenticated restore"),
		)
		return rep
	}
	if err := client.RestoreSnapshot(ctx, rootToken, cfg.snapshotPath); err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoSnapshotVerified", "DigestVerified", "openbao", "snapshot digest matches manifest"),
			conditionFalse("OpenBaoSnapshotRestored", "RestoreFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotRestoreFailed", "openbao", "snapshot restore failed"),
		)
		return rep
	}
	rep.State = "InitializedSealed"
	rep.Conditions = append(rep.Conditions,
		conditionTrue("OpenBaoSnapshotVerified", "DigestVerified", "openbao", "snapshot digest matches manifest"),
		conditionTrue("OpenBaoTemporaryRestoreTargetUnsealed", "TemporaryUnsealComplete", "openbao", "temporary restore target was unsealed for authenticated restore"),
		conditionTrue("OpenBaoSnapshotRestored", "RestoreSubmitted", "openbao", "snapshot restore was submitted"),
		conditionTrue("OpenBaoServerRestartRequired", "AfterSnapshotRestore", "openbao", "restart OpenBao before unsealing restored data"),
		conditionFalse("OpenBaoUnsealed", "UnsealQuorumIncomplete", "openbao", "restored snapshot requires matching unseal material"),
		conditionFalse("OpenBaoRecoveryComplete", "WaitingForUnseal", "openbao", "restored OpenBao must be unsealed"),
	)
	return rep
}

func unsealTemporaryRestoreTarget(ctx context.Context, client openBaoClient, init initResponse) (baoStatus, error) {
	shares := initUnsealShares(init)
	if len(shares) == 0 {
		return baoStatus{}, errors.New("temporary init did not return unseal material")
	}
	var status baoStatus
	for _, share := range shares {
		var err error
		status, err = client.Unseal(ctx, share)
		if err != nil {
			return baoStatus{}, err
		}
		if !status.Sealed {
			return status, nil
		}
	}
	return status, nil
}

func initUnsealShares(init initResponse) []string {
	if len(init.UnsealKeysB64) > 0 {
		return init.UnsealKeysB64
	}
	return init.KeysBase64
}

func freshInit(ctx context.Context, cfg config, client openBaoClient, rep report) report {
	if len(cfg.pgpKeys) != cfg.keyShares {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoInitialized", "InitRecipientIdentityRequired", "openbao", "fresh init requires one PGP recipient per unseal share"),
			conditionFalse("OpenBaoRecoveryComplete", "WaitingForInit", "openbao", "fresh initialization needs operator recipient identities"),
		)
		return rep
	}
	if cfg.initOutputPath == "" {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoInitMaterialDelivered", "InitMaterialDeliveryRequired", "openbao", "fresh init requires an encrypted init material delivery path"),
			conditionFalse("OpenBaoRecoveryComplete", "WaitingForInit", "openbao", "fresh initialization needs an external handoff for encrypted unseal material"),
		)
		return rep
	}
	init, err := client.Init(ctx, initOptions{
		KeyShares: cfg.keyShares,
		Threshold: cfg.threshold,
	})
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoInitialized", "InitFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "InitFailed", "openbao", "fresh initialization failed"),
		)
		return rep
	}
	rootToken := strings.TrimSpace(init.RootToken)
	if rootToken == "" {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized"),
			conditionFalse("OpenBaoRecoveryComplete", "InitialRootTokenMissing", "openbao", "fresh init did not return a transient root token"),
		)
		return rep
	}
	if err := writeEncryptedInitMaterial(cfg, init, nil); err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized"),
			conditionFalse("OpenBaoInitMaterialDelivered", "InitMaterialDeliveryFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "InitMaterialDeliveryFailed", "openbao", "encrypted init material was not delivered"),
		)
		return rep
	}
	status, err := unsealTemporaryRestoreTarget(ctx, client, init)
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized"),
			conditionTrue("OpenBaoInitMaterialDelivered", "InitOutputWritten", "openbao", "encrypted init material was written to the configured handoff path"),
			conditionFalse("OpenBaoUnsealed", "UnsealFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "UnsealFailed", "openbao", "fresh OpenBao could not be unsealed"),
		)
		return rep
	}
	if status.Sealed {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized"),
			conditionTrue("OpenBaoInitMaterialDelivered", "InitOutputWritten", "openbao", "encrypted init material was written to the configured handoff path"),
			conditionFalse("OpenBaoUnsealed", "UnsealQuorumIncomplete", "openbao", "fresh OpenBao remains sealed"),
			conditionFalse("OpenBaoRecoveryComplete", "WaitingForUnseal", "openbao", "fresh OpenBao could not accept in-memory threshold shares"),
		)
		return rep
	}
	rep.State = "InitializedUnsealed"
	rep.Evidence = statusEvidence(status)
	extra := []condition{
		conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized"),
		conditionTrue("OpenBaoInitMaterialDelivered", "InitOutputWritten", "openbao", "encrypted init material was written to the configured handoff path"),
		conditionTrue("OpenBaoUnsealed", "UnsealComplete", "openbao", "OpenBao was unsealed with in-memory init shares"),
	}
	if cfg.baseline.Reconcile {
		return reconcileFreshBaselineWithInitialRootToken(ctx, cfg, client, rep, rootToken, init, extra)
	}
	revokeCondition := revokePresentedToken(ctx, client, rootToken)
	if revokeCondition.Status != "True" {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			revokeCondition,
			conditionFalse("OpenBaoRecoveryComplete", "InitialRootTokenRevocationFailed", "openbao", "fresh OpenBao is unsealed but the initial root token could not be revoked"),
		)
		return rep
	}
	rep.Conditions = append(rep.Conditions, extra...)
	rep.Conditions = append(rep.Conditions,
		revokeCondition,
		conditionTrue("OpenBaoRecoveryComplete", "Recovered", "openbao", "OpenBao is initialized, unsealed, and available"),
	)
	return rep
}

func reconcileFreshBaselineWithInitialRootToken(ctx context.Context, cfg config, client openBaoClient, rep report, rootToken string, init initResponse, extra []condition) report {
	if err := reconcileBaselineWithRetry(ctx, cfg.baseline, client, rootToken); err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions, baselineReconcileFailureConditions(err)...)
		rep.Conditions = append(rep.Conditions, revokePresentedToken(ctx, client, rootToken))
		return rep
	}
	handoffs, err := createOperatorImportTokenHandoffs(ctx, cfg.baseline, client, rootToken)
	if err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
			conditionFalse("OpenBaoOperatorImportTokenDelivered", "TokenCreateFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "OperatorImportTokenDeliveryFailed", "openbao", "fresh OpenBao could not deliver scoped operator import tokens"),
		)
		rep.Conditions = append(rep.Conditions, revokePresentedToken(ctx, client, rootToken))
		return rep
	}
	if err := writeEncryptedInitMaterial(cfg, init, handoffs); err != nil {
		revokeOperatorImportTokens(ctx, client, handoffs)
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
			conditionFalse("OpenBaoOperatorImportTokenDelivered", "InitMaterialRewriteFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "OperatorImportTokenDeliveryFailed", "openbao", "fresh OpenBao could not deliver scoped operator import tokens"),
		)
		rep.Conditions = append(rep.Conditions, revokePresentedToken(ctx, client, rootToken))
		return rep
	}
	revokeCondition := revokePresentedToken(ctx, client, rootToken)
	if revokeCondition.Status != "True" {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
			operatorImportTokenDeliveredCondition(handoffs),
			revokeCondition,
			conditionFalse("OpenBaoRecoveryComplete", "TransientTokenRevocationFailed", "openbao", "baseline was reconciled but the transient token could not be revoked"),
		)
		return rep
	}
	rep.Conditions = append(rep.Conditions, extra...)
	rep.Conditions = append(rep.Conditions,
		conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
		operatorImportTokenDeliveredCondition(handoffs),
		revokeCondition,
		conditionTrue("OpenBaoRecoveryComplete", "Recovered", "openbao", "OpenBao is unsealed and baseline is reconciled"),
	)
	return rep
}

func createOperatorImportTokenHandoffs(ctx context.Context, baseline openBaoBaselineSpec, client openBaoClient, rootToken string) ([]operatorImportTokenHandoff, error) {
	handoffs := make([]operatorImportTokenHandoff, 0, len(baseline.OperatorImportTokens))
	for _, spec := range baseline.OperatorImportTokens {
		token, err := client.CreateToken(ctx, rootToken, spec)
		if err != nil {
			revokeOperatorImportTokens(ctx, client, handoffs)
			return nil, err
		}
		handoffs = append(handoffs, operatorImportTokenHandoff{
			Name:   strings.TrimSpace(spec.Name),
			Policy: strings.TrimSpace(spec.Policy),
			TTL:    strings.TrimSpace(spec.TTL),
			Uses:   spec.Uses,
			Token:  token,
		})
	}
	return handoffs, nil
}

func revokeOperatorImportTokens(ctx context.Context, client openBaoClient, handoffs []operatorImportTokenHandoff) {
	for _, handoff := range handoffs {
		_ = client.RevokeSelf(ctx, handoff.Token)
	}
}

func operatorImportTokenDeliveredCondition(handoffs []operatorImportTokenHandoff) condition {
	if len(handoffs) == 0 {
		return conditionTrue("OpenBaoOperatorImportTokenDelivered", "NoOperatorImportsConfigured", "openbao", "no scoped operator import tokens are configured")
	}
	return conditionTrue("OpenBaoOperatorImportTokenDelivered", "EncryptedImportTokensWritten", "openbao", fmt.Sprintf("%d scoped operator import token(s) were encrypted into init material", len(handoffs)))
}

func operatorImportTokenObservedCondition(cfg config) (condition, bool) {
	if len(cfg.baseline.OperatorImportTokens) == 0 {
		return condition{}, false
	}
	body, err := os.ReadFile(cfg.initOutputPath)
	if err != nil {
		return conditionFalse("OpenBaoOperatorImportTokenDelivered", "InitMaterialUnreadable", "openbao", err.Error()), true
	}
	var material encryptedInitMaterial
	if err := json.Unmarshal(body, &material); err != nil {
		return conditionFalse("OpenBaoOperatorImportTokenDelivered", "InitMaterialInvalid", "openbao", err.Error()), true
	}
	expected := map[string]struct{}{}
	for _, spec := range cfg.baseline.OperatorImportTokens {
		expected[strings.TrimSpace(spec.Name)] = struct{}{}
	}
	seen := map[string]struct{}{}
	for _, item := range material.Spec.OperatorImportTokens {
		name := strings.TrimSpace(item.Name)
		if _, ok := expected[name]; !ok || len(item.EncryptedTokensB64) == 0 {
			continue
		}
		seen[name] = struct{}{}
	}
	for name := range expected {
		if _, ok := seen[name]; !ok {
			return conditionFalse("OpenBaoOperatorImportTokenDelivered", "EncryptedImportTokensMissing", "openbao", fmt.Sprintf("init material is missing encrypted operator import token %q", name)), true
		}
	}
	return conditionTrue("OpenBaoOperatorImportTokenDelivered", "EncryptedImportTokensPresent", "openbao", fmt.Sprintf("%d scoped operator import token handoff(s) are present in init material", len(expected))), true
}

func writeEncryptedInitMaterial(cfg config, init initResponse, operatorImportTokens []operatorImportTokenHandoff) error {
	shares := initUnsealShares(init)
	if len(shares) != cfg.keyShares {
		return fmt.Errorf("unseal share count %d does not match configured key shares %d", len(shares), cfg.keyShares)
	}
	encryptedShares, err := encryptSharesToPGPRecipients(shares, []string(cfg.pgpKeys))
	if err != nil {
		return err
	}
	material := encryptedInitMaterial{
		APIVersion: "openbao.guardianintelligence.org/v1alpha1",
		Kind:       "OpenBaoEncryptedInitMaterial",
		Metadata:   encryptedInitMaterialMeta{Name: "openbao"},
		Spec: encryptedInitMaterialSpec{
			CreatedAt:                time.Now().UTC().Format(time.RFC3339),
			KeyShares:                cfg.keyShares,
			KeyThreshold:             cfg.threshold,
			PGPRecipientCount:        len(cfg.pgpKeys),
			EncryptedUnsealSharesB64: encryptedShares,
		},
	}
	if len(init.RecoveryKeysB64) > 0 {
		encryptedRecoveryKeys, err := encryptSharesToPGPRecipients(init.RecoveryKeysB64, []string(cfg.pgpKeys))
		if err != nil {
			return err
		}
		material.Spec.EncryptedRecoverySharesB64 = encryptedRecoveryKeys
	}
	for _, handoff := range operatorImportTokens {
		encryptedTokens, err := encryptStringToEachPGPRecipient(handoff.Token, []string(cfg.pgpKeys))
		if err != nil {
			return err
		}
		material.Spec.OperatorImportTokens = append(material.Spec.OperatorImportTokens, encryptedOperatorImportTokenMaterial{
			Name:               handoff.Name,
			Policy:             handoff.Policy,
			TTL:                handoff.TTL,
			Uses:               handoff.Uses,
			EncryptedTokensB64: encryptedTokens,
		})
	}
	body, err := json.MarshalIndent(material, "", "  ")
	if err != nil {
		return fmt.Errorf("encode encrypted init material: %w", err)
	}
	body = append(body, '\n')
	return writePrivateFile(cfg.initOutputPath, body, 0o600)
}

func encryptStringToEachPGPRecipient(value string, recipientPaths []string) ([]string, error) {
	out := make([]string, 0, len(recipientPaths))
	for _, recipientPath := range recipientPaths {
		encrypted, err := encryptStringToPGPRecipient(value, recipientPath)
		if err != nil {
			return nil, err
		}
		out = append(out, encrypted)
	}
	return out, nil
}

func encryptSharesToPGPRecipients(shares []string, recipientPaths []string) ([]string, error) {
	if len(shares) != len(recipientPaths) {
		return nil, fmt.Errorf("OpenBao init share count %d does not match PGP recipient count %d", len(shares), len(recipientPaths))
	}
	out := make([]string, 0, len(shares))
	for i, share := range shares {
		encrypted, err := encryptStringToPGPRecipient(share, recipientPaths[i])
		if err != nil {
			return nil, err
		}
		out = append(out, encrypted)
	}
	return out, nil
}

func encryptStringToPGPRecipient(value string, recipientPath string) (string, error) {
	recipients, err := readPGPRecipients(recipientPath)
	if err != nil {
		return "", err
	}
	var encrypted bytes.Buffer
	writer, err := openpgp.Encrypt(&encrypted, recipients, nil, nil, nil)
	if err != nil {
		return "", fmt.Errorf("create OpenBao init-material PGP message: %w", err)
	}
	if _, err := io.WriteString(writer, value); err != nil {
		_ = writer.Close()
		return "", fmt.Errorf("write OpenBao init-material PGP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close OpenBao init-material PGP message: %w", err)
	}
	return base64.StdEncoding.EncodeToString(encrypted.Bytes()), nil
}

func readPGPRecipients(path string) ([]*openpgp.Entity, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read OpenBao PGP recipient %s: %w", path, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(body)))
	if err != nil {
		return nil, fmt.Errorf("decode OpenBao PGP recipient %s: %w", path, err)
	}
	recipients, err := openpgp.ReadKeyRing(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("parse OpenBao PGP recipient %s: %w", path, err)
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("OpenBao PGP recipient %s contained no public keys", path)
	}
	return recipients, nil
}

func statusReport(cfg config, status baoStatus, err error) report {
	rep := report{Action: "status", State: "Unreachable", Address: cfg.addr}
	if err != nil {
		rep.Conditions = []condition{
			conditionFalse("OpenBaoServerReady", "StatusUnreadable", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "ServerUnreachable", "openbao", "OpenBao status is not readable"),
		}
		return rep
	}
	rep.State = classify(status)
	rep.Evidence = statusEvidence(status)
	rep.Conditions = []condition{
		conditionTrue("OpenBaoServerReady", "StatusReadable", "openbao", "OpenBao status is readable"),
	}
	return rep
}

func classify(status baoStatus) string {
	if !status.Initialized {
		return "Uninitialized"
	}
	if status.Sealed {
		return "InitializedSealed"
	}
	return "InitializedUnsealed"
}

func statusEvidence(status baoStatus) map[string]string {
	out := map[string]string{}
	if status.Version != "" {
		out["openbao_version"] = status.Version
	}
	if status.SealType != "" {
		out["seal_type"] = status.SealType
	}
	if status.ClusterID != "" {
		out["cluster_id"] = status.ClusterID
	}
	if status.ClusterName != "" {
		out["cluster_name"] = status.ClusterName
	}
	if status.Threshold > 0 {
		out["unseal_threshold"] = fmt.Sprint(status.Threshold)
	}
	if status.Progress > 0 {
		out["unseal_progress"] = fmt.Sprint(status.Progress)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func saveSnapshot(ctx context.Context, cfg config, client openBaoClient, token string) report {
	rep := report{Action: "snapshot.save", State: "SnapshotPending", Address: cfg.addr}
	if cfg.snapshotOut == "" || cfg.manifestOut == "" {
		rep.State = "SnapshotBlocked"
		rep.Conditions = []condition{conditionFalse("OpenBaoSnapshotSaved", "OutputPathRequired", "openbao", "snapshot-out and manifest-out are required")}
		return rep
	}
	status, err := client.Status(ctx)
	if err != nil {
		rep.State = "SnapshotBlocked"
		rep.Conditions = []condition{conditionFalse("OpenBaoServerReady", "StatusUnreadable", "openbao", err.Error())}
		return rep
	}
	rep.Evidence = statusEvidence(status)
	if !status.Initialized || status.Sealed {
		rep.State = classify(status)
		rep.Conditions = []condition{conditionFalse("OpenBaoSnapshotSaved", "OpenBaoNotUnsealed", "openbao", "snapshot save requires initialized and unsealed OpenBao")}
		return rep
	}
	raw, err := client.SaveSnapshot(ctx, token)
	if err != nil {
		rep.State = "SnapshotFailed"
		rep.Conditions = []condition{conditionFalse("OpenBaoSnapshotSaved", "SnapshotSaveFailed", "openbao", err.Error())}
		return rep
	}
	if err := writePrivateFile(cfg.snapshotOut, raw, 0o600); err != nil {
		rep.State = "SnapshotFailed"
		rep.Conditions = []condition{conditionFalse("OpenBaoSnapshotSaved", "SnapshotWriteFailed", "openbao", err.Error())}
		return rep
	}
	sum := sha256.Sum256(raw)
	manifest := snapshotManifest{
		APIVersion: "backup.openbao.guardianintelligence.org/v1alpha1",
		Kind:       "OpenBaoRaftSnapshot",
		Metadata:   snapshotManifestMeta{Name: "openbao-raft-snapshot"},
		Spec: snapshotManifestSpec{
			CreatedAt:      time.Now().UTC().Format(time.RFC3339),
			SnapshotSHA256: "sha256:" + hex.EncodeToString(sum[:]),
			SnapshotBytes:  int64(len(raw)),
			OpenBaoVersion: status.Version,
			SealType:       status.SealType,
			ClusterID:      status.ClusterID,
			ClusterName:    status.ClusterName,
			SourceAddress:  cfg.addr,
		},
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		rep.State = "SnapshotFailed"
		rep.Conditions = []condition{conditionFalse("OpenBaoSnapshotSaved", "ManifestEncodeFailed", "openbao", err.Error())}
		return rep
	}
	body = append(body, '\n')
	if err := writePrivateFile(cfg.manifestOut, body, 0o600); err != nil {
		rep.State = "SnapshotFailed"
		rep.Conditions = []condition{conditionFalse("OpenBaoSnapshotSaved", "ManifestWriteFailed", "openbao", err.Error())}
		return rep
	}
	rep.State = "SnapshotSaved"
	rep.Snapshot = &manifest
	rep.Conditions = []condition{
		conditionTrue("OpenBaoSnapshotSaved", "SnapshotSaved", "openbao", "snapshot and manifest were written"),
		conditionTrue("OpenBaoSnapshotVerified", "DigestRecorded", "openbao", "snapshot digest is recorded in manifest"),
	}
	return rep
}

type prepareOptions struct{}

func prepare(ctx context.Context, cfg config, _ prepareOptions) error {
	if err := installRuntime(cfg); err != nil {
		return err
	}
	if os.Geteuid() == 0 {
		if err := prepareHost(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}

func installRuntime(cfg config) error {
	artifact := filepath.Join(cfg.repoRoot, "bazel-bin/src/infrastructure-components/openbao/openbao-runtime.tar")
	releaseName, err := artifactReleaseName(artifact)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create OpenBao runtime root: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.runtimeRoot, "install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open OpenBao runtime install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock OpenBao runtime install: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	release := filepath.Join(cfg.runtimeRoot, "releases", releaseName)
	if runtimeInstalled(release) {
		return promoteRuntime(cfg.runtimeRoot, release)
	}
	tmp := filepath.Join(cfg.runtimeRoot, "tmp", releaseName+"."+fmt.Sprint(os.Getpid()))
	if err := os.RemoveAll(tmp); err != nil {
		return fmt.Errorf("clear temporary OpenBao runtime: %w", err)
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return fmt.Errorf("create temporary OpenBao runtime: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open OpenBao runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	if err := extractTar(file, tmp); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create OpenBao runtime releases directory: %w", err)
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("replace OpenBao runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("promote OpenBao runtime release: %w", err)
	}
	return promoteRuntime(cfg.runtimeRoot, release)
}

func artifactReleaseName(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open OpenBao runtime artifact: %w", err)
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash OpenBao runtime artifact: %w", err)
	}
	return "sha256-" + hex.EncodeToString(hash.Sum(nil)), nil
}

func runtimeInstalled(release string) bool {
	for _, rel := range []string{"bin/bao", "bin/openbao-recover"} {
		stat, err := os.Stat(filepath.Join(release, rel))
		if err != nil || !stat.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func promoteRuntime(runtimeRoot string, release string) error {
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create OpenBao runtime root: %w", err)
	}
	next := filepath.Join(runtimeRoot, "current.next")
	current := filepath.Join(runtimeRoot, "current")
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale OpenBao runtime symlink: %w", err)
	}
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create OpenBao runtime symlink: %w", err)
	}
	if stat, err := os.Lstat(current); err == nil && stat.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink OpenBao current runtime: %w", err)
		}
	}
	return os.Rename(next, current)
}

func extractTar(r io.Reader, dest string) error {
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read OpenBao runtime tar: %w", err)
		}
		target, err := safeExtractPath(destAbs, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, modeOrDefault(header.Mode, 0o755)); err != nil {
				return fmt.Errorf("create runtime directory %s: %w", header.Name, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("create runtime parent %s: %w", header.Name, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, modeOrDefault(header.Mode, 0o644))
			if err != nil {
				return fmt.Errorf("create runtime file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("write runtime file %s: %w", header.Name, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("close runtime file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported OpenBao runtime tar entry %s", header.Name)
		}
	}
}

func safeExtractPath(destAbs string, name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." || filepath.IsAbs(clean) || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return "", fmt.Errorf("unsafe OpenBao runtime tar entry %q", name)
	}
	target := filepath.Join(destAbs, clean)
	if target != destAbs && !strings.HasPrefix(target, destAbs+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe OpenBao runtime tar entry %q", name)
	}
	return target, nil
}

func modeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return os.FileMode(mode) & 0o777
}

func prepareHost(ctx context.Context, cfg config) error {
	if err := ensureGroup(ctx, "openbao"); err != nil {
		return err
	}
	if err := ensureUser(ctx, "openbao"); err != nil {
		return err
	}
	openbaoUser, err := user.Lookup("openbao")
	if err != nil {
		return fmt.Errorf("lookup openbao user: %w", err)
	}
	uid, gid, err := userIDs(openbaoUser)
	if err != nil {
		return err
	}
	dirs := []struct {
		path string
		uid  int
		gid  int
		mode os.FileMode
	}{
		{"/etc/openbao", 0, gid, 0o750},
		{"/etc/openbao/tls", 0, gid, 0o750},
		{"/etc/verself/openbao", 0, 0, 0o755},
		{"/run/verself/recovery/openbao", 0, 0, 0o700},
		{"/var/lib/openbao", uid, gid, 0o700},
		{cfg.dataDir, uid, gid, 0o700},
		{"/var/log/openbao", uid, gid, 0o700},
	}
	for _, dir := range dirs {
		if err := mkdirOwned(dir.path, dir.uid, dir.gid, dir.mode); err != nil {
			return err
		}
	}
	certPath := "/etc/openbao/tls/cert.pem"
	keyPath := "/etc/openbao/tls/key.pem"
	publicCA := "/etc/verself/openbao/ca.pem"
	tlsNeedsRotation, err := tlsMaterialNeedsRotation(certPath, keyPath, publicCA)
	if err != nil {
		return err
	}
	if tlsNeedsRotation {
		if err := writeLocalTLS(certPath, keyPath, publicCA, gid); err != nil {
			return err
		}
	}
	config := openBaoConfig(cfg)
	if err := writeFileAtomic(cfg.configPath, []byte(config), 0o640); err != nil {
		return err
	}
	if err := os.Chown(cfg.configPath, 0, gid); err != nil {
		return fmt.Errorf("chown OpenBao config: %w", err)
	}
	return nil
}

func tlsMaterialNeedsRotation(certPath string, keyPath string, caPath string) (bool, error) {
	for _, path := range []string{certPath, keyPath, caPath} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return true, nil
		} else if err != nil {
			return false, fmt.Errorf("stat OpenBao TLS file %s: %w", path, err)
		}
	}
	if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
		return true, nil
	}
	ca, err := readCertificate(caPath)
	if err != nil {
		return true, nil
	}
	if !ca.IsCA {
		return true, nil
	}
	leaf, err := readCertificate(certPath)
	if err != nil {
		return true, nil
	}
	if err := leaf.CheckSignatureFrom(ca); err != nil {
		return true, nil
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:       roots,
		DNSName:     "localhost",
		CurrentTime: time.Now(),
		KeyUsages:   []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return true, nil
	}
	return false, nil
}

func readCertificate(path string) (*x509.Certificate, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(body)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s does not contain a PEM certificate", path)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

func ensureGroup(ctx context.Context, name string) error {
	if err := exec.CommandContext(ctx, "getent", "group", name).Run(); err == nil {
		return nil
	}
	if out, err := exec.CommandContext(ctx, "/usr/sbin/groupadd", "--system", name).CombinedOutput(); err != nil {
		return fmt.Errorf("create group %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func ensureUser(ctx context.Context, name string) error {
	if _, err := user.Lookup(name); err == nil {
		return nil
	}
	out, err := exec.CommandContext(ctx,
		"/usr/sbin/useradd",
		"--system",
		"--gid", name,
		"--home-dir", "/var/lib/openbao",
		"--shell", "/usr/sbin/nologin",
		"--no-create-home",
		name,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create user %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func userIDs(u *user.User) (int, int, error) {
	uid, err := parseUserID(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := parseUserID(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return uid, gid, nil
}

func parseUserID(value string) (int, error) {
	var parsed int
	if _, err := fmt.Sscanf(value, "%d", &parsed); err != nil {
		return 0, fmt.Errorf("parse user id %q: %w", value, err)
	}
	return parsed, nil
}

func mkdirOwned(path string, uid int, gid int, mode os.FileMode) error {
	if err := os.MkdirAll(path, mode); err != nil {
		return fmt.Errorf("create directory %s: %w", path, err)
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return fmt.Errorf("chown directory %s: %w", path, err)
	}
	return os.Chmod(path, mode)
}

func writeLocalTLS(certPath string, keyPath string, caPath string, groupID int) error {
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS CA key: %w", err)
	}
	caSerial, err := randomSerial()
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS CA serial: %w", err)
	}
	now := time.Now()
	caTemplate := x509.Certificate{
		SerialNumber: caSerial,
		Subject: pkix.Name{
			CommonName: "Verself OpenBao local CA",
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create OpenBao TLS CA cert: %w", err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS server key: %w", err)
	}
	serverSerial, err := randomSerial()
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS server serial: %w", err)
	}
	serverTemplate := x509.Certificate{
		SerialNumber: serverSerial,
		Subject: pkix.Name{
			CommonName: "localhost",
		},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &serverTemplate, &caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create OpenBao TLS cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return fmt.Errorf("marshal OpenBao TLS key: %w", err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := writeFileAtomic(caPath, caPEM, 0o644); err != nil {
		return err
	}
	if err := writeFileAtomic(certPath, certPEM, 0o640); err != nil {
		return err
	}
	if err := writeFileAtomic(keyPath, keyPEM, 0o640); err != nil {
		return err
	}
	if err := os.Chown(certPath, 0, groupID); err != nil {
		return fmt.Errorf("chown OpenBao TLS cert: %w", err)
	}
	if err := os.Chown(keyPath, 0, groupID); err != nil {
		return fmt.Errorf("chown OpenBao TLS key: %w", err)
	}
	return nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	if serial.Sign() == 0 {
		return randomSerial()
	}
	return serial, nil
}

func openBaoConfig(cfg config) string {
	return fmt.Sprintf(`ui = false
disable_mlock = true

api_addr = "https://127.0.0.1:8200"
cluster_addr = "https://127.0.0.1:8201"

storage "raft" {
  path = %q
  node_id = "verself-single-node"
}

listener "tcp" {
  address = "127.0.0.1:8200"
  cluster_address = "127.0.0.1:8201"
  tls_cert_file = "/etc/openbao/tls/cert.pem"
  tls_key_file = "/etc/openbao/tls/key.pem"
  tls_min_version = "tls13"

  telemetry {
    unauthenticated_metrics_access = true
  }
}

telemetry {
  prometheus_retention_time = "1m"
  disable_hostname = true
}

audit "file" "verself" {
  description = "Verself forensic backstop audit log"
  options {
    file_path = "/var/log/openbao/audit.log"
    mode = "0600"
  }
}
`, cfg.dataDir)
}

type realOpenBaoClient struct {
	cfg    config
	client *http.Client
}

func newRealOpenBaoClient(cfg config) (*realOpenBaoClient, error) {
	client, err := apiClient(cfg)
	if err != nil {
		return nil, err
	}
	return &realOpenBaoClient{cfg: cfg, client: client}, nil
}

func (c *realOpenBaoClient) Status(ctx context.Context) (baoStatus, error) {
	cmd := exec.CommandContext(ctx, c.cfg.bao, "status", "-format=json")
	cmd.Env = baoEnv(c.cfg)
	out, err := cmd.Output()
	if status, decodeErr := decodeStatusOutput(out); decodeErr == nil {
		return status, nil
	} else if err == nil {
		return baoStatus{}, decodeErr
	}
	return baoStatus{}, commandError("bao status", err)
}

func (c *realOpenBaoClient) Init(ctx context.Context, opts initOptions) (initResponse, error) {
	args := []string{
		"operator", "init",
		fmt.Sprintf("-key-shares=%d", opts.KeyShares),
		fmt.Sprintf("-key-threshold=%d", opts.Threshold),
		"-format=json",
	}
	cmd := exec.CommandContext(ctx, c.cfg.bao, args...)
	cmd.Env = baoEnv(c.cfg)
	out, err := cmd.Output()
	if err != nil {
		return initResponse{}, commandError("bao operator init", err)
	}
	var init initResponse
	if err := json.Unmarshal(bytes.TrimSpace(out), &init); err != nil {
		return initResponse{}, fmt.Errorf("decode bao operator init: %w", err)
	}
	return init, nil
}

func (c *realOpenBaoClient) Unseal(ctx context.Context, share string) (baoStatus, error) {
	var out baoStatus
	if err := c.apiJSON(ctx, "", http.MethodPost, "sys/unseal", map[string]string{"key": share}, &out, http.StatusOK); err != nil {
		return baoStatus{}, err
	}
	return out, nil
}

func (c *realOpenBaoClient) RestoreSnapshot(ctx context.Context, token string, path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	return c.apiRaw(ctx, token, http.MethodPost, "sys/storage/raft/snapshot-force", bytes.NewReader(body), "application/octet-stream", nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) SaveSnapshot(ctx context.Context, token string) ([]byte, error) {
	var out bytes.Buffer
	if err := c.apiRaw(ctx, token, http.MethodGet, "sys/storage/raft/snapshot", nil, "", &out, http.StatusOK); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func (c *realOpenBaoClient) ReconcileBaseline(ctx context.Context, rootToken string, baseline openBaoBaselineSpec) error {
	if !baseline.Reconcile {
		return nil
	}
	for _, mount := range baseline.Mounts {
		options := map[string]any{}
		for key, value := range mount.Options {
			options[key] = value
		}
		if len(options) == 0 {
			options = nil
		}
		if err := c.ensureMount(ctx, rootToken, mount.Path, mount.Type, mount.Description, options); err != nil {
			return err
		}
	}
	for _, policy := range baseline.Policies {
		if err := c.writePolicy(ctx, rootToken, policy); err != nil {
			return err
		}
	}
	for _, secretPath := range baseline.SecretPaths {
		if secretPath.Source != "generated" {
			continue
		}
		if err := c.ensureGeneratedSecret(ctx, rootToken, secretPath); err != nil {
			return err
		}
	}
	for _, auth := range baseline.JWTAuths {
		if err := c.ensureAuth(ctx, rootToken, auth.Path, "jwt", auth.Description); err != nil {
			return err
		}
		if err := c.configureJWTAuth(ctx, rootToken, auth); err != nil {
			return err
		}
		for _, role := range auth.Roles {
			if err := c.writeJWTRole(ctx, rootToken, auth.Path, role); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *realOpenBaoClient) RevokeSelf(ctx context.Context, token string) error {
	return c.apiJSON(ctx, token, http.MethodPost, "auth/token/revoke-self", map[string]any{}, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) LoginJWT(ctx context.Context, authPath string, role string, jwt string) (string, error) {
	authPath = strings.Trim(strings.TrimSpace(authPath), "/")
	role = strings.TrimSpace(role)
	jwt = strings.TrimSpace(jwt)
	if authPath == "" || role == "" || jwt == "" {
		return "", errors.New("OpenBao JWT auth path, role, and JWT are required")
	}
	var response struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := c.apiJSON(ctx, "", http.MethodPost, "auth/"+authPath+"/login", map[string]string{
		"role": role,
		"jwt":  jwt,
	}, &response, http.StatusOK); err != nil {
		return "", err
	}
	token := strings.TrimSpace(response.Auth.ClientToken)
	if token == "" {
		return "", errors.New("OpenBao JWT login returned an empty token")
	}
	return token, nil
}

func (c *realOpenBaoClient) CreateToken(ctx context.Context, rootToken string, spec openBaoOperatorImportTokenSpec) (string, error) {
	name := strings.TrimSpace(spec.Name)
	policy := strings.TrimSpace(spec.Policy)
	ttl := strings.TrimSpace(spec.TTL)
	if name == "" || policy == "" || ttl == "" {
		return "", errors.New("OpenBao operator import token name, policy, and ttl are required")
	}
	body := map[string]any{
		"display_name": name,
		"policies":     []string{policy},
		"ttl":          ttl,
		"renewable":    false,
		// The handoff must survive revocation of the initial root token.
		"no_parent": true,
	}
	if spec.Uses > 0 {
		body["num_uses"] = spec.Uses
	}
	var response struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := c.apiJSON(ctx, rootToken, http.MethodPost, "auth/token/create", body, &response, http.StatusOK); err != nil {
		return "", err
	}
	token := strings.TrimSpace(response.Auth.ClientToken)
	if token == "" {
		return "", errors.New("OpenBao token create returned an empty token")
	}
	return token, nil
}

func (c *realOpenBaoClient) GenerateRootInit(ctx context.Context) (generateRootAttempt, error) {
	var attempt generateRootAttempt
	if err := c.apiJSON(ctx, "", http.MethodPost, "sys/generate-root/attempt", map[string]any{}, &attempt, http.StatusOK); err != nil {
		return generateRootAttempt{}, err
	}
	return attempt, nil
}

func (c *realOpenBaoClient) GenerateRootUpdate(ctx context.Context, share string, nonce string) (generateRootAttempt, error) {
	var attempt generateRootAttempt
	if err := c.apiJSON(ctx, "", http.MethodPost, "sys/generate-root/update", map[string]string{
		"key":   share,
		"nonce": nonce,
	}, &attempt, http.StatusOK); err != nil {
		return generateRootAttempt{}, err
	}
	return attempt, nil
}

func (c *realOpenBaoClient) GenerateRootCancel(ctx context.Context) error {
	return c.apiJSON(ctx, "", http.MethodDelete, "sys/generate-root/attempt", nil, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) DecodeGeneratedRootToken(ctx context.Context, encoded string, otp string) (string, error) {
	var response struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := c.apiJSON(ctx, "", http.MethodPost, "sys/decode-token", map[string]string{
		"encoded_token": strings.TrimSpace(encoded),
		"otp":           strings.TrimSpace(otp),
	}, &response, http.StatusOK); err != nil {
		return "", err
	}
	token := strings.TrimSpace(response.Data.Token)
	if token == "" {
		return "", errors.New("OpenBao decode-token returned an empty token")
	}
	return token, nil
}

func (c *realOpenBaoClient) ensureMount(ctx context.Context, token string, path string, mountType string, description string, options map[string]any) error {
	path = strings.Trim(strings.TrimSpace(path), "/")
	mountType = strings.TrimSpace(mountType)
	if path == "" || mountType == "" {
		return errors.New("OpenBao baseline mount path and type are required")
	}
	var response map[string]any
	if err := c.apiJSON(ctx, token, http.MethodGet, "sys/mounts", nil, &response, http.StatusOK); err != nil {
		return err
	}
	if _, ok := dataMap(response)[path+"/"]; ok {
		return nil
	}
	body := map[string]any{"type": mountType}
	if strings.TrimSpace(description) != "" {
		body["description"] = strings.TrimSpace(description)
	}
	if options != nil {
		body["options"] = options
	}
	return c.apiJSON(ctx, token, http.MethodPost, "sys/mounts/"+path, body, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) ensureAuth(ctx context.Context, token string, path string, authType string, description string) error {
	path = strings.Trim(strings.TrimSpace(path), "/")
	authType = strings.TrimSpace(authType)
	if path == "" || authType == "" {
		return errors.New("OpenBao baseline auth path and type are required")
	}
	var response map[string]any
	if err := c.apiJSON(ctx, token, http.MethodGet, "sys/auth", nil, &response, http.StatusOK); err != nil {
		return err
	}
	if _, ok := dataMap(response)[path+"/"]; ok {
		return nil
	}
	return c.apiJSON(ctx, token, http.MethodPost, "sys/auth/"+path, map[string]any{
		"type":        authType,
		"description": description,
	}, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) writePolicy(ctx context.Context, token string, policy openBaoPolicySpec) error {
	name := strings.TrimSpace(policy.Name)
	hcl := strings.TrimSpace(policy.HCL)
	if name == "" || hcl == "" {
		return errors.New("OpenBao baseline policy name and hcl are required")
	}
	return c.apiJSON(ctx, token, http.MethodPost, "sys/policies/acl/"+name, map[string]any{
		"policy": hcl,
	}, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) ensureGeneratedSecret(ctx context.Context, token string, secretPath openBaoSecretPathSpec) error {
	path := strings.Trim(strings.TrimSpace(secretPath.Path), "/")
	key := strings.TrimSpace(secretPath.Key)
	if path == "" || key == "" || secretPath.Generate == nil {
		return errors.New("OpenBao generated secret path, key, and generation config are required")
	}
	exists, current, err := c.generatedSecretExists(ctx, token, path, key)
	if err != nil {
		return err
	}
	if exists {
		valid, err := generatedSecretMatches(*secretPath.Generate, current)
		if err != nil {
			return err
		}
		if valid {
			return nil
		}
	}
	value, err := generatedSecretValue(*secretPath.Generate)
	if err != nil {
		return err
	}
	return c.apiJSON(ctx, token, http.MethodPost, path, map[string]any{
		"data": map[string]string{key: value},
	}, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) generatedSecretExists(ctx context.Context, token string, path string, key string) (bool, string, error) {
	status, raw, err := c.apiRawStatus(ctx, token, http.MethodGet, path, nil, "", http.StatusOK, http.StatusNotFound)
	if err != nil {
		return false, "", err
	}
	if status == http.StatusNotFound {
		return false, "", nil
	}
	var response struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, "", fmt.Errorf("decode OpenBao generated secret %s: %w", path, err)
	}
	data, _ := response.Data["data"].(map[string]any)
	return true, strings.TrimSpace(fmt.Sprint(data[key])), nil
}

func generatedSecretValue(spec openBaoGenerateSpec) (string, error) {
	if spec.Bytes <= 0 {
		return "", errors.New("OpenBao generated secret bytes must be positive")
	}
	raw := make([]byte, spec.Bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate OpenBao secret material: %w", err)
	}
	switch spec.Encoding {
	case "hex":
		return hex.EncodeToString(raw), nil
	case "base64url":
		return base64.RawURLEncoding.EncodeToString(raw), nil
	case "alphanumeric":
		return randomAlphanumeric(spec.Bytes)
	case "password":
		return randomPassword(spec.Bytes)
	default:
		return "", fmt.Errorf("unsupported OpenBao generated secret encoding %q", spec.Encoding)
	}
}

func generatedSecretMatches(spec openBaoGenerateSpec, value string) (bool, error) {
	value = strings.TrimSpace(value)
	switch spec.Encoding {
	case "hex":
		if len(value) != spec.Bytes*2 {
			return false, nil
		}
		_, err := hex.DecodeString(value)
		return err == nil, nil
	case "base64url":
		if strings.ContainsAny(value, "+/=") {
			return false, nil
		}
		decoded, err := base64.RawURLEncoding.DecodeString(value)
		return err == nil && len(decoded) == spec.Bytes, nil
	case "alphanumeric":
		if len(value) != spec.Bytes {
			return false, nil
		}
		return allRunesIn(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"), nil
	case "password":
		return passwordMeetsBootstrapPolicy(value, spec.Bytes), nil
	default:
		return false, fmt.Errorf("unsupported OpenBao generated secret encoding %q", spec.Encoding)
	}
}

func randomAlphanumeric(length int) (string, error) {
	if length <= 0 {
		return "", errors.New("OpenBao generated alphanumeric secret length must be positive")
	}
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, length)
	max := big.NewInt(int64(len(alphabet)))
	for i := range out {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("generate OpenBao alphanumeric secret material: %w", err)
		}
		out[i] = alphabet[n.Int64()]
	}
	return string(out), nil
}

func randomPassword(length int) (string, error) {
	if length < 4 {
		return "", errors.New("OpenBao generated password secret length must be at least 4")
	}
	const upper = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const lower = "abcdefghijklmnopqrstuvwxyz"
	const digits = "0123456789"
	const symbols = "!#$%&*+-=?@^_"
	const alphabet = upper + lower + digits + symbols
	out := make([]byte, length)
	required := []string{upper, lower, digits, symbols}
	for i, chars := range required {
		char, err := randomAlphabetByte(chars)
		if err != nil {
			return "", err
		}
		out[i] = char
	}
	for i := len(required); i < len(out); i++ {
		char, err := randomAlphabetByte(alphabet)
		if err != nil {
			return "", err
		}
		out[i] = char
	}
	for i := len(out) - 1; i > 0; i-- {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		if err != nil {
			return "", fmt.Errorf("shuffle OpenBao generated password secret: %w", err)
		}
		j := int(n.Int64())
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

func randomAlphabetByte(alphabet string) (byte, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
	if err != nil {
		return 0, fmt.Errorf("generate OpenBao password secret material: %w", err)
	}
	return alphabet[n.Int64()], nil
}

func passwordMeetsBootstrapPolicy(value string, length int) bool {
	if len(value) != length {
		return false
	}
	return containsRuneFrom(value, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") &&
		containsRuneFrom(value, "abcdefghijklmnopqrstuvwxyz") &&
		containsRuneFrom(value, "0123456789") &&
		containsRuneFrom(value, "!#$%&*+-=?@^_")
}

func allRunesIn(value string, alphabet string) bool {
	for _, char := range value {
		if !strings.ContainsRune(alphabet, char) {
			return false
		}
	}
	return true
}

func containsRuneFrom(value string, alphabet string) bool {
	for _, char := range value {
		if strings.ContainsRune(alphabet, char) {
			return true
		}
	}
	return false
}

func (c *realOpenBaoClient) configureJWTAuth(ctx context.Context, token string, auth openBaoJWTAuthSpec) error {
	path := strings.Trim(strings.TrimSpace(auth.Path), "/")
	jwksURL := strings.TrimSpace(auth.JWKSURL)
	if path == "" || len(auth.SupportedAlgs) == 0 {
		return errors.New("OpenBao JWT auth path and supportedAlgs are required")
	}
	body := map[string]any{
		"jwt_supported_algs": auth.SupportedAlgs,
	}
	switch {
	case jwksURL != "" && auth.SPIREBundle == nil:
		body["jwks_url"] = jwksURL
	case jwksURL == "" && auth.SPIREBundle != nil:
		pubkeys, err := spireJWTValidationPubkeys(ctx, *auth.SPIREBundle)
		if err != nil {
			return err
		}
		body["jwt_validation_pubkeys"] = pubkeys
	default:
		return errors.New("OpenBao JWT auth requires exactly one validation source")
	}
	return c.apiJSON(ctx, token, http.MethodPost, "auth/"+path+"/config", body, nil, http.StatusNoContent, http.StatusOK)
}

func spireJWTValidationPubkeys(ctx context.Context, spec openBaoSPIREBundleSpec) ([]string, error) {
	spireServer := strings.TrimSpace(spec.SPIREServerPath)
	socketPath := strings.TrimSpace(spec.SocketPath)
	if spireServer == "" || socketPath == "" {
		return nil, errors.New("OpenBao SPIRE bundle source requires spireServerPath and socketPath")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, spireServer, "bundle", "show", "-socketPath", socketPath, "-format", "spiffe", "-output", "json").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("read SPIRE JWT bundle: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return spireJWTValidationPubkeysFromBundle(out)
}

func spireJWTValidationPubkeysFromBundle(raw []byte) ([]string, error) {
	var bundle struct {
		JWTAuthorities []struct {
			PublicKey string `json:"public_key"`
			Tainted   bool   `json:"tainted"`
		} `json:"jwt_authorities"`
	}
	if err := json.Unmarshal(raw, &bundle); err != nil {
		return nil, fmt.Errorf("decode SPIRE JWT bundle: %w", err)
	}
	pubkeys := make([]string, 0, len(bundle.JWTAuthorities))
	for _, authority := range bundle.JWTAuthorities {
		if authority.Tainted {
			continue
		}
		der, err := base64.StdEncoding.DecodeString(strings.TrimSpace(authority.PublicKey))
		if err != nil {
			return nil, fmt.Errorf("decode SPIRE JWT public key: %w", err)
		}
		if _, err := x509.ParsePKIXPublicKey(der); err != nil {
			return nil, fmt.Errorf("parse SPIRE JWT public key: %w", err)
		}
		pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
		if len(pemBlock) == 0 {
			return nil, errors.New("encode SPIRE JWT public key")
		}
		pubkeys = append(pubkeys, string(pemBlock))
	}
	if len(pubkeys) == 0 {
		return nil, errors.New("SPIRE bundle contains no active JWT authorities")
	}
	return pubkeys, nil
}

func (c *realOpenBaoClient) writeJWTRole(ctx context.Context, token string, authPath string, role openBaoJWTRoleSpec) error {
	authPath = strings.Trim(strings.TrimSpace(authPath), "/")
	name := strings.TrimSpace(role.Name)
	if authPath == "" || name == "" {
		return errors.New("OpenBao JWT auth path and role name are required")
	}
	body := map[string]any{
		"role_type":               role.RoleType,
		"bound_audiences":         role.BoundAudiences,
		"user_claim":              role.UserClaim,
		"user_claim_json_pointer": role.UserClaimJSONPointer,
		"claim_mappings":          role.ClaimMappings,
		"token_type":              role.TokenType,
		"token_policies":          role.TokenPolicies,
		"token_period":            role.TokenPeriod,
		"token_explicit_max_ttl":  role.TokenExplicitMaxTTL,
	}
	if len(role.BoundClaims) > 0 {
		body["bound_claims"] = role.BoundClaims
	}
	if strings.TrimSpace(role.BoundSubject) != "" {
		body["bound_subject"] = strings.TrimSpace(role.BoundSubject)
	}
	if err := requireJWTString("roleType", role.RoleType); err != nil {
		return err
	}
	if err := requireJWTString("userClaim", role.UserClaim); err != nil {
		return err
	}
	if err := requireJWTString("tokenType", role.TokenType); err != nil {
		return err
	}
	if err := requireJWTString("tokenPeriod", role.TokenPeriod); err != nil {
		return err
	}
	if len(role.BoundAudiences) == 0 || len(role.TokenPolicies) == 0 {
		return errors.New("OpenBao JWT role boundAudiences and tokenPolicies are required")
	}
	return c.apiJSON(ctx, token, http.MethodPost, "auth/"+authPath+"/role/"+name, body, nil, http.StatusNoContent, http.StatusOK)
}

func requireJWTString(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("OpenBao JWT role %s is required", field)
	}
	return nil
}

func (c *realOpenBaoClient) apiJSON(ctx context.Context, token string, method string, path string, body any, out any, expected ...int) error {
	var requestBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode OpenBao API body: %w", err)
		}
		requestBody = bytes.NewReader(raw)
	}
	var response bytes.Buffer
	if err := c.apiRaw(ctx, token, method, path, requestBody, "application/json", &response, expected...); err != nil {
		return err
	}
	if out == nil || len(bytes.TrimSpace(response.Bytes())) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Bytes(), out); err != nil {
		return fmt.Errorf("decode OpenBao API response: %w", err)
	}
	return nil
}

func (c *realOpenBaoClient) apiRaw(ctx context.Context, token string, method string, path string, body io.Reader, contentType string, out io.Writer, expected ...int) error {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.addr, "/")+"/v1/"+path, body)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	for _, status := range expected {
		if resp.StatusCode == status {
			if out != nil {
				if _, err := io.Copy(out, resp.Body); err != nil {
					return fmt.Errorf("read openbao %s %s response: %w", method, path, err)
				}
			}
			return nil
		}
	}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func (c *realOpenBaoClient) apiRawStatus(ctx context.Context, token string, method string, path string, body io.Reader, contentType string, expected ...int) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.cfg.addr, "/")+"/v1/"+path, body)
	if err != nil {
		return 0, nil, err
	}
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	if body != nil && contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("openbao %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if readErr != nil {
		return resp.StatusCode, nil, fmt.Errorf("read openbao %s %s response: %w", method, path, readErr)
	}
	for _, status := range expected {
		if resp.StatusCode == status {
			return resp.StatusCode, raw, nil
		}
	}
	return resp.StatusCode, raw, fmt.Errorf("openbao %s %s status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
}

func apiClient(cfg config) (*http.Client, error) {
	pool, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("load system cert pool: %w", err)
	}
	if cfg.caCert != "" {
		body, err := os.ReadFile(cfg.caCert)
		if err != nil {
			return nil, fmt.Errorf("read OpenBao CA cert: %w", err)
		}
		if !pool.AppendCertsFromPEM(body) {
			return nil, fmt.Errorf("OpenBao CA cert %s did not contain a PEM certificate", cfg.caCert)
		}
	}
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS13},
		},
	}, nil
}

func baoEnv(cfg config) []string {
	env := append([]string{}, os.Environ()...)
	env = append(env, "BAO_ADDR="+cfg.addr)
	if cfg.caCert != "" {
		env = append(env, "BAO_CACERT="+cfg.caCert)
	}
	return env
}

func decodeStatusOutput(out []byte) (baoStatus, error) {
	var status baoStatus
	if err := json.Unmarshal(bytes.TrimSpace(out), &status); err != nil {
		return baoStatus{}, fmt.Errorf("decode bao status: %w", err)
	}
	return status, nil
}

func commandError(op string, err error) error {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Errorf("%s: %w: %s", op, err, sanitizeCommandOutput(exit.Stderr))
	}
	return fmt.Errorf("%s: %w", op, err)
}

func sanitizeCommandOutput(out []byte) string {
	text := strings.TrimSpace(string(out))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}

func dataMap(response map[string]any) map[string]any {
	data, ok := response["data"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return data
}

func readToken(stdin io.Reader, enabled bool) (string, error) {
	if !enabled {
		return "", errors.New("operator token stdin is not enabled")
	}
	body, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read operator token stdin: %w", err)
	}
	token := strings.TrimSpace(string(body))
	if token == "" {
		return "", errors.New("operator token stdin was empty")
	}
	return token, nil
}

func readUnsealShares(stdin io.Reader, enabled bool) ([]string, error) {
	if !enabled {
		return nil, errors.New("unseal share stdin is not enabled")
	}
	body, err := io.ReadAll(io.LimitReader(stdin, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read unseal stdin: %w", err)
	}
	var shares []string
	for _, line := range strings.Split(string(body), "\n") {
		share := strings.TrimSpace(line)
		if share != "" {
			shares = append(shares, share)
		}
	}
	if len(shares) == 0 {
		return nil, errors.New("unseal stdin was empty")
	}
	return shares, nil
}

func writeReport(stdout io.Writer, reportPath string, rep report) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode recovery report: %w", err)
	}
	body = append(body, '\n')
	if _, err := stdout.Write(body); err != nil {
		return fmt.Errorf("write recovery report stdout: %w", err)
	}
	if reportPath == "" {
		return nil
	}
	return writePrivateFile(reportPath, body, 0o600)
}

func writeRecoveryReport(stdout io.Writer, reportPath string, rep report) error {
	if err := writeReport(stdout, reportPath, rep); err != nil {
		return err
	}
	complete, ok := findCondition(rep, "OpenBaoRecoveryComplete")
	if ok && complete.Status == "True" {
		return nil
	}
	if ok {
		return fmt.Errorf("OpenBao recovery incomplete: %s: %s", complete.Reason, complete.Message)
	}
	return errors.New("OpenBao recovery incomplete: OpenBaoRecoveryComplete condition missing")
}

func findCondition(rep report, conditionType string) (condition, bool) {
	for _, candidate := range rep.Conditions {
		if candidate.Type == conditionType {
			return candidate, true
		}
	}
	return condition{}, false
}

func writePrivateFile(path string, body []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create parent directory for %s: %w", path, err)
	}
	return writeFileAtomic(path, body, mode)
}

func writeFileAtomic(path string, body []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, mode); err != nil {
		return fmt.Errorf("write temporary file %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace file %s: %w", path, err)
	}
	return os.Chmod(path, mode)
}

func conditionTrue(conditionType string, reason string, resource string, message string) condition {
	return condition{Type: conditionType, Status: "True", Reason: reason, Resource: resource, Message: message}
}

func conditionFalse(conditionType string, reason string, resource string, message string) condition {
	return condition{Type: conditionType, Status: "False", Reason: reason, Resource: resource, Message: message}
}

func readSnapshotManifest(path string) (snapshotManifest, error) {
	if strings.TrimSpace(path) == "" {
		return snapshotManifest{}, errors.New("snapshot manifest path is required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return snapshotManifest{}, fmt.Errorf("read snapshot manifest: %w", err)
	}
	var manifest snapshotManifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return snapshotManifest{}, fmt.Errorf("decode snapshot manifest: %w", err)
	}
	if manifest.Spec.SnapshotSHA256 == "" {
		return snapshotManifest{}, errors.New("snapshot manifest missing spec.snapshotSHA256")
	}
	if manifest.Spec.SnapshotBytes <= 0 {
		return snapshotManifest{}, errors.New("snapshot manifest missing spec.snapshotBytes")
	}
	return manifest, nil
}

func verifySnapshotDigest(path string, manifest snapshotManifest) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read snapshot: %w", err)
	}
	if int64(len(body)) != manifest.Spec.SnapshotBytes {
		return fmt.Errorf("snapshot size %d did not match manifest size %d", len(body), manifest.Spec.SnapshotBytes)
	}
	sum := sha256.Sum256(body)
	got := "sha256:" + hex.EncodeToString(sum[:])
	if got != manifest.Spec.SnapshotSHA256 {
		return fmt.Errorf("snapshot digest %s did not match manifest digest %s", got, manifest.Spec.SnapshotSHA256)
	}
	return nil
}
