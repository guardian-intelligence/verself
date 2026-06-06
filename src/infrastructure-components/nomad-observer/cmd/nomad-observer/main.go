package main

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/hashicorp/nomad/api"
	verselfotel "github.com/verself/observability/otel"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	serviceName    = "nomad-observer"
	serviceVersion = "0.1.0"
	defaultNS      = "default"

	apiVersion      = "nomadobserver.guardianintelligence.org/v1alpha1"
	kind            = "NomadObserver"
	defaultRepoRoot = "/home/ubuntu/.local/state/guardian/repo/current"
	defaultResource = "nomad-observer"

	heartbeatInterval = 60 * time.Second
)

var (
	terminalAllocState = map[string]struct{}{
		api.AllocClientStatusFailed: {},
		api.AllocClientStatusLost:   {},
	}
)

type config struct {
	runtimeArtifact       string
	runtimeRoot           string
	dataDir               string
	graphPath             string
	reportPath            string
	user                  string
	group                 string
	supplementaryGroups   []string
	nomadAddr             string
	namespace             string
	otlpEndpoint          string
	clickhouseAddr        string
	clickhouseUser        string
	clickhouseCACertPath  string
	spiffeSocket          string
	fleetSnapshotInterval time.Duration
}

type graphOptions struct {
	repoRoot      string
	resourceGraph string
	resourceName  string
}

type document struct {
	Resources []resource `json:"resources"`
}

type resource struct {
	APIVersion string          `json:"apiVersion"`
	Kind       string          `json:"kind"`
	Metadata   metadata        `json:"metadata"`
	Spec       json.RawMessage `json:"spec"`
}

type metadata struct {
	Name string `json:"name"`
}

type crdSpec struct {
	RuntimeArtifact     string   `json:"runtimeArtifact"`
	RuntimeRoot         string   `json:"runtimeRoot"`
	DataDir             string   `json:"dataDir"`
	GraphPath           string   `json:"graphPath"`
	ReportPath          string   `json:"reportPath"`
	User                string   `json:"user"`
	Group               string   `json:"group"`
	SupplementaryGroups []string `json:"supplementaryGroups"`
	Nomad               struct {
		Address   string `json:"address"`
		Namespace string `json:"namespace"`
	} `json:"nomad"`
	OTel struct {
		ExporterEndpoint string `json:"exporterEndpoint"`
	} `json:"otel"`
	Fleet struct {
		SnapshotIntervalSeconds int `json:"snapshotIntervalSeconds"`
	} `json:"fleet"`
	ClickHouse struct {
		Address    string `json:"address"`
		User       string `json:"user"`
		CACertPath string `json:"caCertPath"`
	} `json:"clickhouse"`
	SPIFFE struct {
		EndpointSocket string `json:"endpointSocket"`
	} `json:"spiffe"`
}

type condition struct {
	Type     string `json:"type"`
	Status   string `json:"status"`
	Reason   string `json:"reason"`
	Resource string `json:"resource"`
	Message  string `json:"message,omitempty"`
}

type report struct {
	Component             string      `json:"component"`
	ResourceName          string      `json:"resourceName"`
	RuntimeArtifactDigest string      `json:"runtimeArtifactDigest,omitempty"`
	Conditions            []condition `json:"conditions"`
}

type deployMeta struct {
	DeployRunKey   string
	DeploySHA      string
	SpecSHA256     string
	ArtifactSHA256 string
}

func (m deployMeta) empty() bool {
	return m.DeployRunKey == "" && m.DeploySHA == "" && m.SpecSHA256 == "" && m.ArtifactSHA256 == ""
}

type jobMetaCache struct {
	mu     sync.Mutex
	values map[string]deployMeta
}

func newJobMetaCache() *jobMetaCache {
	return &jobMetaCache{values: make(map[string]deployMeta)}
}

func (c *jobMetaCache) get(namespace, jobID string) (deployMeta, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.values[jobCacheKey(namespace, jobID)]
	return meta, ok
}

func (c *jobMetaCache) put(namespace, jobID string, meta deployMeta) {
	if jobID == "" {
		return
	}
	c.mu.Lock()
	c.values[jobCacheKey(namespace, jobID)] = meta
	c.mu.Unlock()
}

func jobCacheKey(namespace, jobID string) string {
	if namespace == "" {
		namespace = defaultNS
	}
	return namespace + "\x00" + jobID
}

type observerStats struct {
	events       atomic.Uint64
	failures     atomic.Uint64
	streamErrors atomic.Uint64
}

type observer struct {
	cfg    config
	client *api.Client
	logger *slog.Logger
	tracer trace.Tracer
	meta   *jobMetaCache
	stats  observerStats
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := dispatch(ctx, os.Args[1:]); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func dispatch(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: nomad-observer recover|run --resource-graph=<path>")
	}
	switch args[0] {
	case "recover":
		return recoverObserver(args[1:])
	case "run":
		cfg, err := configFromGraphArgs(args[1:])
		if err != nil {
			return err
		}
		return run(ctx, cfg)
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func run(ctx context.Context, cfg config) error {
	shutdown, logger, err := verselfotel.Init(ctx, verselfotel.Config{
		ServiceName:      serviceName,
		ServiceVersion:   serviceVersion,
		ExporterEndpoint: cfg.otlpEndpoint,
	})
	if err != nil {
		return fmt.Errorf("otel init: %w", err)
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = shutdown(flushCtx)
		cancel()
	}()
	logger = logger.With(
		slog.String("verself.supervisor", "nomad"),
	)
	slog.SetDefault(logger)

	nomadClient, err := newNomadClient(cfg.nomadAddr)
	if err != nil {
		return fmt.Errorf("nomad client: %w", err)
	}

	if err := startFleetProjector(ctx, cfg, nomadClient, logger); err != nil {
		logger.Warn("nomad-observer clickhouse sink disabled", slog.Any("error", err))
	}

	obs := &observer{
		cfg:    cfg,
		client: nomadClient,
		logger: logger,
		tracer: otel.Tracer(serviceName),
		meta:   newJobMetaCache(),
	}
	go obs.heartbeat(ctx)

	return obs.streamLoop(ctx)
}

func newNomadClient(address string) (*api.Client, error) {
	nomadConfig := api.DefaultConfig()
	nomadConfig.Address = address
	nomadConfig.HttpClient = &http.Client{
		Transport: &http.Transport{
			MaxConnsPerHost:     8,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     30 * time.Second,
		},
	}
	return api.NewClient(nomadConfig)
}

func startFleetProjector(ctx context.Context, cfg config, nomadClient *api.Client, logger *slog.Logger) error {
	if cfg.clickhouseCACertPath == "" {
		return errors.New("NomadObserver.spec.clickhouse.caCertPath must not be empty")
	}
	if cfg.spiffeSocket == "" {
		return errors.New("NomadObserver.spec.spiffe.endpointSocket must not be empty")
	}
	spiffeSource, err := workloadauth.Source(ctx, cfg.spiffeSocket)
	if err != nil {
		return fmt.Errorf("spiffe source: %w", err)
	}
	chTLS, err := workloadauth.TLSConfigWithX509SourceAndCABundle(ctx, spiffeSource, cfg.clickhouseCACertPath)
	if err != nil {
		_ = spiffeSource.Close()
		return fmt.Errorf("clickhouse tls: %w", err)
	}
	chConn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{cfg.clickhouseAddr},
		Auth: clickhouse.Auth{Database: "verself", Username: cfg.clickhouseUser},
		TLS:  chTLS,
	})
	if err != nil {
		_ = spiffeSource.Close()
		return fmt.Errorf("open clickhouse: %w", err)
	}
	pingCtx, pingCancel := context.WithTimeout(ctx, 5*time.Second)
	err = chConn.Ping(pingCtx)
	pingCancel()
	if err != nil {
		_ = chConn.Close()
		_ = spiffeSource.Close()
		return fmt.Errorf("ping clickhouse: %w", err)
	}
	region, err := nomadClient.Agent().Region()
	if err != nil {
		_ = chConn.Close()
		_ = spiffeSource.Close()
		return fmt.Errorf("nomad region: %w", err)
	}
	go func() {
		<-ctx.Done()
		_ = chConn.Close()
		_ = spiffeSource.Close()
	}()
	go (&fleetProjector{
		nomad:    nomadClient,
		ch:       chConn,
		region:   region,
		interval: cfg.fleetSnapshotInterval,
		logger:   logger,
	}).run(ctx)
	return nil
}

func recoverObserver(args []string) error {
	opts, cfg, err := loadGraphConfig(args)
	if err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return errors.New("nomad-observer recover must run as root")
	}
	if err := ensureAccount(cfg); err != nil {
		return err
	}
	if err := prepareDirectories(cfg); err != nil {
		return err
	}
	if err := projectGraph(opts.resourceGraph, cfg); err != nil {
		return err
	}
	digest, err := installRuntime(opts.repoRoot, cfg)
	if err != nil {
		return err
	}
	rep := report{
		Component:             "nomad-observer",
		ResourceName:          opts.resourceName,
		RuntimeArtifactDigest: digest,
		Conditions: []condition{
			conditionTrue("NomadObserverRuntimeInstalled", "RuntimeReady", "repo-built Nomad Observer runtime is installed"),
			conditionTrue("NomadObserverAccountReady", "AccountReady", "Nomad Observer service account and directories are ready"),
			conditionTrue("NomadObserverRecoveryComplete", "Recovered", "Nomad Observer is ready for Nomad to start"),
		},
	}
	return writeReport(cfg.reportPath, rep, cfg)
}

func configFromGraphArgs(args []string) (config, error) {
	_, cfg, err := loadGraphConfig(args)
	return cfg, err
}

func loadGraphConfig(args []string) (graphOptions, config, error) {
	opts, err := parseGraphOptions(args)
	if err != nil {
		return graphOptions{}, config{}, err
	}
	cfg, err := loadConfig(opts)
	if err != nil {
		return graphOptions{}, config{}, err
	}
	if err := validateConfig(cfg); err != nil {
		return graphOptions{}, config{}, err
	}
	return opts, cfg, nil
}

func parseGraphOptions(args []string) (graphOptions, error) {
	opts := graphOptions{
		repoRoot:     defaultRepoRoot,
		resourceName: defaultResource,
	}
	fs := flag.NewFlagSet("nomad-observer", flag.ContinueOnError)
	fs.StringVar(&opts.repoRoot, "repo-root", opts.repoRoot, "Boarded repo root.")
	fs.StringVar(&opts.resourceGraph, "resource-graph", "", "Guardian resource graph document path.")
	fs.StringVar(&opts.resourceName, "resource-name", opts.resourceName, "NomadObserver resource name.")
	if err := fs.Parse(args); err != nil {
		return graphOptions{}, err
	}
	if fs.NArg() != 0 {
		return graphOptions{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	opts.repoRoot = strings.TrimSpace(opts.repoRoot)
	opts.resourceGraph = strings.TrimSpace(opts.resourceGraph)
	opts.resourceName = strings.TrimSpace(opts.resourceName)
	if opts.resourceGraph == "" {
		opts.resourceGraph = filepath.Join(opts.repoRoot, "workspace/.guardian/fly/document.json")
	}
	if opts.repoRoot == "" || opts.resourceName == "" {
		return graphOptions{}, errors.New("--repo-root and --resource-name are required")
	}
	repoRoot, err := filepath.Abs(opts.repoRoot)
	if err != nil {
		return graphOptions{}, fmt.Errorf("resolve repo root: %w", err)
	}
	opts.repoRoot = repoRoot
	return opts, nil
}

func loadConfig(opts graphOptions) (config, error) {
	body, err := os.ReadFile(opts.resourceGraph)
	if err != nil {
		return config{}, fmt.Errorf("read Guardian resource graph: %w", err)
	}
	var doc document
	if err := json.Unmarshal(body, &doc); err != nil {
		return config{}, fmt.Errorf("decode Guardian resource graph: %w", err)
	}
	var matches []resource
	for _, resource := range doc.Resources {
		if resource.APIVersion == apiVersion && resource.Kind == kind && resource.Metadata.Name == opts.resourceName {
			matches = append(matches, resource)
		}
	}
	if len(matches) != 1 {
		return config{}, fmt.Errorf("expected exactly one %s resource named %q, found %d", kind, opts.resourceName, len(matches))
	}
	var spec crdSpec
	if err := json.Unmarshal(matches[0].Spec, &spec); err != nil {
		return config{}, fmt.Errorf("decode NomadObserver spec: %w", err)
	}
	return config{
		runtimeArtifact:       spec.RuntimeArtifact,
		runtimeRoot:           spec.RuntimeRoot,
		dataDir:               spec.DataDir,
		graphPath:             spec.GraphPath,
		reportPath:            spec.ReportPath,
		user:                  spec.User,
		group:                 spec.Group,
		supplementaryGroups:   spec.SupplementaryGroups,
		nomadAddr:             spec.Nomad.Address,
		namespace:             spec.Nomad.Namespace,
		otlpEndpoint:          spec.OTel.ExporterEndpoint,
		clickhouseAddr:        spec.ClickHouse.Address,
		clickhouseUser:        spec.ClickHouse.User,
		clickhouseCACertPath:  spec.ClickHouse.CACertPath,
		spiffeSocket:          spec.SPIFFE.EndpointSocket,
		fleetSnapshotInterval: time.Duration(spec.Fleet.SnapshotIntervalSeconds) * time.Second,
	}, nil
}

func validateConfig(cfg config) error {
	for name, value := range map[string]string{
		"runtimeArtifact":       cfg.runtimeArtifact,
		"runtimeRoot":           cfg.runtimeRoot,
		"dataDir":               cfg.dataDir,
		"graphPath":             cfg.graphPath,
		"reportPath":            cfg.reportPath,
		"user":                  cfg.user,
		"group":                 cfg.group,
		"nomad.address":         cfg.nomadAddr,
		"nomad.namespace":       cfg.namespace,
		"otel.exporterEndpoint": cfg.otlpEndpoint,
		"clickhouse.address":    cfg.clickhouseAddr,
		"clickhouse.user":       cfg.clickhouseUser,
		"clickhouse.caCertPath": cfg.clickhouseCACertPath,
		"spiffe.endpointSocket": cfg.spiffeSocket,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("NomadObserver.spec.%s is required", name)
		}
	}
	if filepath.IsAbs(cfg.runtimeArtifact) || strings.Contains(filepath.ToSlash(cfg.runtimeArtifact), "../") {
		return errors.New("NomadObserver.spec.runtimeArtifact must be repo-relative")
	}
	for name, value := range map[string]string{
		"runtimeRoot":           cfg.runtimeRoot,
		"dataDir":               cfg.dataDir,
		"graphPath":             cfg.graphPath,
		"reportPath":            cfg.reportPath,
		"clickhouse.caCertPath": cfg.clickhouseCACertPath,
	} {
		if !filepath.IsAbs(value) {
			return fmt.Errorf("NomadObserver.spec.%s must be an absolute path", name)
		}
	}
	if cfg.fleetSnapshotInterval < 5*time.Second || cfg.fleetSnapshotInterval > time.Hour {
		return errors.New("NomadObserver.spec.fleet.snapshotIntervalSeconds must be between 5 and 3600")
	}
	return nil
}

func ensureAccount(cfg config) error {
	if _, err := user.LookupGroup(cfg.group); err != nil {
		if _, ok := err.(user.UnknownGroupError); !ok {
			return fmt.Errorf("lookup Nomad Observer group: %w", err)
		}
		if err := command("/usr/sbin/groupadd", "--system", cfg.group); err != nil {
			return err
		}
	}
	groups := strings.Join(cfg.supplementaryGroups, ",")
	if _, err := user.Lookup(cfg.user); err != nil {
		if _, ok := err.(user.UnknownUserError); !ok {
			return fmt.Errorf("lookup Nomad Observer user: %w", err)
		}
		args := []string{"--system", "--gid", cfg.group, "--home-dir", cfg.dataDir, "--shell", "/usr/sbin/nologin", "--create-home"}
		if groups != "" {
			args = append(args, "--groups", groups)
		}
		args = append(args, cfg.user)
		return command("/usr/sbin/useradd", args...)
	}
	if groups != "" {
		if err := command("/usr/sbin/usermod", "--gid", cfg.group, "-a", "-G", groups, cfg.user); err != nil {
			return err
		}
	}
	return nil
}

func prepareDirectories(cfg config) error {
	uid, gid, err := ids(cfg.user, cfg.group)
	if err != nil {
		return err
	}
	for _, dir := range []string{
		cfg.dataDir,
		cfg.runtimeRoot,
		filepath.Dir(cfg.graphPath),
		filepath.Dir(cfg.reportPath),
	} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return fmt.Errorf("chown %s: %w", dir, err)
		}
	}
	return nil
}

func projectGraph(source string, cfg config) error {
	body, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("read Guardian resource graph for projection: %w", err)
	}
	if err := writeOwnedFile(cfg.graphPath, body, 0o640, cfg); err != nil {
		return err
	}
	return nil
}

func ids(userName string, groupName string) (int, int, error) {
	u, err := user.Lookup(userName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup user %s: %w", userName, err)
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return 0, 0, fmt.Errorf("lookup group %s: %w", groupName, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse uid %s: %w", u.Uid, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return 0, 0, fmt.Errorf("parse gid %s: %w", g.Gid, err)
	}
	return uid, gid, nil
}

func installRuntime(repoRoot string, cfg config) (string, error) {
	artifact := filepath.Join(repoRoot, filepath.FromSlash(cfg.runtimeArtifact))
	digest, err := fileSHA256(artifact)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(cfg.runtimeRoot, 0o755); err != nil {
		return "", fmt.Errorf("create Nomad Observer runtime root: %w", err)
	}
	lock, err := os.OpenFile(filepath.Join(cfg.runtimeRoot, ".install.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return "", fmt.Errorf("open Nomad Observer runtime install lock: %w", err)
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return "", fmt.Errorf("lock Nomad Observer runtime install: %w", err)
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()
	release := filepath.Join(cfg.runtimeRoot, "releases", strings.ReplaceAll(digest, ":", "-"))
	if !runtimeInstalled(release) {
		if err := extractRuntimeTar(artifact, release); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(release, 0o755); err != nil {
		return "", fmt.Errorf("chmod Nomad Observer runtime release: %w", err)
	}
	if err := promoteRuntime(cfg.runtimeRoot, release); err != nil {
		return "", err
	}
	return digest, nil
}

func runtimeInstalled(release string) bool {
	stat, err := os.Stat(filepath.Join(release, "bin/nomad-observer"))
	return err == nil && stat.Mode().IsRegular()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open Nomad Observer runtime artifact: %w", err)
	}
	defer file.Close()
	h := sha256.New()
	if _, err := io.Copy(h, file); err != nil {
		return "", fmt.Errorf("hash Nomad Observer runtime artifact: %w", err)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func extractRuntimeTar(artifact string, release string) error {
	if err := os.MkdirAll(filepath.Dir(release), 0o755); err != nil {
		return fmt.Errorf("create Nomad Observer runtime release parent: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Dir(release), "."+filepath.Base(release)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create Nomad Observer runtime staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	if err := extractTar(artifact, tmp); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fmt.Errorf("chmod Nomad Observer runtime staging directory: %w", err)
	}
	if !runtimeInstalled(tmp) {
		return errors.New("Nomad Observer runtime artifact missing bin/nomad-observer")
	}
	if err := os.RemoveAll(release); err != nil {
		return fmt.Errorf("remove stale Nomad Observer runtime release: %w", err)
	}
	if err := os.Rename(tmp, release); err != nil {
		return fmt.Errorf("publish Nomad Observer runtime release: %w", err)
	}
	return nil
}

func extractTar(artifact string, dest string) error {
	file, err := os.Open(artifact)
	if err != nil {
		return fmt.Errorf("open Nomad Observer runtime artifact: %w", err)
	}
	defer file.Close()
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(file)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read Nomad Observer runtime tar: %w", err)
		}
		target, err := safeTarTarget(destAbs, header.Name)
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
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, modeOrDefault(header.Mode, 0o644))
			if err != nil {
				return fmt.Errorf("create runtime file %s: %w", header.Name, err)
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return fmt.Errorf("write runtime file %s: %w", header.Name, err)
			}
			if err := out.Close(); err != nil {
				return fmt.Errorf("close runtime file %s: %w", header.Name, err)
			}
		default:
			return fmt.Errorf("unsupported Nomad Observer runtime tar entry %s type %d", header.Name, header.Typeflag)
		}
	}
}

func safeTarTarget(destAbs string, name string) (string, error) {
	if filepath.IsAbs(name) {
		return "", fmt.Errorf("unsafe Nomad Observer runtime tar entry %s", name)
	}
	target := filepath.Join(destAbs, name)
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if targetAbs != destAbs && !strings.HasPrefix(targetAbs, destAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("unsafe Nomad Observer runtime tar entry %s", name)
	}
	return targetAbs, nil
}

func modeOrDefault(mode int64, fallback os.FileMode) os.FileMode {
	if mode == 0 {
		return fallback
	}
	return os.FileMode(mode).Perm()
}

func promoteRuntime(runtimeRoot string, release string) error {
	if err := os.MkdirAll(runtimeRoot, 0o755); err != nil {
		return fmt.Errorf("create Nomad Observer runtime root: %w", err)
	}
	next := filepath.Join(runtimeRoot, "current.next")
	current := filepath.Join(runtimeRoot, "current")
	if err := os.Remove(next); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale Nomad Observer current symlink: %w", err)
	}
	if err := os.Symlink(release, next); err != nil {
		return fmt.Errorf("create Nomad Observer current symlink: %w", err)
	}
	if stat, err := os.Lstat(current); err == nil && stat.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(current); err != nil {
			return fmt.Errorf("remove non-symlink Nomad Observer current runtime: %w", err)
		}
	}
	if err := os.Rename(next, current); err != nil {
		return fmt.Errorf("publish Nomad Observer current runtime: %w", err)
	}
	return nil
}

func writeReport(path string, rep report, cfg config) error {
	body, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Nomad Observer report: %w", err)
	}
	body = append(body, '\n')
	return writeOwnedFile(path, body, 0o640, cfg)
}

func writeOwnedFile(path string, body []byte, mode os.FileMode, cfg config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("rename %s to %s: %w", tmpName, path, err)
	}
	if cfg.group != "" {
		_, gid, err := ids(cfg.user, cfg.group)
		if err != nil {
			return err
		}
		if err := os.Chown(path, 0, gid); err != nil {
			return fmt.Errorf("chown %s: %w", path, err)
		}
	}
	return nil
}

func conditionTrue(t string, reason string, message string) condition {
	return condition{Type: t, Status: "True", Reason: reason, Resource: "nomad-observer", Message: message}
}

func command(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (o *observer) streamLoop(ctx context.Context) error {
	topics := map[api.Topic][]string{
		api.TopicDeployment: {},
		api.TopicEvaluation: {},
		api.TopicAllocation: {},
		api.TopicJob:        {},
		api.TopicNode:       {},
	}
	var nextIndex uint64
	backoff := time.Second
	for ctx.Err() == nil {
		streamBaseCtx, cancelStream := context.WithCancel(ctx)
		streamCtx, span := o.tracer.Start(streamBaseCtx, "nomad_observer.event_stream",
			trace.WithAttributes(
				attribute.String("nomad.namespace", o.cfg.namespace),
				attribute.Int64("nomad.stream.index", int64(nextIndex)),
			),
		)
		events, err := o.client.EventStream().Stream(streamCtx, topics, nextIndex, &api.QueryOptions{Namespace: o.cfg.namespace})
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			cancelStream()
			span.End()
			o.stats.streamErrors.Add(1)
			o.logger.WarnContext(streamCtx, "nomad.observer.stream_open_failed",
				slog.String("nomad.namespace", o.cfg.namespace),
				slog.Uint64("nomad.stream.index", nextIndex),
				slog.String("error", err.Error()),
			)
			if !sleepContext(ctx, backoff) {
				return ctx.Err()
			}
			backoff = growBackoff(backoff)
			continue
		}
		o.logger.InfoContext(streamCtx, "nomad.observer.stream_connected",
			slog.String("nomad.namespace", o.cfg.namespace),
			slog.Uint64("nomad.stream.index", nextIndex),
		)
		backoff = time.Second
		for batch := range events {
			if batch.Err != nil {
				o.stats.streamErrors.Add(1)
				span.RecordError(batch.Err)
				span.SetStatus(codes.Error, batch.Err.Error())
				o.logger.WarnContext(streamCtx, "nomad.observer.stream_error",
					slog.String("nomad.namespace", o.cfg.namespace),
					slog.Uint64("nomad.stream.index", nextIndex),
					slog.String("error", batch.Err.Error()),
				)
				break
			}
			if batch.Index >= nextIndex {
				nextIndex = batch.Index + 1
			}
			for i := range batch.Events {
				event := batch.Events[i]
				if event.Index >= nextIndex {
					nextIndex = event.Index + 1
				}
				o.stats.events.Add(1)
				o.handleEvent(streamCtx, event)
			}
		}
		cancelStream()
		span.End()
		if !sleepContext(ctx, backoff) {
			return ctx.Err()
		}
		backoff = growBackoff(backoff)
	}
	return ctx.Err()
}

func growBackoff(current time.Duration) time.Duration {
	current *= 2
	if current > 30*time.Second {
		return 30 * time.Second
	}
	return current
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (o *observer) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			o.logger.InfoContext(ctx, "nomad.observer.heartbeat",
				slog.Uint64("nomad.observer.events", o.stats.events.Load()),
				slog.Uint64("nomad.observer.failures", o.stats.failures.Load()),
				slog.Uint64("nomad.observer.stream_errors", o.stats.streamErrors.Load()),
			)
		}
	}
}

func (o *observer) handleEvent(ctx context.Context, event api.Event) {
	switch event.Topic {
	case api.TopicJob:
		o.handleJobEvent(ctx, event)
	case api.TopicDeployment:
		o.handleDeploymentEvent(ctx, event)
	case api.TopicEvaluation:
		o.handleEvaluationEvent(ctx, event)
	case api.TopicAllocation:
		o.handleAllocationEvent(ctx, event)
	case api.TopicNode:
		o.handleNodeEvent(ctx, event)
	default:
		o.logNomadEvent(ctx, slog.LevelInfo, "nomad.event", event, nil)
	}
}

func (o *observer) handleJobEvent(ctx context.Context, event api.Event) {
	job, deleted, err := event.DeregisteredJob()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	if job != nil {
		namespace := ptrValue(job.Namespace)
		jobID := ptrValue(job.ID)
		meta := metadataFromJob(job)
		o.meta.put(namespace, jobID, meta)
		attrs = append(attrs,
			slog.String("nomad.namespace", normalizeNamespace(namespace, o.cfg.namespace)),
			slog.String("nomad.job_id", jobID),
			slog.Bool("nomad.job.deleted", deleted),
			slog.String("nomad.job.status", ptrValue(job.Status)),
			slog.Uint64("nomad.job.version", ptrUint64(job.Version)),
			slog.Uint64("nomad.job.modify_index", ptrUint64(job.ModifyIndex)),
		)
		attrs = appendMetaAttrs(attrs, meta)
	} else {
		attrs = append(attrs, slog.Bool("nomad.job.deleted", deleted))
	}
	o.logAttrs(ctx, slog.LevelInfo, "nomad.job.event", attrs)
}

func (o *observer) handleDeploymentEvent(ctx context.Context, event api.Event) {
	deployment, err := event.Deployment()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if deployment != nil {
		meta, err := o.metadataForJob(ctx, deployment.Namespace, deployment.JobID)
		if err != nil {
			attrs = append(attrs, slog.String("nomad.metadata_error", err.Error()))
		}
		desired, placed, healthy, unhealthy := deploymentAllocCounts(deployment)
		attrs = append(attrs,
			slog.String("nomad.namespace", normalizeNamespace(deployment.Namespace, o.cfg.namespace)),
			slog.String("nomad.job_id", deployment.JobID),
			slog.String("nomad.deployment_id", deployment.ID),
			slog.String("nomad.deployment.status", deployment.Status),
			slog.String("nomad.deployment.status_description", deployment.StatusDescription),
			slog.Uint64("nomad.job.version", deployment.JobVersion),
			slog.Uint64("nomad.deployment.modify_index", deployment.ModifyIndex),
			slog.Int("nomad.deployment.desired_total", desired),
			slog.Int("nomad.deployment.placed_allocs", placed),
			slog.Int("nomad.deployment.healthy_allocs", healthy),
			slog.Int("nomad.deployment.unhealthy_allocs", unhealthy),
		)
		attrs = appendMetaAttrs(attrs, meta)
		if deploymentFailed(deployment.Status) {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.deployment.event", attrs)
}

func (o *observer) handleEvaluationEvent(ctx context.Context, event api.Event) {
	eval, err := event.Evaluation()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if eval != nil {
		meta, err := o.metadataForJob(ctx, eval.Namespace, eval.JobID)
		if err != nil {
			attrs = append(attrs, slog.String("nomad.metadata_error", err.Error()))
		}
		attrs = append(attrs,
			slog.String("nomad.namespace", normalizeNamespace(eval.Namespace, o.cfg.namespace)),
			slog.String("nomad.eval_id", eval.ID),
			slog.String("nomad.job_id", eval.JobID),
			slog.String("nomad.deployment_id", eval.DeploymentID),
			slog.String("nomad.eval.status", eval.Status),
			slog.String("nomad.eval.status_description", eval.StatusDescription),
			slog.String("nomad.eval.triggered_by", eval.TriggeredBy),
			slog.Int("nomad.eval.queued_allocations", sumIntMap(eval.QueuedAllocations)),
			slog.Int("nomad.eval.failed_task_groups", len(eval.FailedTGAllocs)),
			slog.Uint64("nomad.eval.modify_index", eval.ModifyIndex),
		)
		attrs = appendMetaAttrs(attrs, meta)
		if evaluationFailed(eval.Status) {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.evaluation.event", attrs)
}

func (o *observer) handleAllocationEvent(ctx context.Context, event api.Event) {
	alloc, err := event.Allocation()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if alloc != nil {
		namespace := normalizeNamespace(alloc.Namespace, o.cfg.namespace)
		meta := metadataFromAlloc(alloc)
		if meta.empty() {
			if cached, err := o.metadataForJob(ctx, namespace, alloc.JobID); err == nil {
				meta = cached
			} else {
				attrs = append(attrs, slog.String("nomad.metadata_error", err.Error()))
			}
		}
		task, taskState, taskEvent := latestTaskFailure(alloc.TaskStates)
		attrs = append(attrs,
			slog.String("nomad.namespace", namespace),
			slog.String("nomad.alloc_id", alloc.ID),
			slog.String("nomad.alloc_name", alloc.Name),
			slog.String("nomad.job_id", alloc.JobID),
			slog.String("nomad.eval_id", alloc.EvalID),
			slog.String("nomad.deployment_id", alloc.DeploymentID),
			slog.String("nomad.task_group", alloc.TaskGroup),
			slog.String("nomad.node_id", alloc.NodeID),
			slog.String("nomad.node_name", alloc.NodeName),
			slog.String("nomad.alloc.client_status", alloc.ClientStatus),
			slog.String("nomad.alloc.desired_status", alloc.DesiredStatus),
			slog.String("nomad.alloc.client_description", alloc.ClientDescription),
			slog.String("nomad.alloc.desired_description", alloc.DesiredDescription),
			slog.Uint64("nomad.alloc.modify_index", alloc.ModifyIndex),
		)
		if task != "" {
			attrs = append(attrs,
				slog.String("nomad.task", task),
				slog.String("nomad.task.state", taskState.State),
				slog.Bool("nomad.task.failed", taskState.Failed),
				slog.Uint64("nomad.task.restarts", taskState.Restarts),
			)
		}
		if taskEvent != nil {
			attrs = append(attrs,
				slog.String("nomad.task.event.type", taskEvent.Type),
				slog.String("nomad.task.event.message", firstNonEmpty(taskEvent.DisplayMessage, taskEvent.Message, taskEvent.DriverError, taskEvent.SetupError)),
				slog.Int("nomad.task.event.exit_code", taskEvent.ExitCode),
				slog.String("nomad.task.event.driver_error", taskEvent.DriverError),
			)
		}
		attrs = appendMetaAttrs(attrs, meta)
		if allocationFailed(alloc.ClientStatus) {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.allocation.event", attrs)
}

func (o *observer) handleNodeEvent(ctx context.Context, event api.Event) {
	node, err := event.Node()
	if err != nil {
		o.logDecodeError(ctx, event, err)
		return
	}
	attrs := eventAttrs(event)
	level := slog.LevelInfo
	if node != nil {
		attrs = append(attrs,
			slog.String("nomad.node_id", node.ID),
			slog.String("nomad.node_name", node.Name),
			slog.String("nomad.node.datacenter", node.Datacenter),
			slog.String("nomad.node.status", node.Status),
			slog.String("nomad.node.status_description", node.StatusDescription),
			slog.String("nomad.node.eligibility", node.SchedulingEligibility),
			slog.Bool("nomad.node.drain", node.Drain),
			slog.Uint64("nomad.node.modify_index", node.ModifyIndex),
		)
		if strings.EqualFold(node.Status, "down") || strings.EqualFold(node.Status, "lost") {
			level = slog.LevelWarn
			o.stats.failures.Add(1)
		}
	}
	o.logAttrs(ctx, level, "nomad.node.event", attrs)
}

func (o *observer) logDecodeError(ctx context.Context, event api.Event, err error) {
	attrs := eventAttrs(event)
	attrs = append(attrs, slog.String("error", err.Error()))
	o.logAttrs(ctx, slog.LevelWarn, "nomad.event.decode_failed", attrs)
}

func (o *observer) logNomadEvent(ctx context.Context, level slog.Level, message string, event api.Event, attrs []slog.Attr) {
	o.logAttrs(ctx, level, message, append(eventAttrs(event), attrs...))
}

func (o *observer) logAttrs(ctx context.Context, level slog.Level, message string, attrs []slog.Attr) {
	attrs = append([]slog.Attr{slog.String("nomad.event_name", message)}, attrs...)
	o.logger.LogAttrs(ctx, level, message, attrs...)
}

func eventAttrs(event api.Event) []slog.Attr {
	return []slog.Attr{
		slog.String("nomad.topic", event.Topic.String()),
		slog.String("nomad.event.type", event.Type),
		slog.String("nomad.event.key", event.Key),
		slog.Uint64("nomad.event.index", event.Index),
	}
}

func appendMetaAttrs(attrs []slog.Attr, meta deployMeta) []slog.Attr {
	if meta.DeployRunKey != "" {
		attrs = append(attrs, slog.String("verself.deploy_run_key", meta.DeployRunKey))
	}
	if meta.DeploySHA != "" {
		attrs = append(attrs, slog.String("verself.deploy_sha", meta.DeploySHA))
	}
	if meta.SpecSHA256 != "" {
		attrs = append(attrs, slog.String("verself.deploy_spec_sha256", meta.SpecSHA256))
	}
	if meta.ArtifactSHA256 != "" {
		attrs = append(attrs, slog.String("verself.artifact_sha256", meta.ArtifactSHA256))
	}
	return attrs
}

func setMetaSpanAttributes(span trace.Span, meta deployMeta) {
	if meta.DeployRunKey != "" {
		span.SetAttributes(attribute.String("verself.deploy_run_key", meta.DeployRunKey))
	}
	if meta.DeploySHA != "" {
		span.SetAttributes(attribute.String("verself.deploy_sha", meta.DeploySHA))
	}
	if meta.SpecSHA256 != "" {
		span.SetAttributes(attribute.String("verself.deploy_spec_sha256", meta.SpecSHA256))
	}
	if meta.ArtifactSHA256 != "" {
		span.SetAttributes(attribute.String("verself.artifact_sha256", meta.ArtifactSHA256))
	}
}

func (o *observer) metadataForJob(ctx context.Context, namespace, jobID string) (deployMeta, error) {
	if jobID == "" {
		return deployMeta{}, nil
	}
	namespace = normalizeNamespace(namespace, o.cfg.namespace)
	if meta, ok := o.meta.get(namespace, jobID); ok {
		return meta, nil
	}
	job, _, err := o.client.Jobs().Info(jobID, (&api.QueryOptions{Namespace: namespace}).WithContext(ctx))
	if err != nil {
		return deployMeta{}, err
	}
	meta := metadataFromJob(job)
	o.meta.put(namespace, jobID, meta)
	return meta, nil
}

func metadataFromJob(job *api.Job) deployMeta {
	if job == nil {
		return deployMeta{}
	}
	return metadataFromMap(job.Meta)
}

func metadataFromAlloc(alloc *api.Allocation) deployMeta {
	if alloc == nil || alloc.Job == nil {
		return deployMeta{}
	}
	return metadataFromJob(alloc.Job)
}

func metadataFromMap(values map[string]string) deployMeta {
	return deployMeta{
		DeployRunKey:   values["deploy_run_key"],
		DeploySHA:      values["deploy_sha"],
		SpecSHA256:     values["spec_sha256"],
		ArtifactSHA256: values["artifact_sha256"],
	}
}

func latestTaskFailure(states map[string]*api.TaskState) (string, api.TaskState, *api.TaskEvent) {
	if len(states) == 0 {
		return "", api.TaskState{}, nil
	}
	names := make([]string, 0, len(states))
	for name := range states {
		names = append(names, name)
	}
	sort.Strings(names)
	var selectedTask string
	var selectedState *api.TaskState
	var selectedEvent *api.TaskEvent
	var selectedTime int64
	selectedFailed := false
	for _, name := range names {
		state := states[name]
		if state == nil {
			continue
		}
		event := latestTaskEvent(state)
		eventTime := int64(0)
		if event != nil {
			eventTime = event.Time
		}
		failed := taskStateFailed(state)
		if selectedState == nil || (failed && !selectedFailed) || (failed == selectedFailed && eventTime > selectedTime) {
			selectedTask = name
			selectedState = state
			selectedEvent = event
			selectedTime = eventTime
			selectedFailed = failed
		}
	}
	if selectedState == nil {
		return "", api.TaskState{}, nil
	}
	return selectedTask, *selectedState, selectedEvent
}

func latestTaskEvent(state *api.TaskState) *api.TaskEvent {
	if state == nil || len(state.Events) == 0 {
		return nil
	}
	return state.Events[len(state.Events)-1]
}

func taskStateFailed(state *api.TaskState) bool {
	if state == nil {
		return false
	}
	if state.Failed || strings.EqualFold(state.State, "dead") {
		return true
	}
	event := latestTaskEvent(state)
	return event != nil && (event.FailsTask || event.ExitCode != 0 || event.DriverError != "" || event.SetupError != "" || event.ValidationError != "" || event.DownloadError != "")
}

func allocationFailed(status string) bool {
	_, ok := terminalAllocState[status]
	return ok
}

func deploymentFailed(status string) bool {
	switch status {
	case api.DeploymentStatusFailed, api.DeploymentStatusCancelled, api.DeploymentStatusBlocked:
		return true
	default:
		return false
	}
}

func evaluationFailed(status string) bool {
	switch status {
	case api.EvalStatusFailed, api.EvalStatusCancelled, api.EvalStatusBlocked:
		return true
	default:
		return false
	}
}

func deploymentAllocCounts(deployment *api.Deployment) (desired, placed, healthy, unhealthy int) {
	if deployment == nil {
		return 0, 0, 0, 0
	}
	for _, state := range deployment.TaskGroups {
		if state == nil {
			continue
		}
		desired += state.DesiredTotal
		placed += state.PlacedAllocs
		healthy += state.HealthyAllocs
		unhealthy += state.UnhealthyAllocs
	}
	return desired, placed, healthy, unhealthy
}

func sumIntMap(values map[string]int) int {
	total := 0
	for _, value := range values {
		total += value
	}
	return total
}

func ptrValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func ptrUint64(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

func normalizeNamespace(namespace, fallback string) string {
	namespace = strings.TrimSpace(namespace)
	if namespace != "" {
		return namespace
	}
	fallback = strings.TrimSpace(fallback)
	if fallback != "" {
		return fallback
	}
	return defaultNS
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
