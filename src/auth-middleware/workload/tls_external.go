package workload

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spiffe/go-spiffe/v2/svid/x509svid"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

// TLSConfigWithX509SourceAndCABundle builds a client TLS config for external
// servers that trust a SPIFFE-issued client certificate but do not present a
// SPIFFE server identity themselves.
func TLSConfigWithX509SourceAndCABundle(ctx context.Context, source *workloadapi.X509Source, caBundlePath string) (*tls.Config, error) {
	if source == nil {
		return nil, errors.New("spiffe x509 source is required")
	}
	caBundlePath = strings.TrimSpace(caBundlePath)
	if caBundlePath == "" {
		return nil, errors.New("ca bundle path is required")
	}
	_, span := tracer.Start(ctx, "auth.spiffe.external_tls.init")
	defer span.End()
	span.SetAttributes(attribute.String("tls.ca_bundle_path", caBundlePath))

	caPEM, err := os.ReadFile(caBundlePath)
	if err != nil {
		err = fmt.Errorf("read ca bundle %s: %w", caBundlePath, err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	rootCAs := x509.NewCertPool()
	if !rootCAs.AppendCertsFromPEM(caPEM) {
		err := fmt.Errorf("parse ca bundle %s: no certificates found", caBundlePath)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}

	initialSVID, err := source.GetX509SVID()
	if err != nil {
		err = fmt.Errorf("fetch x509-svid: %w", err)
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	initialCert, err := tlsCertificateFromSVID(initialSVID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	span.SetAttributes(attribute.String("spiffe.id", initialSVID.ID.String()))

	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    rootCAs,
		Certificates: []tls.Certificate{
			initialCert,
		},
		GetClientCertificate: func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
			cert, err := currentTLSCertificate(source)
			if err != nil {
				return nil, err
			}
			return &cert, nil
		},
	}, nil
}

func currentTLSCertificate(source *workloadapi.X509Source) (tls.Certificate, error) {
	svid, err := source.GetX509SVID()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("fetch x509-svid: %w", err)
	}
	return tlsCertificateFromSVID(svid)
}

func tlsCertificateFromSVID(svid *x509svid.SVID) (tls.Certificate, error) {
	if svid == nil {
		return tls.Certificate{}, errors.New("x509-svid is required")
	}
	certPEM, keyPEM, err := svid.Marshal()
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal x509-svid: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("build tls certificate from x509-svid: %w", err)
	}
	if len(svid.Certificates) > 0 {
		cert.Leaf = svid.Certificates[0]
	}
	return cert, nil
}
