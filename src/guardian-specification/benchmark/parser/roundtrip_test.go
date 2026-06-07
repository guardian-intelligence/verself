package parser

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/verself/guardian-specification/internal/formatio"
	"github.com/verself/guardian-specification/internal/specdoc"
	"pgregory.net/rapid"
)

var structuredFormats = []string{"json", "yaml", "toml", "toon"}

func TestStructuredCodecNoInformationLoss(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		original := guardianDocument().Draw(t, "document")
		if err := assertNoInformationLoss(original); err != nil {
			t.Fatal(err)
		}
	})
}

func BenchmarkStructuredCodecNoInformationLoss(b *testing.B) {
	corpus := benchmarkCorpus(128)
	bytesPerDocument := averageRoundTripBytes(b, corpus)
	b.SetBytes(bytesPerDocument)
	b.ReportAllocs()

	b.ResetTimer()
	start := time.Now()
	for i := 0; i < b.N; i++ {
		if err := assertNoInformationLoss(corpus[i%len(corpus)]); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()

	elapsed := time.Since(start).Seconds()
	b.ReportMetric(float64(b.N)/elapsed, "docs/s")
	b.ReportMetric(float64(b.N*len(structuredFormats)*len(structuredFormats))/elapsed, "format_pairs/s")
}

func assertNoInformationLoss(original specdoc.Document) error {
	canonicalOriginal, err := specdoc.CanonicalJSON(original)
	if err != nil {
		return err
	}
	for _, inputFormat := range structuredFormats {
		input, err := encode(inputFormat, original)
		if err != nil {
			return fmt.Errorf("encode %s input: %w", inputFormat, err)
		}
		parsed, err := decode(inputFormat, input)
		if err != nil {
			return fmt.Errorf("decode %s input: %w", inputFormat, err)
		}
		if err := assertCanonicalEqual(canonicalOriginal, parsed, inputFormat); err != nil {
			return err
		}
		for _, outputFormat := range structuredFormats {
			output, err := encode(outputFormat, parsed)
			if err != nil {
				return fmt.Errorf("encode %s output from %s: %w", outputFormat, inputFormat, err)
			}
			reparsed, err := decode(outputFormat, output)
			if err != nil {
				return fmt.Errorf("decode %s output from %s: %w", outputFormat, inputFormat, err)
			}
			if err := assertCanonicalEqual(canonicalOriginal, reparsed, inputFormat+"->"+outputFormat); err != nil {
				return err
			}
		}
	}
	return nil
}

func assertCanonicalEqual(expected []byte, actual specdoc.Document, label string) error {
	canonicalActual, err := specdoc.CanonicalJSON(actual)
	if err != nil {
		return fmt.Errorf("canonicalize %s: %w", label, err)
	}
	if !bytes.Equal(canonicalActual, expected) {
		return fmt.Errorf("%s changed document\n got: %s\nwant: %s", label, canonicalActual, expected)
	}
	return nil
}

func encode(format string, doc specdoc.Document) ([]byte, error) {
	var out bytes.Buffer
	if err := formatio.Write(&out, format, doc); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decode(format string, data []byte) (specdoc.Document, error) {
	var doc specdoc.Document
	if err := formatio.Decode(data, format, &doc); err != nil {
		return specdoc.Document{}, err
	}
	if err := specdoc.Validate(doc); err != nil {
		return specdoc.Document{}, err
	}
	return doc, nil
}

func benchmarkCorpus(size int) []specdoc.Document {
	corpus := make([]specdoc.Document, 0, size)
	gen := guardianDocument()
	for example := 0; example < size; example++ {
		corpus = append(corpus, gen.Example(example))
	}
	return corpus
}

func averageRoundTripBytes(tb testing.TB, corpus []specdoc.Document) int64 {
	tb.Helper()
	var total int64
	for _, doc := range corpus {
		n, err := roundTripBytes(doc)
		if err != nil {
			tb.Fatal(err)
		}
		total += n
	}
	return total / int64(len(corpus))
}

func roundTripBytes(doc specdoc.Document) (int64, error) {
	var total int64
	for _, inputFormat := range structuredFormats {
		input, err := encode(inputFormat, doc)
		if err != nil {
			return 0, err
		}
		total += int64(len(input))
		var parsed specdoc.Document
		if err := formatio.Decode(input, inputFormat, &parsed); err != nil {
			return 0, err
		}
		for _, outputFormat := range structuredFormats {
			output, err := encode(outputFormat, parsed)
			if err != nil {
				return 0, err
			}
			total += int64(len(output))
		}
	}
	return total, nil
}

func guardianDocument() *rapid.Generator[specdoc.Document] {
	return rapid.Custom(func(t *rapid.T) specdoc.Document {
		name := token(t, "name")
		substrateName := token(t, "substrate")
		originName := dnsLabel(t, "origin")
		doc := specdoc.Document{
			Entrypoint: specdoc.ObjectRef{
				APIVersion: specdoc.APIGuardian,
				Kind:       specdoc.KindFlyProcedure,
				Name:       name,
			},
			Resources: []specdoc.Resource{
				{
					APIVersion: specdoc.APIGuardian,
					Kind:       specdoc.KindFlyProcedure,
					Metadata:   specdoc.ObjectMeta{Name: name},
					Spec: specdoc.MustResourceSpec(specdoc.FlyProcedureSpec{
						SubstrateRef: specdoc.ObjectRef{
							APIVersion: specdoc.APISubstrate,
							Kind:       specdoc.KindSubstrate,
							Name:       substrateName,
						},
						Preflight: specdoc.Preflight{
							Ansible: specdoc.AnsiblePreflight{
								Playbook: "src/guardian-specification/ansible/" + token(t, "playbook") + ".yml",
							},
						},
						Nomad: specdoc.NomadFly{
							Address:   "http://127.0.0.1:4646",
							Namespace: token(t, "nomad_namespace"),
							Jobs: []specdoc.NomadJob{{
								Name: token(t, "nomad_job"),
								Path: "src/" + token(t, "nomad_job_dir") + "/nomad.hcl",
							}},
						},
					}),
				},
				{
					APIVersion: specdoc.APISubstrate,
					Kind:       specdoc.KindSubstrate,
					Metadata:   specdoc.ObjectMeta{Name: substrateName},
					Spec:       specdoc.MustResourceSpec(specdoc.SubstrateSpec{}),
				},
				{
					APIVersion: specdoc.APINetworking,
					Kind:       specdoc.KindPublicOrigin,
					Metadata:   specdoc.ObjectMeta{Name: originName},
					Spec: specdoc.MustResourceSpec(specdoc.PublicOriginSpec{
						URL: "https://" + originName + ".guardianintelligence.org",
					}),
				},
				{
					APIVersion: "cloudflare.guardianintelligence.org/v1alpha1",
					Kind:       "AccountAuthority",
					Metadata:   specdoc.ObjectMeta{Name: token(t, "cloudflare_authority")},
					Spec:       specdoc.ResourceSpec{},
				},
			},
		}
		return doc
	})
}

func token(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, label)
}

func dnsLabel(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-z][a-z0-9]{0,20}`).Draw(t, label)
}
