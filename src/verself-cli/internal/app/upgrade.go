package app

import (
	"archive/tar"
	"context"
	"crypto/ed25519"
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
	"slices"
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
	TUFMetadataURL     string    `json:"tuf_metadata_url,omitempty"`
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
	client, err := c.distributionUpgradeClient(options.ServerURL, options.DistributionURL, options.Traceparent)
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
		TUFMetadataURL:     target.TUFMetadataURL,
		ReceiptPath:        store.paths.installReceiptPath(product.ProductName),
		Traceparent:        firstNonEmpty(check.Traceparent, options.Traceparent),
	}
	if !check.UpdateAvailable {
		return result, nil
	}
	if target.ArtifactDigest == "" || target.PublicOCIReference == "" {
		return UpgradeResult{}, errors.New("distribution target is missing immutable OCI reference")
	}
	previousRoot := ""
	if receiptErr == nil {
		previousRoot = receipt.TUFRootSHA256
	}
	verified, err := verifyUpgradeMetadata(ctx, http.DefaultClient, verifyUpgradeMetadataInput{
		MetadataBaseURL: target.TUFMetadataURL,
		PackageName:     product.PackageName,
		ChannelName:     channel,
		PlatformOS:      runtime.GOOS,
		PlatformArch:    runtime.GOARCH,
		ArtifactDigest:  target.ArtifactDigest,
		PreviousRoot:    previousRoot,
	})
	if err != nil {
		return UpgradeResult{}, err
	}
	layerDigest, err := downloadAndInstallOCI(ctx, http.DefaultClient, store.paths.upgradeCacheDir(product.ProductName), product.BinaryName, binaryPath, target)
	if err != nil {
		return UpgradeResult{}, err
	}
	if err := recordUpgradeDownloadVerified(ctx, http.DefaultClient, target, product.PackageName, channel, layerDigest, result.Traceparent); err != nil {
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
		TUFMetadataURL:     target.TUFMetadataURL,
		TUFRootSHA256:      verified.RootSHA256,
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

func (c CLI) distributionUpgradeClient(serverURL string, distributionURL string, traceparent string) (*verself.Client, error) {
	serverURL = strings.TrimSpace(firstNonEmpty(serverURL, c.getenv("VERSELF_SERVER_URL")))
	distributionURL = strings.TrimSpace(firstNonEmpty(distributionURL, c.getenv("VERSELF_DISTRIBUTION_API_URL")))
	if store, err := newStore(c.getenv); err == nil {
		if profile, err := store.LoadProfile(""); err == nil {
			serverURL = strings.TrimSpace(firstNonEmpty(serverURL, profile.ServerURL))
			distributionURL = strings.TrimSpace(firstNonEmpty(distributionURL, profile.DistributionURL))
		}
	}
	return verself.New(verself.Options{
		ServerURL:       serverURL,
		DistributionURL: distributionURL,
		Traceparent:     traceparent,
	})
}

type verifyUpgradeMetadataInput struct {
	MetadataBaseURL string
	PackageName     string
	ChannelName     string
	PlatformOS      string
	PlatformArch    string
	ArtifactDigest  string
	PreviousRoot    string
}

type verifiedUpgradeMetadata struct {
	RootSHA256 string
}

func verifyUpgradeMetadata(ctx context.Context, client *http.Client, input verifyUpgradeMetadataInput) (verifiedUpgradeMetadata, error) {
	base := strings.TrimRight(strings.TrimSpace(input.MetadataBaseURL), "/")
	if base == "" {
		return verifiedUpgradeMetadata{}, errors.New("TUF metadata URL is required")
	}
	rootBody, rootEnvelope, err := fetchTUFEnvelope(ctx, client, base, "root")
	if err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	rootSHA := sha256Digest(rootBody)
	// The first receipt pins online root metadata; later updates reject silent root drift.
	if strings.TrimSpace(input.PreviousRoot) != "" && input.PreviousRoot != rootSHA {
		return verifiedUpgradeMetadata{}, fmt.Errorf("TUF root changed from %s to %s; explicit reinstall is required", input.PreviousRoot, rootSHA)
	}
	var root tufRootSigned
	if err := json.Unmarshal(rootEnvelope.Signed, &root); err != nil {
		return verifiedUpgradeMetadata{}, fmt.Errorf("decode TUF root: %w", err)
	}
	if err := verifyTUFRole("root", rootEnvelope, root); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	if err := requireNotExpired("root", root.Expires); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	timestampBody, timestampEnvelope, err := fetchTUFEnvelope(ctx, client, base, "timestamp")
	if err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	if err := verifyTUFRole("timestamp", timestampEnvelope, root); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	var timestamp tufTimestampSigned
	if err := json.Unmarshal(timestampEnvelope.Signed, &timestamp); err != nil {
		return verifiedUpgradeMetadata{}, fmt.Errorf("decode TUF timestamp: %w", err)
	}
	if err := requireNotExpired("timestamp", timestamp.Expires); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	snapshotBody, snapshotEnvelope, err := fetchTUFEnvelope(ctx, client, base, "snapshot")
	if err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	if err := verifyTUFMeta("timestamp snapshot", timestamp.Meta["snapshot.json"], snapshotBody); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	if err := verifyTUFRole("snapshot", snapshotEnvelope, root); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	var snapshot tufSnapshotSigned
	if err := json.Unmarshal(snapshotEnvelope.Signed, &snapshot); err != nil {
		return verifiedUpgradeMetadata{}, fmt.Errorf("decode TUF snapshot: %w", err)
	}
	if err := requireNotExpired("snapshot", snapshot.Expires); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	targetsBody, targetsEnvelope, err := fetchTUFEnvelope(ctx, client, base, "targets")
	if err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	if err := verifyTUFMeta("snapshot targets", snapshot.Meta["targets.json"], targetsBody); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	if err := verifyTUFRole("targets", targetsEnvelope, root); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	var targets tufTargetsSigned
	if err := json.Unmarshal(targetsEnvelope.Signed, &targets); err != nil {
		return verifiedUpgradeMetadata{}, fmt.Errorf("decode TUF targets: %w", err)
	}
	if err := requireNotExpired("targets", targets.Expires); err != nil {
		return verifiedUpgradeMetadata{}, err
	}
	targetPath := input.ChannelName + "/" + input.PlatformOS + "/" + input.PlatformArch + "/" + input.PackageName
	file, ok := targets.Targets[targetPath]
	if !ok {
		return verifiedUpgradeMetadata{}, fmt.Errorf("TUF targets missing %s", targetPath)
	}
	if file.Hashes["sha256"] != digestHex(input.ArtifactDigest) {
		return verifiedUpgradeMetadata{}, fmt.Errorf("TUF target digest %s does not match distribution digest %s", file.Hashes["sha256"], input.ArtifactDigest)
	}
	if file.Custom.Package != input.PackageName || file.Custom.Channel != input.ChannelName || file.Custom.PlatformOS != input.PlatformOS || file.Custom.PlatformArch != input.PlatformArch {
		return verifiedUpgradeMetadata{}, errors.New("TUF target custom metadata does not match requested product target")
	}
	_ = timestampBody
	return verifiedUpgradeMetadata{RootSHA256: rootSHA}, nil
}

func fetchTUFEnvelope(ctx context.Context, client *http.Client, base string, role string) ([]byte, tufEnvelope, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+role+".json", nil)
	if err != nil {
		return nil, tufEnvelope{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, tufEnvelope{}, fmt.Errorf("fetch TUF %s: %w", role, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, tufEnvelope{}, fmt.Errorf("read TUF %s: %w", role, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, tufEnvelope{}, fmt.Errorf("fetch TUF %s failed with HTTP %d", role, resp.StatusCode)
	}
	var envelope tufEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, tufEnvelope{}, fmt.Errorf("decode TUF %s envelope: %w", role, err)
	}
	if len(envelope.Signatures) == 0 || len(envelope.Signed) == 0 {
		return nil, tufEnvelope{}, fmt.Errorf("TUF %s envelope is unsigned", role)
	}
	return body, envelope, nil
}

func verifyTUFRole(role string, envelope tufEnvelope, root tufRootSigned) error {
	roleDescription, ok := root.Roles[role]
	if !ok {
		return fmt.Errorf("TUF root has no %s role", role)
	}
	if roleDescription.Threshold <= 0 {
		return fmt.Errorf("TUF %s threshold is invalid", role)
	}
	verified := map[string]struct{}{}
	for _, signature := range envelope.Signatures {
		if !slices.Contains(roleDescription.KeyIDs, signature.KeyID) {
			continue
		}
		key, ok := root.Keys[signature.KeyID]
		if !ok || key.KeyType != "ed25519" || key.Scheme != "ed25519" {
			continue
		}
		publicHex := key.KeyVal["public"]
		publicKey, err := hex.DecodeString(publicHex)
		if err != nil || len(publicKey) != ed25519.PublicKeySize {
			return fmt.Errorf("TUF %s key %s is invalid", role, signature.KeyID)
		}
		sig, err := hex.DecodeString(signature.Signature)
		if err != nil || len(sig) != ed25519.SignatureSize {
			return fmt.Errorf("TUF %s signature for key %s is invalid", role, signature.KeyID)
		}
		if ed25519.Verify(ed25519.PublicKey(publicKey), envelope.Signed, sig) {
			verified[signature.KeyID] = struct{}{}
		}
	}
	if len(verified) < roleDescription.Threshold {
		return fmt.Errorf("TUF %s has %d valid signatures, threshold %d", role, len(verified), roleDescription.Threshold)
	}
	return nil
}

func verifyTUFMeta(label string, meta tufMetaFile, body []byte) error {
	if meta.Length <= 0 || meta.Length != int64(len(body)) {
		return fmt.Errorf("TUF %s length mismatch", label)
	}
	if meta.Hashes["sha256"] != digestHex(sha256Digest(body)) {
		return fmt.Errorf("TUF %s sha256 mismatch", label)
	}
	return nil
}

func requireNotExpired(role string, expires string) error {
	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(expires))
	if err != nil {
		return fmt.Errorf("TUF %s expires timestamp is invalid: %w", role, err)
	}
	if !time.Now().UTC().Before(expiresAt) {
		return fmt.Errorf("TUF %s metadata expired at %s", role, expiresAt.Format(time.RFC3339))
	}
	return nil
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

func recordUpgradeDownloadVerified(ctx context.Context, client *http.Client, target verself.DistributionTarget, packageName string, channelName string, layerDigest string, traceparent string) error {
	apiBase, err := distributionAPIBaseFromTUF(target.TUFMetadataURL)
	if err != nil {
		return err
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

func distributionAPIBaseFromTUF(tufMetadataURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(strings.TrimSpace(tufMetadataURL), "/"))
	if err != nil {
		return "", err
	}
	idx := strings.LastIndex(parsed.Path, "/tuf/")
	if idx < 0 {
		return "", fmt.Errorf("TUF metadata URL is not under /tuf: %s", tufMetadataURL)
	}
	parsed.Path = parsed.Path[:idx]
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimRight(parsed.String(), "/"), nil
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

func sha256Digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestHex(digest string) string {
	return strings.TrimPrefix(strings.TrimSpace(digest), "sha256:")
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

type tufEnvelope struct {
	Signatures []tufSignature  `json:"signatures"`
	Signed     json.RawMessage `json:"signed"`
}

type tufSignature struct {
	KeyID     string `json:"keyid"`
	Signature string `json:"sig"`
}

type tufRootSigned struct {
	Type               string             `json:"_type"`
	SpecVersion        string             `json:"spec_version"`
	Version            int64              `json:"version"`
	Expires            string             `json:"expires"`
	ConsistentSnapshot bool               `json:"consistent_snapshot"`
	Keys               map[string]tufKey  `json:"keys"`
	Roles              map[string]tufRole `json:"roles"`
}

type tufKey struct {
	KeyType string            `json:"keytype"`
	Scheme  string            `json:"scheme"`
	KeyVal  map[string]string `json:"keyval"`
}

type tufRole struct {
	KeyIDs    []string `json:"keyids"`
	Threshold int      `json:"threshold"`
}

type tufTargetsSigned struct {
	Type        string                   `json:"_type"`
	SpecVersion string                   `json:"spec_version"`
	Version     int64                    `json:"version"`
	Expires     string                   `json:"expires"`
	Targets     map[string]tufTargetFile `json:"targets"`
}

type tufTargetFile struct {
	Length int64             `json:"length"`
	Hashes map[string]string `json:"hashes"`
	Custom tufTargetCustom   `json:"custom"`
}

type tufTargetCustom struct {
	OCIReference string `json:"oci_reference"`
	Package      string `json:"package"`
	Version      string `json:"version"`
	Channel      string `json:"channel"`
	PlatformOS   string `json:"platform_os"`
	PlatformArch string `json:"platform_arch"`
	SourceCommit string `json:"source_commit"`
}

type tufSnapshotSigned struct {
	Type        string                 `json:"_type"`
	SpecVersion string                 `json:"spec_version"`
	Version     int64                  `json:"version"`
	Expires     string                 `json:"expires"`
	Meta        map[string]tufMetaFile `json:"meta"`
}

type tufTimestampSigned struct {
	Type        string                 `json:"_type"`
	SpecVersion string                 `json:"spec_version"`
	Version     int64                  `json:"version"`
	Expires     string                 `json:"expires"`
	Meta        map[string]tufMetaFile `json:"meta"`
}

type tufMetaFile struct {
	Version int64             `json:"version"`
	Length  int64             `json:"length"`
	Hashes  map[string]string `json:"hashes"`
}
