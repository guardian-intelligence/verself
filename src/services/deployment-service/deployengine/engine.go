package deployengine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/verself/deployment-service/deployengine"

var gitSHARE = regexp.MustCompile(`^[0-9a-f]{40}$`)

type Options struct {
	Site                string
	SHA                 string
	DeployRunKey        string
	RepoRoot            string
	R2ControlPlaneToken string
	R2ControlPlaneAddr  string
	NomadAddr           string
	BazelBuildFlags     []string
	Bootstrap           bool
	Tracer              trace.Tracer
	Stdout              io.Writer
}

type Result struct {
	Site                string
	SHA                 string
	DeployRunKey        string
	ControlPlaneSHA256  string
	NomadJobs           []NomadRegisterResult
	NomadSubmittedJobs  uint32
	NomadDispatchedJobs uint32
}

type execution struct {
	Options
}

func Run(ctx context.Context, opts Options) (Result, error) {
	exec, err := newExecution(opts)
	if err != nil {
		return Result{}, err
	}
	if err := prepareSource(ctx, exec); err != nil {
		return Result{}, err
	}
	inputs, err := buildDeployInputs(ctx, exec)
	if err != nil {
		return Result{}, err
	}
	nomadResult, err := registerNomadJobs(ctx, exec, inputs)
	if err != nil {
		return Result{}, err
	}
	return Result{
		Site:                exec.Site,
		SHA:                 exec.SHA,
		DeployRunKey:        exec.DeployRunKey,
		ControlPlaneSHA256:  inputs.ControlPlaneSHA256,
		NomadJobs:           nomadResult.Jobs,
		NomadSubmittedJobs:  nomadResult.SubmittedJobs,
		NomadDispatchedJobs: nomadResult.DispatchedJobs,
	}, nil
}

func newExecution(opts Options) (execution, error) {
	opts.Site = strings.TrimSpace(opts.Site)
	opts.SHA = strings.ToLower(strings.TrimSpace(opts.SHA))
	opts.DeployRunKey = strings.TrimSpace(opts.DeployRunKey)
	opts.RepoRoot = strings.TrimSpace(opts.RepoRoot)
	if opts.Site == "" {
		return execution{}, errors.New("site is required")
	}
	if opts.SHA == "" {
		return execution{}, errors.New("sha is required")
	}
	if !gitSHARE.MatchString(opts.SHA) {
		return execution{}, errors.New("sha must be a 40-character git commit sha")
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
	if opts.Tracer == nil {
		opts.Tracer = otel.Tracer(tracerName)
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

func (e execution) bootstrapMode() bool {
	return e.Bootstrap
}
