package app

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
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
			RenderTargets: []string{"src/host/sites/" + site + "/secrets/host.sops.yml"},
			Generator:     func() (string, error) { return randomSecret(32) },
		},
		{
			Key:           "forgejo.initial_admin_password",
			Kind:          "password",
			RenderTargets: []string{"src/host/sites/" + site + "/secrets/host.sops.yml"},
			Generator:     func() (string, error) { return randomSecret(32) },
		},
		{
			Key:           "billing.cookie_signing_key",
			Kind:          "symmetric_key",
			RenderTargets: []string{"src/host/sites/" + site + "/secrets/external.sops.yml"},
			Generator:     func() (string, error) { return randomSecret(48) },
		},
		{
			Key:           "stripe.webhook_secret",
			Kind:          "webhook_secret",
			RenderTargets: []string{"src/host/sites/" + site + "/secrets/external.sops.yml"},
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
	rootItem, err := ensureRootSOPSIdentity(store, company, reveal)
	if err != nil {
		return nil, err
	}
	items = append(items, rootItem)
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
	if key == rootSOPSKeyName {
		return ensureRootSOPSIdentity(store, company, reveal)
	}
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

func ensureRootSOPSIdentity(store *Store, company *CompanyRecord, reveal bool) (SecretInventoryItem, error) {
	scope := EnvScope{
		Org:         company.OrganizationName,
		Project:     company.Project,
		Environment: envBootstrap,
	}
	if company.RootSOPSKey != nil && company.RootSOPSKey.SecretRef != "" {
		item := SecretInventoryItem{
			Key:           rootSOPSKeyName,
			Destination:   fmt.Sprintf("env %s/%s/%s", scope.Environment, scope.Org, scope.Project),
			RevealCommand: envGetCommand(company.CLIName, scope, rootSOPSKeyName),
		}
		if reveal {
			value, err := store.ReadCredential(company.RootSOPSKey.SecretRef)
			if err != nil {
				return SecretInventoryItem{}, err
			}
			item.Plaintext = value
		}
		return item, nil
	}
	identity, recipient, err := generateAgeIdentity()
	if err != nil {
		return SecretInventoryItem{}, err
	}
	ref, err := store.PutEnvSecret(scope, rootSOPSKeyName, identity)
	if err != nil {
		return SecretInventoryItem{}, err
	}
	company.RootSOPSKey = &RootSOPSKey{
		EnvKey:    rootSOPSKeyName,
		Provider:  "age",
		Scope:     scope,
		Recipient: recipient,
		SecretRef: ref,
		UpdatedAt: time.Now().UTC(),
	}
	item := SecretInventoryItem{
		Key:           rootSOPSKeyName,
		Destination:   fmt.Sprintf("env %s/%s/%s", scope.Environment, scope.Org, scope.Project),
		RevealCommand: envGetCommand(company.CLIName, scope, rootSOPSKeyName),
	}
	if reveal {
		item.Plaintext = identity
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

func generateAgeIdentity() (identity string, recipient string, err error) {
	out, err := exec.Command("age-keygen").CombinedOutput()
	if err != nil {
		return "", "", fmt.Errorf("age-keygen: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "AGE-SECRET-KEY-"):
			identity = line
		case strings.Contains(line, "public key:"):
			_, after, _ := strings.Cut(line, "public key:")
			recipient = strings.TrimSpace(after)
		case strings.HasPrefix(line, "Public key:"):
			recipient = strings.TrimSpace(strings.TrimPrefix(line, "Public key:"))
		}
	}
	if identity == "" {
		return "", "", errors.New("age-keygen did not emit an AGE-SECRET-KEY identity")
	}
	if recipient == "" || !strings.HasPrefix(recipient, "age1") {
		return "", "", errors.New("age-keygen did not emit an age recipient")
	}
	return identity, recipient, nil
}

func envGetCommand(cliName string, scope EnvScope, key string) string {
	if cliName == "" {
		cliName = "verself"
	}
	return fmt.Sprintf("%s env get %s --org %s --project %s --environment %s", cliName, key, scope.Org, scope.Project, scope.Environment)
}
