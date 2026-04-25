package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/forge-metal/source-code-hosting-service/internal/source"
)

func IsGitSmartHTTPRequest(r *http.Request) bool {
	_, err := parseGitSmartHTTPPath(r)
	return err == nil
}

func GitHTTPHandler(svc *source.Service) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, span := apiTracer.Start(r.Context(), "source.git.receive")
		defer span.End()

		gitPath, err := parseGitSmartHTTPPath(r)
		if err != nil {
			span.RecordError(err)
			http.NotFound(w, r)
			return
		}
		span.SetAttributes(
			attribute.String("source.git_org_path", gitPath.OrgPath),
			attribute.String("source.slug", gitPath.Slug),
			attribute.String("source.git_service", gitPath.Service),
			attribute.Bool("source.git_receive_pack", gitPath.ReceivePack),
		)

		username, token, ok := r.BasicAuth()
		if !ok {
			challengeGitBasicAuth(w)
			return
		}
		requiredScopes := []string{"repo:read"}
		if gitPath.ReceivePack {
			requiredScopes = []string{"repo:write"}
		}
		principal, err := svc.AuthenticateGitCredential(ctx, username, token, gitPath.OrgPath, requiredScopes)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			challengeGitBasicAuth(w)
			return
		}
		if principal.OrgPath != gitPath.OrgPath {
			span.SetStatus(codes.Error, "git credential org path mismatch")
			http.Error(w, "git credential is not valid for this org path", http.StatusForbidden)
			return
		}
		if gitPath.ReceivePack && !hasScope(principal, "repo:write") {
			http.Error(w, "git credential cannot push", http.StatusForbidden)
			return
		}
		if gitPath.UploadPack && !hasScope(principal, "repo:read") {
			http.Error(w, "git credential cannot fetch", http.StatusForbidden)
			return
		}

		var repo source.Repository
		if gitPath.ReceivePack {
			repo, _, err = svc.EnsureGitRepository(ctx, principal, gitPath.Slug)
		} else {
			repo, err = svc.GetGitRepository(ctx, principal, gitPath.Slug)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			writeGitError(ctx, w, err)
			return
		}
		span.SetAttributes(attribute.String("source.repo_id", repo.RepoID.String()))

		status := proxyGitToForgejo(w, r, svc, repo, gitPath)
		span.SetAttributes(attribute.Int("http.status_code", status))
		if gitPath.ReceivePack && r.Method == http.MethodPost && status >= 200 && status < 300 {
			if err := svc.AfterGitReceive(ctx, principal, repo); err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		}
	})
}

type gitSmartHTTPPath struct {
	OrgPath     string
	Slug        string
	Endpoint    string
	Service     string
	ReceivePack bool
	UploadPack  bool
}

func parseGitSmartHTTPPath(r *http.Request) (gitSmartHTTPPath, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		return gitSmartHTTPPath{}, errors.New("unsupported git HTTP method")
	}
	parts := strings.Split(strings.Trim(r.URL.EscapedPath(), "/"), "/")
	if len(parts) != 3 && len(parts) != 4 {
		return gitSmartHTTPPath{}, errors.New("unsupported git HTTP path")
	}
	orgPath, _ := url.PathUnescape(parts[0])
	repoPart, _ := url.PathUnescape(parts[1])
	if !strings.HasSuffix(repoPart, ".git") {
		return gitSmartHTTPPath{}, errors.New("git repository path must end in .git")
	}
	slug := strings.TrimSuffix(repoPart, ".git")
	if slug == "" || source.NormalizeSlug(slug) != slug || orgPath == "" {
		return gitSmartHTTPPath{}, errors.New("invalid git repository path")
	}
	endpoint := ""
	service := ""
	switch {
	case len(parts) == 4 && parts[2] == "info" && parts[3] == "refs":
		endpoint = "info/refs"
		service = strings.TrimSpace(r.URL.Query().Get("service"))
		if r.Method != http.MethodGet {
			return gitSmartHTTPPath{}, errors.New("git info refs must be GET")
		}
	case len(parts) == 3 && (parts[2] == "git-upload-pack" || parts[2] == "git-receive-pack"):
		endpoint = parts[2]
		service = parts[2]
		if r.Method != http.MethodPost {
			return gitSmartHTTPPath{}, errors.New("git pack RPC must be POST")
		}
	default:
		return gitSmartHTTPPath{}, errors.New("unsupported git endpoint")
	}
	out := gitSmartHTTPPath{
		OrgPath:     orgPath,
		Slug:        slug,
		Endpoint:    endpoint,
		Service:     service,
		ReceivePack: service == "git-receive-pack",
		UploadPack:  service == "git-upload-pack",
	}
	if !out.ReceivePack && !out.UploadPack {
		return gitSmartHTTPPath{}, errors.New("unsupported git service")
	}
	return out, nil
}

func proxyGitToForgejo(w http.ResponseWriter, r *http.Request, svc *source.Service, repo source.Repository, gitPath gitSmartHTTPPath) int {
	ctx, span := apiTracer.Start(r.Context(), "source.forgejo.git.proxy", trace.WithSpanKind(trace.SpanKindClient))
	defer span.End()
	base, err := url.Parse(strings.TrimRight(svc.Forgejo.BaseURL, "/"))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		http.Error(w, "invalid Forgejo backend URL", http.StatusBadGateway)
		return http.StatusBadGateway
	}
	upstreamPath := "/" + path.Join(
		repo.Backend.BackendOwner,
		repo.Backend.BackendRepo+".git",
		gitPath.Endpoint,
	)
	recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	proxy := &httputil.ReverseProxy{
		Director: func(out *http.Request) {
			out.URL.Scheme = base.Scheme
			out.URL.Host = base.Host
			out.URL.Path = upstreamPath
			out.URL.RawPath = ""
			out.URL.RawQuery = r.URL.RawQuery
			out.Host = base.Host
			out.Header.Del("Cookie")
			out.SetBasicAuth(svc.Forgejo.Owner, svc.Forgejo.Token)
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, proxyErr error) {
			span.RecordError(proxyErr)
			span.SetStatus(codes.Error, proxyErr.Error())
			http.Error(rw, "Forgejo Git backend unavailable", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(recorder, r.WithContext(ctx))
	return recorder.status
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func challengeGitBasicAuth(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Forge Metal Git"`)
	http.Error(w, "git credential required", http.StatusUnauthorized)
}

func writeGitError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, source.ErrUnauthorized):
		challengeGitBasicAuth(w)
	case errors.Is(err, source.ErrNotFound):
		http.Error(w, "repository not found", http.StatusNotFound)
	case errors.Is(err, source.ErrConflict):
		http.Error(w, "repository conflict", http.StatusConflict)
	case errors.Is(err, source.ErrForgejo):
		http.Error(w, "Forgejo Git backend unavailable", http.StatusBadGateway)
	case errors.Is(err, source.ErrInvalid):
		http.Error(w, "invalid git request", http.StatusBadRequest)
	default:
		trace.SpanFromContext(ctx).RecordError(fmt.Errorf("git operation failed: %w", err))
		http.Error(w, "git operation failed", http.StatusInternalServerError)
	}
}

func hasScope(principal source.GitPrincipal, scope string) bool {
	for _, value := range principal.Scopes {
		if value == scope {
			return true
		}
	}
	return false
}
