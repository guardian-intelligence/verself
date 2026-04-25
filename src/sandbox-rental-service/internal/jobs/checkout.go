package jobs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

const (
	defaultCheckoutCacheDir = "/var/lib/verself/sandbox-rental/github-checkout"
	checkoutBundleFilename  = "checkout.pack"
	checkoutRequestedRef    = "refs/verself/requested"
	checkoutFetchedSHARef   = "refs/verself/sha"
)

var (
	ErrCheckoutInvalid      = errors.New("github checkout request is invalid")
	ErrCheckoutUnauthorized = errors.New("github checkout request is not authorized")
	checkoutSHAPattern      = regexp.MustCompile(`\A[0-9a-fA-F]{40}\z`)
)

type CheckoutBundleRequest struct {
	Repository  string
	Ref         string
	SHA         string
	GitHubToken string
}

type CheckoutBundle struct {
	Identity   StickyDiskIdentity
	Repository string
	Ref        string
	SHA        string
	BundlePath string
	SizeBytes  int64
	CacheHit   bool
	PreparedAt time.Time
}

func (r *GitHubRunner) PrepareCheckoutBundle(ctx context.Context, identity StickyDiskIdentity, req CheckoutBundleRequest) (CheckoutBundle, error) {
	ctx, span := tracer.Start(ctx, "github.checkout.bundle")
	defer span.End()

	repository, err := normalizeCheckoutRepository(req.Repository)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	if !strings.EqualFold(repository, strings.TrimSpace(identity.RepositoryFullName)) {
		err := fmt.Errorf("%w: repository does not match allocated github job", ErrCheckoutUnauthorized)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	sha, err := normalizeCheckoutSHA(req.SHA)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	ref, err := normalizeCheckoutRef(req.Ref)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	span.SetAttributes(checkoutAttributes(identity, repository, ref, sha)...)

	r.checkoutMu.Lock()
	defer r.checkoutMu.Unlock()

	bundlePath := r.checkoutBundlePath(identity, sha)
	if stat, err := os.Stat(bundlePath); err == nil && stat.Size() > 0 {
		span.SetAttributes(
			attribute.Bool("github.checkout.cache_hit", true),
			attribute.Int64("github.checkout.bundle_size_bytes", stat.Size()),
		)
		return CheckoutBundle{
			Identity:   identity,
			Repository: repository,
			Ref:        ref,
			SHA:        sha,
			BundlePath: bundlePath,
			SizeBytes:  stat.Size(),
			CacheHit:   true,
			PreparedAt: time.Now().UTC(),
		}, nil
	}

	token, err := r.checkoutFetchToken(ctx, identity, req.GitHubToken)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	mirrorDir := r.checkoutMirrorDir(identity)
	if err := r.ensureCheckoutMirror(ctx, mirrorDir, repository, token); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	if err := r.fetchCheckoutCommit(ctx, mirrorDir, ref, sha, token); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	if err := r.createCheckoutBundle(ctx, mirrorDir, bundlePath, sha); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	stat, err := os.Stat(bundlePath)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return CheckoutBundle{}, err
	}
	span.SetAttributes(
		attribute.Bool("github.checkout.cache_hit", false),
		attribute.Int64("github.checkout.bundle_size_bytes", stat.Size()),
	)
	return CheckoutBundle{
		Identity:   identity,
		Repository: repository,
		Ref:        ref,
		SHA:        sha,
		BundlePath: bundlePath,
		SizeBytes:  stat.Size(),
		CacheHit:   false,
		PreparedAt: time.Now().UTC(),
	}, nil
}

func (r *GitHubRunner) checkoutRoot() string {
	root := strings.TrimSpace(r.service.CheckoutCacheDir)
	if root == "" {
		return defaultCheckoutCacheDir
	}
	return root
}

func (r *GitHubRunner) checkoutMirrorDir(identity StickyDiskIdentity) string {
	return filepath.Join(r.checkoutRoot(), "mirrors", checkoutCacheKey(identity))
}

func (r *GitHubRunner) checkoutBundlePath(identity StickyDiskIdentity, sha string) string {
	return filepath.Join(r.checkoutRoot(), "bundles", checkoutCacheKey(identity), sha, checkoutBundleFilename)
}

func checkoutCacheKey(identity StickyDiskIdentity) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%d", identity.Installation, identity.RepositoryID)))
	return hex.EncodeToString(sum[:])
}

func (r *GitHubRunner) ensureCheckoutMirror(ctx context.Context, mirrorDir, repository, token string) error {
	ctx, span := tracer.Start(ctx, "github.checkout.mirror.ensure")
	defer span.End()

	if err := os.MkdirAll(filepath.Dir(mirrorDir), 0o700); err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(mirrorDir, "HEAD")); errors.Is(err, os.ErrNotExist) {
		if err := runCheckoutGit(ctx, "init_bare", "", nil, "init", "--bare", mirrorDir); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := runCheckoutGit(ctx, "remote_set_url", mirrorDir, nil, "remote", "set-url", "origin", r.checkoutRemoteURL(repository)); err == nil {
		return nil
	}
	return runCheckoutGit(ctx, "remote_add", mirrorDir, nil, "remote", "add", "origin", r.checkoutRemoteURL(repository))
}

func (r *GitHubRunner) fetchCheckoutCommit(ctx context.Context, mirrorDir, ref, sha, token string) error {
	ctx, span := tracer.Start(ctx, "github.checkout.mirror.fetch")
	defer span.End()

	if ref != "" {
		if err := runCheckoutGit(ctx, "fetch_ref", mirrorDir, checkoutGitEnv(r.checkoutRemoteHost(), token),
			"fetch", "--force", "--no-tags", "origin", "+"+ref+":"+checkoutRequestedRef); err != nil {
			return err
		}
	}
	if err := runCheckoutGit(ctx, "cat_file_ref", mirrorDir, nil, "cat-file", "-e", sha+"^{commit}"); err == nil {
		return nil
	}
	if err := runCheckoutGit(ctx, "fetch_sha", mirrorDir, checkoutGitEnv(r.checkoutRemoteHost(), token),
		"fetch", "--force", "--no-tags", "origin", "+"+sha+":"+checkoutFetchedSHARef); err != nil {
		return err
	}
	return runCheckoutGit(ctx, "cat_file_sha", mirrorDir, nil, "cat-file", "-e", sha+"^{commit}")
}

func (r *GitHubRunner) createCheckoutBundle(ctx context.Context, mirrorDir, bundlePath, sha string) error {
	ctx, span := tracer.Start(ctx, "github.checkout.bundle.create")
	defer span.End()

	if err := os.MkdirAll(filepath.Dir(bundlePath), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(bundlePath), ".checkout-*.bundle")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := writeCheckoutPack(ctx, mirrorDir, sha, tmp); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, bundlePath)
}

func (r *GitHubRunner) checkoutRemoteURL(repository string) string {
	webBase := strings.TrimRight(firstNonEmpty(r.cfg.WebBaseURL, "https://github.com"), "/")
	return webBase + "/" + repository + ".git"
}

func (r *GitHubRunner) checkoutRemoteHost() string {
	webBase := strings.TrimSpace(firstNonEmpty(r.cfg.WebBaseURL, "https://github.com"))
	parsed, err := url.Parse(webBase)
	if err != nil || parsed.Host == "" {
		return "github.com"
	}
	return parsed.Host
}

func checkoutGitEnv(host, token string) []string {
	credential := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=http.https://" + host + "/.extraheader",
		"GIT_CONFIG_VALUE_0=AUTHORIZATION: basic " + credential,
	}
}

func runCheckoutGit(ctx context.Context, label, dir string, extraEnv []string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), extraEnv...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", label, err, strings.TrimSpace(string(output)))
	}
	return nil
}

func writeCheckoutPack(ctx context.Context, mirrorDir, sha string, out *os.File) error {
	revList := exec.CommandContext(ctx, "git", "rev-list", "--objects", "--no-object-names", "-1", sha)
	revList.Dir = mirrorDir
	packObjects := exec.CommandContext(ctx, "git", "pack-objects", "--stdout")
	packObjects.Dir = mirrorDir

	var revErr bytes.Buffer
	var packErr bytes.Buffer
	revList.Stderr = &revErr
	packObjects.Stderr = &packErr
	revStdout, err := revList.StdoutPipe()
	if err != nil {
		return err
	}
	packObjects.Stdin = revStdout
	packObjects.Stdout = out
	if err := packObjects.Start(); err != nil {
		return fmt.Errorf("git pack_objects: %w", err)
	}
	if err := revList.Start(); err != nil {
		_ = packObjects.Process.Kill()
		return fmt.Errorf("git rev_list: %w", err)
	}
	revWaitErr := revList.Wait()
	packWaitErr := packObjects.Wait()
	if revWaitErr != nil {
		return fmt.Errorf("git rev_list: %w: %s", revWaitErr, strings.TrimSpace(revErr.String()))
	}
	if packWaitErr != nil {
		return fmt.Errorf("git pack_objects: %w: %s", packWaitErr, strings.TrimSpace(packErr.String()))
	}
	return nil
}

func (r *GitHubRunner) checkoutFetchToken(ctx context.Context, identity StickyDiskIdentity, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token != "" {
		if strings.ContainsAny(token, "\x00\r\n") {
			return "", fmt.Errorf("%w: github token contains control characters", ErrCheckoutInvalid)
		}
		return token, nil
	}
	return r.installationToken(ctx, identity.Installation)
}

func normalizeCheckoutRepository(repository string) (string, error) {
	repository = strings.TrimSpace(repository)
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("%w: repository must be owner/name", ErrCheckoutInvalid)
	}
	for _, part := range parts {
		if strings.ContainsAny(part, "\x00\r\n\t ") || strings.Contains(part, "..") {
			return "", fmt.Errorf("%w: invalid repository", ErrCheckoutInvalid)
		}
	}
	return repository, nil
}

func normalizeCheckoutSHA(sha string) (string, error) {
	sha = strings.ToLower(strings.TrimSpace(sha))
	if !checkoutSHAPattern.MatchString(sha) {
		return "", fmt.Errorf("%w: sha must be a 40-character commit sha", ErrCheckoutInvalid)
	}
	return sha, nil
}

func normalizeCheckoutRef(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	if strings.ContainsAny(ref, "\x00\r\n\t ") || strings.Contains(ref, "..") {
		return "", fmt.Errorf("%w: invalid ref", ErrCheckoutInvalid)
	}
	switch {
	case strings.HasPrefix(ref, "refs/heads/"),
		strings.HasPrefix(ref, "refs/tags/"),
		strings.HasPrefix(ref, "refs/pull/"):
		return ref, nil
	default:
		return "", fmt.Errorf("%w: ref must be a full GitHub ref", ErrCheckoutInvalid)
	}
}

func checkoutAttributes(identity StickyDiskIdentity, repository, ref, sha string) []attribute.KeyValue {
	return []attribute.KeyValue{
		traceOrgID(identity.OrgID),
		attribute.String("execution.id", identity.ExecutionID.String()),
		attribute.String("attempt.id", identity.AttemptID.String()),
		attribute.String("github.allocation_id", identity.AllocationID.String()),
		attribute.Int64("github.installation_id", identity.Installation),
		attribute.Int64("github.repository_id", identity.RepositoryID),
		attribute.String("github.repository", repository),
		attribute.Int64("github.job_id", identity.GitHubJobID),
		attribute.String("github.ref", ref),
		attribute.String("github.sha", sha),
	}
}
