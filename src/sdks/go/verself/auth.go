package verself

import (
	"context"
	"errors"
	"fmt"
	"strings"

	iamcore "github.com/verself/verself-go/internal/transport/iam"
)

const (
	AuthChannelBrowser  AuthChannel = "browser"
	AuthChannelCLI      AuthChannel = "cli"
	AuthChannelSDK      AuthChannel = "sdk"
	AuthChannelMobile   AuthChannel = "mobile"
	AuthChannelWorkload AuthChannel = "workload"

	AuthCodeUnauthenticated               = "auth.unauthenticated"
	AuthCodeDeviceSessionRequired         = "auth.device_session_required"
	AuthCodeReauthenticationRequired      = "auth.reauthentication_required"
	AuthCodeAccountLinkRequired           = "auth.account_link_required"
	AuthCodeProviderEmailUnverified       = "auth.provider_email_unverified"
	AuthCodeProviderSubjectAlreadyLinked  = "auth.provider_subject_already_linked"
	AuthCodeSessionRevoked                = "auth.session_revoked"
	AuthCodeNoAccessibleOrganization      = "auth.no_accessible_organization"
	AuthCodeOrganizationSelectionRequired = "auth.org_selection_required"
	AuthCodeAccountSelectionRequired      = "auth.account_selection_required"
	AuthCodeInsufficientAssurance         = "auth.insufficient_assurance"
)

type (
	AuthChannel string
	AuthMethod  string
)

type Account struct {
	AccountID     string `json:"accountId"`
	Issuer        string `json:"issuer"`
	Subject       string `json:"subject"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	DisplayName   string `json:"displayName"`
	CreatedAt     string `json:"createdAt"`
}

type AuthOrganization struct {
	OrgID       string `json:"orgId"`
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug,omitempty"`
	Selected    bool   `json:"selected"`
}

type DeviceSession struct {
	SessionID   string      `json:"sessionId"`
	AccountID   string      `json:"accountId"`
	Channel     AuthChannel `json:"channel"`
	State       string      `json:"state"`
	Current     bool        `json:"current"`
	DeviceLabel string      `json:"deviceLabel"`
	CreatedAt   string      `json:"createdAt"`
	LastSeenAt  string      `json:"lastSeenAt"`
	ExpiresAt   string      `json:"expiresAt"`
}

type AccountConnection struct {
	ConnectionID  string `json:"connectionId"`
	AccountID     string `json:"accountId"`
	Issuer        string `json:"issuer"`
	Subject       string `json:"subject"`
	State         string `json:"state"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"emailVerified"`
	CreatedAt     string `json:"createdAt"`
	LastSeenAt    string `json:"lastSeenAt"`
}

type AuthContext struct {
	Account       Account            `json:"account"`
	Session       DeviceSession      `json:"session"`
	Organizations []AuthOrganization `json:"organizations"`
	SelectedOrgID string             `json:"selectedOrgId,omitempty"`
	AuthTime      string             `json:"authTime"`
	AuthMethods   []AuthMethod       `json:"authMethods"`
}

type CreateDeviceSessionInput struct {
	Channel        AuthChannel
	DeviceLabel    string
	IdempotencyKey string
}

type AuthClient struct {
	client *iamcore.Client
}

func (c *AuthClient) GetContext(ctx context.Context) (AuthContext, error) {
	if c == nil || c.client == nil {
		return AuthContext{}, fmt.Errorf("verself sdk: auth client is not initialized")
	}
	response, err := c.client.GetAuthContext(ctx, iamcore.GetAuthContextRequest{})
	if err != nil {
		return AuthContext{}, err
	}
	if response.Result == nil {
		return AuthContext{}, iamAPIError("get auth context", response.StatusCode, response.Problem, response.Body)
	}
	return authContextFromGenerated(*response.Result), nil
}

func (c *AuthClient) CreateDeviceSession(ctx context.Context, input CreateDeviceSessionInput) (AuthContext, error) {
	if c == nil || c.client == nil {
		return AuthContext{}, fmt.Errorf("verself sdk: auth client is not initialized")
	}
	channel := AuthChannel(strings.TrimSpace(string(input.Channel)))
	if channel == "" {
		channel = AuthChannelSDK
	}
	deviceLabel := strings.TrimSpace(input.DeviceLabel)
	if deviceLabel == "" {
		deviceLabel = string(channel)
	}
	key, err := mutationKey("iam-device-session-create", input.IdempotencyKey)
	if err != nil {
		return AuthContext{}, err
	}
	response, err := c.client.CreateDeviceSession(ctx, iamcore.CreateDeviceSessionRequest{
		IdempotencyKey: iamcore.IdempotencyKey(key),
		Body: iamcore.CreateDeviceSessionInputBody{
			Channel:     iamcore.AuthChannel(channel),
			DeviceLabel: iamcore.DisplayName(deviceLabel),
		},
	})
	if err != nil {
		return AuthContext{}, err
	}
	if response.Result == nil {
		return AuthContext{}, iamAPIError("create device session", response.StatusCode, response.Problem, response.Body)
	}
	return authContextFromGenerated(*response.Result), nil
}

func (c *AuthClient) ListDeviceSessions(ctx context.Context) ([]DeviceSession, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: auth client is not initialized")
	}
	response, err := c.client.ListDeviceSessions(ctx, iamcore.ListDeviceSessionsRequest{})
	if err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, iamAPIError("list device sessions", response.StatusCode, response.Problem, response.Body)
	}
	out := make([]DeviceSession, 0, len(response.Result.Sessions))
	for _, session := range response.Result.Sessions {
		out = append(out, deviceSessionFromGenerated(session))
	}
	return out, nil
}

func (c *AuthClient) RevokeDeviceSession(ctx context.Context, sessionID string) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("verself sdk: auth client is not initialized")
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("verself sdk: session id is required")
	}
	response, err := c.client.RevokeDeviceSession(ctx, iamcore.RevokeDeviceSessionRequest{SessionID: iamcore.DeviceSessionId(sessionID)})
	if err != nil {
		return err
	}
	if response.Result == nil {
		return iamAPIError("revoke device session", response.StatusCode, response.Problem, response.Body)
	}
	return nil
}

func (c *AuthClient) ListAccountConnections(ctx context.Context) ([]AccountConnection, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("verself sdk: auth client is not initialized")
	}
	response, err := c.client.ListAccountConnections(ctx, iamcore.ListAccountConnectionsRequest{})
	if err != nil {
		return nil, err
	}
	if response.Result == nil {
		return nil, iamAPIError("list account connections", response.StatusCode, response.Problem, response.Body)
	}
	out := make([]AccountConnection, 0, len(response.Result.Connections))
	for _, connection := range response.Result.Connections {
		out = append(out, accountConnectionFromGenerated(connection))
	}
	return out, nil
}

func (c *AuthClient) RemoveAccountConnection(ctx context.Context, connectionID string) error {
	if c == nil || c.client == nil {
		return fmt.Errorf("verself sdk: auth client is not initialized")
	}
	connectionID = strings.TrimSpace(connectionID)
	if connectionID == "" {
		return fmt.Errorf("verself sdk: account connection id is required")
	}
	response, err := c.client.RemoveAccountConnection(ctx, iamcore.RemoveAccountConnectionRequest{ConnectionID: iamcore.AccountConnectionId(connectionID)})
	if err != nil {
		return err
	}
	if response.Result == nil {
		return iamAPIError("remove account connection", response.StatusCode, response.Problem, response.Body)
	}
	return nil
}

func IsAuthCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == strings.TrimSpace(code)
}

func authContextFromGenerated(input iamcore.AuthContext) AuthContext {
	out := AuthContext{
		Account:       accountFromGenerated(input.Account),
		Session:       deviceSessionFromGenerated(input.Session),
		Organizations: make([]AuthOrganization, 0, len(input.Organizations)),
		SelectedOrgID: stringValue(input.SelectedOrgID),
		AuthTime:      input.AuthTime,
		AuthMethods:   make([]AuthMethod, 0, len(input.AuthMethods)),
	}
	for _, org := range input.Organizations {
		out.Organizations = append(out.Organizations, authOrganizationFromGenerated(org))
	}
	for _, method := range input.AuthMethods {
		out.AuthMethods = append(out.AuthMethods, AuthMethod(method))
	}
	return out
}

func accountFromGenerated(input iamcore.AccountSummary) Account {
	return Account{
		AccountID:     string(input.AccountID),
		Issuer:        string(input.Issuer),
		Subject:       input.Subject,
		Email:         string(input.Email),
		EmailVerified: input.EmailVerified,
		DisplayName:   string(input.DisplayName),
		CreatedAt:     input.CreatedAt,
	}
}

func authOrganizationFromGenerated(input iamcore.AuthOrganizationContext) AuthOrganization {
	return AuthOrganization{
		OrgID:       string(input.OrgID),
		DisplayName: string(input.DisplayName),
		Slug:        stringValue(input.Slug),
		Selected:    input.Selected,
	}
}

func deviceSessionFromGenerated(input iamcore.DeviceSessionSummary) DeviceSession {
	return DeviceSession{
		SessionID:   string(input.SessionID),
		AccountID:   string(input.AccountID),
		Channel:     AuthChannel(input.Channel),
		State:       string(input.State),
		Current:     input.Current,
		DeviceLabel: string(input.DeviceLabel),
		CreatedAt:   input.CreatedAt,
		LastSeenAt:  input.LastSeenAt,
		ExpiresAt:   input.ExpiresAt,
	}
}

func accountConnectionFromGenerated(input iamcore.AccountConnectionSummary) AccountConnection {
	return AccountConnection{
		ConnectionID:  string(input.ConnectionID),
		AccountID:     string(input.AccountID),
		Issuer:        string(input.Issuer),
		Subject:       input.Subject,
		State:         string(input.State),
		Email:         string(input.Email),
		EmailVerified: input.EmailVerified,
		CreatedAt:     input.CreatedAt,
		LastSeenAt:    input.LastSeenAt,
	}
}
