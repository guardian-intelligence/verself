package main

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/deployment-tools/internal/nomadclient"
	"github.com/verself/deployment-tools/internal/runtime"
	"github.com/verself/deployment-tools/internal/sshtun"
)

const defaultNomadRemotePort = 4646

func openNomadForward(ctx context.Context, rt *runtime.Runtime, addr string) (*sshtun.Forward, error) {
	port := defaultNomadRemotePort
	if addr != "" {
		parsed, err := url.Parse(addr)
		if err != nil {
			return nil, fmt.Errorf("parse nomad_addr %q: %w", addr, err)
		}
		if parsed.Port() != "" {
			p, err := strconv.Atoi(parsed.Port())
			if err != nil || p <= 0 || p > 65535 {
				return nil, fmt.Errorf("invalid nomad_addr port %q", parsed.Port())
			}
			port = p
		}
	}
	return rt.SSH.Forward(ctx, "nomad", port)
}

type nomadRegisterResult struct {
	JobID          string
	EvalID         string
	JobModifyIndex uint64
}

func registerNomadJobs(ctx context.Context, rt *runtime.Runtime, inputs *deployInputs) ([]nomadRegisterResult, error) {
	forward, err := openNomadForward(ctx, rt, inputs.SiteCfg.NomadAddr)
	if err != nil {
		return nil, err
	}
	defer func() { _ = forward.Close() }()
	client, err := nomadclient.New("http://" + forward.ListenAddr)
	if err != nil {
		return nil, err
	}
	jobs, err := prepareNomadJobs(ctx, client, rt.RepoRoot, inputs.Components)
	if err != nil {
		return nil, err
	}
	results := make([]nomadRegisterResult, 0, len(jobs))
	for _, job := range jobs {
		submitted, err := registerNomadJob(ctx, rt, client, job)
		if err != nil {
			return results, err
		}
		results = append(results, submitted)
	}
	return results, nil
}

func registerNomadJob(ctx context.Context, rt *runtime.Runtime, client *nomadclient.Client, job nomadJob) (nomadRegisterResult, error) {
	ctx, span := rt.Tracer.Start(ctx, "verself_deploy.nomad.register",
		trace.WithAttributes(
			attribute.String("nomad.job_id", job.JobID),
			attribute.String("verself.component", job.Component),
			attribute.String("nomad.jobspec_source", job.Source),
		),
	)
	defer span.End()
	submitted, err := client.Register(ctx, job.Job)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nomadRegisterResult{}, fmt.Errorf("%s: %w", job.JobID, err)
	}
	fmt.Printf("verself-deploy: %s registered job_modify_index=%d eval_id=%s\n", submitted.JobID, submitted.JobModifyIndex, submitted.EvalID)
	span.SetAttributes(
		attribute.String("nomad.eval_id", submitted.EvalID),
		attribute.String("nomad.job_modify_index", strconv.FormatUint(submitted.JobModifyIndex, 10)),
	)
	span.SetStatus(codes.Ok, "")
	return nomadRegisterResult{
		JobID:          submitted.JobID,
		EvalID:         submitted.EvalID,
		JobModifyIndex: submitted.JobModifyIndex,
	}, nil
}
