package pgsocket

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/iancoleman/strcase"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"go.temporal.io/server/common/auth"
	"go.temporal.io/server/common/config"
)

const (
	connectProtocolUnix = "unix"

	sslMode        = "sslmode"
	sslModeNoop    = "disable"
	sslModeRequire = "require"
	sslModeFull    = "verify-full"

	sslCA   = "sslrootcert"
	sslKey  = "sslkey"
	sslCert = "sslcert"
)

func ConfigureTemporalDatastores(cfg *config.Config) error {
	if cfg == nil {
		return errors.New("temporal config is required")
	}
	storeNames := []string{
		cfg.Persistence.DefaultStore,
		cfg.Persistence.VisibilityStore,
		cfg.Persistence.SecondaryVisibilityStore,
	}
	seen := make(map[string]struct{}, len(storeNames))
	for _, storeName := range storeNames {
		storeName = strings.TrimSpace(storeName)
		if storeName == "" {
			continue
		}
		if _, ok := seen[storeName]; ok {
			continue
		}
		seen[storeName] = struct{}{}

		store, ok := cfg.Persistence.DataStores[storeName]
		if !ok {
			return fmt.Errorf("temporal datastore %q not found in config", storeName)
		}
		if store.SQL == nil {
			return fmt.Errorf("temporal datastore %q is not configured as SQL", storeName)
		}
		if err := ConfigureSQL(store.SQL); err != nil {
			return fmt.Errorf("configure temporal datastore %q: %w", storeName, err)
		}
	}
	return nil
}

func ConfigureSQL(sqlCfg *config.SQL) error {
	if sqlCfg == nil {
		return errors.New("sql config is required")
	}
	if !strings.HasPrefix(strings.TrimSpace(sqlCfg.PluginName), "postgres") {
		return fmt.Errorf("unsupported temporal SQL plugin %q", sqlCfg.PluginName)
	}
	socketDir, err := socketDirectory(sqlCfg)
	if err != nil {
		return err
	}
	if err := ensureConnectAttributes(sqlCfg, socketDir); err != nil {
		return err
	}

	// Temporal's stock PostgreSQL DSN builder always resolves ConnectAddr into the
	// URL authority, which defeats peer-auth Unix sockets; override it centrally.
	sqlCfg.Connect = func(current *config.SQL) (*sqlx.DB, error) {
		return Open(current)
	}
	return nil
}

func Open(sqlCfg *config.SQL) (*sqlx.DB, error) {
	if sqlCfg == nil {
		return nil, errors.New("sql config is required")
	}
	dsn, err := BuildDSN(sqlCfg)
	if err != nil {
		return nil, err
	}
	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if sqlCfg.MaxConns > 0 {
		db.SetMaxOpenConns(sqlCfg.MaxConns)
	}
	if sqlCfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(sqlCfg.MaxIdleConns)
	}
	if sqlCfg.MaxConnLifetime > 0 {
		db.SetConnMaxLifetime(sqlCfg.MaxConnLifetime)
	}
	db.MapperFunc(strcase.ToSnake)
	return db, nil
}

func BuildDSN(sqlCfg *config.SQL) (string, error) {
	if sqlCfg == nil {
		return "", errors.New("sql config is required")
	}
	socketDir, err := socketDirectory(sqlCfg)
	if err != nil {
		return "", err
	}
	attrs, err := buildAttributes(sqlCfg)
	if err != nil {
		return "", err
	}
	attrs.Set("host", socketDir)

	user := url.User(strings.TrimSpace(sqlCfg.User))
	if strings.TrimSpace(sqlCfg.Password) != "" {
		user = url.UserPassword(strings.TrimSpace(sqlCfg.User), sqlCfg.Password)
	}
	dsn := &url.URL{
		Scheme:   "postgres",
		User:     user,
		Path:     "/" + strings.TrimSpace(sqlCfg.DatabaseName),
		RawQuery: attrs.Encode(),
	}
	return dsn.String(), nil
}

func socketDirectory(sqlCfg *config.SQL) (string, error) {
	if sqlCfg == nil {
		return "", errors.New("sql config is required")
	}
	if host := strings.TrimSpace(sqlCfg.ConnectAttributes["host"]); host != "" {
		return host, nil
	}
	if strings.EqualFold(strings.TrimSpace(sqlCfg.ConnectProtocol), connectProtocolUnix) {
		socketDir := strings.TrimSpace(sqlCfg.ConnectAddr)
		if socketDir == "" {
			return "", errors.New("unix SQL config requires connectAddr to be the PostgreSQL socket directory")
		}
		return socketDir, nil
	}
	return "", fmt.Errorf("temporal SQL config must use unix sockets or set connectAttributes.host, got protocol %q", sqlCfg.ConnectProtocol)
}

func ensureConnectAttributes(sqlCfg *config.SQL, socketDir string) error {
	if sqlCfg.ConnectAttributes == nil {
		sqlCfg.ConnectAttributes = map[string]string{}
	}
	if strings.TrimSpace(sqlCfg.ConnectAttributes["host"]) == "" {
		sqlCfg.ConnectAttributes["host"] = socketDir
	}
	if _, err := buildAttributes(sqlCfg); err != nil {
		return err
	}
	return nil
}

func buildAttributes(sqlCfg *config.SQL) (url.Values, error) {
	parameters := make(url.Values, len(sqlCfg.ConnectAttributes))
	for key, value := range sqlCfg.ConnectAttributes {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if existing := parameters.Get(trimmedKey); existing != "" {
			return nil, fmt.Errorf("duplicate PostgreSQL connect attribute %q with values %q and %q", trimmedKey, existing, trimmedValue)
		}
		parameters.Set(trimmedKey, trimmedValue)
	}

	if sqlCfg.TLS != nil && sqlCfg.TLS.Enabled {
		if err := applyTLSAttributes(parameters, sqlCfg.TLS); err != nil {
			return nil, err
		}
	} else if parameters.Get(sslMode) == "" {
		parameters.Set(sslMode, sslModeNoop)
	}

	return parameters, nil
}

func applyTLSAttributes(parameters url.Values, tlsCfg *auth.TLS) error {
	if parameters.Get(sslMode) == "" {
		if tlsCfg.EnableHostVerification {
			parameters.Set(sslMode, sslModeFull)
		} else {
			parameters.Set(sslMode, sslModeRequire)
		}
	}

	if parameters.Get(sslCA) == "" && tlsCfg.CaFile != "" {
		parameters.Set(sslCA, tlsCfg.CaFile)
	}

	if parameters.Get(sslKey) == "" {
		if parameters.Get(sslCert) != "" {
			return errors.New("failed to build postgresql DSN: sslcert connect attribute is set but sslkey is not set")
		}
		if tlsCfg.KeyFile != "" {
			if tlsCfg.CertFile == "" {
				return errors.New("failed to build postgresql DSN: TLS keyFile is set but TLS certFile is not set")
			}
			parameters.Set(sslKey, tlsCfg.KeyFile)
			parameters.Set(sslCert, tlsCfg.CertFile)
		} else if tlsCfg.CertFile != "" {
			return errors.New("failed to build postgresql DSN: TLS certFile is set but TLS keyFile is not set")
		}
	} else if parameters.Get(sslCert) == "" {
		return errors.New("failed to build postgresql DSN: sslkey connect attribute is set but sslcert is not set")
	}

	return nil
}
