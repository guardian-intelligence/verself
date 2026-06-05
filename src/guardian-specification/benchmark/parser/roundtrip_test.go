package parser

import (
	"bytes"
	"fmt"
	"slices"
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
	for seed := 0; seed < size; seed++ {
		corpus = append(corpus, gen.Example(seed))
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
		doc := specdoc.Document{
			Kind: "FlyProcedure",
			Name: optionalToken(t, "name"),
			StaticConfig: specdoc.StaticConfig{
				BaseURL:        "https://" + token(t, "site") + ".guardianintelligence.test",
				CredentialsRef: token(t, "credentials"),
			},
			Board: specdoc.Board{
				Substrate: specdoc.Substrate{
					StateDir: "/var/lib/" + token(t, "state"),
				},
				Access: specdoc.Access{
					SSH: specdoc.SSHAccess{
						Host:           fmt.Sprintf("192.0.2.%d", rapid.IntRange(1, 254).Draw(t, "ssh_host_octet")),
						Port:           rapid.IntRange(1, 65535).Draw(t, "ssh_port"),
						User:           token(t, "user"),
						KnownHostsFile: "~/.ssh/" + token(t, "known_hosts"),
						IdentityFile:   optionalPath(t, "~/.ssh/", "identity"),
						ConnectTimeout: optionalDuration(t),
					},
				},
				Seed: specdoc.Seed{
					TargetRoot: "/var/lib/guardian/seeds/" + token(t, "seed_root"),
					Paths:      seedPaths(t),
				},
			},
			Nomad: specdoc.Nomad{
				Address:   fmt.Sprintf("http://127.0.0.1:%d", rapid.IntRange(30000, 50000).Draw(t, "nomad_port")),
				Namespace: token(t, "namespace"),
				Jobs:      nomadJobs(t),
			},
		}
		if rapid.Bool().Draw(t, "wireguard_fallback") {
			doc.Board.Access.SSH.WireGuardFallback = &specdoc.WireGuardFallback{
				Host: fmt.Sprintf(
					"10.%d.%d.%d",
					rapid.IntRange(0, 255).Draw(t, "wg_host_a"),
					rapid.IntRange(0, 255).Draw(t, "wg_host_b"),
					rapid.IntRange(1, 254).Draw(t, "wg_host_c"),
				),
				Port:      rapid.IntRange(1, 65535).Draw(t, "wg_port"),
				Interface: optionalToken(t, "wg"),
			}
		}
		return doc
	})
}

func seedPaths(t *rapid.T) []specdoc.SeedPath {
	count := rapid.IntRange(1, 5).Draw(t, "seed_path_count")
	paths := make([]specdoc.SeedPath, 0, count)
	for i := 0; i < count; i++ {
		name := token(t, fmt.Sprintf("seed%d", i))
		paths = append(paths, specdoc.SeedPath{
			Source: "bazel-bin/" + name + "/artifact",
			Target: "files/" + name,
			Mode:   rapid.SampledFrom([]string{"0644", "0755", "0600", "0700"}).Draw(t, "seed_mode"),
		})
	}
	return paths
}

func nomadJobs(t *rapid.T) []specdoc.NomadJob {
	count := rapid.IntRange(1, 5).Draw(t, "nomad_job_count")
	jobs := make([]specdoc.NomadJob, 0, count)
	for i := 0; i < count; i++ {
		name := token(t, fmt.Sprintf("job%d", i))
		jobs = append(jobs, specdoc.NomadJob{
			Path:        "jobs/" + name + ".nomad.hcl",
			RequiredFor: requiredFor(t),
		})
	}
	return jobs
}

func requiredFor(t *rapid.T) []string {
	values := rapid.Permutation([]string{"recovery", "deploy", "observe"}).Draw(t, "required_for_permutation")
	count := rapid.IntRange(1, len(values)).Draw(t, "required_for_count")
	return slices.Clone(values[:count])
}

func optionalToken(t *rapid.T, label string) string {
	if rapid.Bool().Draw(t, label+"_present") {
		return token(t, label)
	}
	return ""
}

func optionalPath(t *rapid.T, dir string, label string) string {
	if rapid.Bool().Draw(t, label+"_present") {
		return dir + token(t, label)
	}
	return ""
}

func optionalDuration(t *rapid.T) string {
	if rapid.Bool().Draw(t, "connect_timeout_present") {
		return fmt.Sprintf("%ds", rapid.IntRange(1, 30).Draw(t, "connect_timeout_seconds"))
	}
	return ""
}

func token(t *rapid.T, label string) string {
	return rapid.StringMatching(`[a-z][a-z0-9-]{0,20}`).Draw(t, label)
}
