package release

import (
	"context"
	"fmt"

	distributioninternalclient "github.com/verself/distribution-service/internalclient"
)

type Distribution interface {
	PromoteTarget(ctx context.Context, req DistributionPromoteTargetRequest) (DistributionTarget, error)
}

type DistributionPromoteTargetRequest struct {
	PackageName    string
	ChannelName    string
	ArtifactDigest string
	PlatformOS     string
	PlatformArch   string
	PolicyRef      string
	PromotedBy     string
	Reason         string
	IdempotencyKey string
}

type DistributionTarget struct {
	ArtifactDigest     string
	PublicOCIReference string
	DownloadURL        string
	TUFMetadataURL     string
}

type DistributionClient struct {
	Client *distributioninternalclient.Client
}

func (c DistributionClient) PromoteTarget(ctx context.Context, req DistributionPromoteTargetRequest) (DistributionTarget, error) {
	if c.Client == nil {
		return DistributionTarget{}, fmt.Errorf("%w: distribution client is required", ErrDistribution)
	}
	resp, err := c.Client.PromoteTarget(ctx, distributioninternalclient.PromoteTargetRequest{
		PackageName:    req.PackageName,
		ChannelName:    req.ChannelName,
		ArtifactDigest: req.ArtifactDigest,
		PlatformOS:     req.PlatformOS,
		PlatformArch:   req.PlatformArch,
		PolicyRef:      req.PolicyRef,
		PromotedBy:     req.PromotedBy,
		Reason:         req.Reason,
	}, req.IdempotencyKey)
	if err != nil {
		return DistributionTarget{}, fmt.Errorf("%w: %v", ErrDistribution, err)
	}
	if resp.Problem != nil {
		return DistributionTarget{}, fmt.Errorf("%w: %s", ErrDistribution, problemDetail(resp.Problem))
	}
	if resp.Result == nil {
		return DistributionTarget{}, fmt.Errorf("%w: empty target response", ErrDistribution)
	}
	target := resp.Result.Target
	return DistributionTarget{
		ArtifactDigest:     target.ArtifactDigest,
		PublicOCIReference: target.PublicOCIReference,
		DownloadURL:        target.DownloadURL,
		TUFMetadataURL:     target.TUFMetadataURL,
	}, nil
}

func problemDetail(problem *distributioninternalclient.ErrorModel) string {
	if problem == nil {
		return "unknown distribution problem"
	}
	if problem.Code != nil && *problem.Code != "" {
		if problem.Detail != nil && *problem.Detail != "" {
			return *problem.Code + ": " + *problem.Detail
		}
		return *problem.Code
	}
	if problem.Detail != nil && *problem.Detail != "" {
		return *problem.Detail
	}
	if problem.Title != nil && *problem.Title != "" {
		return *problem.Title
	}
	return "unknown distribution problem"
}
