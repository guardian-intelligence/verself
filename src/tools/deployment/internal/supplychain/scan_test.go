package supplychain

import "testing"

func TestScanMavenLockFindsJarArtifacts(t *testing.T) {
	raw := []byte(`{
  "artifacts": {
    "com.google.googlejavaformat:google-java-format": {
      "version": "1.35.0",
      "shasums": {
        "jar": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
      }
    },
    "software.amazon.smithy:smithy-cli": {
      "version": "1.70.0",
      "shasums": {
        "pom": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
      }
    }
  }
}`)

	findings := scanMavenLock("maven_install.json", raw)
	if len(findings) != 1 {
		t.Fatalf("scanMavenLock() returned %d findings, want 1", len(findings))
	}

	got := findings[0]
	if got.SourceKind != "maven_artifact" {
		t.Fatalf("SourceKind = %q, want maven_artifact", got.SourceKind)
	}
	if got.Surface != "build-time" {
		t.Fatalf("Surface = %q, want build-time", got.Surface)
	}
	if got.Artifact != "com.google.googlejavaformat:google-java-format:1.35.0" {
		t.Fatalf("Artifact = %q", got.Artifact)
	}
	wantURL := "https://repo1.maven.org/maven2/com/google/googlejavaformat/google-java-format/1.35.0/google-java-format-1.35.0.jar"
	if got.UpstreamURL != wantURL {
		t.Fatalf("UpstreamURL = %q, want %q", got.UpstreamURL, wantURL)
	}
	wantDigest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if got.Digest != wantDigest {
		t.Fatalf("Digest = %q, want %q", got.Digest, wantDigest)
	}
	if got.Line == 0 {
		t.Fatalf("Line = 0, want source line")
	}
}

func TestScanMavenLockIgnoresInvalidLockfiles(t *testing.T) {
	if findings := scanMavenLock("maven_install.json", []byte(`not-json`)); len(findings) != 0 {
		t.Fatalf("scanMavenLock(invalid) returned %d findings, want 0", len(findings))
	}
}
