package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	distributioninternalclient "github.com/verself/distribution-service/internalclient"
	workloadauth "github.com/verself/service-runtime/workload"
)

const (
	defaultBuilderID      = "spiffe://prod.verself.sh/svc/release-builder"
	defaultSignerIdentity = "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main"
)

type config struct {
	packageName       string
	packageVersion    string
	channelName       string
	platformOS        string
	platformArch      string
	flavor            string
	originRegistryURL string
	publicRegistryURL string
	ociRepository     string
	ociDigest         string
	ociMediaType      string
	ociSizeBytes      int64
	sourceRepository  string
	sourceCommit      string
	sourceRef         string
	policyRef         string
	builderID         string
	signerIdentity    string
	submittedBy       string
}

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	cfg := config{}
	fs := flag.NewFlagSet("distribution-rehearsal", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.StringVar(&cfg.packageName, "package", "", "Package name.")
	fs.StringVar(&cfg.packageVersion, "version", "", "Package version.")
	fs.StringVar(&cfg.channelName, "channel", "", "Distribution channel.")
	fs.StringVar(&cfg.platformOS, "platform-os", "", "Target operating system.")
	fs.StringVar(&cfg.platformArch, "platform-arch", "", "Target architecture.")
	fs.StringVar(&cfg.flavor, "flavor", "", "Artifact flavor.")
	fs.StringVar(&cfg.originRegistryURL, "origin-registry-url", "", "Origin OCI registry URL.")
	fs.StringVar(&cfg.publicRegistryURL, "public-registry-url", "", "Public OCI registry URL.")
	fs.StringVar(&cfg.ociRepository, "oci-repository", "", "OCI repository.")
	fs.StringVar(&cfg.ociDigest, "oci-digest", "", "OCI manifest digest.")
	fs.StringVar(&cfg.ociMediaType, "oci-media-type", "", "OCI manifest media type.")
	fs.Int64Var(&cfg.ociSizeBytes, "oci-size-bytes", 0, "OCI manifest size in bytes.")
	fs.StringVar(&cfg.sourceRepository, "source-repository", "", "Source repository.")
	fs.StringVar(&cfg.sourceCommit, "source-commit", "", "Source commit.")
	fs.StringVar(&cfg.sourceRef, "source-ref", "", "Source ref.")
	fs.StringVar(&cfg.policyRef, "policy-ref", "", "Policy reference.")
	fs.StringVar(&cfg.builderID, "builder-id", defaultBuilderID, "Trusted builder identity.")
	fs.StringVar(&cfg.signerIdentity, "signer-identity", defaultSignerIdentity, "Trusted signer identity.")
	fs.StringVar(&cfg.submittedBy, "submitted-by", "", "Submitting actor.")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := require(cfg); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	source, err := workloadauth.Source(ctx, "")
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	httpClient, err := workloadauth.MTLSClientForService(source, workloadauth.ServiceDistribution, nil)
	if err != nil {
		return err
	}
	client, err := distributioninternalclient.NewClient(workloadauth.InternalURL(workloadauth.ServiceDistribution), distributioninternalclient.WithHTTPClient(httpClient))
	if err != nil {
		return err
	}

	admit, err := client.AdmitArtifact(ctx, distributioninternalclient.AdmitArtifactRequest{
		PackageName:       cfg.packageName,
		PackageVersion:    cfg.packageVersion,
		ChannelName:       cfg.channelName,
		PlatformOS:        cfg.platformOS,
		PlatformArch:      cfg.platformArch,
		Flavor:            cfg.flavor,
		OriginRegistryURL: cfg.originRegistryURL,
		PublicRegistryURL: cfg.publicRegistryURL,
		OCIRepository:     cfg.ociRepository,
		OCIDigest:         cfg.ociDigest,
		OCIMediaType:      cfg.ociMediaType,
		OCISizeBytes:      cfg.ociSizeBytes,
		BuilderID:         cfg.builderID,
		SignerIdentity:    cfg.signerIdentity,
		SourceRepository:  cfg.sourceRepository,
		SourceCommit:      cfg.sourceCommit,
		SourceRef:         cfg.sourceRef,
		PolicyRef:         cfg.policyRef,
		Evidence:          evidence(cfg.ociDigest),
		SubmittedBy:       cfg.submittedBy,
	}, idempotencyKey("admit", cfg))
	if err != nil {
		return err
	}
	if admit.Problem != nil {
		return fmt.Errorf("admit artifact: %s", problemDetail(admit.Problem))
	}

	target, err := client.PromoteTarget(ctx, distributioninternalclient.PromoteTargetRequest{
		PackageName:    cfg.packageName,
		PackageVersion: cfg.packageVersion,
		ChannelName:    cfg.channelName,
		ArtifactDigest: cfg.ociDigest,
		PlatformOS:     cfg.platformOS,
		PlatformArch:   cfg.platformArch,
		Flavor:         cfg.flavor,
		PolicyRef:      cfg.policyRef,
		PromotedBy:     cfg.submittedBy,
		Reason:         "distribution rehearsal",
	}, idempotencyKey("promote", cfg))
	if err != nil {
		return err
	}
	if target.Problem != nil {
		return fmt.Errorf("promote target: %s", problemDetail(target.Problem))
	}

	return json.NewEncoder(os.Stdout).Encode(map[string]any{
		"artifact": admit.Result.Artifact,
		"target":   target.Result.Target,
	})
}

func require(cfg config) error {
	missing := []string{}
	check := map[string]string{
		"package":             cfg.packageName,
		"version":             cfg.packageVersion,
		"channel":             cfg.channelName,
		"platform-os":         cfg.platformOS,
		"platform-arch":       cfg.platformArch,
		"flavor":              cfg.flavor,
		"origin-registry-url": cfg.originRegistryURL,
		"public-registry-url": cfg.publicRegistryURL,
		"oci-repository":      cfg.ociRepository,
		"oci-digest":          cfg.ociDigest,
		"oci-media-type":      cfg.ociMediaType,
		"source-repository":   cfg.sourceRepository,
		"source-commit":       cfg.sourceCommit,
		"source-ref":          cfg.sourceRef,
		"policy-ref":          cfg.policyRef,
		"builder-id":          cfg.builderID,
		"signer-identity":     cfg.signerIdentity,
		"submitted-by":        cfg.submittedBy,
	}
	for key, value := range check {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	if cfg.ociSizeBytes <= 0 {
		missing = append(missing, "oci-size-bytes")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required flags: %s", strings.Join(missing, ", "))
	}
	return nil
}

func evidence(subject string) []distributioninternalclient.Evidence {
	return []distributioninternalclient.Evidence{
		{EvidenceKind: "cosign_signature", PredicateType: "application/vnd.dev.cosign.simplesigning.v1+json", SubjectDigest: subject, DocumentDigest: digestFor("cosign", subject), OCIReferrerDigest: digestFor("cosign-referrer", subject)},
		{EvidenceKind: "slsa_provenance", PredicateType: "https://slsa.dev/provenance/v1", SubjectDigest: subject, DocumentDigest: digestFor("slsa", subject), OCIReferrerDigest: digestFor("slsa-referrer", subject)},
		{EvidenceKind: "sbom", PredicateType: "https://spdx.dev/Document", SubjectDigest: subject, DocumentDigest: digestFor("sbom", subject), OCIReferrerDigest: digestFor("sbom-referrer", subject)},
		{EvidenceKind: "test", PredicateType: "https://verself.sh/predicate/test/v1", SubjectDigest: subject, DocumentDigest: digestFor("test", subject), OCIReferrerDigest: digestFor("test-referrer", subject)},
	}
}

func digestFor(kind string, subject string) string {
	sum := sha256.Sum256([]byte(kind + "\n" + subject))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func idempotencyKey(operation string, cfg config) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		operation,
		cfg.packageName,
		cfg.packageVersion,
		cfg.channelName,
		cfg.platformOS,
		cfg.platformArch,
		cfg.flavor,
		cfg.ociDigest,
	}, "\n")))
	return "distribution-rehearsal-" + operation + "-" + hex.EncodeToString(sum[:16])
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
