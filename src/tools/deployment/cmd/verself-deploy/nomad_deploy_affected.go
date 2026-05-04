package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/bazelbuild"
	"github.com/verself/deployment-tools/internal/garage"
	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/render"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const (
	// defaultNomadRemotePort matches the loopback Nomad agent port the
	// substrate role binds; the artifact origin's port is read from
	// the manifest's artifact_delivery.origin.port.
	defaultNomadRemotePort      = 4646
	deployAffectedSubmitTimeout = 5 * time.Minute
)

func runNomadDeployAffected(args []string) int {
	fs := flag.NewFlagSet("nomad deploy-affected", flag.ContinueOnError)
	site := fs.String("site", "prod", "site whose authored Nomad job target should be resolved")
	repoRoot := fs.String("repo-root", "", "path to the verself-sh repo root (defaults to cwd)")
	publishOnly := fs.Bool("publish-only", false, "publish artifacts and exit before Nomad submit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *repoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "verself-deploy nomad deploy-affected: cwd: %v\n", err)
			return 1
		}
		*repoRoot = cwd
	}

	parentCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	rt, err := runtime.Init(parentCtx, runtime.Options{
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		Site:           *site,
		RepoRoot:       *repoRoot,
		SkipClickHouse: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "verself-deploy nomad deploy-affected: %v\n", err)
		return 1
	}
	defer rt.Close()

	ctx, span := rt.Tracer.Start(rt.Ctx, "verself_deploy.nomad.deploy_affected",
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(
			attribute.String("verself.site", *site),
			attribute.String("verself.repo_root", *repoRoot),
			attribute.Bool("verself.publish_only", *publishOnly),
		),
	)
	defer span.End()

	if err := deployAffected(ctx, rt, span, *site, *repoRoot, *publishOnly); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		fmt.Fprintf(os.Stderr, "verself-deploy nomad deploy-affected: %v\n", err)
		return 1
	}
	span.SetStatus(codes.Ok, "")
	return 0
}

func deployAffected(ctx context.Context, rt *runtime.Runtime, span trace.Span, site, repoRoot string, publishOnly bool) error {
	closeViteplusRegistry, err := prepareViteplusWorkspace(ctx, rt, repoRoot)
	if err != nil {
		return err
	}
	defer closeViteplusRegistry()

	jobsTarget := fmt.Sprintf("//src/tools/deployment/nomad:%s_nomad_jobs", site)
	// The Nomad jobs target depends transitively on every per-component
	// artifact tarball; building it materialises every publishable file
	// the manifest references.
	build, err := bazelbuild.Build(ctx, repoRoot, []string{jobsTarget}, "--config=remote-writer")
	if err != nil {
		return err
	}
	files, err := build.Stream.ResolveOutputs(jobsTarget, repoRoot)
	if err != nil {
		return fmt.Errorf("resolve %s outputs: %w", jobsTarget, err)
	}
	// prod_nomad_jobs declares a TreeArtifact directory; BEP enumerates
	// each leaf file inside, so we recover the directory by taking the
	// common parent. The resolver's contract is "all resolved specs live
	// in one directory".
	nomadJobsDir := commonParentDir(files)
	if nomadJobsDir == "" {
		return fmt.Errorf("%s outputs have no common parent: %v", jobsTarget, files)
	}
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

	if len(manifest.Artifacts) > 0 {
		if err := publishArtifacts(ctx, rt.SSH, manifest, repoRoot); err != nil {
			return err
		}
	}
	if publishOnly {
		return nil
	}
	if len(submitEntries) == 0 {
		return nil
	}
	return submitResolvedEntries(ctx, rt, site, repoRoot, nomadJobsDir, submitEntries)
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

func submitResolvedEntries(ctx context.Context, rt *runtime.Runtime, site, repoRoot, jobsDir string, entries []render.SubmitEntry) error {
	forward, err := rt.SSH.Forward(ctx, "nomad", defaultNomadRemotePort)
	if err != nil {
		return err
	}
	nomadAddr := "http://" + forward.ListenAddr
	client, err := nomadclient.New(nomadAddr)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := submitOneEntry(ctx, rt.Tracer, client, jobsDir, entry); err != nil {
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
	timeoutCtx, cancel := context.WithTimeout(ctx, deployAffectedSubmitTimeout)
	defer cancel()

	if err := submitOnce(timeoutCtx, span, client, specPath); err != nil {
		recordFailure(span, err)
		return err
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// commonParentDir returns the longest path that is a directory
// prefix of every input. Empty input or paths with disjoint roots
// returns the empty string. The resolver emits one TreeArtifact, so
// this is a safe substitute for declaring a directory output in BEP
// terms.
func commonParentDir(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	prefix := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		dir := filepath.Dir(p)
		for !strings.HasPrefix(dir+string(filepath.Separator), prefix+string(filepath.Separator)) && dir != prefix {
			prefix = filepath.Dir(prefix)
			if prefix == "/" || prefix == "." {
				return ""
			}
		}
	}
	return prefix
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
