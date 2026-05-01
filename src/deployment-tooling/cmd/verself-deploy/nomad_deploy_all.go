package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tooling/internal/bazelbuild"
	"github.com/verself/deployment-tooling/internal/garage"
	"github.com/verself/deployment-tooling/internal/inventory"
	"github.com/verself/deployment-tooling/internal/nomadclient"
	"github.com/verself/deployment-tooling/internal/render"
	"github.com/verself/deployment-tooling/internal/sshtun"
)

const (
	// defaultNomadRemotePort matches the loopback Nomad agent port the
	// substrate role binds; the artifact origin's port is read from
	// the manifest's artifact_delivery.origin.port.
	defaultNomadRemotePort = 4646
	deployAllSubmitTimeout = 5 * time.Minute
)

func runNomadDeployAll(args []string) int {
	fs := flag.NewFlagSet("nomad deploy-all", flag.ContinueOnError)
	site := fs.String("site", "prod", "site whose .cache/render/<site>/jobs/ holds publish.json + submit.tsv")
	repoRoot := fs.String("repo-root", "", "path to the verself-sh repo root (defaults to cwd)")
	publishOnly := fs.Bool("publish-only", false, "publish artifacts and exit before Nomad submit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy nomad deploy-all: cwd: %v\n", err)
			return 1
		}
		*repoRoot = cwd
	}

	ctx, flushOnExit, stop, err := initContext()
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy: %v\n", err)
		return 1
	}
	defer flushOnExit()
	defer stop()

	tracer := otel.Tracer(serviceName)
	ctx, span := tracer.Start(ctx, "verself_deploy.nomad.deploy_all",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", *site),
			attribute.String("verself.repo_root", *repoRoot),
			attribute.Bool("verself.publish_only", *publishOnly),
		),
	)
	defer span.End()

	if err := deployAll(ctx, span, *site, *repoRoot, *publishOnly); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy nomad deploy-all: %v\n", err)
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func deployAll(ctx context.Context, span trace.Span, site, repoRoot string, publishOnly bool) error {
	jobsTarget := fmt.Sprintf("//src/cue-renderer:%s_nomad_jobs", site)
	// The renderer's nomad-jobs target depends transitively on every
	// per-component artifact tarball; building it materialises every
	// publishable file the manifest references.
	build, err := bazelbuild.Build(ctx, repoRoot, []string{jobsTarget}, "--config=remote-writer")
	if err != nil {
		return err
	}
	jobsDirs, err := build.Stream.ResolveOutputs(jobsTarget)
	if err != nil {
		return fmt.Errorf("resolve %s outputs: %w", jobsTarget, err)
	}
	if len(jobsDirs) != 1 {
		return fmt.Errorf("expected 1 output for %s, got %d", jobsTarget, len(jobsDirs))
	}
	nomadJobsDir := jobsDirs[0]
	span.SetAttributes(attribute.String("verself.nomad_jobs_dir", nomadJobsDir))

	manifest, err := render.LoadManifest(filepath.Join(nomadJobsDir, "publish.json"))
	if err != nil {
		return err
	}
	submitEntries, err := render.LoadSubmit(filepath.Join(nomadJobsDir, "submit.tsv"))
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.Int("verself.artifact_count", len(manifest.Artifacts)),
		attribute.Int("verself.submit_count", len(submitEntries)),
	)

	if len(submitEntries) == 0 && len(manifest.Artifacts) == 0 {
		fmt.Fprintln(os.Stderr, "verself-deploy: no nomad-supervised components in resolved job set")
		return nil
	}

	infra, err := resolveInfraHost(repoRoot, site)
	if err != nil {
		return err
	}
	span.SetAttributes(
		attribute.String("ssh.host", infra.Host),
		attribute.String("ssh.user", infra.User),
	)

	sshClient, err := sshtun.Dial(ctx, infra.Host, infra.User)
	if err != nil {
		return err
	}
	defer sshClient.Close()

	if len(manifest.Artifacts) > 0 {
		if err := publishArtifacts(ctx, sshClient, manifest, repoRoot); err != nil {
			return err
		}
	}
	if publishOnly {
		return nil
	}
	if len(submitEntries) == 0 {
		return nil
	}
	return submitAll(ctx, sshClient, nomadJobsDir, submitEntries)
}

func resolveInfraHost(repoRoot, site string) (*inventory.Host, error) {
	cachePath := filepath.Join(repoRoot, ".cache/render", site, "inventory", "hosts.ini")
	if _, err := os.Stat(cachePath); err == nil {
		return inventory.LoadInfra(cachePath)
	}
	return inventory.LoadInfra(filepath.Join(repoRoot, "src/substrate/ansible/inventory", site+".ini"))
}

func publishArtifacts(ctx context.Context, sshClient *sshtun.Client, manifest *render.Manifest, repoRoot string) error {
	forward, err := sshClient.Forward(ctx, "artifact", manifest.ArtifactDelivery.Origin.Port)
	if err != nil {
		return err
	}

	credBytes, err := sudoCat(ctx, sshClient, manifest.ArtifactDelivery.PublisherCredentials.EnvironmentFile)
	if err != nil {
		return fmt.Errorf("read publisher credentials: %w", err)
	}
	access, secret, err := garage.ParseEnvFile(
		credBytes,
		manifest.ArtifactDelivery.PublisherCredentials.AccessKeyIDEnv,
		manifest.ArtifactDelivery.PublisherCredentials.SecretAccessKeyEnv,
	)
	if err != nil {
		return err
	}

	caPEM, err := sudoCat(ctx, sshClient, manifest.ArtifactDelivery.Origin.CABundlePath)
	if err != nil {
		return fmt.Errorf("read artifact origin CA bundle: %w", err)
	}

	pub, err := garage.New(manifest.ArtifactDelivery, garage.Config{
		ConnectAddress:  forward.ListenAddr,
		CABundlePEM:     caPEM,
		AccessKeyID:     access,
		SecretAccessKey: secret,
	})
	if err != nil {
		return err
	}
	return pub.PublishAll(ctx, manifest, repoRoot)
}

func submitAll(ctx context.Context, sshClient *sshtun.Client, jobsDir string, entries []render.SubmitEntry) error {
	forward, err := sshClient.Forward(ctx, "nomad", defaultNomadRemotePort)
	if err != nil {
		return err
	}
	nomadAddr := "http://" + forward.ListenAddr
	client, err := nomadclient.New(nomadAddr)
	if err != nil {
		return err
	}

	tracer := otel.Tracer(serviceName)
	for _, entry := range entries {
		if err := submitOneEntry(ctx, tracer, client, jobsDir, entry); err != nil {
			return fmt.Errorf("%s: %w", entry.JobID, err)
		}
	}
	return nil
}

func submitOneEntry(ctx context.Context, tracer trace.Tracer, client *nomadclient.Client, jobsDir string, entry render.SubmitEntry) error {
	ctx, span := tracer.Start(ctx, "verself_deploy.nomad.submit",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("nomad.job_id", entry.JobID),
			attribute.String("verself.spec_file", entry.SpecFile),
		),
	)
	defer span.End()

	specPath := filepath.Join(jobsDir, entry.SpecFile)
	timeoutCtx, cancel := context.WithTimeout(ctx, deployAllSubmitTimeout)
	defer cancel()

	if err := submitOnce(timeoutCtx, span, client, specPath); err != nil {
		recordFailure(span, err)
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// sudoCat reads a controller-side file the operator user can read
// only via sudo. Mirrors the existing bash incantation; the only
// shell metacharacter we need to escape is the path itself, which
// goes through strconv.Quote rather than printf %q.
func sudoCat(ctx context.Context, sshClient *sshtun.Client, path string) ([]byte, error) {
	if path == "" {
		return nil, errors.New("sudoCat: path is empty")
	}
	if strings.ContainsAny(path, "'\\") {
		return nil, fmt.Errorf("sudoCat: refusing to escape path with special chars: %q", path)
	}
	cmd := "sudo cat " + strconv.Quote(path)
	return sshClient.Exec(ctx, cmd)
}
