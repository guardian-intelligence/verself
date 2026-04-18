package governance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
)

const (
	DefaultExportTTL = 7 * 24 * time.Hour
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrStore           = errors.New("store unavailable")
)

var tracer = otel.Tracer("governance-service/internal/governance")

type Service struct {
	PG            *pgxpool.Pool
	IdentityPG    *pgxpool.Pool
	BillingPG     *pgxpool.Pool
	SandboxPG     *pgxpool.Pool
	CH            driver.Conn
	Logger        *slog.Logger
	HMACKey       []byte
	ExportDir     string
	ExportTTL     time.Duration
	PublicBaseURL string
}

type Principal struct {
	OrgID   string
	Subject string
	Email   string
	Type    string
}

func (s *Service) Validate() error {
	if s == nil {
		return fmt.Errorf("%w: service is nil", ErrStore)
	}
	if s.PG == nil {
		return fmt.Errorf("%w: governance postgres is nil", ErrStore)
	}
	if s.CH == nil {
		return fmt.Errorf("%w: clickhouse is nil", ErrStore)
	}
	if len(s.HMACKey) < 32 {
		return fmt.Errorf("%w: audit hmac key must be at least 32 bytes", ErrInvalidArgument)
	}
	if strings.TrimSpace(s.ExportDir) == "" {
		return fmt.Errorf("%w: export dir is required", ErrInvalidArgument)
	}
	if s.ExportTTL <= 0 {
		s.ExportTTL = DefaultExportTTL
	}
	if s.Logger == nil {
		s.Logger = slog.Default()
	}
	return nil
}

func (s *Service) Ready(ctx context.Context) error {
	ctx, span := tracer.Start(ctx, "governance.ready")
	defer span.End()
	if err := s.Validate(); err != nil {
		return err
	}
	if err := s.PG.Ping(ctx); err != nil {
		return fmt.Errorf("%w: governance postgres ping: %v", ErrStore, err)
	}
	for name, pool := range map[string]*pgxpool.Pool{
		"identity": s.IdentityPG,
		"billing":  s.BillingPG,
		"sandbox":  s.SandboxPG,
	} {
		if pool == nil {
			continue
		}
		if err := pool.Ping(ctx); err != nil {
			return fmt.Errorf("%w: %s postgres ping: %v", ErrStore, name, err)
		}
	}
	if err := s.CH.Ping(ctx); err != nil {
		return fmt.Errorf("%w: clickhouse ping: %v", ErrStore, err)
	}
	if err := os.MkdirAll(s.ExportDir, 0o750); err != nil {
		return fmt.Errorf("%w: create export dir: %v", ErrStore, err)
	}
	return nil
}

func (s *Service) exportPath(orgID, exportID string) (string, error) {
	orgID = sanitizePathPart(orgID)
	if orgID == "" || exportID == "" {
		return "", fmt.Errorf("%w: org id and export id are required", ErrInvalidArgument)
	}
	path := filepath.Join(s.ExportDir, orgID)
	if err := os.MkdirAll(path, 0o750); err != nil {
		return "", fmt.Errorf("%w: create org export dir: %v", ErrStore, err)
	}
	return filepath.Join(path, exportID+".tar.gz"), nil
}

func sanitizePathPart(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		}
	}
	return b.String()
}
