package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type config struct {
	originHostname string
	listen         string
	target         string
	caBundle       string
	caKey          string
	serverCert     string
	serverKey      string
}

func main() {
	cfg := parse()
	if err := run(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "garage-artifact-origin: %v\n", err)
		os.Exit(1)
	}
}

func parse() config {
	cfg := config{}
	flag.StringVar(&cfg.originHostname, "origin-hostname", "", "artifact origin TLS DNS name")
	flag.StringVar(&cfg.listen, "listen", "127.0.0.1:9443", "HTTPS listen address")
	flag.StringVar(&cfg.target, "target", "http://127.0.0.1:3900", "Garage S3 target URL")
	flag.StringVar(&cfg.caBundle, "ca-bundle", "/etc/verself/pki/nomad-artifacts-ca.pem", "CA certificate path")
	flag.StringVar(&cfg.caKey, "ca-key", "/etc/verself/pki/private/nomad-artifacts-ca-key.pem", "CA key path")
	flag.StringVar(&cfg.serverCert, "server-cert", "/etc/verself/pki/nomad-artifacts-server.pem", "server certificate path")
	flag.StringVar(&cfg.serverKey, "server-key", "/etc/verself/pki/private/nomad-artifacts-server-key.pem", "server key path")
	flag.Parse()
	return cfg
}

func run(cfg config) error {
	if cfg.originHostname == "" {
		return errors.New("--origin-hostname is required")
	}
	target, err := url.Parse(cfg.target)
	if err != nil {
		return fmt.Errorf("parse target: %w", err)
	}
	if err := ensureCertificate(cfg); err != nil {
		return err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorLog = nil
	server := &http.Server{
		Addr: cfg.listen,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.URL.Scheme = target.Scheme
			r.URL.Host = target.Host
			r.Host = target.Host
			proxy.ServeHTTP(w, r)
		}),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
	}
	return server.ListenAndServeTLS(cfg.serverCert, cfg.serverKey)
}

func ensureCertificate(cfg config) error {
	for _, path := range []string{cfg.caBundle, cfg.caKey, cfg.serverCert, cfg.serverKey} {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return fmt.Errorf("create %s parent: %w", path, err)
		}
	}
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate CA key: %w", err)
	}
	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("generate server key: %w", err)
	}
	now := time.Now().UTC()
	caTmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "Verself Nomad artifact origin CA"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create CA cert: %w", err)
	}
	serverTmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: cfg.originHostname},
		DNSNames:     []string{cfg.originHostname},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTmpl, caTmpl, &serverKey.PublicKey, caKey)
	if err != nil {
		return fmt.Errorf("create server cert: %w", err)
	}
	if err := writePEM(cfg.caBundle, 0o644, "CERTIFICATE", caDER); err != nil {
		return err
	}
	caKeyDER, err := x509.MarshalECPrivateKey(caKey)
	if err != nil {
		return fmt.Errorf("marshal CA key: %w", err)
	}
	if err := writePEM(cfg.caKey, 0o600, "EC PRIVATE KEY", caKeyDER); err != nil {
		return err
	}
	if err := writePEM(cfg.serverCert, 0o644, "CERTIFICATE", serverDER); err != nil {
		return err
	}
	serverKeyDER, err := x509.MarshalECPrivateKey(serverKey)
	if err != nil {
		return fmt.Errorf("marshal server key: %w", err)
	}
	return writePEM(cfg.serverKey, 0o600, "EC PRIVATE KEY", serverKeyDER)
}

func writePEM(path string, mode os.FileMode, typ string, der []byte) error {
	tmp := path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmp, err)
	}
	if err := pem.Encode(file, &pem.Block{Type: typ, Bytes: der}); err != nil {
		_ = file.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

func serial() *big.Int {
	max := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return big.NewInt(time.Now().UnixNano())
	}
	return n
}
