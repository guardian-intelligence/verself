package app

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	verself "github.com/verself/verself-go"
)

const (
	defaultUpgradeChannel = verself.DistributionChannelStable
)

var (
	sha256DigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

	verselfCLIUpgradeProduct = upgradeProduct{
		ProductName: "verself-cli",
		PackageName: verself.DistributionProductVerselfCLI,
		BinaryName:  "verself",
	}
	mkskUpgradeProduct = upgradeProduct{
		ProductName: "mksk",
		PackageName: verself.DistributionProductMksk,
		BinaryName:  "mksk",
	}
)

type upgradeProduct struct {
	ProductName string
	PackageName string
	BinaryName  string
}

type upgradeOptions struct {
	Product         upgradeProduct
	ChannelName     string
	ServerURL       string
	DistributionURL string
	BinaryPath      string
	Traceparent     string
	JSON            bool
}

type UpgradeResult struct {
	ProductName        string    `json:"product_name"`
	BinaryName         string    `json:"binary_name"`
	ChannelName        string    `json:"channel_name"`
	UpdateAvailable    bool      `json:"update_available"`
	InstalledPath      string    `json:"installed_path"`
	InstalledVersion   string    `json:"installed_version,omitempty"`
	ArtifactDigest     string    `json:"artifact_digest,omitempty"`
	LayerDigest        string    `json:"layer_digest,omitempty"`
	PublicOCIReference string    `json:"public_oci_reference,omitempty"`
	ReceiptPath        string    `json:"receipt_path,omitempty"`
	Traceparent        string    `json:"traceparent,omitempty"`
	InstalledAt        time.Time `json:"installed_at,omitempty"`
}

func (c CLI) runUpgrade(ctx context.Context, args []string) error {
	return c.runProductUpgrade(ctx, verselfCLIUpgradeProduct, args)
}

func (c CLI) runInternalUpgrade(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: internal-upgrade mksk [flags]")
	}
	switch args[0] {
	case "mksk":
		return c.runProductUpgrade(ctx, mkskUpgradeProduct, args[1:])
	default:
		return fmt.Errorf("unknown internal upgrade product %q", args[0])
	}
}

func (c CLI) runProductUpgrade(ctx context.Context, product upgradeProduct, args []string) error {
	fs := flag.NewFlagSet(product.BinaryName+" upgrade", flag.ContinueOnError)
	fs.SetOutput(c.err)
	channel := fs.String("channel", defaultUpgradeChannel, "distribution channel")
	serverURL := fs.String("server-url", "", "Verself installation URL")
	distributionURL := fs.String("distribution-url", "", "distribution service base URL")
	binaryPath := fs.String("binary-path", "", "binary path to replace")
	traceparent := fs.String("traceparent", "", "trace context to join")
	jsonOut := fs.Bool("json", false, "json output")
	if err := parseInterspersed(fs, args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: %s upgrade [--channel CHANNEL] [--json]", product.BinaryName)
	}
	result, err := c.upgradeProduct(ctx, upgradeOptions{
		Product:         product,
		ChannelName:     *channel,
		ServerURL:       *serverURL,
		DistributionURL: *distributionURL,
		BinaryPath:      *binaryPath,
		Traceparent:     *traceparent,
		JSON:            *jsonOut,
	})
	if err != nil {
		return err
	}
	if *jsonOut {
		return writeJSON(c.out, result)
	}
	if !result.UpdateAvailable {
		return writef(c.out, "%s is up to date at %s\n", result.BinaryName, result.ArtifactDigest)
	}
	return writef(c.out, "upgraded %s to %s at %s\n", result.BinaryName, result.ArtifactDigest, result.InstalledPath)
}

func (c CLI) upgradeProduct(ctx context.Context, options upgradeOptions) (UpgradeResult, error) {
	product := options.Product
	if product.ProductName == "" || product.PackageName == "" || product.BinaryName == "" {
		return UpgradeResult{}, errors.New("upgrade product is incomplete")
	}
	store, err := newStore(c.getenv)
	if err != nil {
		return UpgradeResult{}, err
	}
	binaryPath := strings.TrimSpace(options.BinaryPath)
	if binaryPath == "" {
		binaryPath, err = os.Executable()
		if err != nil {
			return UpgradeResult{}, fmt.Errorf("resolve current executable: %w", err)
		}
	}
	binaryPath, err = filepath.Abs(binaryPath)
	if err != nil {
		return UpgradeResult{}, err
	}
	channel := strings.TrimSpace(options.ChannelName)
	if channel == "" {
		channel = defaultUpgradeChannel
	}
	client, distributionAPIBase, err := c.distributionUpgradeClient(options.ServerURL, options.DistributionURL, options.Traceparent)
	if err != nil {
		return UpgradeResult{}, err
	}
	receipt, receiptErr := store.LoadInstallReceipt(product.ProductName)
	if receiptErr != nil && !errors.Is(receiptErr, os.ErrNotExist) {
		return UpgradeResult{}, receiptErr
	}
	installedDigest := ""
	if receiptErr == nil {
		installedDigest = receipt.ArtifactDigest
	}
	check, err := client.Distribution.CheckUpdate(ctx, verself.DistributionCheckUpdateInput{
		PackageName:     product.PackageName,
		ChannelName:     channel,
		PlatformOS:      runtime.GOOS,
		PlatformArch:    runtime.GOARCH,
		InstalledDigest: installedDigest,
	})
	if err != nil {
		return UpgradeResult{}, err
	}
	target := check.Target
	result := UpgradeResult{
		ProductName:        product.ProductName,
		BinaryName:         product.BinaryName,
		ChannelName:        channel,
		UpdateAvailable:    check.UpdateAvailable,
		InstalledPath:      binaryPath,
		InstalledVersion:   target.PackageVersion,
		ArtifactDigest:     target.ArtifactDigest,
		PublicOCIReference: target.PublicOCIReference,
		ReceiptPath:        store.paths.installReceiptPath(product.ProductName),
		Traceparent:        firstNonEmpty(check.Traceparent, options.Traceparent),
	}
	if !check.UpdateAvailable {
		return result, nil
	}
	if target.ArtifactDigest == "" || target.PublicOCIReference == "" {
		return UpgradeResult{}, errors.New("distribution target is missing immutable OCI reference")
	}
	layerDigest, err := downloadAndInstallOCI(ctx, http.DefaultClient, store.paths.upgradeCacheDir(product.ProductName), product.BinaryName, binaryPath, target)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := recordUpgradeDownloadVerified(ctx, http.DefaultClient, distributionAPIBase, target, product.PackageName, channel, layerDigest, result.Traceparent); err != nil {
		return UpgradeResult{}, err
	}
	receipt = InstallReceipt{
		Version:            1,
		ProductName:        product.ProductName,
		BinaryName:         product.BinaryName,
		ChannelName:        channel,
		PlatformOS:         runtime.GOOS,
		PlatformArch:       runtime.GOARCH,
		PackageVersion:     target.PackageVersion,
		ArtifactDigest:     target.ArtifactDigest,
		LayerDigest:        layerDigest,
		PublicOCIReference: target.PublicOCIReference,
		InstalledPath:      binaryPath,
		Traceparent:        result.Traceparent,
		InstalledAt:        time.Now().UTC(),
	}
	if err := store.SaveInstallReceipt(receipt); err != nil {
		return UpgradeResult{}, err
	}
	result.LayerDigest = layerDigest
	result.InstalledAt = receipt.InstalledAt
	return result, nil
}

func (c CLI) distributionUpgradeClient(serverURL string, distributionURL string, traceparent string) (*verself.Client, string, error) {
	serverURL = strings.TrimSpace(firstNonEmpty(serverURL, c.getenv("VERSELF_SERVER_URL")))
	distributionURL = strings.TrimSpace(firstNonEmpty(distributionURL, c.getenv("VERSELF_DISTRIBUTION_API_URL")))
	if store, err := newStore(c.getenv); err == nil {
		if profile, err := store.LoadProfile(""); err == nil {
			serverURL = strings.TrimSpace(firstNonEmpty(serverURL, profile.ServerURL))
			distributionURL = strings.TrimSpace(firstNonEmpty(distributionURL, profile.DistributionURL))
		}
	}
	distributionAPIBase, err := distributionServiceURL(distributionURL, serverURL)
	if err != nil {
		return nil, "", err
	}
	client, err := verself.New(verself.Options{
		ServerURL:       serverURL,
		DistributionURL: distributionAPIBase,
		Traceparent:     traceparent,
	})
	if err != nil {
		return nil, "", err
	}
	return client, distributionAPIBase, nil
}

func downloadAndInstallOCI(ctx context.Context, client *http.Client, cacheDir string, binaryName string, binaryPath string, target verself.DistributionTarget) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	ref, err := parseOCIReference(target.PublicOCIReference)
	if err != nil {
		return "", err
	}
	manifestURL := strings.TrimSpace(target.DownloadURL)
	if manifestURL == "" {
		manifestURL = "https://" + ref.Registry + "/v2/" + ref.Repository + "/manifests/" + ref.Digest
	}
	manifestBody, err := httpGetBytes(ctx, client, manifestURL, "application/vnd.oci.image.manifest.v1+json")
	if err != nil {
		return "", err
	}
	if got := sha256Digest(manifestBody); got != target.ArtifactDigest {
		return "", fmt.Errorf("OCI manifest digest %s does not match distribution digest %s", got, target.ArtifactDigest)
	}
	var manifest ociManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		return "", fmt.Errorf("decode OCI manifest: %w", err)
	}
	if len(manifest.Layers) != 1 {
		return "", fmt.Errorf("OCI upgrade manifest must contain exactly one layer, got %d", len(manifest.Layers))
	}
	layer := manifest.Layers[0]
	if !sha256DigestPattern.MatchString(layer.Digest) {
		return "", fmt.Errorf("OCI layer digest is invalid: %s", layer.Digest)
	}
	if cacheDir == "" {
		return "", errors.New("upgrade cache directory is required")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", err
	}
	blobURL, err := blobURLForManifest(manifestURL, layer.Digest)
	if err != nil {
		return "", err
	}
	blobPath, err := downloadVerifiedBlob(ctx, client, blobURL, layer.Digest, layer.Size, cacheDir)
	if err != nil {
		return "", err
	}
	if err := installBinaryFromTar(blobPath, binaryName, binaryPath); err != nil {
		return "", err
	}
	return layer.Digest, nil
}

func recordUpgradeDownloadVerified(ctx context.Context, client *http.Client, distributionAPIBase string, target verself.DistributionTarget, packageName string, channelName string, layerDigest string, traceparent string) error {
	apiBase := strings.TrimRight(strings.TrimSpace(distributionAPIBase), "/")
	if apiBase == "" {
		return errors.New("distribution API URL is required")
	}
	body, err := json.Marshal(map[string]string{
		"package_name":      packageName,
		"channel_name":      channelName,
		"platform_os":       runtime.GOOS,
		"platform_arch":     runtime.GOARCH,
		"artifact_digest":   target.ArtifactDigest,
		"layer_digest":      layerDigest,
		"installed_version": target.PackageVersion,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/api/v1/distribution/upgrades:recordVerifiedDownload", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "verself-upgrade/1")
	if strings.TrimSpace(traceparent) != "" {
		req.Header.Set("Traceparent", strings.TrimSpace(traceparent))
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("record upgrade verification failed with HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	return nil
}

func httpGetBytes(ctx context.Context, client *http.Client, rawURL string, accept string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "verself-upgrade/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s failed with HTTP %d", rawURL, resp.StatusCode)
	}
	return body, nil
}

func downloadVerifiedBlob(ctx context.Context, client *http.Client, rawURL string, digest string, expectedSize int64, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	req.Header.Set("User-Agent", "verself-upgrade/1")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s failed with HTTP %d", rawURL, resp.StatusCode)
	}
	tmp, err := os.CreateTemp(dir, ".blob-*.tmp")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()
	hasher := sha256.New()
	written, err := copyHash(tmp, resp.Body, hasher)
	if err != nil {
		_ = tmp.Close()
		return "", err
	}
	if expectedSize > 0 && written != expectedSize {
		_ = tmp.Close()
		return "", fmt.Errorf("OCI blob length %d does not match expected length %d", written, expectedSize)
	}
	got := "sha256:" + hex.EncodeToString(hasher.Sum(nil))
	if got != digest {
		_ = tmp.Close()
		return "", fmt.Errorf("OCI blob digest %s does not match descriptor digest %s", got, digest)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	finalPath := filepath.Join(dir, strings.ReplaceAll(digest, ":", "-")+".tar")
	if err := os.Rename(tmpName, finalPath); err != nil {
		return "", err
	}
	removeTmp = false
	return finalPath, nil
}

func copyHash(dst io.Writer, src io.Reader, hasher hash.Hash) (int64, error) {
	return io.Copy(io.MultiWriter(dst, hasher), src)
}

func installBinaryFromTar(tarPath string, binaryName string, binaryPath string) error {
	file, err := os.Open(tarPath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	reader := tar.NewReader(file)
	want := "bin/" + binaryName
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read release tar: %w", err)
		}
		if path.Clean(header.Name) != want {
			continue
		}
		if header.FileInfo().IsDir() {
			return fmt.Errorf("release tar entry %s is a directory", want)
		}
		return replaceBinary(reader, header, binaryPath)
	}
	return fmt.Errorf("release tar missing %s", want)
}

func replaceBinary(src io.Reader, header *tar.Header, binaryPath string) error {
	dir := filepath.Dir(binaryPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(binaryPath)+".*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		return err
	}
	mode := header.FileInfo().Mode() & 0o777
	if mode == 0 || mode&0o111 == 0 {
		mode = 0o755
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, binaryPath); err != nil {
		return err
	}
	return syncDir(dir)
}

func syncDir(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer func() { _ = handle.Close() }()
	return handle.Sync()
}

type ociReference struct {
	Registry   string
	Repository string
	Digest     string
}

func parseOCIReference(value string) (ociReference, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "://") {
		return ociReference{}, errors.New("OCI reference must be scheme-less")
	}
	name, digest, ok := strings.Cut(value, "@")
	if !ok || !sha256DigestPattern.MatchString(digest) {
		return ociReference{}, fmt.Errorf("OCI reference must use an immutable sha256 digest: %s", value)
	}
	registry, repository, ok := strings.Cut(name, "/")
	if !ok || registry == "" || repository == "" {
		return ociReference{}, fmt.Errorf("OCI reference is missing registry or repository: %s", value)
	}
	// OCI references are scheme-less; HTTP endpoints add schemes at the network boundary.
	return ociReference{Registry: registry, Repository: strings.Trim(repository, "/"), Digest: digest}, nil
}

func blobURLForManifest(manifestURL string, digest string) (string, error) {
	parsed, err := url.Parse(manifestURL)
	if err != nil {
		return "", err
	}
	idx := strings.LastIndex(parsed.Path, "/manifests/")
	if idx < 0 {
		return "", fmt.Errorf("manifest URL is not an OCI manifest endpoint: %s", manifestURL)
	}
	parsed.Path = parsed.Path[:idx] + "/blobs/" + digest
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func distributionServiceURL(distributionURL string, serverURL string) (string, error) {
	if strings.TrimSpace(distributionURL) != "" {
		return normalizeUpgradeURL(distributionURL)
	}
	if strings.TrimSpace(serverURL) == "" {
		return verself.DefaultDistributionURL, nil
	}
	normalized, err := normalizeUpgradeURL(serverURL)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	host := parsed.Hostname()
	if host == "" {
		return "", errors.New("distribution URL host is empty")
	}
	const prefix = "distribution.api."
	if strings.HasPrefix(host, prefix) {
		parsed.Path = ""
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return strings.TrimRight(parsed.String(), "/"), nil
	}
	if strings.Contains(host, ".api.") {
		return "", errors.New("server URL must be the installation apex; pass --distribution-url for service API hosts")
	}
	serviceHost := prefix + host
	if port := parsed.Port(); port != "" {
		serviceHost += ":" + port
	}
	parsed.Host = serviceHost
	parsed.Path = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func normalizeUpgradeURL(value string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(value), "/")
	if trimmed == "" {
		return "", errors.New("distribution URL is empty")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("distribution URL must be absolute")
	}
	return trimmed, nil
}

func sha256Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

type ociManifest struct {
	MediaType string          `json:"mediaType"`
	Config    ociDescriptor   `json:"config"`
	Layers    []ociDescriptor `json:"layers"`
}

type ociDescriptor struct {
	MediaType   string            `json:"mediaType"`
	Digest      string            `json:"digest"`
	Size        int64             `json:"size"`
	Annotations map[string]string `json:"annotations,omitempty"`
}
