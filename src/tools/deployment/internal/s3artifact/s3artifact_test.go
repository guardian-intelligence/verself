package s3artifact

import "testing"

func TestParseGetterPrefix(t *testing.T) {
	endpoint, bucket, err := parseGetterPrefix("s3::https://abc.r2.cloudflarestorage.com/nomad-artifacts-gamma")
	if err != nil {
		t.Fatalf("parseGetterPrefix: %v", err)
	}
	if endpoint.String() != "https://abc.r2.cloudflarestorage.com" {
		t.Fatalf("endpoint = %s", endpoint)
	}
	if bucket != "nomad-artifacts-gamma" {
		t.Fatalf("bucket = %s", bucket)
	}
}

func TestParseEnvFile(t *testing.T) {
	access, secret, session, err := ParseEnvFile([]byte("AWS_ACCESS_KEY_ID=ak\nAWS_SECRET_ACCESS_KEY='sk'\nAWS_SESSION_TOKEN=session\n"), "AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_SESSION_TOKEN")
	if err != nil {
		t.Fatalf("ParseEnvFile: %v", err)
	}
	if access != "ak" || secret != "sk" || session != "session" {
		t.Fatalf("got %q %q %q", access, secret, session)
	}
}
