package app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

type SecretInventoryItem struct {
	Key           string `json:"key"`
	Destination   string `json:"destination"`
	RevealCommand string `json:"reveal_command,omitempty"`
	Plaintext     string `json:"plaintext,omitempty"`
}

func secretCatalog(_ string) []SecretSpec {
	generated := func(n int) func() (string, error) {
		return func() (string, error) { return randomSecret(n) }
	}
	return []SecretSpec{
		{Key: "postgresql_admin_password", Kind: "password", SiteConfigKey: "postgresql_admin_password", Generator: generated(32)},
		{Key: "postgresql_billing_password", Kind: "password", SiteConfigKey: "postgresql_billing_password", Generator: generated(32)},
		{Key: "postgresql_sandbox_rental_password", Kind: "password", SiteConfigKey: "postgresql_sandbox_rental_password", Generator: generated(32)},
		{Key: "postgresql_iam_service_password", Kind: "password", SiteConfigKey: "postgresql_iam_service_password", Generator: generated(32)},
		{Key: "postgresql_email_service_password", Kind: "password", SiteConfigKey: "postgresql_email_service_password", Generator: generated(32)},
		{Key: "zitadel_db_password", Kind: "password", SiteConfigKey: "zitadel_db_password", Generator: generated(32)},
		{Key: "zitadel_masterkey", Kind: "symmetric_key", SiteConfigKey: "zitadel_masterkey", Generator: generated(32)},
		{Key: "zitadel_admin_password", Kind: "password", SiteConfigKey: "zitadel_admin_password", Generator: generated(32)},
		{Key: "stalwart_admin_password", Kind: "password", SiteConfigKey: "stalwart_admin_password", Generator: generated(32)},
		{Key: "platform_agent_password", Kind: "password", SiteConfigKey: "platform_agent_password", Generator: generated(32)},
		{Key: "iam_service_email_identity_hmac_key", Kind: "symmetric_key", SiteConfigKey: "iam_service_email_identity_hmac_key", Generator: generated(48)},
		{
			Key:           "stripe.webhook_secret",
			Kind:          "webhook_secret",
			SiteConfigKey: "stripe_webhook_secret",
			Generator: func() (string, error) {
				value, err := randomSecret(32)
				if err != nil {
					return "", err
				}
				return "whsec_" + value, nil
			},
		},
	}
}

func ensureAllGeneratedSecrets(store *Store, company *CompanyRecord, reveal bool) ([]SecretInventoryItem, error) {
	items := make([]SecretInventoryItem, 0)
	for _, spec := range secretCatalog(company.Site) {
		item, err := ensureSecretSpec(store, company, spec, reveal)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func ensureGeneratedSecret(store *Store, company *CompanyRecord, key string, reveal bool) (SecretInventoryItem, error) {
	return ensureSecretSpec(store, company, findSecretSpec(company.Site, key), reveal)
}

func ensureSecretSpec(store *Store, company *CompanyRecord, spec SecretSpec, reveal bool) (SecretInventoryItem, error) {
	destination := siteConfigDestination(secretSpecSiteConfigKey(spec))
	existing := findSecret(company, spec.Key)
	if existing != nil && existing.ValueRef != "" {
		item := SecretInventoryItem{
			Key:           spec.Key,
			Destination:   destination,
			RevealCommand: existing.RevealCommand,
		}
		if reveal {
			value, err := store.ReadCredential(existing.ValueRef)
			if err != nil {
				return SecretInventoryItem{}, err
			}
			item.Plaintext = value
		}
		return item, nil
	}
	value, err := spec.Generator()
	if err != nil {
		return SecretInventoryItem{}, err
	}
	ref, err := store.SaveCredential(value)
	if err != nil {
		return SecretInventoryItem{}, err
	}
	cmd := fmt.Sprintf("verself company secret reveal %s --key %s", company.Name, spec.Key)
	secret := CompanySecret{
		Key:           spec.Key,
		Kind:          spec.Kind,
		Sensitivity:   "secret",
		ValueRef:      ref,
		Destinations:  []string{destination},
		RevealCommand: cmd,
		UpdatedAt:     time.Now().UTC(),
	}
	upsertSecret(company, secret)
	item := SecretInventoryItem{Key: spec.Key, Destination: destination, RevealCommand: cmd}
	if reveal {
		item.Plaintext = value
	}
	return item, nil
}

func findSecret(company *CompanyRecord, key string) *CompanySecret {
	for i := range company.Secrets {
		if company.Secrets[i].Key == key {
			return &company.Secrets[i]
		}
	}
	return nil
}

func upsertSecret(company *CompanyRecord, secret CompanySecret) {
	for i := range company.Secrets {
		if company.Secrets[i].Key == secret.Key {
			company.Secrets[i] = secret
			return
		}
	}
	company.Secrets = append(company.Secrets, secret)
}

func randomSecret(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func siteConfigDestination(name string) string {
	return siteConfigMount + "/" + siteConfigPrefix + "/" + name
}

func secretSpecSiteConfigKey(spec SecretSpec) string {
	if spec.SiteConfigKey != "" {
		return spec.SiteConfigKey
	}
	return siteConfigKey(spec.Key)
}

func siteConfigKey(key string) string {
	r := strings.NewReplacer(".", "_", "-", "_")
	return r.Replace(key)
}
