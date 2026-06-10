package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	"github.com/verself/service-runtime/pgtest"
)

// Migration 003 cuts over pre-existing deployment-channel rows whose
// signer_identity was the self-asserted builder id. The 001 inline CHECK
// (length(btrim(signer_identity)) > 0) fires on every row write, so 003 must
// DROP it before the cutover UPDATE or it aborts on any database that has
// processed a deployment admission, leaving schema_migrations dirty.
func TestSignedDeploymentEvidenceCutoverMigratesSeededDeploymentRows(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	t.Setenv("VERSELF_PGTEST_ROOT", filepath.Join(os.TempDir(), "verself-pgtest-distribution-service"))
	db := pgtest.Acquire(ctx, t, pgtest.Template{
		Service:     "distribution-service-cutover",
		Fingerprint: "pre-003-" + Fingerprint(),
		Migrate: func(ctx context.Context, dsn string) error {
			return migrateTo(ctx, dsn, 2)
		},
	})
	conn, err := sql.Open("postgres", db.DSN)
	if err != nil {
		t.Fatalf("open migration test database: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	seedArtifact(ctx, t, conn, "deployment", "verself://site-bootstrap/dev", "11")
	seedArtifact(ctx, t, conn, "nightly", "https://github.com/guardian-intelligence/verself/.github/workflows/release.yml@refs/heads/main", "22")

	if err := UpDSN(ctx, "distribution-service", db.DSN); err != nil {
		t.Fatalf("migrate up over seeded deployment rows: %v", err)
	}

	var deploymentSigner, releaseSigner string
	if err := conn.QueryRowContext(ctx, `SELECT signer_identity FROM distribution_artifacts WHERE channel_name = 'deployment'`).Scan(&deploymentSigner); err != nil {
		t.Fatalf("read deployment signer_identity: %v", err)
	}
	if deploymentSigner != "" {
		t.Fatalf("deployment signer_identity = %q, want empty after cutover", deploymentSigner)
	}
	if err := conn.QueryRowContext(ctx, `SELECT signer_identity FROM distribution_artifacts WHERE channel_name = 'nightly'`).Scan(&releaseSigner); err != nil {
		t.Fatalf("read nightly signer_identity: %v", err)
	}
	if releaseSigner == "" {
		t.Fatal("nightly signer_identity must be preserved by cutover")
	}

	// The replacement constraint must enforce the inverted contract.
	if _, err := conn.ExecContext(ctx, `UPDATE distribution_artifacts SET signer_identity = 'self-asserted' WHERE channel_name = 'deployment'`); err == nil {
		t.Fatal("non-empty deployment signer_identity must violate the channel-aware constraint")
	}
	if _, err := conn.ExecContext(ctx, `UPDATE distribution_artifacts SET signer_identity = '' WHERE channel_name = 'nightly'`); err == nil {
		t.Fatal("empty release signer_identity must violate the channel-aware constraint")
	}
}

func migrateTo(ctx context.Context, dsn string, version uint) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open migration database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping migration database: %w", err)
	}
	sourceDriver, err := iofs.New(Files, ".")
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("migration source: %w", err)
	}
	databaseDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("migration database driver: %w", err)
	}
	runner, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", databaseDriver)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("migration runner: %w", err)
	}
	err = runner.Migrate(version)
	sourceErr, databaseErr := runner.Close()
	if err != nil {
		return fmt.Errorf("migrate to %d: %w", version, err)
	}
	if sourceErr != nil {
		return fmt.Errorf("close migration source: %w", sourceErr)
	}
	return databaseErr
}

// seedArtifact inserts a row shaped like a pre-003 admission: deployment-channel
// rows carried the builder id as signer_identity, which the 001 CHECK required
// to be non-empty.
func seedArtifact(ctx context.Context, t *testing.T, conn *sql.DB, channel, signer, digestPrefix string) {
	t.Helper()
	digest := "sha256:" + digestPrefix + strings.Repeat("0", 64-len(digestPrefix))
	_, err := conn.ExecContext(ctx, `
INSERT INTO distribution_artifacts (
    artifact_id, package_name, package_version, channel_name, platform_os, platform_arch, flavor,
    origin_registry_url, public_registry_url, oci_repository, oci_digest, oci_media_type, oci_size_bytes,
    public_oci_reference, builder_id, signer_identity, source_repository, source_commit, source_ref,
    policy_ref, state, verification_decision, verification_reason, retention_class, replication_state,
    submitted_by, admitted_at, available_at, created_at, updated_at
) VALUES (
    gen_random_uuid(), 'example-service', '0.0.1', $1, 'linux', 'amd64', 'dev',
    'http://127.0.0.1:5080', 'https://oci.verself.sh', 'verself/example-service', $2, 'application/vnd.oci.image.manifest.v1+json', 42,
    'oci.verself.sh/verself/example-service@' || $2, $3, $3, 'guardian-intelligence/verself', '0123456789012345678901234567890123456789', 'refs/heads/main',
    'verself://policy/deployment-oci/v1', 'available', 'allowed', 'seeded', 'canary', 'available',
    'deployment-service', now(), now(), now(), now()
)`, channel, digest, signer)
	if err != nil {
		t.Fatalf("seed %s artifact: %v", channel, err)
	}
}
