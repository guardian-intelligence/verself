package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/verself/iam-service/internal/contractapi"
	"github.com/verself/iam-service/internal/identity"
	identitystore "github.com/verself/iam-service/internal/store"
	auth "github.com/verself/service-runtime/auth"
)

const (
	defaultDeviceSessionTTL = 30 * 24 * time.Hour
	accountIDPrefix         = "acct_"
	deviceSessionIDPrefix   = "sess_"
	connectionIDPrefix      = "conn_"
)

type productAccount struct {
	AccountID     string
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
	CreatedAt     time.Time
}

func (h publicHandlers) GetAuthContext(ctx context.Context, input *contractapi.GetAuthContextInput) (*contractapi.GetAuthContextOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	pg, err := h.accountPG()
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "IAM account store unavailable", err)
	}
	q := identitystore.New(pg)
	account, err := h.resolveAccount(ctx, q, authIdentity, false)
	if err != nil {
		return nil, err
	}
	session, err := h.requireDeviceSession(ctx, q, account, string(input.SessionID))
	if err != nil {
		return nil, err
	}
	out, err := h.authContext(ctx, authIdentity, account, session)
	if err != nil {
		return nil, err
	}
	return &contractapi.GetAuthContextOutput{Body: out}, nil
}

func (h publicHandlers) CreateDeviceSession(ctx context.Context, input *contractapi.CreateDeviceSessionInput) (*contractapi.CreateDeviceSessionOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	channel := strings.TrimSpace(string(input.Body.Channel))
	switch channel {
	case "browser", "cli", "sdk", "mobile", "workload":
	default:
		return nil, badRequest(ctx, "request.validation_failed", "unsupported auth channel", nil)
	}
	providerSessionID := providerSessionIDFromClaims(authIdentity.Raw)
	if channel != "workload" && providerSessionID == "" {
		return nil, forbidden(ctx, "auth.reauthentication_required", "provider session proof is required; sign in again")
	}
	deviceLabel := firstNonEmpty(string(input.Body.DeviceLabel), channel)
	pg, err := h.accountPG()
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "IAM account store unavailable", err)
	}
	tx, err := pg.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "begin account transaction failed", err)
	}
	defer rollbackPG(ctx, tx)
	q := identitystore.New(tx)
	account, err := h.resolveAccount(ctx, q, authIdentity, true)
	if err != nil {
		return nil, err
	}
	info := operationRequestInfoFromContext(ctx)
	sessionID := stablePublicID(deviceSessionIDPrefix, account.AccountID, string(input.IdempotencyKey))
	session, err := q.UpsertDeviceSession(ctx, identitystore.UpsertDeviceSessionParams{
		SessionID:             sessionID,
		AccountID:             account.AccountID,
		Issuer:                account.Issuer,
		Subject:               account.Subject,
		Channel:               channel,
		DeviceLabel:           deviceLabel,
		CredentialFingerprint: credentialFingerprint(authIdentity.Raw),
		ProviderSessionID:     providerSessionID,
		AuthTime:              timestamptz(authTime(authIdentity.Raw)),
		AuthMethods:           authMethods(authIdentity.Raw),
		ExpiresAt:             timestamptz(deviceSessionExpiresAt()),
		ClientIp:              info.ClientIP,
		ClientIpTrusted:       info.Attribution.Trusted,
		ClientIpSource:        info.Attribution.ClientIPSource,
		UserAgent:             info.UserAgent,
	})
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "persist device session failed", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "commit device session failed", err)
	}
	trace.SpanFromContext(ctx).AddEvent("iam.device_session.created", trace.WithAttributes(
		attribute.String("iam.account_id", account.AccountID),
		attribute.String("iam.session_id", session.SessionID),
		attribute.String("iam.auth_channel", channel),
	))
	out, err := h.authContext(ctx, authIdentity, account, session)
	if err != nil {
		return nil, err
	}
	return &contractapi.CreateDeviceSessionOutput{Body: out}, nil
}

func (h publicHandlers) ListDeviceSessions(ctx context.Context, input *contractapi.ListDeviceSessionsInput) (*contractapi.ListDeviceSessionsOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	q, account, current, err := h.accountAndSession(ctx, authIdentity, string(input.SessionID))
	if err != nil {
		return nil, err
	}
	rows, err := q.ListDeviceSessionsForAccount(ctx, identitystore.ListDeviceSessionsForAccountParams{AccountID: account.AccountID})
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "list device sessions failed", err)
	}
	out := make(contractapi.DeviceSessionSummaries, 0, len(rows))
	for _, row := range rows {
		out = append(out, deviceSessionSummary(row, row.SessionID == current.SessionID))
	}
	return &contractapi.ListDeviceSessionsOutput{Body: contractapi.ListDeviceSessionsOutputBody{Sessions: out}}, nil
}

func (h publicHandlers) RevokeDeviceSession(ctx context.Context, input *contractapi.RevokeDeviceSessionInput) (*contractapi.RevokeDeviceSessionOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	q, account, current, err := h.accountAndSession(ctx, authIdentity, string(input.CurrentSessionID))
	if err != nil {
		return nil, err
	}
	targetID := strings.TrimSpace(string(input.SessionID))
	if targetID == "" {
		return nil, badRequest(ctx, "request.validation_failed", "session id is required", nil)
	}
	target, err := q.GetDeviceSession(ctx, identitystore.GetDeviceSessionParams{SessionID: targetID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, notFound(ctx, "resource.not_found", "device session not found")
	}
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "load device session failed", err)
	}
	if target.AccountID != account.AccountID {
		return nil, notFound(ctx, "resource.not_found", "device session not found")
	}
	if target.State == "revoked" {
		return &contractapi.RevokeDeviceSessionOutput{}, nil
	}
	if err := h.revokeProviderDeviceSession(ctx, target, current, authIdentity); err != nil {
		return nil, err
	}
	if err := q.RevokeDeviceSession(ctx, identitystore.RevokeDeviceSessionParams{
		AccountID:     account.AccountID,
		SessionID:     targetID,
		RevokedReason: "user_requested",
	}); err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "revoke device session failed", err)
	}
	trace.SpanFromContext(ctx).AddEvent("iam.device_session.revoked", trace.WithAttributes(
		attribute.String("iam.account_id", account.AccountID),
		attribute.String("iam.session_id", targetID),
	))
	return &contractapi.RevokeDeviceSessionOutput{}, nil
}

func (h publicHandlers) revokeProviderDeviceSession(ctx context.Context, session identitystore.IamDeviceSession, current identitystore.IamDeviceSession, authIdentity *auth.Identity) error {
	sessionID := strings.TrimSpace(session.ProviderSessionID)
	if sessionID == "" && session.SessionID == current.SessionID && authIdentity != nil {
		sessionID = providerSessionIDFromClaims(authIdentity.Raw)
	}
	if sessionID == "" {
		if session.Channel == "workload" {
			return nil
		}
		return forbidden(ctx, "auth.reauthentication_required", "provider session proof is required; sign in again")
	}
	if h.providerSession == nil {
		return internalFailure(ctx, "service.unavailable", "identity provider session revoker unavailable", nil)
	}
	if err := h.providerSession.DeleteSession(ctx, sessionID); err != nil {
		return identityError(ctx, err)
	}
	trace.SpanFromContext(ctx).AddEvent("iam.provider_session.revoked", trace.WithAttributes(
		attribute.String("iam.session_id", session.SessionID),
		attribute.String("iam.provider_session_hash", stablePublicID("", sessionID)),
	))
	return nil
}

func (h publicHandlers) ListAccountConnections(ctx context.Context, input *contractapi.ListAccountConnectionsInput) (*contractapi.ListAccountConnectionsOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	q, account, _, err := h.accountAndSession(ctx, authIdentity, string(input.SessionID))
	if err != nil {
		return nil, err
	}
	rows, err := q.ListAccountConnections(ctx, identitystore.ListAccountConnectionsParams{AccountID: account.AccountID})
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "list account connections failed", err)
	}
	out := make(contractapi.AccountConnectionSummaries, 0, len(rows))
	for _, row := range rows {
		out = append(out, accountConnectionSummary(row))
	}
	return &contractapi.ListAccountConnectionsOutput{Body: contractapi.ListAccountConnectionsOutputBody{Connections: out}}, nil
}

func (h publicHandlers) RemoveAccountConnection(ctx context.Context, input *contractapi.RemoveAccountConnectionInput) (*contractapi.RemoveAccountConnectionOutput, error) {
	authIdentity, err := requireIdentity(ctx)
	if err != nil {
		return nil, err
	}
	q, account, _, err := h.accountAndSession(ctx, authIdentity, string(input.CurrentSessionID))
	if err != nil {
		return nil, err
	}
	connectionID := strings.TrimSpace(string(input.ConnectionID))
	connections, err := q.ListAccountConnections(ctx, identitystore.ListAccountConnectionsParams{AccountID: account.AccountID})
	if err != nil {
		return nil, internalFailure(ctx, "service.unavailable", "list account connections failed", err)
	}
	for _, connection := range connections {
		if connection.ConnectionID != connectionID {
			continue
		}
		if connection.Issuer == account.Issuer && connection.Subject == account.Subject {
			return nil, forbidden(ctx, "auth.reauthentication_required", "the active sign-in connection cannot be removed from the current session")
		}
		if err := q.RemoveAccountConnection(ctx, identitystore.RemoveAccountConnectionParams{AccountID: account.AccountID, ConnectionID: connectionID}); err != nil {
			return nil, internalFailure(ctx, "service.unavailable", "remove account connection failed", err)
		}
		return &contractapi.RemoveAccountConnectionOutput{}, nil
	}
	return nil, notFound(ctx, "resource.not_found", "account connection not found")
}

func (h publicHandlers) accountAndSession(ctx context.Context, authIdentity *auth.Identity, sessionID string) (*identitystore.Queries, productAccount, identitystore.IamDeviceSession, error) {
	q, account, session, err := h.accountAndOptionalSession(ctx, authIdentity, sessionID)
	if err != nil {
		return nil, productAccount{}, identitystore.IamDeviceSession{}, err
	}
	if strings.TrimSpace(session.SessionID) == "" {
		return nil, productAccount{}, identitystore.IamDeviceSession{}, deviceSessionRequired(ctx)
	}
	return q, account, session, nil
}

func (h publicHandlers) accountAndOptionalSession(ctx context.Context, authIdentity *auth.Identity, sessionID string) (*identitystore.Queries, productAccount, identitystore.IamDeviceSession, error) {
	pg, err := h.accountPG()
	if err != nil {
		return nil, productAccount{}, identitystore.IamDeviceSession{}, internalFailure(ctx, "service.unavailable", "IAM account store unavailable", err)
	}
	q := identitystore.New(pg)
	account, err := h.resolveAccount(ctx, q, authIdentity, false)
	if err != nil {
		return nil, productAccount{}, identitystore.IamDeviceSession{}, err
	}
	if strings.TrimSpace(sessionID) == "" {
		return q, account, identitystore.IamDeviceSession{}, nil
	}
	session, err := h.requireDeviceSession(ctx, q, account, sessionID)
	if err != nil {
		return nil, productAccount{}, identitystore.IamDeviceSession{}, err
	}
	return q, account, session, nil
}

func (h publicHandlers) requireDeviceSession(ctx context.Context, q *identitystore.Queries, account productAccount, sessionID string) (identitystore.IamDeviceSession, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return identitystore.IamDeviceSession{}, deviceSessionRequired(ctx)
	}
	info := operationRequestInfoFromContext(ctx)
	session, err := q.TouchDeviceSession(ctx, identitystore.TouchDeviceSessionParams{
		SessionID:       sessionID,
		AccountID:       account.AccountID,
		ClientIp:        info.ClientIP,
		ClientIpTrusted: info.Attribution.Trusted,
		ClientIpSource:  info.Attribution.ClientIPSource,
		UserAgent:       info.UserAgent,
	})
	if err == nil {
		return session, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return identitystore.IamDeviceSession{}, internalFailure(ctx, "service.unavailable", "load device session failed", err)
	}
	existing, getErr := q.GetDeviceSession(ctx, identitystore.GetDeviceSessionParams{SessionID: sessionID})
	if getErr == nil && existing.AccountID == account.AccountID {
		return identitystore.IamDeviceSession{}, forbidden(ctx, "auth.session_revoked", "device session is expired or revoked")
	}
	return identitystore.IamDeviceSession{}, deviceSessionRequired(ctx)
}

func (h publicHandlers) resolveAccount(ctx context.Context, q *identitystore.Queries, authIdentity *auth.Identity, create bool) (productAccount, error) {
	if authIdentity == nil || strings.TrimSpace(authIdentity.Subject) == "" {
		return productAccount{}, unauthorized(ctx)
	}
	issuer := firstNonEmpty(claimString(authIdentity.Raw, "iss"), "unknown")
	subject := strings.TrimSpace(authIdentity.Subject)
	row, err := q.GetAccountSubject(ctx, identitystore.GetAccountSubjectParams{Issuer: issuer, Subject: subject})
	if err == nil {
		return productAccountFromSubject(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "load account subject failed", err)
	}
	if !create {
		return productAccount{}, deviceSessionRequired(ctx)
	}
	accountID := stablePublicID(accountIDPrefix, issuer, subject)
	email := strings.TrimSpace(authIdentity.Email)
	emailVerified := claimBool(authIdentity.Raw, "email_verified")
	if email == "" || !emailVerified {
		return productAccount{}, forbidden(ctx, "auth.provider_email_unverified", "the identity provider did not return a verified email address")
	}
	parsedEmail, err := identity.ParseEmailAddress(email)
	if err != nil {
		return productAccount{}, forbidden(ctx, "auth.provider_email_unverified", "the identity provider returned an unsupported email address")
	}
	if err := q.LockAccountEmailIdentityKey(ctx, identitystore.LockAccountEmailIdentityKeyParams{EmailIdentityKey: parsedEmail.IdentityKey}); err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "lock email identity failed", err)
	}
	row, err = q.GetAccountSubject(ctx, identitystore.GetAccountSubjectParams{Issuer: issuer, Subject: subject})
	if err == nil {
		return productAccountFromSubject(row), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "load account subject failed", err)
	}
	connection, err := q.GetAccountConnectionByProviderSubject(ctx, identitystore.GetAccountConnectionByProviderSubjectParams{Issuer: issuer, Subject: subject})
	if err == nil && connection.AccountID != accountID {
		return productAccount{}, conflict(ctx, "auth.provider_subject_already_linked", "provider subject is already linked to another Verself account", nil)
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "load account connection failed", err)
	}
	collisionCount, err := q.CountVerifiedAccountEmailsByIdentityKey(ctx, identitystore.CountVerifiedAccountEmailsByIdentityKeyParams{EmailIdentityKey: parsedEmail.IdentityKey})
	if err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "check account email collisions failed", err)
	}
	if collisionCount > 0 {
		return productAccount{}, conflict(ctx, "auth.account_link_required", "a Verself account already uses this verified email; explicit account linking is required", nil)
	}
	displayName := firstNonEmpty(claimString(authIdentity.Raw, "name"), claimString(authIdentity.Raw, "preferred_username"), parsedEmail.DeliveryAddress)
	if err := q.CreateAccount(ctx, identitystore.CreateAccountParams{
		AccountID:    accountID,
		PrimaryEmail: parsedEmail.DeliveryAddress,
		DisplayName:  displayName,
	}); err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "create account failed", err)
	}
	claimsJSON, err := json.Marshal(authIdentity.Raw)
	if err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "encode identity claims failed", err)
	}
	rows, err := q.UpsertAccountSubject(ctx, identitystore.UpsertAccountSubjectParams{
		Issuer:        issuer,
		Subject:       subject,
		AccountID:     accountID,
		Email:         parsedEmail.DeliveryAddress,
		EmailVerified: emailVerified,
		DisplayName:   displayName,
		ClaimsJson:    claimsJSON,
	})
	if err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "link account subject failed", err)
	}
	if rows == 0 {
		return productAccount{}, conflict(ctx, "auth.provider_subject_already_linked", "provider subject is already linked to another Verself account", nil)
	}
	if err := q.UpsertAccountEmail(ctx, identitystore.UpsertAccountEmailParams{
		AccountID:        accountID,
		Email:            parsedEmail.DeliveryAddress,
		EmailIdentityKey: parsedEmail.IdentityKey,
		Verified:         emailVerified,
		Source:           "oidc",
	}); err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "record account email failed", err)
	}
	rows, err = q.UpsertAccountConnection(ctx, identitystore.UpsertAccountConnectionParams{
		ConnectionID:  stablePublicID(connectionIDPrefix, issuer, subject),
		AccountID:     accountID,
		Issuer:        issuer,
		Subject:       subject,
		Email:         parsedEmail.DeliveryAddress,
		EmailVerified: emailVerified,
	})
	if err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "record account connection failed", err)
	}
	if rows == 0 {
		return productAccount{}, conflict(ctx, "auth.provider_subject_already_linked", "provider subject is already linked to another Verself account", nil)
	}
	row, err = q.GetAccountSubject(ctx, identitystore.GetAccountSubjectParams{Issuer: issuer, Subject: subject})
	if err != nil {
		return productAccount{}, internalFailure(ctx, "service.unavailable", "load created account subject failed", err)
	}
	return productAccountFromSubject(row), nil
}

func deviceSessionRequired(ctx context.Context) error {
	return forbidden(ctx, "auth.device_session_required", "device session proof is required")
}

func (h publicHandlers) authContext(ctx context.Context, authIdentity *auth.Identity, account productAccount, session identitystore.IamDeviceSession) (contractapi.AuthContext, error) {
	organizations, err := h.authOrganizations(ctx, authIdentity, strings.TrimSpace(authIdentity.OrgID))
	if err != nil {
		return contractapi.AuthContext{}, err
	}
	var selected *contractapi.OrgID
	for _, org := range organizations {
		if org.Selected {
			value := org.OrgID
			selected = &value
			break
		}
	}
	return contractapi.AuthContext{
		Account:       accountSummary(account),
		Session:       deviceSessionSummary(session, true),
		Organizations: organizations,
		SelectedOrgID: selected,
		AuthTime:      formatTime(authTime(authIdentity.Raw)),
		AuthMethods:   contractAuthMethods(authMethods(authIdentity.Raw)),
	}, nil
}

func (h publicHandlers) authOrganizations(ctx context.Context, authIdentity *auth.Identity, selectedOrgID string) (contractapi.AuthOrganizationContexts, error) {
	if h.authz == nil {
		return nil, internalFailure(ctx, "service.unavailable", "authorization graph unavailable", nil)
	}
	orgIDs, _, err := h.authz.LookupOrganizations(ctx, authzSubjectFromIdentity(authIdentity), "read", "")
	if err != nil {
		return nil, authzError(ctx, err)
	}
	orgIDs = compactUniqueStrings(orgIDs)
	if len(orgIDs) == 0 {
		return contractapi.AuthOrganizationContexts{}, nil
	}
	metadata, err := h.service.Store.ListOrganizationMetadataByOrgIDs(ctx, orgIDs)
	if err != nil {
		return nil, identityError(ctx, err)
	}
	out := make(contractapi.AuthOrganizationContexts, 0, len(metadata))
	for _, org := range metadata {
		orgID := contractapi.OrgID(org.OrgID)
		var slug *contractapi.OrgSlug
		if strings.TrimSpace(org.Slug) != "" {
			value := contractapi.OrgSlug(org.Slug)
			slug = &value
		}
		out = append(out, contractapi.AuthOrganizationContext{
			OrgID:       orgID,
			DisplayName: contractapi.DisplayName(org.DisplayName),
			Slug:        slug,
			Selected:    org.OrgID == selectedOrgID,
		})
	}
	return out, nil
}

func (h publicHandlers) accountPG() (*pgxpool.Pool, error) {
	if h.service == nil || h.service.Store == nil {
		return nil, errors.New("identity service store is required")
	}
	sqlStore, ok := h.service.Store.(identity.SQLStore)
	if !ok || sqlStore.PG == nil {
		return nil, errors.New("identity SQL store is required")
	}
	return sqlStore.PG, nil
}

func productAccountFromSubject(row identitystore.GetAccountSubjectRow) productAccount {
	displayName := firstNonEmpty(row.AccountDisplayName, row.DisplayName, row.Email)
	return productAccount{
		AccountID:     row.AccountID,
		Issuer:        row.Issuer,
		Subject:       row.Subject,
		Email:         row.Email,
		EmailVerified: row.EmailVerified,
		DisplayName:   displayName,
		CreatedAt:     requiredTime(row.AccountCreatedAt),
	}
}

func accountSummary(account productAccount) contractapi.AccountSummary {
	return contractapi.AccountSummary{
		AccountID:     contractapi.AccountID(account.AccountID),
		Issuer:        contractapi.IdentityIssuer(account.Issuer),
		Subject:       contractapi.SubjectId(account.Subject),
		Email:         contractapi.EmailAddress(account.Email),
		EmailVerified: account.EmailVerified,
		DisplayName:   contractapi.DisplayName(firstNonEmpty(account.DisplayName, account.Email)),
		CreatedAt:     formatTime(account.CreatedAt),
	}
}

func deviceSessionSummary(session identitystore.IamDeviceSession, current bool) contractapi.DeviceSessionSummary {
	return contractapi.DeviceSessionSummary{
		SessionID:   contractapi.DeviceSessionID(session.SessionID),
		AccountID:   contractapi.AccountID(session.AccountID),
		Channel:     contractapi.AuthChannel(session.Channel),
		State:       contractapi.DeviceSessionState(sessionState(session)),
		Current:     current,
		DeviceLabel: contractapi.DisplayName(firstNonEmpty(session.DeviceLabel, session.Channel)),
		CreatedAt:   formatTime(requiredTime(session.CreatedAt)),
		LastSeenAt:  formatTime(requiredTime(session.LastSeenAt)),
		ExpiresAt:   formatTime(requiredTime(session.ExpiresAt)),
	}
}

func accountConnectionSummary(connection identitystore.IamAccountConnection) contractapi.AccountConnectionSummary {
	return contractapi.AccountConnectionSummary{
		ConnectionID:  contractapi.AccountConnectionID(connection.ConnectionID),
		AccountID:     contractapi.AccountID(connection.AccountID),
		Issuer:        contractapi.IdentityIssuer(connection.Issuer),
		Subject:       contractapi.SubjectId(connection.Subject),
		State:         contractapi.AccountConnectionState(connection.State),
		Email:         contractapi.EmailAddress(connection.Email),
		EmailVerified: connection.EmailVerified,
		CreatedAt:     formatTime(requiredTime(connection.CreatedAt)),
		LastSeenAt:    formatTime(requiredTime(connection.LastSeenAt)),
	}
}

func sessionState(session identitystore.IamDeviceSession) string {
	if session.State != "active" {
		return session.State
	}
	if time.Now().UTC().After(requiredTime(session.ExpiresAt)) {
		return "expired"
	}
	return "active"
}

func stablePublicID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{"iam-public-id", prefix}, parts...), "\x00")))
	return prefix + crockfordEncodeBytes(sum[:])[:publicIDPayloadLength]
}

func credentialFingerprint(claims map[string]any) string {
	return stablePublicID("", firstNonEmpty(claimString(claims, "jti"), claimString(claims, "sub"), claimString(claims, "iat")))
}

func authTime(claims map[string]any) time.Time {
	return claimUnixTime(claims, "auth_time", firstNonEmpty(claimString(claims, "iat"), claimString(claims, "exp")))
}

func deviceSessionExpiresAt() time.Time {
	return time.Now().UTC().Add(defaultDeviceSessionTTL)
}

func claimUnixTime(claims map[string]any, key string, fallback string) time.Time {
	if claims != nil {
		switch value := claims[key].(type) {
		case float64:
			if value > 0 {
				return time.Unix(int64(value), 0).UTC()
			}
		case json.Number:
			if n, err := value.Int64(); err == nil && n > 0 {
				return time.Unix(n, 0).UTC()
			}
		case string:
			if parsed, err := time.Parse(time.RFC3339, value); err == nil {
				return parsed.UTC()
			}
		}
	}
	if fallback != "" {
		if parsed, err := time.Parse(time.RFC3339, fallback); err == nil {
			return parsed.UTC()
		}
	}
	return time.Now().UTC()
}

func authMethods(claims map[string]any) []string {
	return authMethodsFromClaims(claims)
}

func contractAuthMethods(values []string) contractapi.AuthMethods {
	out := make(contractapi.AuthMethods, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, contractapi.AuthMethod(trimmed))
		}
	}
	if len(out) == 0 {
		return contractapi.AuthMethods{"unknown"}
	}
	return out
}

func claimBool(claims map[string]any, key string) bool {
	if claims == nil {
		return false
	}
	value, ok := claims[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		value = time.Unix(0, 0).UTC()
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func rollbackPG(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}
