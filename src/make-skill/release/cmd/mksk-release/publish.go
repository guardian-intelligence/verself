package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/verself/make-skill/release/internal/ocipublish"
	releaseinput "github.com/verself/releaseattest"
	"oras.land/oras-go/v2"
)

const (
	mediaTypeLicenseEvidence = "application/vnd.verself.release.licenses.v1+json"

	annotationReleasePackage      = "sh.verself.release.package"
	annotationReleaseChannel      = "sh.verself.release.channel"
	annotationReleasePlatform     = "sh.verself.release.platform"
	annotationReleaseFlavor       = "sh.verself.release.flavor"
	annotationReleaseSourceRef    = "sh.verself.release.source-ref"
	annotationReleaseSourceCommit = "sh.verself.release.source-commit"
	annotationReleaseBuilderID    = "sh.verself.release.builder-id"
	annotationReleaseDigestSHA256 = "sh.verself.release.digest.sha256"
	annotationReleaseSizeBytes    = "sh.verself.release.size-bytes"

	signingModeDisabled       = "disabled"
	signingModeOpenBaoTransit = "openbao-transit"
)

var ociTagRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

type PublishRequest struct {
	Build                BuildRequest
	RegistryURL          string
	PublicRegistryURL    string
	Repository           string
	RegistryUsername     string
	RegistryPasswordFile string
	Ceremony             releaseCeremony
	Signer               releaseSigner
}

type publishFlagValues struct {
	registryURL                string
	publicRegistryURL          string
	repository                 string
	registryUsername           string
	registryPasswordFile       string
	signingMode                string
	openBaoAddr                string
	openBaoTokenFile           string
	openBaoTransitMount        string
	openBaoKey                 string
	openBaoNamespace           string
	distributionChallenge      string
	tpmReleasePublicName       string
	tpmReleasePublicBlobDigest string
}

type releaseCeremony struct {
	DistributionChallenge      string
	TPMReleasePublicName       string
	TPMReleasePublicBlobDigest string
}

func parsePublishRequest(ctx context.Context, args []string) (PublishRequest, error) {
	releaseValues := defaultReleaseFlagValues()
	publishValues := publishFlagValues{
		registryURL:          defaultRegistryURL,
		publicRegistryURL:    defaultPublicURL,
		repository:           defaultRepository,
		registryUsername:     defaultRegistryUser,
		registryPasswordFile: defaultRegistryPasswordFile,
		signingMode:          signingModeDisabled,
		openBaoTransitMount:  "transit",
	}
	fs := flagSet("mksk-release publish")
	bindReleaseFlags(fs, &releaseValues)
	bindPublishFlags(fs, &publishValues)
	if err := fs.Parse(args); err != nil {
		return PublishRequest{}, err
	}
	if fs.NArg() != 0 {
		return PublishRequest{}, fmt.Errorf("unexpected positional args: %s", strings.Join(fs.Args(), " "))
	}
	build, err := buildRequestFromReleaseFlags(ctx, releaseValues)
	if err != nil {
		return PublishRequest{}, err
	}
	signer, err := releaseSignerFromFlags(publishValues)
	if err != nil {
		return PublishRequest{}, err
	}
	req := PublishRequest{
		Build:                build,
		RegistryURL:          strings.TrimRight(strings.TrimSpace(publishValues.registryURL), "/"),
		PublicRegistryURL:    strings.TrimRight(strings.TrimSpace(publishValues.publicRegistryURL), "/"),
		Repository:           strings.Trim(strings.TrimSpace(publishValues.repository), "/"),
		RegistryUsername:     strings.TrimSpace(publishValues.registryUsername),
		RegistryPasswordFile: strings.TrimSpace(publishValues.registryPasswordFile),
		Ceremony: releaseCeremony{
			DistributionChallenge:      strings.TrimSpace(publishValues.distributionChallenge),
			TPMReleasePublicName:       strings.TrimSpace(publishValues.tpmReleasePublicName),
			TPMReleasePublicBlobDigest: strings.TrimSpace(publishValues.tpmReleasePublicBlobDigest),
		},
		Signer: signer,
	}
	if req.RegistryURL == "" {
		return PublishRequest{}, fmt.Errorf("--registry is required")
	}
	if req.PublicRegistryURL == "" {
		return PublishRequest{}, fmt.Errorf("--public-registry is required")
	}
	if req.Repository == "" {
		return PublishRequest{}, fmt.Errorf("--repository is required")
	}
	if req.Ceremony.enabled() {
		if err := validateReleaseCeremony(req.Ceremony); err != nil {
			return PublishRequest{}, err
		}
	}
	return req, nil
}

func flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	return fs
}

func bindPublishFlags(fs *flag.FlagSet, values *publishFlagValues) {
	fs.StringVar(&values.registryURL, "registry", defaultRegistryURL, "Origin OCI registry URL.")
	fs.StringVar(&values.publicRegistryURL, "public-registry", defaultPublicURL, "Public OCI registry URL recorded for operators.")
	fs.StringVar(&values.repository, "repository", defaultRepository, "OCI repository for make-skill artifacts.")
	fs.StringVar(&values.registryUsername, "registry-username", defaultRegistryUser, "OCI registry publisher username. Empty with --registry-password-file empty for anonymous registries.")
	fs.StringVar(&values.registryPasswordFile, "registry-password-file", defaultRegistryPasswordFile, "File containing the OCI registry publisher password.")
	fs.StringVar(&values.signingMode, "signing", signingModeDisabled, "Signing mode: disabled or openbao-transit.")
	fs.StringVar(&values.openBaoAddr, "openbao-addr", "", "OpenBao base URL for Transit signing.")
	fs.StringVar(&values.openBaoTokenFile, "openbao-token-file", "", "File containing the OpenBao token for Transit signing.")
	fs.StringVar(&values.openBaoTransitMount, "openbao-transit-mount", "transit", "OpenBao Transit mount path.")
	fs.StringVar(&values.openBaoKey, "openbao-key", "", "OpenBao Transit key name for release signatures.")
	fs.StringVar(&values.openBaoNamespace, "openbao-namespace", "", "OpenBao namespace header value. Empty means root namespace.")
	fs.StringVar(&values.distributionChallenge, "distribution-challenge", "", "One-use distribution-service challenge bound into the release input digest.")
	fs.StringVar(&values.tpmReleasePublicName, "tpm-release-public-name", "", "TPM Name for the ephemeral release signing key.")
	fs.StringVar(&values.tpmReleasePublicBlobDigest, "tpm-release-public-blob-digest", "", "sha256 digest of the TPM release public blob.")
}

func registryCredential(username string, passwordFile string) (ocipublish.RegistryAuth, error) {
	username = strings.TrimSpace(username)
	passwordFile = strings.TrimSpace(passwordFile)
	if username == "" && passwordFile == "" {
		return ocipublish.RegistryAuth{}, nil
	}
	if username == "" || passwordFile == "" {
		return ocipublish.RegistryAuth{}, fmt.Errorf("--registry-username and --registry-password-file must be provided together")
	}
	data, err := os.ReadFile(passwordFile)
	if err != nil {
		return ocipublish.RegistryAuth{}, fmt.Errorf("read registry password file %s: %w", passwordFile, err)
	}
	password := strings.TrimSpace(string(data))
	if password == "" {
		return ocipublish.RegistryAuth{}, fmt.Errorf("registry password file %s is empty", passwordFile)
	}
	return ocipublish.RegistryAuth{Username: username, Password: password}, nil
}

func newPublishTarget(req PublishRequest, credential ocipublish.RegistryAuth) (oras.GraphTarget, error) {
	return ocipublish.NewRemoteTarget(req.RegistryURL, req.Repository, credential)
}

func publishBundle(ctx context.Context, target oras.GraphTarget, req PublishRequest, bundle BuildBundle) (ocipublish.Result, error) {
	reference, err := releaseOCIReference(bundle.Subject)
	if err != nil {
		return ocipublish.Result{}, err
	}
	return ocipublish.Publish(ctx, target, ocipublish.Request{
		Reference: reference,
		Subject: ocipublish.Manifest{
			ArtifactType: ocipublish.ArtifactTypeRelease,
			Files: []ocipublish.File{{
				Path:        bundle.Evidence.Artifact.Path,
				MediaType:   bundle.Evidence.Artifact.MediaType,
				Annotations: evidenceAnnotations(bundle.Evidence.Artifact, "make-skill.tar", "artifact"),
			}},
			Annotations: releaseAnnotations(bundle.Subject, req.Build.BuilderID),
		},
		Referrers: evidenceReferrers(bundle),
	})
}

func evidenceReferrers(bundle BuildBundle) []ocipublish.Manifest {
	referrers := []ocipublish.Manifest{
		evidenceManifest(bundle.Subject, bundle.Evidence.ArtifactSBOM, ocipublish.MediaTypeSPDX, "make-skill.artifact.spdx.json", "artifact-sbom"),
		evidenceManifest(bundle.Subject, bundle.Evidence.SourceSBOM, ocipublish.MediaTypeSPDX, "make-skill.source.spdx.json", "source-sbom"),
		evidenceManifest(bundle.Subject, bundle.Evidence.LicenseEvidence, mediaTypeLicenseEvidence, "make-skill.cargo-about.json", "licenses"),
		evidenceManifest(bundle.Subject, bundle.Evidence.Provenance, ocipublish.MediaTypeInToto, "make-skill.provenance.intoto.json", "provenance"),
	}
	return referrers
}

func evidenceManifest(subject ReleaseSubject, evidence EvidenceFile, artifactType string, title string, kind string) ocipublish.Manifest {
	annotations := releaseAnnotations(subject, "")
	annotations[v1.AnnotationTitle] = title
	annotations[ocipublish.AnnotationEvidenceKind] = kind
	return ocipublish.Manifest{
		ArtifactType: artifactType,
		Files: []ocipublish.File{{
			Path:        evidence.Path,
			MediaType:   evidence.MediaType,
			Annotations: evidenceAnnotations(evidence, title, kind),
		}},
		Annotations: annotations,
	}
}

func evidenceAnnotations(evidence EvidenceFile, title string, kind string) map[string]string {
	return map[string]string{
		v1.AnnotationTitle:                title,
		ocipublish.AnnotationEvidenceKind: kind,
		annotationReleaseDigestSHA256:     evidence.Digest,
		annotationReleaseSizeBytes:        strconv.FormatInt(evidence.SizeBytes, 10),
	}
}

func releaseAnnotations(subject ReleaseSubject, builderID string) map[string]string {
	annotations := map[string]string{
		v1.AnnotationTitle:            subject.Package + " " + subject.Version,
		v1.AnnotationSource:           subject.SourceRepository,
		v1.AnnotationRevision:         subject.SourceCommit,
		v1.AnnotationVersion:          subject.Version,
		v1.AnnotationURL:              releaseMetadataURL(subject.Version),
		annotationReleasePackage:      subject.Package,
		annotationReleaseChannel:      string(subject.Channel),
		annotationReleasePlatform:     subject.Platform.String(),
		annotationReleaseFlavor:       subject.Flavor,
		annotationReleaseSourceRef:    subject.SourceRef,
		annotationReleaseSourceCommit: subject.SourceCommit,
	}
	if strings.TrimSpace(builderID) != "" {
		annotations[annotationReleaseBuilderID] = strings.TrimSpace(builderID)
	}
	return annotations
}

func releaseOCIReference(subject ReleaseSubject) (string, error) {
	tag := strings.Join([]string{
		releaseTagPrefix + subject.Version,
		subject.Platform.token(),
		subject.Flavor,
	}, "-")
	if !ociTagRE.MatchString(tag) {
		return "", fmt.Errorf("derived OCI tag %q is not a valid distribution tag", tag)
	}
	return tag, nil
}

func publicOCIReference(publicRegistryURL string, repository string, digest string) string {
	return registryReferenceHost(publicRegistryURL) + "/" + strings.Trim(repository, "/") + "@" + digest
}

func registryReferenceHost(registryURL string) string {
	registryURL = strings.TrimSpace(registryURL)
	if before, after, ok := strings.Cut(registryURL, "://"); ok && before != "" {
		registryURL = after
	}
	host, _, _ := strings.Cut(strings.Trim(registryURL, "/"), "/")
	return host
}

type releaseSigningEnvelope struct {
	Subject                    ReleaseSubject
	PublicOCIReference         string
	SubjectDigest              string
	SubjectMediaType           string
	SubjectSizeBytes           int64
	Referrers                  []releaseSigningReferrer
	DistributionChallenge      string
	ReleaseInputDigest         string
	ArtifactDigest             string
	ProvenanceDigest           string
	SBOMDigest                 string
	TPMReleasePublicName       string
	TPMReleasePublicBlobDigest string
}

type releaseSigningReferrer struct {
	ArtifactType string
	Digest       string
	MediaType    string
	SizeBytes    int64
}

type releaseSignature struct {
	Mode          string
	PayloadDigest string
}

type releaseSigner interface {
	Preflight(context.Context) error
	SignRelease(context.Context, releaseSigningEnvelope) (releaseSignature, error)
}

type disabledSigner struct{}

func (disabledSigner) Preflight(context.Context) error {
	return nil
}

func (disabledSigner) SignRelease(_ context.Context, envelope releaseSigningEnvelope) (releaseSignature, error) {
	return releaseSignature{
		Mode:          signingModeDisabled,
		PayloadDigest: envelope.PayloadDigest(),
	}, nil
}

type openBaoTransitConfig struct {
	Address      string
	TokenFile    string
	TransitMount string
	KeyName      string
	Namespace    string
}

type openBaoTransitSigner struct {
	config openBaoTransitConfig
}

func (s openBaoTransitSigner) Preflight(context.Context) error {
	return fmt.Errorf("OpenBao Transit signing is not implemented yet; release signing will call %s/v1/%s/sign/%s after OCI referrer verification", strings.TrimRight(s.config.Address, "/"), strings.Trim(s.config.TransitMount, "/"), s.config.KeyName)
}

func (s openBaoTransitSigner) SignRelease(_ context.Context, envelope releaseSigningEnvelope) (releaseSignature, error) {
	return releaseSignature{}, fmt.Errorf("OpenBao Transit signing is not implemented yet for payload sha256:%s", envelope.PayloadDigest())
}

func releaseSignerFromFlags(values publishFlagValues) (releaseSigner, error) {
	mode := strings.TrimSpace(values.signingMode)
	switch mode {
	case "", signingModeDisabled:
		return disabledSigner{}, nil
	case "openbao", signingModeOpenBaoTransit:
		config := openBaoTransitConfig{
			Address:      strings.TrimRight(strings.TrimSpace(values.openBaoAddr), "/"),
			TokenFile:    strings.TrimSpace(values.openBaoTokenFile),
			TransitMount: strings.Trim(strings.TrimSpace(values.openBaoTransitMount), "/"),
			KeyName:      strings.TrimSpace(values.openBaoKey),
			Namespace:    strings.TrimSpace(values.openBaoNamespace),
		}
		if config.Address == "" {
			return nil, fmt.Errorf("--openbao-addr is required with --signing=%s", mode)
		}
		if config.TokenFile == "" {
			return nil, fmt.Errorf("--openbao-token-file is required with --signing=%s", mode)
		}
		if config.TransitMount == "" {
			return nil, fmt.Errorf("--openbao-transit-mount is required with --signing=%s", mode)
		}
		if config.KeyName == "" {
			return nil, fmt.Errorf("--openbao-key is required with --signing=%s", mode)
		}
		if err := requireReleaseCeremony(values); err != nil {
			return nil, err
		}
		if _, err := os.Stat(config.TokenFile); err != nil {
			return nil, fmt.Errorf("read OpenBao token file %s: %w", config.TokenFile, err)
		}
		return openBaoTransitSigner{config: config}, nil
	default:
		return nil, fmt.Errorf("--signing must be disabled or openbao-transit")
	}
}

func requireReleaseCeremony(values publishFlagValues) error {
	return validateReleaseCeremony(releaseCeremony{
		DistributionChallenge:      values.distributionChallenge,
		TPMReleasePublicName:       values.tpmReleasePublicName,
		TPMReleasePublicBlobDigest: values.tpmReleasePublicBlobDigest,
	})
}

func validateReleaseCeremony(ceremony releaseCeremony) error {
	missing := []string{}
	if strings.TrimSpace(ceremony.DistributionChallenge) == "" {
		missing = append(missing, "--distribution-challenge")
	}
	if strings.TrimSpace(ceremony.TPMReleasePublicName) == "" {
		missing = append(missing, "--tpm-release-public-name")
	}
	if strings.TrimSpace(ceremony.TPMReleasePublicBlobDigest) == "" {
		missing = append(missing, "--tpm-release-public-blob-digest")
	}
	if len(missing) > 0 {
		return fmt.Errorf("release ceremony requires %s", strings.Join(missing, ", "))
	}
	if _, err := releaseinput.DigestBytes(ceremony.TPMReleasePublicBlobDigest); err != nil {
		return fmt.Errorf("--tpm-release-public-blob-digest: %w", err)
	}
	return nil
}

func newReleaseSigningEnvelope(bundle BuildBundle, publicRef string, result ocipublish.Result, ceremony releaseCeremony) (releaseSigningEnvelope, error) {
	referrers := make([]releaseSigningReferrer, 0, len(result.Referrers))
	for _, referrer := range result.Referrers {
		referrers = append(referrers, releaseSigningReferrer{
			ArtifactType: referrer.ArtifactType,
			Digest:       referrer.Descriptor.Digest.String(),
			MediaType:    referrer.Descriptor.MediaType,
			SizeBytes:    referrer.Descriptor.Size,
		})
	}
	sort.Slice(referrers, func(i, j int) bool {
		return referrers[i].Digest < referrers[j].Digest
	})
	envelope := releaseSigningEnvelope{
		Subject:            bundle.Subject,
		PublicOCIReference: publicRef,
		SubjectDigest:      result.Subject.Descriptor.Digest.String(),
		SubjectMediaType:   result.Subject.Descriptor.MediaType,
		SubjectSizeBytes:   result.Subject.Descriptor.Size,
		Referrers:          referrers,
	}
	if ceremony.enabled() {
		input := releaseinput.ReleaseInput{
			DistributionChallenge:      ceremony.DistributionChallenge,
			Package:                    bundle.Subject.Package,
			Version:                    bundle.Subject.Version,
			SourceCommit:               bundle.Subject.SourceCommit,
			Platform:                   bundle.Subject.Platform.String(),
			Flavor:                     bundle.Subject.Flavor,
			OCIManifestDigest:          result.Subject.Descriptor.Digest.String(),
			ArtifactDigest:             sha256Digest(bundle.Evidence.Artifact.Digest),
			ProvenanceDigest:           sha256Digest(bundle.Evidence.Provenance.Digest),
			SBOMDigest:                 sha256Digest(bundle.Evidence.ArtifactSBOM.Digest),
			TPMReleasePublicName:       ceremony.TPMReleasePublicName,
			TPMReleasePublicBlobDigest: ceremony.TPMReleasePublicBlobDigest,
		}
		digest, err := input.Digest()
		if err != nil {
			return releaseSigningEnvelope{}, err
		}
		envelope.DistributionChallenge = ceremony.DistributionChallenge
		envelope.ReleaseInputDigest = digest
		envelope.ArtifactDigest = input.ArtifactDigest
		envelope.ProvenanceDigest = input.ProvenanceDigest
		envelope.SBOMDigest = input.SBOMDigest
		envelope.TPMReleasePublicName = ceremony.TPMReleasePublicName
		envelope.TPMReleasePublicBlobDigest = ceremony.TPMReleasePublicBlobDigest
	}
	return envelope, nil
}

func (e releaseSigningEnvelope) PayloadDigest() string {
	sum := sha256.Sum256(e.CanonicalPayload())
	return hex.EncodeToString(sum[:])
}

func (e releaseSigningEnvelope) CanonicalPayload() []byte {
	lines := []string{
		"verself-release-signing-v1",
		"package=" + e.Subject.Package,
		"version=" + e.Subject.Version,
		"channel=" + string(e.Subject.Channel),
		"platform=" + e.Subject.Platform.String(),
		"flavor=" + e.Subject.Flavor,
		"source_repository=" + e.Subject.SourceRepository,
		"source_ref=" + e.Subject.SourceRef,
		"source_commit=" + e.Subject.SourceCommit,
		"public_oci_reference=" + e.PublicOCIReference,
		"subject_digest=" + e.SubjectDigest,
		"subject_media_type=" + e.SubjectMediaType,
		"subject_size_bytes=" + strconv.FormatInt(e.SubjectSizeBytes, 10),
	}
	if e.ReleaseInputDigest != "" {
		lines = append(lines,
			"distribution_challenge="+e.DistributionChallenge,
			"release_input_digest="+e.ReleaseInputDigest,
			"artifact_digest="+e.ArtifactDigest,
			"provenance_digest="+e.ProvenanceDigest,
			"sbom_digest="+e.SBOMDigest,
			"tpm_release_public_name="+e.TPMReleasePublicName,
			"tpm_release_public_blob_digest="+e.TPMReleasePublicBlobDigest,
		)
	}
	for _, referrer := range e.Referrers {
		lines = append(lines, strings.Join([]string{
			"referrer",
			referrer.ArtifactType,
			referrer.Digest,
			referrer.MediaType,
			strconv.FormatInt(referrer.SizeBytes, 10),
		}, "="))
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func (c releaseCeremony) enabled() bool {
	return c.DistributionChallenge != "" || c.TPMReleasePublicName != "" || c.TPMReleasePublicBlobDigest != ""
}

func sha256Digest(hexDigest string) string {
	return "sha256:" + strings.TrimSpace(hexDigest)
}
