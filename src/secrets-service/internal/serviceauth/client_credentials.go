package serviceauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

type ClientCredentialsConfig struct {
	IssuerURL    string
	TokenURL     string
	ClientID     string
	ClientSecret string
	Audience     string
	Transport    http.RoundTripper
	Timeout      time.Duration
}

func NewBearerTokenRequestEditor(cfg ClientCredentialsConfig) (func(context.Context, *http.Request) error, error) {
	tokenSource, err := newTokenSource(cfg)
	if err != nil {
		return nil, err
	}

	return func(_ context.Context, req *http.Request) error {
		token, err := tokenSource.Token()
		if err != nil {
			return fmt.Errorf("serviceauth: fetch access token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token.AccessToken)
		return nil
	}, nil
}

func newTokenSource(cfg ClientCredentialsConfig) (oauth2.TokenSource, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	issuer, err := url.Parse(cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("serviceauth: parse issuer url: %w", err)
	}

	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	httpClient := &http.Client{
		// Zitadel routes single-node loopback requests by Host, so token fetches
		// need the external auth domain even when the socket is 127.0.0.1:8085.
		Transport: &hostOverrideTransport{
			base: transport,
			host: issuer.Host,
		},
		Timeout: timeout,
	}
	sourceCtx := context.WithValue(context.Background(), oauth2.HTTPClient, httpClient)
	tokenConfig := clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     cfg.TokenURL,
		AuthStyle:    oauth2.AuthStyleInHeader,
		Scopes: []string{
			"openid",
			"profile",
			fmt.Sprintf("urn:zitadel:iam:org:project:id:%s:aud", cfg.Audience),
			"urn:zitadel:iam:org:projects:roles",
		},
	}
	return oauth2.ReuseTokenSource(nil, tokenConfig.TokenSource(sourceCtx)), nil
}

func (cfg ClientCredentialsConfig) validate() error {
	var problems []error
	if strings.TrimSpace(cfg.IssuerURL) == "" {
		problems = append(problems, errors.New("issuer url is required"))
	}
	if strings.TrimSpace(cfg.TokenURL) == "" {
		problems = append(problems, errors.New("token url is required"))
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		problems = append(problems, errors.New("client id is required"))
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		problems = append(problems, errors.New("client secret is required"))
	}
	if strings.TrimSpace(cfg.Audience) == "" {
		problems = append(problems, errors.New("audience is required"))
	}
	if len(problems) == 0 {
		return nil
	}
	return errors.Join(problems...)
}

type hostOverrideTransport struct {
	base http.RoundTripper
	host string
}

func (t *hostOverrideTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Host = t.host
	return t.base.RoundTrip(req)
}
