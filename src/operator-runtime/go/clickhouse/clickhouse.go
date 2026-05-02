package clickhouse

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	chdriver "github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	opruntime "github.com/verself/operator-runtime/runtime"
)

const (
	DefaultDatabase           = "verself"
	DefaultOperatorUser       = "clickhouse_operator"
	DefaultOperatorConfigPath = "/etc/clickhouse-client/operator.xml"
)

type Config struct {
	Database           string
	Username           string
	OperatorConfigPath string
}

type Client struct {
	Conn    chdriver.Conn
	forward *opruntime.Forward
}

func OpenOperator(ctx context.Context, rt *opruntime.Runtime, cfg Config) (*Client, error) {
	if rt == nil || rt.SSH == nil {
		return nil, fmt.Errorf("clickhouse: operator runtime with SSH is required")
	}
	if cfg.Database == "" {
		cfg.Database = DefaultDatabase
	}
	if cfg.Username == "" {
		cfg.Username = DefaultOperatorUser
	}
	if cfg.OperatorConfigPath == "" {
		cfg.OperatorConfigPath = DefaultOperatorConfigPath
	}
	rawConfig, err := opruntime.ReadRemoteFile(ctx, rt.SSH, cfg.OperatorConfigPath)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: read operator config: %w", err)
	}
	operatorCfg, err := parseOperatorConfig(rawConfig)
	if err != nil {
		return nil, err
	}
	forward, err := rt.SSH.Forward(ctx, "clickhouse-native", fmt.Sprintf("127.0.0.1:%d", operatorCfg.Port))
	if err != nil {
		return nil, fmt.Errorf("clickhouse: open native forward: %w", err)
	}
	material, err := readTLSMaterial(ctx, rt, operatorCfg)
	if err != nil {
		_ = forward.Close()
		return nil, err
	}
	tlsConfig, err := buildTLSConfig(material)
	if err != nil {
		_ = forward.Close()
		return nil, err
	}
	conn, err := ch.Open(&ch.Options{
		Addr: []string{forward.ListenAddr},
		Auth: ch.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
		},
		TLS:             tlsConfig,
		DialTimeout:     time.Second,
		ReadTimeout:     5 * time.Second,
		MaxOpenConns:    2,
		MaxIdleConns:    2,
		ConnMaxLifetime: time.Hour,
	})
	if err != nil {
		_ = forward.Close()
		return nil, fmt.Errorf("clickhouse: open native client: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		_ = forward.Close()
		return nil, fmt.Errorf("clickhouse: ping native client: %w", err)
	}
	return &Client{Conn: conn, forward: forward}, nil
}

func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	var err error
	if c.Conn != nil {
		err = c.Conn.Close()
		c.Conn = nil
	}
	if c.forward != nil {
		if closeErr := c.forward.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		c.forward = nil
	}
	return err
}

func (c *Client) QueryTable(ctx context.Context, query string) (opruntime.Table, error) {
	rows, err := c.Conn.Query(ctx, query)
	if err != nil {
		return opruntime.Table{}, err
	}
	defer rows.Close()
	columns := rows.Columns()
	columnTypes := rows.ColumnTypes()
	vars := make([]any, len(columnTypes))
	for i, ct := range columnTypes {
		scanType := ct.ScanType()
		if scanType == nil {
			vars[i] = new(any)
			continue
		}
		vars[i] = reflect.New(scanType).Interface()
	}
	out := opruntime.Table{Headers: columns}
	for rows.Next() {
		if err := rows.Scan(vars...); err != nil {
			return opruntime.Table{}, err
		}
		row := make([]string, len(vars))
		for i, value := range vars {
			row[i] = opruntime.FormatValue(value)
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return opruntime.Table{}, err
	}
	return out, nil
}

func (c *Client) QuerySingleStringColumn(ctx context.Context, query string) ([]string, error) {
	rows, err := c.Conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

type operatorConfig struct {
	Host            string
	Port            int
	CertificateFile string
	PrivateKeyFile  string
	CAConfig        string
}

type clickHouseClientConfigXML struct {
	XMLName xml.Name
	Secure  string `xml:"secure"`
	Host    string `xml:"host"`
	Port    int    `xml:"port"`
	OpenSSL struct {
		Client struct {
			CertificateFile string `xml:"certificateFile"`
			PrivateKeyFile  string `xml:"privateKeyFile"`
			CAConfig        string `xml:"caConfig"`
		} `xml:"client"`
	} `xml:"openSSL"`
}

func parseOperatorConfig(raw []byte) (operatorConfig, error) {
	var parsed clickHouseClientConfigXML
	if err := xml.Unmarshal(raw, &parsed); err != nil {
		return operatorConfig{}, fmt.Errorf("clickhouse: parse operator config xml: %w", err)
	}
	cfg := operatorConfig{
		Host:            strings.TrimSpace(parsed.Host),
		Port:            parsed.Port,
		CertificateFile: strings.TrimSpace(parsed.OpenSSL.Client.CertificateFile),
		PrivateKeyFile:  strings.TrimSpace(parsed.OpenSSL.Client.PrivateKeyFile),
		CAConfig:        strings.TrimSpace(parsed.OpenSSL.Client.CAConfig),
	}
	secure, err := strconv.ParseBool(strings.TrimSpace(parsed.Secure))
	if err != nil {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator secure flag must parse as bool: %w", err)
	}
	if !secure {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator config must use TLS")
	}
	if cfg.Host == "" {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator config missing host")
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator config has invalid port %d", cfg.Port)
	}
	if cfg.CertificateFile == "" {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator config missing certificateFile")
	}
	if cfg.PrivateKeyFile == "" {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator config missing privateKeyFile")
	}
	if cfg.CAConfig == "" {
		return operatorConfig{}, fmt.Errorf("clickhouse: operator config missing caConfig")
	}
	return cfg, nil
}

type tlsMaterial struct {
	certPEM []byte
	keyPEM  []byte
	caPEM   []byte
}

func readTLSMaterial(ctx context.Context, rt *opruntime.Runtime, cfg operatorConfig) (tlsMaterial, error) {
	certPEM, err := opruntime.ReadRemoteFile(ctx, rt.SSH, cfg.CertificateFile)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("clickhouse: read operator certificate: %w", err)
	}
	keyPEM, err := opruntime.ReadRemoteFile(ctx, rt.SSH, cfg.PrivateKeyFile)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("clickhouse: read operator private key: %w", err)
	}
	caPEM, err := opruntime.ReadRemoteFile(ctx, rt.SSH, cfg.CAConfig)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("clickhouse: read operator CA: %w", err)
	}
	return tlsMaterial{certPEM: certPEM, keyPEM: keyPEM, caPEM: caPEM}, nil
}

func buildTLSConfig(material tlsMaterial) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(material.certPEM, material.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("clickhouse: load operator certificate pair: %w", err)
	}
	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM(material.caPEM); !ok {
		return nil, fmt.Errorf("clickhouse: load operator CA: no PEM certificates found")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   "127.0.0.1",
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
	}, nil
}
