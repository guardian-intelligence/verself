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

func secretCatalog(site string) []SecretSpec {
	return []SecretSpec{
		{
			Key:           "zitadel.initial_admin_password",
			Kind:          "password",
			RenderTargets: []string{openBaoRuntimeTarget("zitadel.initial_admin_password")},
			Generator:     func() (string, error) { return randomSecret(32) },
		},
		{
			Key:           "forgejo.initial_admin_password",
			Kind:          "password",
			RenderTargets: []string{openBaoRuntimeTarget("forgejo.initial_admin_password")},
			Generator:     func() (string, error) { return randomSecret(32) },
		},
		{
			Key:           "billing.cookie_signing_key",
			Kind:          "symmetric_key",
			RenderTargets: []string{openBaoRuntimeTarget("billing.cookie_signing_key")},
			Generator:     func() (string, error) { return randomSecret(48) },
		},
		{
			Key:           "stripe.webhook_secret",
			Kind:          "webhook_secret",
			RenderTargets: []string{openBaoRuntimeTarget("stripe.webhook_secret")},
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

func openBaoRuntimeTarget(key string) string {
	return "openbao://kv-runtime/secret/org/" + key
}

func ensureAllGeneratedSecrets(store *Store, company *CompanyRecord, reveal bool) ([]SecretInventoryItem, error) {
	items := make([]SecretInventoryItem, 0)
	for _, spec := range secretCatalog(company.Site) {
		existing := findSecret(company, spec.Key)
		if existing != nil && existing.ValueRef != "" {
			items = append(items, SecretInventoryItem{
				Key:           spec.Key,
				Destination:   strings.Join(spec.RenderTargets, ","),
				RevealCommand: existing.RevealCommand,
			})
			continue
		}
		value, err := spec.Generator()
		if err != nil {
			return nil, err
		}
		ref, err := store.SaveCredential(value)
		if err != nil {
			return nil, err
		}
		cmd := fmt.Sprintf("verself company secret reveal %s --key %s", company.Name, spec.Key)
		secret := CompanySecret{
			Key:           spec.Key,
			Kind:          spec.Kind,
			Sensitivity:   "secret",
			ValueRef:      ref,
			RenderTargets: spec.RenderTargets,
			RevealCommand: cmd,
			UpdatedAt:     time.Now().UTC(),
		}
		upsertSecret(company, secret)
		item := SecretInventoryItem{Key: spec.Key, Destination: strings.Join(spec.RenderTargets, ","), RevealCommand: cmd}
		if reveal {
			item.Plaintext = value
		}
		items = append(items, item)
	}
	return items, nil
}

func ensureGeneratedSecret(store *Store, company *CompanyRecord, key string, reveal bool) (SecretInventoryItem, error) {
	spec := findSecretSpec(company.Site, key)
	existing := findSecret(company, spec.Key)
	if existing != nil && existing.ValueRef != "" {
		item := SecretInventoryItem{
			Key:           spec.Key,
			Destination:   strings.Join(spec.RenderTargets, ","),
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
		RenderTargets: spec.RenderTargets,
		RevealCommand: cmd,
		UpdatedAt:     time.Now().UTC(),
	}
	upsertSecret(company, secret)
	item := SecretInventoryItem{Key: spec.Key, Destination: strings.Join(spec.RenderTargets, ","), RevealCommand: cmd}
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
