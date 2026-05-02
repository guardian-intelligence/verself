package deploydb

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"

	"github.com/verself/deployment-tools/internal/sshtun"
)

type tlsMaterial struct {
	certPEM []byte
	keyPEM  []byte
	caPEM   []byte
}

func readTLSMaterial(ctx context.Context, ssh *sshtun.Client, cfg operatorConfig) (tlsMaterial, error) {
	certPEM, err := readRemoteFile(ctx, ssh, cfg.CertificateFile)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("deploydb: read clickhouse operator certificate: %w", err)
	}
	keyPEM, err := readRemoteFile(ctx, ssh, cfg.PrivateKeyFile)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("deploydb: read clickhouse operator private key: %w", err)
	}
	caPEM, err := readRemoteFile(ctx, ssh, cfg.CAConfig)
	if err != nil {
		return tlsMaterial{}, fmt.Errorf("deploydb: read clickhouse operator CA: %w", err)
	}
	return tlsMaterial{certPEM: certPEM, keyPEM: keyPEM, caPEM: caPEM}, nil
}

func buildTLSConfig(material tlsMaterial) (*tls.Config, error) {
	cert, err := tls.X509KeyPair(material.certPEM, material.keyPEM)
	if err != nil {
		return nil, fmt.Errorf("deploydb: load clickhouse operator certificate pair: %w", err)
	}
	roots := x509.NewCertPool()
	if ok := roots.AppendCertsFromPEM(material.caPEM); !ok {
		return nil, fmt.Errorf("deploydb: load clickhouse operator CA: no PEM certificates found")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		ServerName:   "127.0.0.1",
		RootCAs:      roots,
		Certificates: []tls.Certificate{cert},
	}, nil
}
