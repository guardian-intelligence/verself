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
	"time"
)

const (
	defaultRepoRoot    = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultRuntimeRoot = "/var/lib/openbao/runtime"
	defaultDataDir     = "/var/lib/openbao/raft"
	defaultConfigPath  = "/etc/openbao/openbao.hcl"
	defaultAddr        = "https://127.0.0.1:8200"
	defaultCACert      = "/etc/openbao/tls/cert.pem"
	defaultReportPath  = "/run/verself/recovery/openbao/report.json"
	defaultKeyShares   = 3
	defaultThreshold   = 2
)

type config struct {
	repoRoot          string
	runtimeRoot       string
	dataDir           string
	configPath        string
	reportPath        string
	addr              string
	caCert            string
	bao               string
	keyShares         int
	threshold         int
	pgpKeys           stringList
	rootTokenPGPKey   string
	initOutputPath    string
	snapshotPath      string
	snapshotManifest  string
	snapshotOut       string
	manifestOut       string
	tokenStdin        bool
	unsealStdin       bool
	generateRootStdin bool
	loopInterval      time.Duration
	resourceGraph     string
	resourceName      string
	pgpKeyDir         string
	baseline          openBaoBaselineSpec
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
	KeyShares       int
	Threshold       int
	PGPKeys         []string
	RootTokenPGPKey string
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
	CreatedAt                  string   `json:"createdAt"`
	KeyShares                  int      `json:"keyShares"`
	KeyThreshold               int      `json:"keyThreshold"`
	PGPRecipientCount          int      `json:"pgpRecipientCount"`
	RootTokenPGPRecipient      bool     `json:"rootTokenPGPRecipient"`
	EncryptedUnsealSharesB64   []string `json:"encryptedUnsealSharesB64"`
	EncryptedRecoverySharesB64 []string `json:"encryptedRecoverySharesB64,omitempty"`
	EncryptedRootTokenB64      string   `json:"encryptedRootTokenB64,omitempty"`
}

type openBaoClient interface {
	Status(context.Context) (baoStatus, error)
	Init(context.Context, initOptions) (initResponse, error)
	Unseal(context.Context, string) (baoStatus, error)
	RestoreSnapshot(context.Context, string, string) error
	SaveSnapshot(context.Context, string) ([]byte, error)
	ReconcileBaseline(context.Context, string, openBaoBaselineSpec) error
	RevokeSelf(context.Context, string) error
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
		return prepare(ctx, cfg)
	case "recover":
		cfg, err := parseConfig("openbao-recover recover", args[1:], true, false)
		if err != nil {
			return err
		}
		client, err := newRealOpenBaoClient(cfg)
		if err != nil {
			return err
		}
		rep := recoverOnce(ctx, cfg, client, stdin)
		return writeReport(stdout, cfg.reportPath, rep)
	case "loop":
		cfg, err := parseConfig("openbao-recover loop", args[1:], true, false)
		if err != nil {
			return err
		}
		client, err := newRealOpenBaoClient(cfg)
		if err != nil {
			return err
		}
		return loop(ctx, cfg, client, stdout, stdin)
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
		loopInterval: 15 * time.Second,
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
	fs.DurationVar(&cfg.loopInterval, "loop-interval", cfg.loopInterval, "loop command interval")
	fs.StringVar(&cfg.resourceGraph, "resource-graph", "", "Guardian resource graph document path")
	fs.StringVar(&cfg.resourceName, "resource-name", cfg.resourceName, "OpenBaoCluster resource name")
	fs.StringVar(&cfg.pgpKeyDir, "pgp-key-dir", cfg.pgpKeyDir, "directory for public PGP key files derived from the resource graph")
	if recoveryFlags {
		fs.IntVar(&cfg.keyShares, "key-shares", cfg.keyShares, "fresh init key shares")
		fs.IntVar(&cfg.threshold, "key-threshold", cfg.threshold, "fresh init key threshold")
		fs.Var(&cfg.pgpKeys, "pgp-key", "PGP public recipient for encrypted init output; repeat")
		fs.StringVar(&cfg.rootTokenPGPKey, "root-token-pgp-key", "", "optional PGP public recipient for root token output")
		fs.StringVar(&cfg.initOutputPath, "init-output", "", "non-durable path for PGP-encrypted init material")
		fs.StringVar(&cfg.snapshotPath, "snapshot", "", "verified Raft snapshot path to restore")
		fs.StringVar(&cfg.snapshotManifest, "snapshot-manifest", "", "snapshot manifest path")
		fs.BoolVar(&cfg.unsealStdin, "unseal-stdin", false, "read unseal shares from stdin, one per line")
		fs.BoolVar(&cfg.tokenStdin, "operator-token-stdin", false, "read an operator token from stdin")
		fs.BoolVar(&cfg.generateRootStdin, "generate-root-token-stdin", false, "generate a transient root token from stdin unseal shares")
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
	cfg = normalizeConfig(cfg)
	if cfg.resourceGraph != "" {
		next, err := applyResourceGraphConfig(cfg)
		if err != nil {
			return config{}, err
		}
		if !baoExplicit {
			next.bao = ""
		}
		cfg = normalizeConfig(next)
	}
	if cfg.tokenStdin && cfg.generateRootStdin {
		return config{}, errors.New("--operator-token-stdin and --generate-root-token-stdin are mutually exclusive")
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
	cfg.rootTokenPGPKey = strings.TrimSpace(cfg.rootTokenPGPKey)
	cfg.initOutputPath = strings.TrimSpace(cfg.initOutputPath)
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
	LoopInterval     string              `json:"loopInterval"`
	Seal             openBaoSealSpec     `json:"seal"`
	Snapshots        openBaoSnapshotSpec `json:"snapshots"`
	Baseline         openBaoBaselineSpec `json:"baseline"`
}

type openBaoSealSpec struct {
	Shamir openBaoShamirSpec `json:"shamir"`
}

type openBaoShamirSpec struct {
	KeyShares             int         `json:"keyShares"`
	KeyThreshold          int         `json:"keyThreshold"`
	PGPRecipientRefs      []objectRef `json:"pgpRecipientRefs"`
	RootTokenRecipientRef *objectRef  `json:"rootTokenRecipientRef"`
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
	Reconcile bool                     `json:"reconcile"`
	Mounts    []openBaoMountSpec       `json:"mounts"`
	Policies  []openBaoPolicySpec      `json:"policies"`
	NomadJWT  *openBaoNomadJWTAuthSpec `json:"nomadJWT"`
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

type openBaoNomadJWTAuthSpec struct {
	Path          string                    `json:"path"`
	Description   string                    `json:"description"`
	JWKSURL       string                    `json:"jwksURL"`
	SupportedAlgs []string                  `json:"supportedAlgs"`
	Roles         []openBaoNomadJWTRoleSpec `json:"roles"`
}

type openBaoNomadJWTRoleSpec struct {
	Name                 string            `json:"name"`
	RoleType             string            `json:"roleType"`
	BoundAudiences       []string          `json:"boundAudiences"`
	BoundClaims          map[string]string `json:"boundClaims"`
	UserClaim            string            `json:"userClaim"`
	UserClaimJSONPointer bool              `json:"userClaimJSONPointer"`
	ClaimMappings        map[string]string `json:"claimMappings"`
	TokenType            string            `json:"tokenType"`
	TokenPolicies        []string          `json:"tokenPolicies"`
	TokenPeriod          string            `json:"tokenPeriod"`
	TokenExplicitMaxTTL  int               `json:"tokenExplicitMaxTTL"`
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
	if spec.LoopInterval != "" {
		interval, err := time.ParseDuration(spec.LoopInterval)
		if err != nil {
			return config{}, fmt.Errorf("OpenBaoCluster %q spec.loopInterval: %w", name, err)
		}
		cfg.loopInterval = interval
	}
	if spec.Snapshots.Restore != nil {
		cfg.snapshotPath = spec.Snapshots.Restore.SnapshotPath
		cfg.snapshotManifest = spec.Snapshots.Restore.ManifestPath
	}
	pgpFiles, err := materializePGPRecipients(cfg, resources, spec.Seal.Shamir.PGPRecipientRefs)
	if err != nil {
		return config{}, err
	}
	cfg.pgpKeys = stringList(pgpFiles)
	if spec.Seal.Shamir.RootTokenRecipientRef != nil {
		rootFiles, err := materializePGPRecipients(cfg, resources, []objectRef{*spec.Seal.Shamir.RootTokenRecipientRef})
		if err != nil {
			return config{}, err
		}
		if len(rootFiles) == 1 {
			cfg.rootTokenPGPKey = rootFiles[0]
		}
	}
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
  openbao-recover loop [flags]
  openbao-recover status [flags]
  openbao-recover snapshot save --snapshot-out <path> --manifest-out <path> --token-stdin [flags]
  openbao-recover snapshot verify --snapshot <path> --snapshot-manifest <path> [flags]

Recovery reports contain conditions and fingerprints only. Root trust material
must be provided through ephemeral operator paths, not host files.
`)
}

func loop(ctx context.Context, cfg config, client openBaoClient, stdout io.Writer, stdin io.Reader) error {
	for {
		rep := recoverOnce(ctx, cfg, client, stdin)
		if err := writeReport(stdout, cfg.reportPath, rep); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.loopInterval):
		}
	}
}

func recoverOnce(ctx context.Context, cfg config, client openBaoClient, stdin io.Reader) report {
	if err := prepare(ctx, cfg); err != nil {
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
		if cfg.baseline.Reconcile && !cfg.tokenStdin && !cfg.generateRootStdin {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("RootTrustMaterialAvailable", "OperatorRootCredentialsRequired", "openbao", "baseline reconciliation requires operator root authority"),
				conditionFalse("OpenBaoBaselineReconciled", "OperatorRootCredentialsRequired", "openbao", "baseline reconciliation requires operator root authority"),
				conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
			)
			return rep
		}
		if !cfg.tokenStdin && !cfg.generateRootStdin {
			rep.Conditions = append(rep.Conditions,
				conditionTrue("RootTrustMaterialAvailable", "NotRequired", "openbao", "OpenBao is already unsealed"),
				conditionTrue("OpenBaoRecoveryComplete", "Available", "openbao", "OpenBao is initialized, unsealed, and available"),
			)
			return rep
		}
		if cfg.generateRootStdin {
			shares, err := readUnsealShares(stdin, true)
			if err != nil {
				rep.Conditions = append(rep.Conditions,
					conditionFalse("RootTrustMaterialAvailable", "UnsealQuorumIncomplete", "openbao", "threshold unseal material is required to generate a transient root token"),
					conditionFalse("OpenBaoGeneratedRootToken", "UnsealQuorumIncomplete", "openbao", "threshold unseal material is required to generate a transient root token"),
					conditionFalse("OpenBaoBaselineReconciled", "OperatorRootCredentialsRequired", "openbao", "baseline reconciliation requires root authority"),
					conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
				)
				return rep
			}
			token, err := generateRootTokenFromShares(ctx, client, shares)
			if err != nil {
				rep.Conditions = append(rep.Conditions,
					conditionFalse("RootTrustMaterialAvailable", "GenerateRootFailed", "openbao", "threshold unseal material did not produce a transient root token"),
					conditionFalse("OpenBaoGeneratedRootToken", "GenerateRootFailed", "openbao", err.Error()),
					conditionFalse("OpenBaoBaselineReconciled", "OperatorRootCredentialsRequired", "openbao", "baseline reconciliation requires root authority"),
					conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
				)
				return rep
			}
			rootTrust := conditionTrue("RootTrustMaterialAvailable", "GeneratedRootToken", "openbao", "threshold unseal material generated a transient root token")
			extra := []condition{conditionTrue("OpenBaoGeneratedRootToken", "Generated", "openbao", "transient root token was generated from threshold unseal material")}
			return reconcileBaselineWithToken(ctx, cfg.baseline, client, rep, token, rootTrust, extra)
		}
		token, err := readToken(stdin, true)
		if err != nil {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("RootTrustMaterialAvailable", "OperatorRootCredentialsRequired", "openbao", "baseline reconciliation requires an operator token"),
				conditionFalse("OpenBaoBaselineReconciled", "OperatorRootCredentialsRequired", "openbao", "baseline reconciliation requires an operator token"),
				conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
			)
			return rep
		}
		rootTrust := conditionTrue("RootTrustMaterialAvailable", "Presented", "openbao", "operator token was presented through stdin")
		return reconcileBaselineWithToken(ctx, cfg.baseline, client, rep, token, rootTrust, nil)
	case "InitializedSealed":
		shares, err := readUnsealShares(stdin, cfg.unsealStdin)
		if err != nil {
			rep.Conditions = append(rep.Conditions,
				conditionFalse("RootTrustMaterialAvailable", "UnsealQuorumIncomplete", "openbao", "threshold unseal material is required"),
				conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "OpenBao is sealed"),
			)
			return rep
		}
		for _, share := range shares {
			status, err = client.Unseal(ctx, share)
			if err != nil {
				rep.Conditions = append(rep.Conditions,
					conditionFalse("RootTrustMaterialAvailable", "SnapshotTrustMaterialMismatch", "openbao", "unseal material was rejected"),
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
				conditionFalse("RootTrustMaterialAvailable", "UnsealQuorumIncomplete", "openbao", "not enough unseal shares were presented"),
				conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "OpenBao remains sealed"),
			)
			return rep
		}
		rep.State = "InitializedUnsealed"
		rep.Evidence = statusEvidence(status)
		rep.Conditions = append(rep.Conditions,
			conditionTrue("RootTrustMaterialAvailable", "Presented", "openbao", "threshold unseal material was presented through stdin"),
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

func reconcileBaselineWithToken(ctx context.Context, baseline openBaoBaselineSpec, client openBaoClient, rep report, token string, rootTrust condition, extra []condition) report {
	if err := client.ReconcileBaseline(ctx, token, baseline); err != nil {
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions, baselineReconcileFailureConditions(err, rootTrust)...)
		rep.Conditions = append(rep.Conditions, revokePresentedToken(ctx, client, token))
		return rep
	}
	revokeCondition := revokePresentedToken(ctx, client, token)
	if revokeCondition.Status != "True" {
		rep.Conditions = append(rep.Conditions, rootTrust)
		rep.Conditions = append(rep.Conditions, extra...)
		rep.Conditions = append(rep.Conditions,
			conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
			revokeCondition,
			conditionFalse("OpenBaoRecoveryComplete", "OperatorTokenRevocationFailed", "openbao", "baseline was reconciled but the presented operator token could not be revoked"),
		)
		return rep
	}
	rep.Conditions = append(rep.Conditions, rootTrust)
	rep.Conditions = append(rep.Conditions, extra...)
	rep.Conditions = append(rep.Conditions,
		conditionTrue("OpenBaoBaselineReconciled", "BaselineReady", "openbao", "baseline mounts, auth, and policies are reconciled"),
		revokeCondition,
		conditionTrue("OpenBaoRecoveryComplete", "Recovered", "openbao", "OpenBao is unsealed and baseline is reconciled"),
	)
	return rep
}

func baselineReconcileFailureConditions(err error, rootTrust condition) []condition {
	if isOpenBaoPermissionDenied(err) {
		return []condition{
			conditionFalse("RootTrustMaterialAvailable", "OperatorRootCredentialsRequired", "openbao", "presented operator token does not have baseline reconciliation authority"),
			conditionFalse("OpenBaoBaselineReconciled", "OperatorRootCredentialsRequired", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "BaselineBlocked", "openbao", "OpenBao is unsealed but baseline reconciliation is blocked"),
		}
	}
	return []condition{
		rootTrust,
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
		return conditionFalse("OpenBaoOperatorTokenRevoked", reason, "openbao", err.Error())
	}
	return conditionTrue("OpenBaoOperatorTokenRevoked", "Revoked", "openbao", "presented operator token was revoked")
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

func restoreSnapshot(ctx context.Context, cfg config, client openBaoClient, rep report) report {
	if cfg.snapshotPath == "" || cfg.snapshotManifest == "" {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("RootTrustMaterialAvailable", "BackupRetrievalAuthorityRequired", "openbao", "snapshot and manifest are required for restore"),
			conditionFalse("OpenBaoRecoveryComplete", "SnapshotMissing", "openbao", "snapshot restore cannot start"),
		)
		return rep
	}
	manifest, err := readSnapshotManifest(cfg.snapshotManifest)
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("RootTrustMaterialAvailable", "BackupRetrievalAuthorityRequired", "openbao", err.Error()),
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
		conditionFalse("RootTrustMaterialAvailable", "UnsealQuorumIncomplete", "openbao", "restored snapshot requires matching unseal material"),
		conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "restored OpenBao must be unsealed"),
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
			conditionFalse("RootTrustMaterialAvailable", "InitRecipientIdentityRequired", "openbao", "fresh init requires one PGP recipient per unseal share"),
			conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "fresh initialization needs operator recipient identities"),
		)
		return rep
	}
	if cfg.rootTokenPGPKey == "" {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("RootTrustMaterialAvailable", "InitRootTokenRecipientIdentityRequired", "openbao", "fresh init requires a PGP recipient for the generated root token"),
			conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "fresh initialization needs an operator recipient identity for the generated root token"),
		)
		return rep
	}
	if cfg.initOutputPath == "" {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("RootTrustMaterialAvailable", "InitMaterialDeliveryRequired", "openbao", "fresh init requires an encrypted init material delivery path"),
			conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "fresh initialization needs an external handoff for encrypted unseal material"),
		)
		return rep
	}
	init, err := client.Init(ctx, initOptions{
		KeyShares:       cfg.keyShares,
		Threshold:       cfg.threshold,
		PGPKeys:         []string(cfg.pgpKeys),
		RootTokenPGPKey: cfg.rootTokenPGPKey,
	})
	if err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionFalse("OpenBaoInitialized", "InitFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "InitFailed", "openbao", "fresh initialization failed"),
		)
		return rep
	}
	if err := writeEncryptedInitMaterial(cfg, init); err != nil {
		rep.Conditions = append(rep.Conditions,
			conditionTrue("RootTrustMaterialAvailable", "Created", "openbao", "PGP recipient identities were provided"),
			conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized with encrypted unseal material"),
			conditionFalse("OpenBaoInitMaterialDelivered", "InitMaterialDeliveryFailed", "openbao", err.Error()),
			conditionFalse("OpenBaoRecoveryComplete", "InitMaterialDeliveryFailed", "openbao", "encrypted init material was not delivered"),
		)
		return rep
	}
	rep.State = "InitializedSealed"
	rep.Conditions = append(rep.Conditions,
		conditionTrue("RootTrustMaterialAvailable", "Created", "openbao", "PGP recipient identities were provided"),
		conditionTrue("OpenBaoInitialized", "FreshInitComplete", "openbao", "OpenBao was initialized with encrypted unseal material"),
		conditionTrue("OpenBaoInitMaterialDelivered", "InitOutputWritten", "openbao", "encrypted init material was written to the configured handoff path"),
		conditionFalse("OpenBaoRecoveryComplete", "WaitingForRootTrustMaterial", "openbao", "OpenBao is initialized and waiting for operator-held unseal material"),
	)
	return rep
}

func writeEncryptedInitMaterial(cfg config, init initResponse) error {
	shares := initUnsealShares(init)
	if len(shares) != cfg.keyShares {
		return fmt.Errorf("encrypted unseal share count %d does not match configured key shares %d", len(shares), cfg.keyShares)
	}
	material := encryptedInitMaterial{
		APIVersion: "roottrust.openbao.guardianintelligence.org/v1alpha1",
		Kind:       "OpenBaoEncryptedInitMaterial",
		Metadata:   encryptedInitMaterialMeta{Name: "openbao"},
		Spec: encryptedInitMaterialSpec{
			CreatedAt:                time.Now().UTC().Format(time.RFC3339),
			KeyShares:                cfg.keyShares,
			KeyThreshold:             cfg.threshold,
			PGPRecipientCount:        len(cfg.pgpKeys),
			RootTokenPGPRecipient:    cfg.rootTokenPGPKey != "",
			EncryptedUnsealSharesB64: shares,
			EncryptedRootTokenB64:    strings.TrimSpace(init.RootToken),
		},
	}
	if len(init.RecoveryKeysB64) > 0 {
		material.Spec.EncryptedRecoverySharesB64 = append([]string{}, init.RecoveryKeysB64...)
	}
	body, err := json.MarshalIndent(material, "", "  ")
	if err != nil {
		return fmt.Errorf("encode encrypted init material: %w", err)
	}
	body = append(body, '\n')
	return writePrivateFile(cfg.initOutputPath, body, 0o600)
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

func prepare(ctx context.Context, cfg config) error {
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
	tlsMissing, err := tlsPairMissing(certPath, keyPath)
	if err != nil {
		return err
	}
	if tlsMissing {
		if err := writeSelfSignedTLS(certPath, keyPath, gid); err != nil {
			return err
		}
	}
	publicCA := "/etc/verself/openbao/ca.pem"
	certBytes, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read OpenBao cert: %w", err)
	}
	if err := writeFileAtomic(publicCA, certBytes, 0o644); err != nil {
		return err
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

func tlsPairMissing(certPath string, keyPath string) (bool, error) {
	for _, path := range []string{certPath, keyPath} {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return true, nil
		} else if err != nil {
			return false, fmt.Errorf("stat OpenBao TLS file %s: %w", path, err)
		}
	}
	return false, nil
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

func writeSelfSignedTLS(certPath string, keyPath string, groupID int) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate OpenBao TLS key: %w", err)
	}
	serial := make([]byte, 16)
	if _, err := rand.Read(serial); err != nil {
		return fmt.Errorf("generate OpenBao TLS serial: %w", err)
	}
	template := x509.Certificate{
		SerialNumber:          new(big.Int).SetBytes(serial),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"localhost"},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return fmt.Errorf("create OpenBao TLS cert: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal OpenBao TLS key: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
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
	if len(opts.PGPKeys) > 0 {
		args = append(args, "-pgp-keys="+strings.Join(opts.PGPKeys, ","))
	}
	if opts.RootTokenPGPKey != "" {
		args = append(args, "-root-token-pgp-key="+opts.RootTokenPGPKey)
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
	if baseline.NomadJWT == nil {
		return nil
	}
	auth := *baseline.NomadJWT
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
	return nil
}

func (c *realOpenBaoClient) RevokeSelf(ctx context.Context, token string) error {
	return c.apiJSON(ctx, token, http.MethodPost, "auth/token/revoke-self", map[string]any{}, nil, http.StatusNoContent, http.StatusOK)
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

func (c *realOpenBaoClient) configureJWTAuth(ctx context.Context, token string, auth openBaoNomadJWTAuthSpec) error {
	path := strings.Trim(strings.TrimSpace(auth.Path), "/")
	jwksURL := strings.TrimSpace(auth.JWKSURL)
	if path == "" || jwksURL == "" || len(auth.SupportedAlgs) == 0 {
		return errors.New("OpenBao Nomad JWT auth path, jwksURL, and supportedAlgs are required")
	}
	return c.apiJSON(ctx, token, http.MethodPost, "auth/"+path+"/config", map[string]any{
		"jwks_url":           jwksURL,
		"jwt_supported_algs": auth.SupportedAlgs,
	}, nil, http.StatusNoContent, http.StatusOK)
}

func (c *realOpenBaoClient) writeJWTRole(ctx context.Context, token string, authPath string, role openBaoNomadJWTRoleSpec) error {
	authPath = strings.Trim(strings.TrimSpace(authPath), "/")
	name := strings.TrimSpace(role.Name)
	if authPath == "" || name == "" {
		return errors.New("OpenBao Nomad JWT auth path and role name are required")
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
		return errors.New("OpenBao Nomad JWT role boundAudiences and tokenPolicies are required")
	}
	return c.apiJSON(ctx, token, http.MethodPost, "auth/"+authPath+"/role/"+name, body, nil, http.StatusNoContent, http.StatusOK)
}

func requireJWTString(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("OpenBao Nomad JWT role %s is required", field)
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
