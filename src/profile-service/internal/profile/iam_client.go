package profile

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	iaminternalclient "github.com/verself/iam-service/internalclient"
)

type IAMInternalClient struct {
	Client *iaminternalclient.ClientWithResponses
}

func (c IAMInternalClient) UpdateHumanProfile(ctx context.Context, subjectID string, input UpdateIdentityRequest, bearerToken string) (IdentitySummary, error) {
	if c.Client == nil {
		return IdentitySummary{}, ErrIdentityUnavailable
	}
	displayName := strings.TrimSpace(input.DisplayName)
	req := iaminternalclient.IAMUpdateHumanProfileRequest{
		GivenName:   input.GivenName,
		FamilyName:  input.FamilyName,
		DisplayName: &displayName,
	}
	resp, err := c.Client.UpdateHumanProfileWithResponse(ctx, subjectID, req, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
		return nil
	})
	if err != nil {
		return IdentitySummary{}, fmt.Errorf("%w: %v", ErrIdentityUnavailable, err)
	}
	if resp.StatusCode() < 200 || resp.StatusCode() >= 300 || resp.JSON200 == nil {
		return IdentitySummary{}, fmt.Errorf("%w: status %d", ErrIdentityUnavailable, resp.StatusCode())
	}
	syncedAt := resp.JSON200.SyncedAt.UTC()
	if syncedAt.IsZero() {
		syncedAt = time.Now().UTC()
	}
	return IdentitySummary{
		Email:       resp.JSON200.Email,
		GivenName:   resp.JSON200.GivenName,
		FamilyName:  resp.JSON200.FamilyName,
		DisplayName: resp.JSON200.DisplayName,
		SyncedAt:    &syncedAt,
	}, nil
}
