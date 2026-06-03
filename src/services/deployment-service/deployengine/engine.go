package deployengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/verself/deployment-service/deployengine"

type Options struct {
	Site                      string
	ArtifactNamespace         string
	DeployRunKey              string
	RepoRoot                  string
	NomadComponentDescriptors []string
	ArtifactPublisher         ArtifactPublisher
	NomadAddr                 string
	TaskUserResolver          TaskUserResolver
	Tracer                    trace.Tracer
	Stdout                    io.Writer
}

type Result struct {
	Site               string
	DeployRunKey       string
	NomadJobs          []NomadRegisterResult
	NomadSubmittedJobs uint32
}

type ArtifactPublisher interface {
	PublishDeploymentArtifacts(context.Context, ArtifactPublishRequest) (ArtifactPublishResult, error)
}

type ArtifactPublishRequest struct {
	Site              string
	ArtifactNamespace string
	DeployRunKey      string
	Artifacts         []ArtifactPublishCandidate
}

type ArtifactPublishCandidate struct {
	Output    string
	SHA256    string
	LocalPath string
	Body      []byte
	SizeBytes int64
	Label     string
}

type ArtifactPublishResult struct {
	GetterSources map[string]string
}

type execution struct {
	Options
}

func Submit(ctx context.Context, opts Options) (Result, error) {
	exec, err := newExecution(opts)
	if err != nil {
		return Result{}, err
	}
	inputs, err := buildDeployInputs(exec)
	if err != nil {
		return Result{}, err
	}
	nomadResult, err := registerNomadJobs(ctx, exec, inputs)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Site:               exec.Site,
		DeployRunKey:       exec.DeployRunKey,
		NomadJobs:          nomadResult.Jobs,
		NomadSubmittedJobs: nomadResult.SubmittedJobs,
	}, nil
}

func newExecution(opts Options) (execution, error) {
	opts.Site = strings.TrimSpace(opts.Site)
	opts.ArtifactNamespace = strings.TrimSpace(opts.ArtifactNamespace)
	opts.DeployRunKey = strings.TrimSpace(opts.DeployRunKey)
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)
	if opts.Site == "" {
		return execution{}, errors.New("site is required")
	}
	if opts.ArtifactNamespace == "" {
		return execution{}, errors.New("artifact namespace is required")
	}
	if opts.DeployRunKey == "" {
		return execution{}, errors.New("deploy run key is required")
	}
	if opts.RepoRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return execution{}, fmt.Errorf("repo root: %w", err)
		}
		opts.RepoRoot = cwd
	}
	if len(opts.NomadComponentDescriptors) == 0 {
		return execution{}, errors.New("at least one Nomad component descriptor is required")
	}
	if opts.ArtifactPublisher == nil {
		return execution{}, errors.New("artifact publisher is required")
	}
	if opts.Tracer == nil {
		opts.Tracer = otel.Tracer(tracerName)
	}
	if opts.TaskUserResolver == nil {
		opts.TaskUserResolver = localTaskUserResolver
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	return execution{Options: opts}, nil
}

func (e execution) stdout() io.Writer {
	if e.Stdout == nil {
		return os.Stdout
	}
	return e.Stdout
}
