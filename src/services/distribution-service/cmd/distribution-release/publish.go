package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	distributioninternalclient "github.com/verself/distribution-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	defaultOriginRegistryURL = "http://127.0.0.1:5080"
	defaultPublicRegistryURL = "https://oci.verself.sh"
	defaultOCIRepository     = "verself/mksk"
	defaultZotPublisherUser  = "artifact-publisher"
	defaultZotPasswordFile   = "/etc/zot/publisher-password"
	defaultPolicyRef         = "distribution-policies/mksk/v1"
	defaultSubmittedBy       = "distribution-release"
	artifactTarMediaType     = "application/vnd.verself.mksk.release.tar+tar"
	sbomArtifactType         = "application/spdx+json"
	testArtifactType         = "application/vnd.verself.test-evidence.v1"
	licenseArtifactType      = "application/vnd.verself.license-report.v1+json"
)

type publishConfig struct {
	payloadPath        string
	artifactRoot       string
	toolsDir           string
	sourceRepository   string
	originRegistryURL  string
	publicRegistryURL  string
	ociRepository      string
	zotUsername        string
	zotPassword        string
	zotPasswordFile    string
	cosignKey          string
	signerIdentity     string
	policyRef          string
	submittedBy        string
	distributionServer string
}

type ociPublishResult struct {
	SubjectRef      string
	RegistryHost    string
	Repository      string
	Digest          string
	MediaType       string
	SizeBytes       int64
	SignatureDigest string
	Evidence        []distributioninternalclient.Evidence
}

type orasDescriptor struct {
	MediaType    string           `json:"mediaType"`
	ArtifactType string           `json:"artifactType"`
	Digest       string           `json:"digest"`
	Size         int64            `json:"size"`
	Referrers    []orasDescriptor `json:"referrers"`
	Manifests    []orasDescriptor `json:"manifests"`
}

func runMkskPublish(ctx context.Context, args []string) error {
	cfg := publishConfig{}
	fs := flag.NewFlagSet("distribution-release mksk publish", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.payloadPath, "payload", "", "Nomad dispatch payload JSON file.")
	fs.StringVar(&cfg.artifactRoot, "artifact-root", "", "Exact output root for release artifacts.")
	fs.StringVar(&cfg.toolsDir, "tools-dir", "local", "Directory containing release tools under bin/.")
	fs.StringVar(&cfg.sourceRepository, "source-repository", envOr("DISTRIBUTION_RELEASE_SOURCE_REPOSITORY_URL", sourceRepositoryURL), "Git repository URL used for exact source checkout.")
	fs.StringVar(&cfg.originRegistryURL, "origin-registry-url", envOr("DISTRIBUTION_RELEASE_ORIGIN_REGISTRY_URL", defaultOriginRegistryURL), "Origin Zot registry URL.")
	fs.StringVar(&cfg.publicRegistryURL, "public-registry-url", envOr("DISTRIBUTION_RELEASE_PUBLIC_REGISTRY_URL", defaultPublicRegistryURL), "Public distribution registry URL.")
	fs.StringVar(&cfg.ociRepository, "oci-repository", envOr("DISTRIBUTION_RELEASE_OCI_REPOSITORY", defaultOCIRepository), "OCI repository to push.")
	fs.StringVar(&cfg.zotUsername, "zot-username", envOr("DISTRIBUTION_RELEASE_ZOT_USERNAME", defaultZotPublisherUser), "Zot publisher username.")
	fs.StringVar(&cfg.zotPassword, "zot-password", os.Getenv("DISTRIBUTION_RELEASE_ZOT_PASSWORD"), "Zot publisher password. Prefer --zot-password-file.")
	fs.StringVar(&cfg.zotPasswordFile, "zot-password-file", envOr("DISTRIBUTION_RELEASE_ZOT_PASSWORD_FILE", defaultZotPasswordFile), "File containing the Zot publisher password.")
	fs.StringVar(&cfg.cosignKey, "cosign-key", os.Getenv("DISTRIBUTION_RELEASE_COSIGN_KEY"), "Cosign key reference.")
	fs.StringVar(&cfg.signerIdentity, "signer-identity", envOr("DISTRIBUTION_RELEASE_SIGNER_ID", defaultBuilderID), "Signer identity admitted by distribution-service.")
	fs.StringVar(&cfg.policyRef, "policy-ref", envOr("DISTRIBUTION_RELEASE_POLICY_REF", defaultPolicyRef), "Distribution policy reference.")
	fs.StringVar(&cfg.submittedBy, "submitted-by", envOr("DISTRIBUTION_RELEASE_SUBMITTED_BY", defaultSubmittedBy), "Actor recorded for admission and promotion.")
	fs.StringVar(&cfg.distributionServer, "distribution-server", workloadauth.InternalURL(workloadauth.ServiceDistribution), "Internal distribution-service base URL.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	payload, err := loadPublishPayload(cfg.payloadPath)
	if err != nil {
		return err
	}
	if err := validatePublishConfig(cfg, payload); err != nil {
		return err
	}
	sourceRoot, cleanup, err := checkoutSourceCommit(ctx, cfg.sourceRepository, payload.SourceCommit)
	if err != nil {
		return err
	}
	defer cleanup()
	out, err := generateReleaseArtifacts(ctx, mkskConfig{
		repoRoot:     sourceRoot,
		toolsDir:     cfg.toolsDir,
		outRoot:      filepath.Dir(cfg.artifactRoot),
		releaseRoot:  cfg.artifactRoot,
		channel:      payload.Channel,
		version:      payload.Version,
		sourceRef:    payload.SourceRef,
		sourceCommit: payload.SourceCommit,
		builderID:    payload.BuilderID,
		platform:     payload.Platform,
	})
	if err != nil {
		return err
	}
	oci, err := publishOCI(ctx, cfg, out)
	if err != nil {
		return err
	}
	artifact, target, err := admitAndPromote(ctx, cfg, payload, out, oci)
	if err != nil {
		return err
	}
	summary := map[string]any{
		"artifact_root": out.paths.root,
		"artifact":      artifact,
		"target":        target,
		"oci_subject":   oci.SubjectRef,
	}
	summaryBody, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out.paths.root, "publish-summary.json"), append(summaryBody, '\n'), 0o644); err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(summaryBody, '\n'))
	return err
}

func loadPublishPayload(path string) (mkskPublishPayload, error) {
	if strings.TrimSpace(path) == "" {
		return mkskPublishPayload{}, fmt.Errorf("--payload is required")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return mkskPublishPayload{}, err
	}
	var payload mkskPublishPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return mkskPublishPayload{}, err
	}
	return payload, nil
}

func validatePublishConfig(cfg publishConfig, payload mkskPublishPayload) error {
	missing := []string{}
	required := map[string]string{
		"payload.package":       payload.PackageName,
		"payload.channel":       payload.Channel,
		"payload.source_ref":    payload.SourceRef,
		"payload.source_commit": payload.SourceCommit,
		"payload.platform":      payload.Platform,
		"payload.builder_id":    payload.BuilderID,
		"artifact-root":         cfg.artifactRoot,
		"tools-dir":             cfg.toolsDir,
		"source-repository":     cfg.sourceRepository,
		"origin-registry-url":   cfg.originRegistryURL,
		"public-registry-url":   cfg.publicRegistryURL,
		"oci-repository":        cfg.ociRepository,
		"zot-username":          cfg.zotUsername,
		"cosign-key":            cfg.cosignKey,
		"signer-identity":       cfg.signerIdentity,
		"policy-ref":            cfg.policyRef,
		"submitted-by":          cfg.submittedBy,
	}
	for key, value := range required {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if strings.TrimSpace(cfg.zotPassword) == "" && strings.TrimSpace(cfg.zotPasswordFile) == "" {
		missing = append(missing, "zot-password-file")
	}
	if payload.PackageName != mkskPackageName {
		return fmt.Errorf("payload package = %q, want %q", payload.PackageName, mkskPackageName)
	}
	if err := validateDispatchVersion(payload.Channel, payload.Version); err != nil {
		return err
	}
	if _, _, err := parsePlatform(payload.Platform); err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required release inputs: %s", strings.Join(missing, ", "))
	}
	return nil
}

func checkoutSourceCommit(ctx context.Context, repoURL, commit string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "verself-mksk-release-publish-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if _, err := runCommand(ctx, "", "git", "clone", "--no-checkout", "--filter=blob:none", repoURL, dir); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if _, err := runCommand(ctx, dir, "git", "fetch", "--depth=1", "origin", commit); err != nil {
		cleanup()
		return "", func() {}, err
	}
	if _, err := runCommand(ctx, dir, "git", "checkout", "--detach", commit); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return dir, cleanup, nil
}

func publishOCI(ctx context.Context, cfg publishConfig, out releaseOutput) (ociPublishResult, error) {
	tools := filepath.Join(cfg.toolsDir, "bin")
	oras := filepath.Join(tools, "oras")
	cosign := filepath.Join(tools, "cosign")
	origin, err := url.Parse(cfg.originRegistryURL)
	if err != nil {
		return ociPublishResult{}, err
	}
	registryHost := origin.Host
	if registryHost == "" {
		registryHost = strings.TrimPrefix(strings.TrimPrefix(cfg.originRegistryURL, "http://"), "https://")
	}
	if registryHost == "" {
		return ociPublishResult{}, fmt.Errorf("origin registry URL must include a host")
	}
	dockerConfig, err := os.MkdirTemp("", "verself-release-docker-config-")
	if err != nil {
		return ociPublishResult{}, err
	}
	defer func() { _ = os.RemoveAll(dockerConfig) }()
	env := map[string]string{"DOCKER_CONFIG": dockerConfig}
	password, err := zotPassword(cfg)
	if err != nil {
		return ociPublishResult{}, err
	}
	plainHTTP := origin.Scheme == "http"
	loginArgs := []string{"login", registryHost, "--username", cfg.zotUsername, "--password-stdin"}
	if plainHTTP {
		loginArgs = append(loginArgs, "--plain-http")
	}
	if _, err := runCommandInputEnv(ctx, "", oras, loginArgs, env, strings.NewReader(password)); err != nil {
		return ociPublishResult{}, err
	}
	tag := sanitizeOCITag(out.version)
	target := registryHost + "/" + cfg.ociRepository + ":" + tag
	pushArgs := []string{
		"push",
		"--format", "json",
		"--annotation", "org.opencontainers.image.title=make-skill",
		"--annotation", "org.opencontainers.image.version=" + out.version,
		"--annotation", "org.opencontainers.image.revision=" + out.sourceCommit,
		target,
		out.artifactPath + ":" + artifactTarMediaType,
	}
	if plainHTTP {
		pushArgs = append([]string{"push", "--plain-http"}, pushArgs[1:]...)
	}
	pushResult, err := runCommandEnv(ctx, "", oras, pushArgs, env)
	if err != nil {
		return ociPublishResult{}, err
	}
	descriptor, err := parseORASDescriptor(pushResult.stdout)
	if err != nil {
		return ociPublishResult{}, err
	}
	if descriptor.Digest == "" {
		return ociPublishResult{}, fmt.Errorf("oras push did not return a digest")
	}
	subjectRef := registryHost + "/" + cfg.ociRepository + "@" + descriptor.Digest
	signArgs := []string{"sign", "--yes", "--tlog-upload=false", "--key", cfg.cosignKey}
	if plainHTTP {
		signArgs = append(signArgs, "--allow-insecure-registry")
	}
	signArgs = append(signArgs, subjectRef)
	if _, err := runCommandEnv(ctx, "", cosign, signArgs, env); err != nil {
		return ociPublishResult{}, err
	}
	signatureDigest, err := findCosignSignatureReferrer(ctx, oras, env, subjectRef, plainHTTP)
	if err != nil {
		return ociPublishResult{}, err
	}
	attestationDigest, err := cosignAttestProvenance(ctx, cosign, oras, env, subjectRef, plainHTTP, cfg.cosignKey, out.provenancePath)
	if err != nil {
		return ociPublishResult{}, err
	}
	evidence, err := attachEvidence(ctx, oras, env, subjectRef, plainHTTP, descriptor.Digest, attestationDigest, out)
	if err != nil {
		return ociPublishResult{}, err
	}
	evidence = append([]distributioninternalclient.Evidence{{
		EvidenceKind:      "cosign_signature",
		PredicateType:     "https://sigstore.dev/cosign/v1",
		SubjectDigest:     descriptor.Digest,
		DocumentDigest:    signatureDigest,
		OCIReferrerDigest: signatureDigest,
	}}, evidence...)
	if err := verifyReferrers(ctx, oras, env, subjectRef, plainHTTP, evidence); err != nil {
		return ociPublishResult{}, err
	}
	return ociPublishResult{
		SubjectRef:      subjectRef,
		RegistryHost:    registryHost,
		Repository:      cfg.ociRepository,
		Digest:          descriptor.Digest,
		MediaType:       valueOr(descriptor.MediaType, "application/vnd.oci.image.manifest.v1+json"),
		SizeBytes:       descriptor.Size,
		SignatureDigest: signatureDigest,
		Evidence:        evidence,
	}, nil
}

func zotPassword(cfg publishConfig) (string, error) {
	if strings.TrimSpace(cfg.zotPassword) != "" {
		return cfg.zotPassword, nil
	}
	body, err := os.ReadFile(cfg.zotPasswordFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func parseORASDescriptor(output string) (orasDescriptor, error) {
	var descriptor orasDescriptor
	if err := json.Unmarshal([]byte(output), &descriptor); err != nil {
		return orasDescriptor{}, err
	}
	return descriptor, nil
}

func findCosignSignatureReferrer(ctx context.Context, oras string, env map[string]string, subjectRef string, plainHTTP bool) (string, error) {
	referrers, err := discoverReferrers(ctx, oras, env, subjectRef, plainHTTP)
	if err != nil {
		return "", err
	}
	for _, referrer := range referrers {
		if strings.Contains(referrer.ArtifactType, "cosign") || strings.Contains(referrer.MediaType, "cosign") {
			if referrer.Digest != "" {
				return referrer.Digest, nil
			}
		}
	}
	return "", fmt.Errorf("cosign did not publish an OCI referrer for %s", subjectRef)
}

func cosignAttestProvenance(ctx context.Context, cosign, oras string, env map[string]string, subjectRef string, plainHTTP bool, key, predicatePath string) (string, error) {
	before, err := discoverReferrers(ctx, oras, env, subjectRef, plainHTTP)
	if err != nil {
		return "", err
	}
	seen := map[string]struct{}{}
	for _, referrer := range before {
		seen[referrer.Digest] = struct{}{}
	}
	args := []string{"attest", "--yes", "--key", key, "--statement", predicatePath}
	if plainHTTP {
		args = append(args, "--allow-insecure-registry")
	}
	args = append(args, subjectRef)
	if _, err := runCommandEnv(ctx, "", cosign, args, env); err != nil {
		return "", err
	}
	after, err := discoverReferrers(ctx, oras, env, subjectRef, plainHTTP)
	if err != nil {
		return "", err
	}
	for _, referrer := range after {
		if _, ok := seen[referrer.Digest]; ok {
			continue
		}
		if strings.Contains(referrer.ArtifactType, "attestation") || strings.Contains(referrer.MediaType, "in-toto") || strings.Contains(referrer.ArtifactType, "in-toto") {
			return referrer.Digest, nil
		}
	}
	return "", fmt.Errorf("cosign attest did not publish an OCI referrer for %s", subjectRef)
}

func attachEvidence(ctx context.Context, oras string, env map[string]string, subjectRef string, plainHTTP bool, subjectDigest string, slsaReferrerDigest string, out releaseOutput) ([]distributioninternalclient.Evidence, error) {
	var evidence []distributioninternalclient.Evidence
	provenanceDigest, err := combinedDocumentDigest([]string{out.provenancePath})
	if err != nil {
		return nil, err
	}
	evidence = append(evidence, distributioninternalclient.Evidence{
		EvidenceKind:      "slsa_provenance",
		PredicateType:     "https://slsa.dev/provenance/v1",
		SubjectDigest:     subjectDigest,
		DocumentDigest:    provenanceDigest,
		OCIReferrerDigest: slsaReferrerDigest,
	})
	sbom, err := attachEvidenceFile(ctx, oras, env, subjectRef, plainHTTP, out.artifactSBOMPath, sbomArtifactType, "application/spdx+json", "sbom", "https://spdx.dev/Document", subjectDigest)
	if err != nil {
		return nil, err
	}
	evidence = append(evidence, sbom)
	sourceSBOM, err := attachEvidenceFile(ctx, oras, env, subjectRef, plainHTTP, out.sourceSBOMPath, sbomArtifactType, "application/spdx+json", "source_sbom", "https://spdx.dev/Document", subjectDigest)
	if err != nil {
		return nil, err
	}
	evidence = append(evidence, sourceSBOM)
	licenses, err := attachEvidenceFile(ctx, oras, env, subjectRef, plainHTTP, out.licensesPath, licenseArtifactType, "application/json", "license_report", "https://github.com/EmbarkStudios/cargo-about", subjectDigest)
	if err != nil {
		return nil, err
	}
	evidence = append(evidence, licenses)
	if out.channel == "rc" || out.channel == "stable" {
		testEvidence, err := attachEvidenceFiles(ctx, oras, env, subjectRef, plainHTTP, out.testEvidencePaths, testArtifactType, "application/junit+xml", "test", "https://verself.sh/test-evidence/v1", subjectDigest)
		if err != nil {
			return nil, err
		}
		evidence = append(evidence, testEvidence)
	}
	return evidence, nil
}

func attachEvidenceFile(ctx context.Context, oras string, env map[string]string, subjectRef string, plainHTTP bool, path, artifactType, mediaType, kind, predicate, subjectDigest string) (distributioninternalclient.Evidence, error) {
	return attachEvidenceFiles(ctx, oras, env, subjectRef, plainHTTP, []string{path}, artifactType, mediaType, kind, predicate, subjectDigest)
}

func attachEvidenceFiles(ctx context.Context, oras string, env map[string]string, subjectRef string, plainHTTP bool, paths []string, artifactType, mediaType, kind, predicate, subjectDigest string) (distributioninternalclient.Evidence, error) {
	args := []string{"attach", "--format", "json", "--artifact-type", artifactType, subjectRef}
	if plainHTTP {
		args = append([]string{"attach", "--plain-http"}, args[1:]...)
	}
	for _, path := range paths {
		args = append(args, path+":"+mediaType)
	}
	result, err := runCommandEnv(ctx, "", oras, args, env)
	if err != nil {
		return distributioninternalclient.Evidence{}, err
	}
	descriptor, err := parseORASDescriptor(result.stdout)
	if err != nil {
		return distributioninternalclient.Evidence{}, err
	}
	if descriptor.Digest == "" {
		return distributioninternalclient.Evidence{}, fmt.Errorf("oras attach did not return a digest for %s", kind)
	}
	documentDigest, err := combinedDocumentDigest(paths)
	if err != nil {
		return distributioninternalclient.Evidence{}, err
	}
	return distributioninternalclient.Evidence{
		EvidenceKind:      kind,
		PredicateType:     predicate,
		SubjectDigest:     subjectDigest,
		DocumentDigest:    documentDigest,
		OCIReferrerDigest: descriptor.Digest,
	}, nil
}

func verifyReferrers(ctx context.Context, oras string, env map[string]string, subjectRef string, plainHTTP bool, evidence []distributioninternalclient.Evidence) error {
	referrers, err := discoverReferrers(ctx, oras, env, subjectRef, plainHTTP)
	if err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, referrer := range referrers {
		if referrer.Digest != "" {
			seen[referrer.Digest] = struct{}{}
		}
	}
	for _, item := range evidence {
		if _, ok := seen[item.OCIReferrerDigest]; !ok {
			return fmt.Errorf("missing OCI referrer %s for evidence %s", item.OCIReferrerDigest, item.EvidenceKind)
		}
	}
	return nil
}

func discoverReferrers(ctx context.Context, oras string, env map[string]string, subjectRef string, plainHTTP bool) ([]orasDescriptor, error) {
	args := []string{"discover", "--format", "json", subjectRef}
	if plainHTTP {
		args = append([]string{"discover", "--plain-http"}, args[1:]...)
	}
	result, err := runCommandEnv(ctx, "", oras, args, env)
	if err != nil {
		return nil, err
	}
	descriptor, err := parseORASDescriptor(result.stdout)
	if err != nil {
		return nil, err
	}
	out := append([]orasDescriptor{}, descriptor.Referrers...)
	out = append(out, descriptor.Manifests...)
	return out, nil
}

func admitAndPromote(ctx context.Context, cfg publishConfig, payload mkskPublishPayload, out releaseOutput, oci ociPublishResult) (distributioninternalclient.ArtifactRecord, distributioninternalclient.TargetRecord, error) {
	source, err := workloadauth.Source(ctx, "")
	if err != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, err
	}
	defer func() { _ = source.Close() }()
	if _, err := workloadauth.CurrentIDForService(source, workloadauth.ServiceDistributionRelease); err != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, err
	}
	httpClient, err := workloadauth.MTLSClientForServiceWithTimeouts(source, workloadauth.ServiceDistribution, nil, workloadauth.ServiceClientTimeouts{Total: 10 * time.Second})
	if err != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, err
	}
	client, err := distributioninternalclient.NewClient(cfg.distributionServer, distributioninternalclient.WithHTTPClient(httpClient))
	if err != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, err
	}
	admit, err := client.AdmitArtifact(ctx, distributioninternalclient.AdmitArtifactRequest{
		PackageName:       mkskPackageName,
		PackageVersion:    out.version,
		ChannelName:       out.channel,
		PlatformOS:        out.platformOS,
		PlatformArch:      out.platformArch,
		OriginRegistryURL: cfg.originRegistryURL,
		PublicRegistryURL: cfg.publicRegistryURL,
		OCIRepository:     oci.Repository,
		OCIDigest:         oci.Digest,
		OCIMediaType:      oci.MediaType,
		OCISizeBytes:      positiveSize(oci.SizeBytes),
		BuilderID:         payload.BuilderID,
		SignerIdentity:    cfg.signerIdentity,
		SourceRepository:  cfg.sourceRepository,
		SourceCommit:      out.sourceCommit,
		SourceRef:         out.sourceRef,
		BazelTarget:       mkskBazelTarget,
		PolicyRef:         cfg.policyRef,
		Evidence:          oci.Evidence,
		SubmittedBy:       cfg.submittedBy,
	}, "mksk-admit-"+digestKey(oci.Digest))
	if err != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, err
	}
	if admit.Problem != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, fmt.Errorf("admit artifact: %s", problemDetail(admit.Problem))
	}
	target, err := client.PromoteTarget(ctx, distributioninternalclient.PromoteTargetRequest{
		PackageName:    mkskPackageName,
		ChannelName:    out.channel,
		ArtifactDigest: oci.Digest,
		PlatformOS:     out.platformOS,
		PlatformArch:   out.platformArch,
		PolicyRef:      cfg.policyRef,
		PromotedBy:     cfg.submittedBy,
		Reason:         "mksk " + out.channel + " release " + out.version,
	}, "mksk-promote-"+out.channel+"-"+digestKey(oci.Digest))
	if err != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, err
	}
	if target.Problem != nil {
		return distributioninternalclient.ArtifactRecord{}, distributioninternalclient.TargetRecord{}, fmt.Errorf("promote target: %s", problemDetail(target.Problem))
	}
	return admit.Result.Artifact, target.Result.Target, nil
}

func combinedDocumentDigest(paths []string) (string, error) {
	if len(paths) == 1 {
		sum, _, err := fileSHA256(paths[0])
		if err != nil {
			return "", err
		}
		return "sha256:" + sum, nil
	}
	h := sha256.New()
	for _, path := range paths {
		sum, _, err := fileSHA256(path)
		if err != nil {
			return "", err
		}
		if _, err := io.WriteString(h, sum+"\n"); err != nil {
			return "", err
		}
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func sanitizeOCITag(value string) string {
	replacer := strings.NewReplacer("+", "_", "/", "_", ":", "_")
	return replacer.Replace(value)
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
}

func positiveSize(value int64) int64 {
	if value > 0 {
		return value
	}
	return 1
}

func digestKey(digest string) string {
	return strings.ReplaceAll(digest, ":", "-")
}

func problemDetail(problem *distributioninternalclient.ErrorModel) string {
	if problem.Code != nil && *problem.Code != "" {
		if problem.Detail != nil && *problem.Detail != "" {
			return *problem.Code + ": " + *problem.Detail
		}
		return *problem.Code
	}
	if problem.Detail != nil && *problem.Detail != "" {
		return *problem.Detail
	}
	if problem.Title != nil && *problem.Title != "" {
		return *problem.Title
	}
	return "unknown distribution problem"
}
