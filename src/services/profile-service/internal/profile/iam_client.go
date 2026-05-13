package profile

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	iamclient "github.com/verself/iam-service/client"
)

type IAMInternalClient struct {
	Client *iamclient.Client
}

func (c IAMInternalClient) UpdateHumanProfile(ctx context.Context, subjectID string, input UpdateIdentityRequest, bearerToken string) (IdentitySummary, error) {
	if c.Client == nil {
		return IdentitySummary{}, ErrIdentityUnavailable
	}
	displayName := strings.TrimSpace(input.DisplayName)
	req := iamclient.UpdateHumanProfileRequest{
		SubjectID: iamclient.SubjectId(subjectID),
		Body: iamclient.UpdateHumanProfileInputBody{
			GivenName:   iamclient.GivenName(input.GivenName),
			FamilyName:  iamclient.FamilyName(input.FamilyName),
			DisplayName: (*iamclient.DisplayName)(&displayName),
		},
	}
	resp, err := c.Client.UpdateHumanProfile(ctx, req, func(_ context.Context, request *http.Request) error {
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
		return nil
	})
	if err != nil {
		return IdentitySummary{}, fmt.Errorf("%w: %v", ErrIdentityUnavailable, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Result == nil {
		return IdentitySummary{}, fmt.Errorf("%w: status %d", ErrIdentityUnavailable, resp.StatusCode)
	}
	syncedAt, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(resp.Result.SyncedAt))
	if syncedAt.IsZero() {
		syncedAt = time.Now().UTC()
	}
	return IdentitySummary{
		Email:       string(resp.Result.Email),
		GivenName:   string(resp.Result.GivenName),
		FamilyName:  string(resp.Result.FamilyName),
		DisplayName: string(resp.Result.DisplayName),
		SyncedAt:    &syncedAt,
	}, nil
}
