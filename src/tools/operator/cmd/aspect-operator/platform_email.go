package main

import (
	"context"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
)

const platformForwardingDestination = "integrations.anveio@gmail.com"

type platformEmailAddressSpec struct {
	LocalPart       string
	Domain          string
	DisplayName     string
	IdentityType    string
	AddressType     string
	InboundEnabled  bool
	OutboundEnabled bool
}

func platformCompanyEmailLocalParts() []string {
	return []string{
		"anveio",
		"hello",
		"sales",
		"support",
		"security",
		"abuse",
		"privacy",
		"legal",
		"billing",
		"careers",
		"press",
		"postmaster",
		"hostmaster",
		"webmaster",
		"noreply",
		"updates",
		"agents",
	}
}

func platformCompanyEmailSpecs(domain string) []platformEmailAddressSpec {
	specs := make([]platformEmailAddressSpec, 0, len(platformCompanyEmailLocalParts()))
	for _, local := range platformCompanyEmailLocalParts() {
		identityType := "managed"
		addressType := "role"
		displayName := local
		switch local {
		case "anveio":
			identityType = "human"
			addressType = "personal"
			displayName = "Anveio"
		case "noreply", "updates", "agents":
			addressType = "system"
		case "postmaster", "hostmaster", "webmaster":
			addressType = "operator"
		}
		specs = append(specs, platformEmailAddressSpec{
			LocalPart:       local,
			Domain:          domain,
			DisplayName:     displayName,
			IdentityType:    identityType,
			AddressType:     addressType,
			InboundEnabled:  true,
			OutboundEnabled: true,
		})
	}
	return specs
}

func platformEmailSpecs(domain string) []platformEmailAddressSpec {
	specs := platformCompanyEmailSpecs(domain)
	specs = append(specs, platformEmailAddressSpec{
		LocalPart:       "noreply",
		Domain:          "notify." + domain,
		DisplayName:     "Verself Notifications",
		IdentityType:    "system",
		AddressType:     "system",
		InboundEnabled:  false,
		OutboundEnabled: true,
	})
	return specs
}

func (s platformEmailAddressSpec) address() string {
	return s.LocalPart + "@" + s.Domain
}

func (r *platformRunner) ensurePlatformEmail(owner platformZitadelUser) error {
	return r.withSpan("platform.email.ensure", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "email_service"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
		attribute.String("verself.owner_subject", owner.ID),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "email_service")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		defer rollbackTx(ctx, tx)
		now := time.Now().UTC()
		for _, spec := range platformEmailSpecs(r.cfg.VerselfDomain) {
			address := spec.address()
			if _, err := mail.ParseAddress(address); err != nil {
				return fmt.Errorf("platform email %s invalid: %w", address, err)
			}
			identityID := "email_identity:" + address
			zitadelUserID := ""
			if spec.IdentityType == "human" {
				zitadelUserID = owner.ID
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO email_identities (
    identity_id, org_id, identity_type, primary_address, display_name,
    zitadel_user_id, status, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, 'active', $7)
ON CONFLICT (identity_id) DO UPDATE
SET org_id = EXCLUDED.org_id,
    identity_type = EXCLUDED.identity_type,
    primary_address = EXCLUDED.primary_address,
    display_name = EXCLUDED.display_name,
    zitadel_user_id = EXCLUDED.zitadel_user_id,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at`,
				identityID,
				r.cfg.PublicOrgIDText,
				spec.IdentityType,
				address,
				spec.DisplayName,
				zitadelUserID,
				now,
			); err != nil {
				return fmt.Errorf("upsert email identity %s: %w", address, err)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO email_addresses (
    address, org_id, identity_id, local_part, domain, address_type,
    inbound_enabled, outbound_enabled, status, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9)
ON CONFLICT (address) DO UPDATE
SET org_id = EXCLUDED.org_id,
    identity_id = EXCLUDED.identity_id,
    local_part = EXCLUDED.local_part,
    domain = EXCLUDED.domain,
    address_type = EXCLUDED.address_type,
    inbound_enabled = EXCLUDED.inbound_enabled,
    outbound_enabled = EXCLUDED.outbound_enabled,
    status = EXCLUDED.status,
    updated_at = EXCLUDED.updated_at`,
				address,
				r.cfg.PublicOrgIDText,
				identityID,
				spec.LocalPart,
				spec.Domain,
				spec.AddressType,
				spec.InboundEnabled,
				spec.OutboundEnabled,
				now,
			); err != nil {
				return fmt.Errorf("upsert email address %s: %w", address, err)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO email_identity_memberships (identity_id, subject_id, role, granted_by)
VALUES ($1, $2, 'owner', $3)
ON CONFLICT (identity_id, subject_id) DO UPDATE
SET role = EXCLUDED.role,
    granted_by = EXCLUDED.granted_by`,
				identityID,
				owner.ID,
				platformActor,
			); err != nil {
				return fmt.Errorf("upsert email identity membership %s: %w", address, err)
			}
			if _, err := tx.Exec(ctx, `
INSERT INTO send_as_grants (address, subject_id, granted_by, revoked_at)
VALUES ($1, $2, $3, NULL)
ON CONFLICT (address, subject_id) DO UPDATE
SET granted_by = EXCLUDED.granted_by,
    revoked_at = NULL`,
				address,
				owner.ID,
				platformActor,
			); err != nil {
				return fmt.Errorf("upsert send-as grant %s: %w", address, err)
			}
		}
		destinationID := "forward:" + strings.ToLower(platformForwardingDestination)
		if _, err := tx.Exec(ctx, `
INSERT INTO forwarding_destinations (destination_id, org_id, destination_email, status, verified_at)
VALUES ($1, $2, $3, 'verified', $4)
ON CONFLICT (org_id, destination_email) DO UPDATE
SET status = EXCLUDED.status,
    verified_at = EXCLUDED.verified_at`,
			destinationID,
			r.cfg.PublicOrgIDText,
			platformForwardingDestination,
			now,
		); err != nil {
			return fmt.Errorf("upsert forwarding destination: %w", err)
		}
		for _, spec := range platformCompanyEmailSpecs(r.cfg.VerselfDomain) {
			address := spec.address()
			if _, err := tx.Exec(ctx, `
INSERT INTO forwarding_rules (rule_id, source_address, destination_id, keep_local_copy, enabled)
VALUES ($1, $2, $3, true, true)
ON CONFLICT (source_address, destination_id) DO UPDATE
SET keep_local_copy = EXCLUDED.keep_local_copy,
    enabled = EXCLUDED.enabled`,
				"forward-rule:"+address+"->"+destinationID,
				address,
				destinationID,
			); err != nil {
				return fmt.Errorf("upsert forwarding rule %s: %w", address, err)
			}
		}
		for _, domain := range []string{r.cfg.VerselfDomain, "notify." + r.cfg.VerselfDomain} {
			if _, err := tx.Exec(ctx, `
INSERT INTO provider_bindings (
    provider, domain, from_address, active, requests_per_second_limit,
    daily_message_limit, metadata, updated_at
)
VALUES ('resend', $1, '', true, 2, 0, '{"source":"platform-seed"}'::jsonb, $2)
ON CONFLICT (provider, domain, from_address) DO UPDATE
SET active = EXCLUDED.active,
    requests_per_second_limit = EXCLUDED.requests_per_second_limit,
    daily_message_limit = EXCLUDED.daily_message_limit,
    metadata = EXCLUDED.metadata,
    updated_at = EXCLUDED.updated_at`,
				domain,
				now,
			); err != nil {
				return fmt.Errorf("upsert provider binding %s: %w", domain, err)
			}
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		r.markChanged("email.platform_addresses.seeded")
		return nil
	})
}

func (r *platformRunner) checkPlatformEmail(issues *[]string) platformBoundaryRow {
	row := platformBoundaryRow{Boundary: "email", Status: "ok"}
	err := r.withSpan("platform.email.check", []attribute.KeyValue{
		attribute.String("db.system", "postgresql"),
		attribute.String("db.name", "email_service"),
		attribute.String("verself.org_id", r.cfg.PublicOrgIDText),
	}, func(ctx context.Context) error {
		conn, err := r.openPG(ctx, "email_service")
		if err != nil {
			return err
		}
		defer func() { _ = conn.Close(context.Background()) }()
		expected := len(platformCompanyEmailSpecs(r.cfg.VerselfDomain))
		var addressCount int
		if err := conn.QueryRow(ctx, `
SELECT count(*)::int
FROM email_addresses
WHERE org_id = $1 AND domain = $2`,
			r.cfg.PublicOrgIDText,
			r.cfg.VerselfDomain,
		).Scan(&addressCount); err != nil {
			if err == pgx.ErrNoRows {
				addressCount = 0
			} else {
				return err
			}
		}
		if addressCount < expected {
			return fmt.Errorf("email addresses: got %d, want at least %d", addressCount, expected)
		}
		var senderCount int
		if err := conn.QueryRow(ctx, `
SELECT count(*)::int
FROM email_addresses
WHERE org_id = $1 AND address = $2 AND outbound_enabled AND status = 'active'`,
			r.cfg.PublicOrgIDText,
			"noreply@notify."+r.cfg.VerselfDomain,
		).Scan(&senderCount); err != nil {
			return err
		}
		if senderCount != 1 {
			return fmt.Errorf("email sender noreply@notify.%s is not active", r.cfg.VerselfDomain)
		}
		return nil
	})
	if err != nil {
		row.Status = "error"
		row.Detail = err.Error()
		*issues = append(*issues, err.Error())
	}
	return row
}
