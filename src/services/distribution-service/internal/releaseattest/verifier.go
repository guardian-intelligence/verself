package releaseattest

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/google/go-attestation/attest"
	releaseinput "github.com/verself/releaseattest"
)

type Verifier struct {
	TrustedAKs  map[string]TrustedAK
	PCRPolicies map[string]PCRPolicy
	OpenBao     OpenBaoAuthorizer
}

func (v Verifier) Verify(ctx context.Context, req Request) (Result, error) {
	expectedDigest, err := req.Input.Digest()
	if err != nil {
		return Result{}, err
	}
	if expectedDigest != strings.TrimSpace(req.ReleaseInputDigest) {
		return Result{}, fmt.Errorf("release input digest mismatch: computed %s but request carried %s", expectedDigest, req.ReleaseInputDigest)
	}
	digestBytes, err := releaseinput.DigestBytes(expectedDigest)
	if err != nil {
		return Result{}, err
	}
	akDigest := digestBytesFor(req.TPM.AKPublic)
	trustedAK, ok := v.TrustedAKs[akDigest]
	if !ok {
		return Result{}, fmt.Errorf("untrusted TPM attestation key %s", akDigest)
	}
	if trustedAK.BuilderID != "" && trustedAK.BuilderID != req.BuilderID {
		return Result{}, fmt.Errorf("TPM attestation key %s is not enrolled for builder %s", akDigest, req.BuilderID)
	}
	policy, ok := v.PCRPolicies[req.PolicyID]
	if !ok {
		return Result{}, fmt.Errorf("unknown TPM PCR policy %q", req.PolicyID)
	}
	if policy.BuilderID != "" && policy.BuilderID != req.BuilderID {
		return Result{}, fmt.Errorf("TPM PCR policy %q is not enrolled for builder %s", req.PolicyID, req.BuilderID)
	}
	if req.Input.TPMReleasePublicName != strings.TrimSpace(req.TPM.ReleasePublicName) {
		return Result{}, fmt.Errorf("TPM release public name does not match release input")
	}
	if req.Input.TPMReleasePublicBlobDigest != strings.TrimSpace(req.TPM.ReleasePublicBlobDigest) {
		return Result{}, fmt.Errorf("TPM release public blob digest does not match release input")
	}
	if err := verifyTPM(req.TPM, policy, digestBytes); err != nil {
		return Result{}, err
	}
	result := Result{
		ReleaseInputDigest:     expectedDigest,
		AKPublicDigest:         akDigest,
		PCRPolicyID:            policy.ID,
		TPMReleasePublicName:   req.TPM.ReleasePublicName,
		TPMReleasePublicDigest: req.TPM.ReleasePublicBlobDigest,
		VerifiedPCRCount:       len(req.TPM.PCRs),
		VerifiedQuoteCount:     len(req.TPM.Quotes),
		EventLogReplayed:       len(req.TPM.EventLog) > 0,
		CheckedAt:              req.CheckedAt,
	}
	if v.OpenBao != nil {
		authorization, err := v.OpenBao.AuthorizeRelease(ctx, result)
		if err != nil {
			return Result{}, err
		}
		result.OpenBaoAuthorizationHint = authorization
	}
	return result, nil
}

func verifyTPM(evidence TPMEvidence, policy PCRPolicy, releaseDigest []byte) error {
	if len(evidence.AKPublic) == 0 {
		return fmt.Errorf("TPM AK public blob is required")
	}
	if len(evidence.Quotes) == 0 {
		return fmt.Errorf("TPM quote is required")
	}
	if len(evidence.PCRs) == 0 {
		return fmt.Errorf("TPM PCR values are required")
	}
	if strings.TrimSpace(evidence.ReleasePublicName) == "" {
		return fmt.Errorf("TPM release public name is required")
	}
	if len(evidence.ReleasePublicBlob) == 0 {
		return fmt.Errorf("TPM release public blob is required")
	}
	if digestBytesFor(evidence.ReleasePublicBlob) != strings.TrimSpace(evidence.ReleasePublicBlobDigest) {
		return fmt.Errorf("TPM release public blob digest mismatch")
	}
	if err := verifyPCRPolicy(policy, evidence.PCRs); err != nil {
		return err
	}
	ak, err := attest.ParseAKPublic(evidence.AKPublic)
	if err != nil {
		return fmt.Errorf("parse TPM AK public blob: %w", err)
	}
	quotes := make([]attest.Quote, 0, len(evidence.Quotes))
	for _, quote := range evidence.Quotes {
		if len(quote.Quote) == 0 || len(quote.Signature) == 0 {
			return fmt.Errorf("TPM quote and signature are required")
		}
		quotes = append(quotes, attest.Quote{Quote: quote.Quote, Signature: quote.Signature})
	}
	pcrs, err := attestPCRs(evidence.PCRs)
	if err != nil {
		return err
	}
	if err := ak.VerifyAll(quotes, pcrs, releaseDigest); err != nil {
		return fmt.Errorf("verify TPM quote: %w", err)
	}
	for i := range pcrs {
		if !pcrs[i].QuoteVerified() {
			return fmt.Errorf("TPM PCR %d was not quote verified", pcrs[i].Index)
		}
	}
	if len(evidence.EventLog) > 0 {
		eventLog, err := attest.ParseEventLog(evidence.EventLog)
		if err != nil {
			return fmt.Errorf("parse TPM event log: %w", err)
		}
		if _, err := eventLog.Verify(pcrs); err != nil {
			return fmt.Errorf("replay TPM event log: %w", err)
		}
	}
	if evidence.ReleaseKeyCertification.empty() {
		return fmt.Errorf("TPM release key certification is required")
	}
	params := attest.CertificationParameters{
		Public:            evidence.ReleaseKeyCertification.Public,
		CreateData:        evidence.ReleaseKeyCertification.CreateData,
		CreateAttestation: evidence.ReleaseKeyCertification.CreateAttestation,
		CreateSignature:   evidence.ReleaseKeyCertification.CreateSignature,
	}
	if err := params.Verify(attest.VerifyOpts{Public: ak.Public, Hash: ak.Hash}); err != nil {
		return fmt.Errorf("verify TPM release key certification: %w", err)
	}
	return nil
}

func verifyPCRPolicy(policy PCRPolicy, actual []PCR) error {
	if strings.TrimSpace(policy.ID) == "" {
		return fmt.Errorf("TPM PCR policy id is required")
	}
	expected := map[string]string{}
	for _, pcr := range policy.PCRs {
		key := pcrKey(pcr.Index, pcr.DigestAlg)
		expected[key] = strings.TrimSpace(pcr.Digest)
	}
	if len(expected) == 0 {
		return fmt.Errorf("TPM PCR policy %q has no expected PCRs", policy.ID)
	}
	for _, pcr := range actual {
		key := pcrKey(pcr.Index, pcr.DigestAlg)
		want, ok := expected[key]
		if !ok {
			continue
		}
		if strings.TrimSpace(pcr.Digest) != want {
			return fmt.Errorf("TPM PCR %s digest mismatch", key)
		}
		delete(expected, key)
	}
	if len(expected) > 0 {
		missing := make([]string, 0, len(expected))
		for key := range expected {
			missing = append(missing, key)
		}
		sort.Strings(missing)
		return fmt.Errorf("TPM PCR policy %q missing PCRs: %s", policy.ID, strings.Join(missing, ", "))
	}
	return nil
}

func attestPCRs(pcrs []PCR) ([]attest.PCR, error) {
	out := make([]attest.PCR, 0, len(pcrs))
	for _, pcr := range pcrs {
		hashAlg, err := cryptoHash(pcr.DigestAlg)
		if err != nil {
			return nil, err
		}
		digest, err := hex.DecodeString(strings.TrimPrefix(strings.TrimSpace(pcr.Digest), "sha256:"))
		if err != nil {
			return nil, fmt.Errorf("decode TPM PCR %d digest: %w", pcr.Index, err)
		}
		out = append(out, attest.PCR{Index: pcr.Index, Digest: digest, DigestAlg: hashAlg})
	}
	return out, nil
}

func cryptoHash(name string) (crypto.Hash, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sha256", "sha256:":
		return crypto.SHA256, nil
	default:
		return 0, fmt.Errorf("unsupported TPM PCR digest algorithm %q", name)
	}
}

func digestBytesFor(payload []byte) string {
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func pcrKey(index int, alg string) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(alg)), index)
}

func (c ReleaseKeyCertification) empty() bool {
	return len(c.Public) == 0 && len(c.CreateData) == 0 && len(c.CreateAttestation) == 0 && len(c.CreateSignature) == 0
}
