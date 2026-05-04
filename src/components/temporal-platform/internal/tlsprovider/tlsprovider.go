package tlsprovider

import (
	"context"
	"crypto/md5"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"github.com/spiffe/go-spiffe/v2/spiffetls/tlsconfig"
	"github.com/spiffe/go-spiffe/v2/workloadapi"
	workloadauth "github.com/verself/service-runtime/workload"
	"go.temporal.io/server/common/rpc/encryption"
)

type Snapshot struct {
	InternodeServerClientAuth tls.ClientAuthType
	InternodeClientServerName string
	FrontendServerClientAuth  tls.ClientAuthType
	FrontendClientServerName  string
	RemoteClusterConfigs      int
}

type Provider struct {
	source      *workloadapi.X509Source
	trustDomain spiffeid.TrustDomain

	internodeServerConfig      *tls.Config
	internodeClientConfig      *tls.Config
	frontendServerConfig       *tls.Config
	frontendClientConfig       *tls.Config
	remoteClusterClientConfigs map[string]*tls.Config
}

var _ encryption.TLSConfigProvider = (*Provider)(nil)

func New(ctx context.Context, socketPath string) (*Provider, error) {
	source, err := workloadauth.Source(ctx, socketPath)
	if err != nil {
		return nil, fmt.Errorf("open spiffe x509 source: %w", err)
	}
	serverID, err := workloadauth.CurrentIDForService(source, workloadauth.ServiceTemporalServer)
	if err != nil {
		_ = source.Close()
		return nil, err
	}
	frontendClientIDs, err := workloadauth.PeerIDsForSource(
		source,
		workloadauth.ServiceTemporalServer,
		workloadauth.ServiceSandboxRental,
		workloadauth.ServiceBilling,
	)
	if err != nil {
		_ = source.Close()
		return nil, err
	}

	return &Provider{
		source:                     source,
		trustDomain:                serverID.TrustDomain(),
		internodeServerConfig:      newServerTLSConfig(source, serverID.TrustDomain(), tlsconfig.AuthorizeID(serverID)),
		internodeClientConfig:      tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(serverID)),
		frontendServerConfig:       newServerTLSConfig(source, serverID.TrustDomain(), tlsconfig.AuthorizeOneOf(frontendClientIDs...)),
		frontendClientConfig:       tlsconfig.MTLSClientConfig(source, source, tlsconfig.AuthorizeID(serverID)),
		remoteClusterClientConfigs: map[string]*tls.Config{},
	}, nil
}

func (p *Provider) Close() error {
	if p == nil || p.source == nil {
		return nil
	}
	return p.source.Close()
}

func (p *Provider) Snapshot() Snapshot {
	if p == nil {
		return Snapshot{}
	}
	snapshot := Snapshot{
		RemoteClusterConfigs: len(p.remoteClusterClientConfigs),
	}
	if p.internodeServerConfig != nil {
		snapshot.InternodeServerClientAuth = p.internodeServerConfig.ClientAuth
	}
	if p.internodeClientConfig != nil {
		snapshot.InternodeClientServerName = p.internodeClientConfig.ServerName
	}
	if p.frontendServerConfig != nil {
		snapshot.FrontendServerClientAuth = p.frontendServerConfig.ClientAuth
	}
	if p.frontendClientConfig != nil {
		snapshot.FrontendClientServerName = p.frontendClientConfig.ServerName
	}
	return snapshot
}

func (p *Provider) GetInternodeServerConfig() (*tls.Config, error) {
	return cloneTLSConfig(p.internodeServerConfig), nil
}

func (p *Provider) GetInternodeClientConfig() (*tls.Config, error) {
	return cloneTLSConfig(p.internodeClientConfig), nil
}

func (p *Provider) GetFrontendServerConfig() (*tls.Config, error) {
	return cloneTLSConfig(p.frontendServerConfig), nil
}

func (p *Provider) GetFrontendClientConfig() (*tls.Config, error) {
	return cloneTLSConfig(p.frontendClientConfig), nil
}

func (p *Provider) GetRemoteClusterClientConfig(hostname string) (*tls.Config, error) {
	return cloneTLSConfig(p.remoteClusterClientConfigs[hostname]), nil
}

func (p *Provider) GetExpiringCerts(
	timeWindow time.Duration,
) (encryption.CertExpirationMap, encryption.CertExpirationMap, error) {
	expiring := make(encryption.CertExpirationMap)
	expired := make(encryption.CertExpirationMap)
	if p == nil || p.source == nil {
		return expiring, expired, nil
	}

	deadline := time.Now().UTC().Add(timeWindow)

	svid, err := p.source.GetX509SVID()
	if err != nil {
		return nil, nil, fmt.Errorf("load temporal x509-svid: %w", err)
	}
	addExpiringCerts(expiring, expired, deadline, false, svid.Certificates)

	bundle, err := p.source.GetX509BundleForTrustDomain(p.trustDomain)
	if err != nil {
		return nil, nil, fmt.Errorf("load temporal x509 bundle for %s: %w", p.trustDomain, err)
	}
	addExpiringCerts(expiring, expired, deadline, true, bundle.X509Authorities())

	return expiring, expired, nil
}

func cloneTLSConfig(config *tls.Config) *tls.Config {
	if config == nil {
		return nil
	}
	return config.Clone()
}

func newServerTLSConfig(
	source *workloadapi.X509Source,
	trustDomain spiffeid.TrustDomain,
	authorizer tlsconfig.Authorizer,
) *tls.Config {
	// Go's server-side mTLS path is more reliable when ClientCAs is populated
	// explicitly; relying on VerifyPeerCertificate alone allowed anonymous TLS
	// handshakes through Temporal's frontend listener.
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			return buildServerTLSConfig(source, trustDomain, authorizer)
		},
	}
}

func buildServerTLSConfig(
	source *workloadapi.X509Source,
	trustDomain spiffeid.TrustDomain,
	authorizer tlsconfig.Authorizer,
) (*tls.Config, error) {
	svid, err := source.GetX509SVID()
	if err != nil {
		return nil, fmt.Errorf("load temporal x509-svid: %w", err)
	}
	bundle, err := source.GetX509BundleForTrustDomain(trustDomain)
	if err != nil {
		return nil, fmt.Errorf("load temporal x509 bundle for %s: %w", trustDomain, err)
	}
	cert := &tls.Certificate{
		Certificate: make([][]byte, 0, len(svid.Certificates)),
		PrivateKey:  svid.PrivateKey,
		Leaf:        svid.Certificates[0],
	}
	for _, svidCert := range svid.Certificates {
		cert.Certificate = append(cert.Certificate, svidCert.Raw)
	}
	clientCAPool := x509.NewCertPool()
	for _, authority := range bundle.X509Authorities() {
		clientCAPool.AddCert(authority)
	}
	return &tls.Config{
		MinVersion:            tls.VersionTLS12,
		Certificates:          []tls.Certificate{*cert},
		ClientAuth:            tls.RequireAndVerifyClientCert,
		ClientCAs:             clientCAPool,
		VerifyPeerCertificate: tlsconfig.VerifyPeerCertificate(source, authorizer),
	}, nil
}

func addExpiringCerts(
	expiring encryption.CertExpirationMap,
	expired encryption.CertExpirationMap,
	deadline time.Time,
	isCA bool,
	certs []*x509.Certificate,
) {
	for _, cert := range certs {
		if cert == nil {
			continue
		}
		thumbprint := encryption.CertThumbprint(md5.Sum(cert.Raw)) //nolint:gosec // Temporal uses MD5 thumbprints for cert identity in expiration reporting.
		data := encryption.CertExpirationData{
			Thumbprint: thumbprint,
			IsCA:       isCA,
			DNSNames:   append([]string(nil), cert.DNSNames...),
			Expiration: cert.NotAfter.UTC(),
		}
		if data.Expiration.Before(time.Now().UTC()) {
			expired[thumbprint] = data
			continue
		}
		if !data.Expiration.After(deadline) {
			expiring[thumbprint] = data
		}
	}
}
