package release

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Registry interface {
	ManifestExists(ctx context.Context, registryURL string, repository string, digest string) error
}

type OCIRegistry struct {
	Client   *http.Client
	Username string
	Password string
}

func (r OCIRegistry) ManifestExists(ctx context.Context, registryURL string, repository string, digest string) error {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(registryURL), "/"))
	if err != nil {
		return fmt.Errorf("%w: parse registry url: %v", ErrRegistry, err)
	}
	repo := strings.Trim(strings.TrimSpace(repository), "/")
	if repo == "" {
		return fmt.Errorf("%w: repository is required", ErrInvalid)
	}
	ref := strings.TrimSpace(digest)
	if ref == "" {
		return fmt.Errorf("%w: digest is required", ErrInvalid)
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/v2/" + repo + "/manifests/" + ref
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base.String(), nil)
	if err != nil {
		return fmt.Errorf("%w: build manifest request: %v", ErrRegistry, err)
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json, application/vnd.oci.artifact.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json")
	if r.Username != "" || r.Password != "" {
		req.SetBasicAuth(r.Username, r.Password)
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: manifest head failed: %v", ErrRegistry, err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		return fmt.Errorf("%w: manifest not found", ErrNotFound)
	default:
		return fmt.Errorf("%w: manifest head returned %d", ErrRegistry, resp.StatusCode)
	}
}
